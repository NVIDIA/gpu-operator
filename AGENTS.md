# AGENTS.md

Guidance for AI coding agents working in this repository. Human contributors should also read
[CONTRIBUTING.md](CONTRIBUTING.md) and the [documentation site](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/index.html)
which are the source of truth.

## Project Summary

The NVIDIA GPU Operator is a Kubernetes operator (built with Kubebuilder v3) that automates
the management of all NVIDIA software components needed to provision GPU nodes in a Kubernetes
cluster. These components include the NVIDIA drivers (to enable CUDA), Kubernetes Device Plugin
for NVIDIA GPUs, the NVIDIA Container Toolkit, GFD for node labeling, DCGM based monitoring and
others. The NVIDIA GPU Operator manages several CRDs, defined in `PROJECT`:

- `ClusterPolicy` (`api/v1`) — a singleton CRD describing the entire GPU software stack; uses
   the Kubernetes Device Plugin framework for allocating GPUs.
- `GPUCluster` (`api/v1alpha1`) — a singleton CRD describing the entire GPU software stack; uses
   Dynamic Resource Allocation for allocating GPUs; an alternative to the `ClusterPolicy` CRD.
- `NVIDIADriver` (`api/v1alpha1`) — per-node-pool NVIDIA driver configuration.

## Repository layout

- `api/` — CRD Go types (`nvidia/` for the API groups, `versioned/` generated clientset).
- `controllers/` — reconciliation logic for each CRD/controller; `object_controls.go`,
  `state_manager.go`, and `resource_manager.go` contain helpers that render and apply the
   k8s objects that make up each operand "state."
- `internal/` — supporting packages; notably `internal/state` is used for state rendering.
- `cmd/` — binary entrypoints (operator manager and CLI tools).
- `deployments/gpu-operator/` — Helm chart for the operator.
- `assets/state-*` — per-operand manifests rendered by the `ClusterPolicy` state manager.
- `manifests/` — per-operand manifests rendered by the `GPUCluster` / `NVIDIADriver` state managers.
- `config/` — kubebuilder scaffolding and generated manifests (i.e. CRDs, RBAC).
- `bundle/` — OLM bundle for distributing the operator to the OpenShift operator catalog.
- `tests/` — end-to-end test scripts; used to install / test gpu-operator on a real Kubernetes cluster.
- `hack/` — dev scripts (boilerplate license header, must-gather, environment prep).
- `tools/` — miscellaneous tooling used by `make`.

## Build, test, and lint

All standard tasks go through the Makefile. Prefer make targets over invoking tools directly
so CI and local runs stay consistent.

- `make build` / `make cmds` — compile the operator binaries.
- `make unit-test` — run unit tests (excludes `tests/e2e`).
- `make lint` — run `golangci-lint run ./...` (config in `.golangci.yml`).
- `make fmt` — check `gofmt -s` formatting (diff only, does not write).
- `make goimports` — run `goimports -local github.com/NVIDIA/gpu-operator -w` (writes files).
- `make generate` / `make manifests` — regenerate deepcopy code and CRD/RBAC manifests from
  kubebuilder markers after changing `api/` types; run these after editing `*_types.go` files.
- `make sync-crds` — copy generated CRDs in `config/crd/bases/*` to helm chart and OLM bundle.
- `make validate-generated-assets` — verifies generated manifests/clientset/CRDs are up to date;
  CI will fail if generated output is stale, so run this (or `generate`+`manifests`+
  `generate-clientset`+`sync-crds`) after touching API types or Helm chart CRDs.
- `make validate-helm-values` — verifies that images referenced in helm values are valid; requires
  the `helm` binary.
- `make validate-csv` — validates that the CSV file in the OLM bundle directory is properly
  formatted and all image references are valid.
- `make check-third-party-notices` — checks whether `THIRD_PARTY_NOTICES.md` is up-to-date.

Always run `make fmt` and `make unit-test` before considering Go changes complete.

After editing anything under `api/nvidia`, run `make generate` / `make manifests` (or at least
`make validate-generated-assets` to confirm nothing is stale) and commit the regenerated output
alongside the source change. If any CRDs were modified, run `make sync-crds` to make sure
the CRDs in the helm chart and OLM bundle are in-sync.

## Coding Conventions

- Idiomatic Go, client-go/controller conventions. Controllers and reconcile-style loops must be
  idempotent and safe under retries.
- Comments explain **why**, not **what**. Identifier names should carry the "what."
- Keep changes scoped to the task. No drive-by refactors, speculative abstractions, or unrelated
  formatting churn — one concern per PR.
- Every `.go` file starts with the Apache-2.0 boilerplate header (`hack/boilerplate.go.txt`), match 
  the existing header exactly rather than inventing a variant. `make license-check` verifies this.
- Follow existing patterns in `controllers/` and `internal/` for logging, error wrapping, and
  client usage rather than introducing new libraries.
- Vendor directory (`vendor/`) is checked in; run `go mod tidy`/`go mod vendor` after
  dependency changes and do not hand-edit vendored code.
- `zz_generated.deepcopy.go` files are generated; do not hand-edit them.

## Testing Conventions

- Unit tests are co-located `*_test.go` files using `testify` (`require`/`assert`), typically table-driven
  with a `map[string]struct{...}` of cases and `t.Run(name, ...)`. Run via `make unit-test`. New behavior
  needs appropriate test coverage.
- When fixing a bug, add a regression test that fails without the fix.

## Contribution process (see `CONTRIBUTING.md`)

- For any significant change (architectural change, new feature, breaking change, non-trivial
  bug fix), an issue should exist describing the problem/proposal before implementation begins —
  check for or ask about a linked issue rather than assuming a PR alone is sufficient.
- All commits must be signed off (DCO): `git commit -s`, producing a trailing
  `Signed-off-by: Name <email>` line. Do not fabricate a sign-off identity — use the configured
  git user's identity.
- Do not open, push to, or comment on GitHub issues/PRs without explicit user confirmation.
- Keep PR titles short and imperative; the body should explain motivation ("why"), not just restate the diff.

## Things to avoid

- Never commit credentials, API keys, tokens, passwords, kubeconfigs, or private keys.
- Do not hand-edit generated files. Run `make generate`/`make manifests` to regenerate deepcopy code and
  CRD/RBAC manifests from kubebuilder markers after changing `api/` types. Run `go mod tidy`/`go mod vendor` 
  after dependency changes and do not hand-edit vendored code.
- Do not commit built binaries, `coverage.out`, or anything the [.gitignore](.gitignore) already excludes.
- Do not modify [CODEOWNERS](CODEOWNERS) or [GOVERNANCE.md](GOVERNANCE.md) unless the task is explicitly
  about that.
