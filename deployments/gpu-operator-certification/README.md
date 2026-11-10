# GPU Operator Self-Certification

A self-certification test suite for ISVs to validate NVIDIA GPU Operator compatibility on their platforms. Runs as a Kubernetes Job and outputs structured JSON results and an HTML report. The `gpu-operator-certification` binary ships in the GPU Operator image (`ghcr.io/nvidia/gpu-operator:main-latest`).

## Quick Start

```bash
# Deploy (GPU Operator must be installed)
kubectl apply -k deployments/gpu-operator-certification/

# View results
kubectl logs job/gpu-operator-certification -n gpu-operator

# Download results.json and report.html
hack/download-certification-results.sh
```

## Configuration

All settings live in [`kustomization.yaml`](kustomization.yaml), which generates the `gpu-operator-certification-config` ConfigMap. `gpu-operator-certification.yaml` does not need to change.

```yaml
# Namespace the GPU Operator runs in, for example nvidia-gpu-operator on OpenShift.
namespace: gpu-operator
...
    literals:
      - TEST_SETS=all                # Run all TestSets
      # - TEST_SETS=all,!TS1003      # Exclude specific sets with "!"
      # - TEST_SETS=TS1000,TS1001    # Or list specific sets to run
```

`namespace` applies to every object.

Edit `kustomization.yaml` directly, or use these commands.

To change the namespace:

```bash
sed -i.bak 's/^namespace: .*/namespace: nvidia-gpu-operator/' deployments/gpu-operator-certification/kustomization.yaml
```

To change the test sets:

```bash
sed -i.bak 's/- TEST_SETS=.*/- TEST_SETS=TS1000,TS1001/' deployments/gpu-operator-certification/kustomization.yaml
```

When the namespace is not `gpu-operator`, pass the same namespace to `kubectl logs -n` and run the download script as `NAMESPACE=<namespace> hack/download-certification-results.sh`.

| Key | Default | Description |
|-------|---------|-------------|
| `namespace` | `gpu-operator` | GPU Operator namespace |
| `TEST_SETS` | `all` | TestSets to run (use `!` prefix to exclude) |
| `TARGET_DRIVER_REPOSITORY` | `""` | Driver repository for TS1003 (skipped if any driver key is empty) |
| `TARGET_DRIVER_IMAGE` | `""` | Driver image for TS1003 |
| `TARGET_DRIVER_VERSION` | `""` | Driver version for TS1003 |

### Rerunning with a different configuration

A Job runs only once. To run it again, for example with different test sets, edit `kustomization.yaml` directly or use the commands above, then delete the previous run and apply again:

```bash
kubectl delete -k deployments/gpu-operator-certification/
kubectl apply -k deployments/gpu-operator-certification/
```

Changes made directly in the cluster, for example with `kubectl edit configmap`, are lost on a rerun.

## Test Sets

| ID       | Name                           | Description |
|----------|--------------------------------|-------------|
| `TS1000` | Default Settings               | Operands, GPU allocatable, GFD labels, DCGM metrics, CUDA and PyTorch workloads |
| `TS1001` | GPU Sharing via Timeslicing    | GPU sharing via time-slicing ConfigMap |
| `TS1002` | DCGM Standalone                | Standalone DCGM enablement |
| `TS1003` | Driver Update via GPU Operator | Driver version update via ClusterPolicy |
| `TS1004` | MIG Mode (Single Strategy)     | MIG mode with single strategy |
| `TS1005` | MIG Mode (Mixed Strategy)      | MIG mode with mixed strategy |
| `TS1006` | GPUDirectRDMA                  | RDMA write bandwidth test between two GPU nodes |
| `TS1007` | GPUDirectRDMA with DMA-BUF     | RDMA write bandwidth test between two GPU nodes using DMA-BUF |
| `TS1008` | GDRCopy Enabled                | GDRCopy sidecar and workload |

## Output

**Structured results** are JSON on stdout:

```json
{
  "runId": "...",
  "duration": "25.4s",
  "environment": {...},
  "testSets": [
    {
      "id": "TS1000",
      "description": "Default Settings",
      "status": "passed",
      "tests": [
        {"name": "operand-verification", "status": "passed", ...},
        {"name": "gpu-allocatable", "status": "passed", ...}
      ]
    }
  ],
  "summary": {"total": 1, "passed": 1, "failed": 0, "skipped": 0}
}
```

The summary counts TestSets. The results and an HTML report are also stored as `results.json` and `report.html` in the `gpu-operator-certification-results` ConfigMap.

**Real-time progress** is logged to stderr for visibility during execution.

## Adding Tests

Tests are registered via `init()` in `internal/certification/testsuite/register.go`. Each `TestSet` groups reusable `Test` primitives:

```go
// Reusable test primitives
var (
    OperandCheck  = &baseline.Operands{}
    WorkloadCheck = &baseline.Workload{}
)

func init() {
    certification.RegisterTestSet(&certification.TestSet{
        Name:        "TS1000",
        Description: "Default Settings",
        Tests: []certification.Test{
            OperandCheck,         // GPU Operator operands are running
            AllocatableCheck,     // nvidia.com/gpu in node allocatable
            GFDLabelsCheck,       // GFD labels on nodes
            DCGMMetricsCheck,     // DCGM exporter metrics retrievable
            WorkloadCheck,        // Sample GPU pod runs successfully
            PyTorchWorkloadCheck, // PyTorch GPU training workload
        },
    })
}
```

Each `Test` implements:
```go
type Test interface {
    Name() string
    Run(ctx context.Context, clients *k8s.Clients, cfg Config) *TestResult
}
```

## Development

```bash
go build ./...                                                # Build
go test ./...                                                 # Test
make build-image                                              # GPU Operator image, which includes gpu-operator-certification
```

## Project Structure

```
internal/certification/
├── *.go             # Framework (runner, config, registry)
├── environment/     # Cluster environment collectors
├── k8s/             # Shared K8s utilities
├── reporting/       # Result reporters
├── utils/time/
└── testsuite/
    ├── register.go  # TestSet registrations
    ├── baseline/    # Default settings tests
    └── clusterpolicy/ # ClusterPolicy mutation tests
```
