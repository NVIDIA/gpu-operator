# GPU Operator Self-Certification

A self-certification test suite for ISVs to validate NVIDIA GPU Operator compatibility on their platforms. Runs as a Kubernetes Job and outputs structured JSON results. The latest image (nvcr.io/ea-cnt/nv_only/gpu-operator-certification:latest) can be provided for users to use for self certification.

## Quick Start

```bash
# Build and push a multi-arch image (linux/amd64, linux/arm64)
make docker-build IMAGE_TAG=latest

# Deploy (GPU Operator must be installed)
helm install gpu-operator-certification deploy/helm/gpu-operator-certification \
  --namespace gpu-operator

# View results
kubectl logs job/gpu-operator-certification -n gpu-operator
```

## Configuration

Configure via Helm values:

```yaml
namespace: gpu-operator
timeout: "15m"
testSets:
  - "all"              # Run all TestSets
  # - "!TS1003"        # Exclude specific sets with "!"
  # - "TS1000"         # Or list specific sets to run
```

| Value | Default | Description |
|-------|---------|-------------|
| `namespace` | `gpu-operator` | GPU Operator namespace |
| `timeout` | `15m` | Timeout for readiness checks |
| `testSets` | `["all"]` | TestSets to run (use `!` prefix to exclude) |

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

Tests are registered via `init()` in `internal/testsuite/register.go`. Each `TestSet` groups reusable `Test` primitives:

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

```bash
go build ./...                                                # Build
go test ./...                                                 # Test
make docker-build IMAGE_TAG=dev                               # linux/amd64 + linux/arm64, pushes to the registry
make docker-buildx IMAGE_TAG=dev                              # Same as docker-build (forces multi-arch push)
make docker-build BUILD_MULTI_ARCH_IMAGES=false IMAGE_TAG=dev # Image for the host architecture
make docker-build docker-push BUILD_MULTI_ARCH_IMAGES=false IMAGE_TAG=dev # Host-arch build, then docker push
```

`BUILD_MULTI_ARCH_IMAGES` defaults to `true`. Multi-arch images are pushed during build because a manifest list cannot be loaded into the local Docker daemon. `make docker-buildx` is equivalent to `make docker-build BUILD_MULTI_ARCH_IMAGES=true PUSH_ON_BUILD=true`.

## Project Structure

```
internal/
├── certification/   # Framework (runner, config, registry)
├── helpers/         # Shared K8s utilities
└── testsuite/
    ├── register.go  # TestSet registrations
    ├── baseline/    # Default settings tests
    └── clusterpolicy/ # ClusterPolicy mutation tests
```
