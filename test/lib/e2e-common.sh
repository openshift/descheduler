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

e2e_repo_root() {
  local script_dir="$1"
  cd "${script_dir}/.." && pwd
}

collect_logs() {
  local namespace pod
  echo "Collecting pods and logs"
  for namespace in "$@"; do
    kubectl get pods -n "${namespace}"
    for pod in $(kubectl get pods -n "${namespace}" -o name); do
      echo "Logs for ${pod}"
      kubectl logs -n "${namespace}" "${pod}"
    done
  done
}

kind_load_image() {
  local image="$1"
  if [ "${CONTAINER_ENGINE}" = "podman" ]; then
    local archive
    archive="/tmp/$(echo "${image}" | tr '/:' '_').tar"
    podman save "${image}" -o "${archive}"
    ${KIND_SUDO} kind load image-archive "${archive}"
    rm "${archive}"
  else
    ${KIND_SUDO} kind load docker-image "${image}"
  fi
}
