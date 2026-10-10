# GPU Operator Self-Certification

A self-certification test suite for ISVs to validate NVIDIA GPU Operator compatibility on their platforms. Runs as a Kubernetes Job and outputs structured JSON results. The `gpu-operator-certification` binary ships in the GPU Operator image (`nvcr.io/nvidia/gpu-operator`), and this chart runs it from there.

## Quick Start

```bash
# Deploy (GPU Operator must be installed); match image.tag to the installed operator version
helm install gpu-operator-certification deployments/gpu-operator-certification \
  --namespace gpu-operator \
  --set image.tag=<gpu-operator-version>

# View results
kubectl logs job/gpu-operator-certification -n gpu-operator
```

## Configuration

Configure via Helm values:

```yaml
testTimeout: "15m"
testSets:
  - "all"              # Run all TestSets
  # - "!TS1003"        # Exclude specific sets with "!"
  # - "TS1000"         # Or list specific sets to run
targetDriver:          # TS1003 is skipped unless all three are set
  repository: "nvcr.io/nvidia"
  image: "driver"
  version: "580.126.16"
```

The GPU Operator namespace is not a value. The Job runs in the Helm release namespace (`--namespace`), which it passes to the binary through the `NAMESPACE` environment variable.

| Value | Default | Description |
|-------|---------|-------------|
| `testTimeout` | `15m` | Timeout for readiness checks, as a Go duration |
| `testSets` | `["all"]` | TestSets to run (use `!` prefix to exclude, `[]` to run nothing) |
| `testWorkloadImage` | `nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04` | Image for the GPU workload validation pod (TS1000) |
| `targetDriver.repository` | `""` | Driver image repository to update to in TS1003 |
| `targetDriver.image` | `""` | Driver image name to update to in TS1003 |
| `targetDriver.version` | `""` | Driver version to update to in TS1003; TS1003 is skipped unless all three `targetDriver` fields are set |
| `rdmaBaseImage` | `nvcr.io/nvidia/cuda:13.1.1-base-ubuntu24.04` | Base CUDA image for TS1006/TS1007 RDMA test pods; its glibc must be at least as new as the host MOFED libraries |
| `image.repository` | `nvcr.io/nvidia/gpu-operator` | GPU Operator image that contains `gpu-operator-certification` |
| `image.tag` | chart `appVersion` | GPU Operator image tag |
| `image.pullPolicy` | `Always` | Pull policy for the certification image |
| `imagePullSecrets` | `[]` | Image pull secrets for the certification Job pod |

## Test Sets

| ID       | Name                        | Status | Description |
|----------|-----------------------------|--------|-------------|
| `TS1000` | Default Settings            | ✅ | Operands, GPU allocatable, GFD labels, DCGM metrics, workload |
| `TS1001` | Timeslicing                 | ✅ | GPU sharing via time-slicing ConfigMap |
| `TS1002` | DCGM Standalone             | ✅ | Standalone DCGM enablement |
| `TS1003` | Driver Update               | ✅ | Driver version update via ClusterPolicy |
| `TS1004` | MIG (single)                | ✅ | MIG mode with single strategy |
| `TS1005` | MIG (mixed)                 | ✅ | MIG mode with mixed strategy |
| `TS1006` | GPUDirect RDMA              | ✅ | RDMA bandwidth/NCCL tests |
| `TS1007` | GPUDirect RDMA with DMA-BUF | ✅ | RDMA bandwidth/NCCL tests |
| `TS1008` | GDRCopy                     | ✅ | GDRCopy sidecar and workload |

## Output

**Structured results** are JSON on stdout:

```json
{
  "duration": "25.4s",
  "tests": [
    {"name": "operand-verification", "status": "passed", ...},
    {"name": "gpu-allocatable", "status": "passed", ...}
  ],
  "summary": {"total": 5, "passed": 5, "failed": 0, "skipped": 0}
}
```

**Real-time progress** is logged to stderr for visibility during execution. A UI for viewing and sharing results is planned for a future release.

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
        Name:        "TS1",
        Description: "Default Settings",
        Tests: []certification.Test{
            OperandCheck,     // GPU Operator operands are running
            AllocatableCheck, // nvidia.com/gpu in node allocatable
            GFDLabelsCheck,   // GFD labels on nodes
            DCGMMetricsCheck, // DCGM exporter metrics retrievable
            WorkloadCheck,    // Sample GPU pod runs successfully
        },
    })
}
```

Each `Test` implements:
```go
type Test interface {
    Name() string
    Run(ctx context.Context, client kubernetes.Interface, cfg Config) *TestResult
}
```

## Development

Run from the root of the GPU Operator repository:

```bash
make cmd-gpu-operator-certification   # Build the binary
go test ./internal/certification/...  # Test
make build-image                      # GPU Operator image, which includes gpu-operator-certification
```

## Project Structure

```
cmd/gpu-operator-certification/          # Entry point
internal/certification/                  # Framework (runner, config, registry)
├── k8s/                                 # Shared K8s utilities
├── environment/                         # Cluster environment collectors
├── reporting/                           # JSON and HTML report output
├── testsuite/
│   ├── register.go                      # TestSet registrations
│   ├── baseline/                        # Default settings tests
│   └── clusterpolicy/                   # ClusterPolicy mutation tests
└── utils/time/                          # Time helpers
deployments/gpu-operator-certification/  # Helm chart
hack/download-certification-results.sh   # Fetch results from the results ConfigMap
```
