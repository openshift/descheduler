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

KIND_NODE_IMAGE=${KIND_NODE_IMAGE:-localhost/kindest/node:${K8S_VERSION}}

if [ -z "${SKIP_KUBECTL_INSTALL}" ]; then
  curl -Lo kubectl "https://dl.k8s.io/release/${K8S_VERSION}/bin/linux/amd64/kubectl" && chmod +x kubectl && mv kubectl /usr/local/bin/
fi
if [ -z "${SKIP_KIND_INSTALL}" ]; then
  wget "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-amd64"
  chmod +x kind-linux-amd64
  mv kind-linux-amd64 kind
  export PATH=$PATH:$PWD
fi

if [ -z "${SKIP_INSTALL}" ]; then
  ${KIND_SUDO} kind build node-image "${K8S_VERSION}" --image "${KIND_NODE_IMAGE}"
  ${KIND_SUDO} kind create cluster --image "${KIND_NODE_IMAGE}" --config="${REPO_ROOT}/hack/kind_config.yaml"
fi
${CONTAINER_ENGINE} pull registry.k8s.io/pause
kind_load_image registry.k8s.io/pause
kind_load_image "${DESCHEDULER_IMAGE}"
${KIND_SUDO} kind get kubeconfig > /tmp/admin.conf

export KUBECONFIG="/tmp/admin.conf"
mkdir -p ~/gopath/src/sigs.k8s.io/
