# GPU Operator Self-Certification

A self-certification test suite for ISVs to validate NVIDIA GPU Operator compatibility on their platforms. Runs as a Kubernetes Job and outputs structured JSON results and an HTML report. The `gpu-operator-certification` binary ships in the GPU Operator image (`nvcr.io/nvidia/gpu-operator`).

## Quick Start

```bash
# Deploy (GPU Operator must be installed)
kubectl apply -f deployments/gpu-operator-certification/gpu-operator-certification.yaml

# View results
kubectl logs job/gpu-operator-certification -n gpu-operator

# Download results.json and report.html
hack/download-certification-results.sh
```

## Configuration

Configure via the `gpu-operator-certification-config` ConfigMap in `gpu-operator-certification.yaml`:

```yaml
TEST_TIMEOUT: "15m"
TEST_SETS: "all"               # Run all TestSets
# TEST_SETS: "all,!TS1003"     # Exclude specific sets with "!"
# TEST_SETS: "TS1000"          # Or list specific sets to run
```

| Key | Default | Description |
|-------|---------|-------------|
| `namespace` | `gpu-operator` | GPU Operator namespace |
| `TEST_TIMEOUT` | `15m` | Timeout for readiness checks |
| `TEST_SETS` | `all` | TestSets to run (use `!` prefix to exclude) |
| `TEST_WORKLOAD_IMAGE` | `nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04` | Image for the TS1000 GPU workload pod |
| `TARGET_DRIVER_REPOSITORY` | `""` | Driver repository for TS1003 (skipped if any driver key is empty) |
| `TARGET_DRIVER_IMAGE` | `""` | Driver image for TS1003 |
| `TARGET_DRIVER_VERSION` | `""` | Driver version for TS1003 |
| `RDMA_BASE_IMAGE` | `nvcr.io/nvidia/cuda:13.1.1-base-ubuntu24.04` | Base CUDA image for TS1006/TS1007 pods |

The Job reads the ConfigMap only when its pod starts, so delete the Job before applying a changed configuration:

```bash
kubectl delete job gpu-operator-certification -n gpu-operator
kubectl apply -f deployments/gpu-operator-certification/gpu-operator-certification.yaml
```

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
