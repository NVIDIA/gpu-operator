package testsuite

import (
	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/testsuite/baseline"
	"github.com/NVIDIA/gpu-operator/internal/certification/testsuite/clusterpolicy"
)

// Reusable test primitives - can be composed into any TestSet
var (
	OperandCheck         = &baseline.Operands{}
	AllocatableCheck     = &baseline.Allocatable{}
	GFDLabelsCheck       = &baseline.GFDLabels{}
	DCGMMetricsCheck     = &baseline.DCGMMetrics{}
	WorkloadCheck        = &baseline.Workload{}
	PyTorchWorkloadCheck = &baseline.PyTorchWorkload{}
)

func init() {
	// Validates baseline GPU Operator functionality
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

	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1001",
		Description: "GPU Sharing via Timeslicing",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.GPUSharing{},
		},
	})

	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1002",
		Description: "DCGM Standalone",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.EnableDCGM{},
			DCGMMetricsCheck,
		},
	})

	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1003",
		Description: "Driver Update via GPU Operator",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.ImageUpdates{},
			OperandCheck, // verify operands come back
			WorkloadCheck,
		},
	})

	// TS1004 - MIG mode with single strategy
	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1004",
		Description: "MIG Mode (Single Strategy)",
		Tests: []certification.Test{
			OperandCheck,
			GFDLabelsCheck,
			&clusterpolicy.MIG{Strategy: "single"},
		},
	})

	// TS1005 - MIG mode with mixed strategy
	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1005",
		Description: "MIG Mode (Mixed Strategy)",
		Tests: []certification.Test{
			OperandCheck,
			GFDLabelsCheck,
			&clusterpolicy.MIG{Strategy: "mixed"},
		},
	})

	// TS1006 - GPUDirectRDMA
	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1006",
		Description: "GPUDirectRDMA",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.RDMA{},
		},
	})

	// TS1007 - GPUDirectRDMA with DMA-BUF
	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1007",
		Description: "GPUDirectRDMA with DMA-BUF",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.RDMA{DMABuf: true},
		},
	})

	// TS1008 - GDRCopy enabled
	certification.RegisterTestSet(&certification.TestSet{
		Name:        "TS1008",
		Description: "GDRCopy Enabled",
		Tests: []certification.Test{
			OperandCheck,
			&clusterpolicy.GDRCopy{},
		},
	})
}
