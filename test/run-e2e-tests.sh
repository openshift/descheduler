#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -x
set -o errexit
set -o nounset

# ---------------------------------------------------------------------------
# Architecture detection — derive GOARCH from the host once; used for all
# binary downloads and image preloads so the script works on amd64, arm64,
# and s390x without manual overrides.
# Override by setting GOARCH in the environment before invoking this script.
# ---------------------------------------------------------------------------
detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        s390x)   echo "s390x" ;;
        ppc64le) echo "ppc64le" ;;
        armv7l)  echo "arm"   ;;
        *)
            echo "WARNING: unrecognised machine type '${machine}', defaulting GOARCH to amd64" >&2
            echo "amd64"
            ;;
    esac
}
GOARCH="${GOARCH:-$(detect_arch)}"
echo "Detected host architecture: ${GOARCH}"

# Set to empty if unbound/empty
SKIP_INSTALL=${SKIP_INSTALL:-}
KIND_E2E=${KIND_E2E:-}
CONTAINER_ENGINE=${CONTAINER_ENGINE:-docker}
KIND_SUDO=${KIND_SUDO:-}
KIND_VERSION=${KIND_VERSION:-v0.31.0}
SKIP_KUBECTL_INSTALL=${SKIP_KUBECTL_INSTALL:-}
SKIP_KIND_INSTALL=${SKIP_KIND_INSTALL:-}
SKIP_KUBEVIRT_INSTALL=${SKIP_KUBEVIRT_INSTALL:-}
KUBEVIRT_VERSION=${KUBEVIRT_VERSION:-v1.6.2}

# PAUSE_IMAGE — the container image pre-loaded into KIND and passed to go test
# via --pause-image. registry.k8s.io/pause is a multi-arch manifest; Docker/
# podman pulls the correct platform layer automatically, so the default works
# on all architectures. Override only when a custom or mirrored image is needed.
PAUSE_IMAGE="${PAUSE_IMAGE:-registry.k8s.io/pause}"

# CPU_STRESSOR_IMAGE — image used by TestLowNodeUtilizationKubernetesMetrics to
# generate CPU load. Defaults to the upstream Docker Hub image (amd64-only).
# On architectures that cannot pull Docker Hub images, or where a multi-arch
# stressor is needed, set this before running the script.
CPU_STRESSOR_IMAGE="${CPU_STRESSOR_IMAGE:-narmidm/k8s-pod-cpu-stressor:latest}"

# Build a descheduler image
IMAGE_TAG=v$(date +%Y%m%d)-$(git describe --tags)
BASEDIR=$(dirname "$0")
VERSION="${IMAGE_TAG}" make -C ${BASEDIR}/.. image

export DESCHEDULER_IMAGE="docker.io/library/descheduler:${IMAGE_TAG}"
echo "DESCHEDULER_IMAGE: ${DESCHEDULER_IMAGE}"

# This just runs e2e tests.
if [ -n "$KIND_E2E" ]; then
    K8S_VERSION=${KUBERNETES_VERSION:-v1.35.1}
    if [ -z "${SKIP_KUBECTL_INSTALL}" ]; then
        # Use the detected architecture for the kubectl binary download.
        curl -Lo kubectl "https://dl.k8s.io/release/${K8S_VERSION}/bin/linux/${GOARCH}/kubectl" \
            && chmod +x kubectl && mv kubectl /usr/local/bin/
    fi
    if [ -z "${SKIP_KIND_INSTALL}" ]; then
        # Use the detected architecture for the kind binary download.
        wget "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-${GOARCH}"
        chmod +x "kind-linux-${GOARCH}"
        mv "kind-linux-${GOARCH}" kind
        export PATH=$PATH:$PWD
    fi

    # If we did not set SKIP_INSTALL
    if [ -z "$SKIP_INSTALL" ]; then
        ${KIND_SUDO} kind create cluster --image kindest/node:${K8S_VERSION} --config=./hack/kind_config.yaml
    fi
    # Pre-load the pause image into KIND nodes so test pods start immediately
    # without requiring an in-cluster pull. PAUSE_IMAGE is a multi-arch manifest
    # by default so no arch-specific tag is required.
    ${CONTAINER_ENGINE} pull "${PAUSE_IMAGE}"
    if [ "${CONTAINER_ENGINE}" == "podman" ]; then
      podman save "${PAUSE_IMAGE}" -o /tmp/pause.tar
      ${KIND_SUDO} kind load image-archive /tmp/pause.tar
      rm /tmp/pause.tar
      podman save ${DESCHEDULER_IMAGE} -o /tmp/descheduler.tar
      ${KIND_SUDO} kind load image-archive /tmp/descheduler.tar
      rm /tmp/descheduler.tar
    else
      ${KIND_SUDO} kind load docker-image "${PAUSE_IMAGE}"
      ${KIND_SUDO} kind load docker-image ${DESCHEDULER_IMAGE}
    fi
    ${KIND_SUDO} kind get kubeconfig > /tmp/admin.conf

    export KUBECONFIG="/tmp/admin.conf"
    mkdir -p ~/gopath/src/sigs.k8s.io/
fi

# Deploy rbac, sa and binding for a descheduler running through a deployment
kubectl apply -f kubernetes/base/rbac.yaml

collect_logs() {
  echo "Collecting pods and logs"
  kubectl get pods -n default
  kubectl get pods -n kubevirt

  for pod in $(kubectl get pods -n default -o name); do
    echo "Logs for ${pod}"
    kubectl logs -n default ${pod}
  done

  for pod in $(kubectl get pods -n kubevirt -o name); do
    echo "Logs for ${pod}"
    kubectl logs -n kubevirt ${pod}
  done
}

trap "collect_logs" ERR

if [ -z "${SKIP_KUBEVIRT_INSTALL}" ]; then
  kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml
  kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml
  kubectl wait --timeout=180s --for=condition=Available -n kubevirt kv/kubevirt
  kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
fi

METRICS_SERVER_VERSION="v0.8.1"
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/${METRICS_SERVER_VERSION}/components.yaml
kubectl patch -n kube-system deployment metrics-server --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'

PRJ_PREFIX="sigs.k8s.io/descheduler"

# --pod-run-as-user-id / --pod-run-as-group-id are intentionally NOT passed here.
#
# getRunAsForNamespace() in the test suite determines the correct UID/GID
# automatically:
#   - On OpenShift: reads the openshift.io/sa.scc.uid-range namespace annotation
#     (single API call; polls only for the narrow transient window during admission).
#   - On vanilla Kubernetes / KIND: annotation absent → returns (0, 0) → no
#     SecurityContext fields set, which is correct since KIND has no SCC enforcement.
#
# Passing a hardcoded UID (e.g. 1000) here would activate the override path in
# getRunAsForNamespace and skip SCC derivation.  On an OpenShift cluster whose
# allowed UID range excludes 1000 that would produce CreateContainerConfigError.
# The flags remain available as an explicit escape hatch for debugging:
#   go test ... --args --pod-run-as-user-id=<N> --pod-run-as-group-id=<N>
# --pause-image and --cpu-stressor-image pass the configurable image references
# through to the Go tests so every image used by the suite is controlled from
# this single env-var surface — no hardcoded values remain in test source.
go test ${PRJ_PREFIX}/test/e2e/ -v -timeout 0 \
    --args \
    --descheduler-image "${DESCHEDULER_IMAGE}" \
    --pause-image "${PAUSE_IMAGE}" \
    --cpu-stressor-image "${CPU_STRESSOR_IMAGE}"
