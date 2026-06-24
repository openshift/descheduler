# CLAUDE.md

## Directory Structure

### Root Files (OpenShift-Specific)
- `.ci-operator.yaml` - OpenShift CI configuration
- `Dockerfile` - Used by Konflux for building the descheduler image

### Directories Used in OpenShift
- `.tekton/` - Konflux configuration (see Konflux section)
- `cmd/` - Main application entrypoint
- `hack/` - Build and development scripts (partially used, not production code)
- `images/` - OpenShift CI only (not Konflux). Expected to be dropped after Konflux takes over all CI jobs
- `metrics/` - Metrics related code
- `pkg/` - Core Go packages
- `test/` - Currently upstream tests. Some OpenShift teams may run these in OpenShift CI.

### Not Used in OpenShift (Upstream Only)
- `.github/` - GitHub workflows ([docs](https://docs.github.com/en/actions))
- `assets/` - Project logo
- `charts/` - Helm charts
- `docs/` - Documentation
- `examples/` - Examples of descheduler policies
- `keps/` - Kubernetes Enhancement Proposals
- `kubernetes/` - Kubernetes manifests used by Charts and E2Es

## RBAC Management

Upstream descheduler RBAC resources are located under `charts/` but are **not used in OpenShift**. Instead, all RBAC resources are managed by the operator and located under https://github.com/openshift/cluster-kube-descheduler-operator/tree/main/bindata/assets/kube-descheduler.

## Rebasing from Upstream

**Important**: Every non-upstream commit is expected to be prefixed with `UPSTREAM: <carry>: ` or `UPSTREAM: <drop>: `.

**Note**: The version numbers below (1.33 and 4.20) will differ for each release. Rule of thumb: the upstream minor version and OpenShift minor version are 13 units apart (33 - 20 = 13).

1. $ git checkout release-1.33
2. $ git merge -s ours release-4.20
3. $ git checkout release-4.20
4. $ git merge release-1.33
5. $ git cherry-pick carry commits from the previous release and update them accordingly (e.g. bumping .ci-operator.yaml file)

## Konflux

The `.tekton/` configs are carried over from the previous release via rebasing as mentioned above.

The `.tekton/` configs get updated regularly by Konflux automation and these commits are **not** prefixed with `UPSTREAM: `.

**Expected workflow**:
1. Copy paste the latest `.tekton/` configs from the previous release
2. Bump the OCP versions
3. Create vanilla commits that resemble the previous `<carry>` commit(s)
