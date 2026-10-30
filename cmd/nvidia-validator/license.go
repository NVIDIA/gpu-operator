/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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
**/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

// licenseAnnotationKey is the Node annotation that reports each GPU's vGPU license
// state as a JSON object:
//
//	{"gpus": [
//	  {"uuid": "GPU-...", "pciBusID": "00000000:03:00.0", "status": "Licensed",
//	   "licensedProduct": "NVIDIA Virtual Compute Server", "expiresAt": "2026-10-30T19:27:30Z"}]}
//
// status is Licensed, Unlicensed, NotApplicable (see isLicensingEvaluated) or
// Unknown. licensedProduct is the product that nvidia-smi reports, which for a
// passed-through GPU without another license is NVIDIA Virtual Applications.
// expiresAt is omitted unless nvidia-smi reports an expiry date.
const licenseAnnotationKey = "nvidia.com/vgpu-license"

// licenseReport is the value of licenseAnnotationKey.
type licenseReport struct {
	GPUs []gpuLicense `json:"gpus"`
}

// gpuLicense identifies a GPU and its observed license state.
type gpuLicense struct {
	UUID            string           `json:"uuid"`
	PCIBusID        string           `json:"pciBusID"`
	Status          gpuLicenseStatus `json:"status"`
	LicensedProduct string           `json:"licensedProduct,omitempty"`
	ExpiresAt       *time.Time       `json:"expiresAt,omitempty"`
}

type gpuLicenseStatus string

const (
	gpuLicensed             gpuLicenseStatus = "Licensed"
	gpuUnlicensed           gpuLicenseStatus = "Unlicensed"
	gpuLicenseNotApplicable gpuLicenseStatus = "NotApplicable"
	gpuLicenseUnknown       gpuLicenseStatus = "Unknown"
)

// LicenseReporter publishes the local GPUs' vGPU license states on their Node.
type LicenseReporter struct {
	waiter func(ctx context.Context, interval time.Duration) bool

	// readSMIOutput returns the parsed nvidia-smi output
	readSMIOutput func(context.Context) (smiXMLOutput, error)

	// nodes and nodeName identify the k8s Node that publishLicenseReport writes to
	nodes    typedcorev1.NodeInterface
	nodeName string
}

// NewLicenseReporter returns a reporter that can read the license state of the node's GPUs
// and report them as a licenseAnnotationKey annotation on the Node object.
func NewLicenseReporter() (*LicenseReporter, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	log.Printf("vGPU license: reporting the license state of node %s", nodeNameFlag)

	reader := &smiXMLReader{
		statusFile: filepath.Join(outputDirFlag, driverStatusFile),
		hostRoot:   "/host",
		driverRoot: driverInstallDirCtrPathFlag,
	}
	return &LicenseReporter{
		readSMIOutput: reader.Read,
		nodes:         kubeClient.CoreV1().Nodes(),
		nodeName:      nodeNameFlag,
		waiter:        waitForPoll,
	}, nil
}

// licensePollInterval is how often the reporter queries nvidia-smi and patches
// the Node. License states change rarely, so a few minutes of latency keeps the
// load on the API server low in large clusters.
const licensePollInterval = 5 * time.Minute

// Run polls nvidia-smi and updates the Node annotation until ctx is canceled.
func (r *LicenseReporter) Run(ctx context.Context) {
	for ctx.Err() == nil {
		out, err := r.readSMIOutput(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return
		case errors.Is(err, errDriverNotReady):
			log.Infof("vGPU license: %v", err)
		case err != nil:
			log.Errorf("vGPU license: failed to read nvidia-smi output: %v", err)
		default:
			report, err := extractLicenseReport(out)
			if err != nil {
				log.Warnf("vGPU license: GPUs with an unknown license state: %v", err)
			}
			if err := r.publishLicenseReport(ctx, report); err != nil {
				log.Errorf("vGPU license: failed to publish license report: %v", err)
			}
		}

		if !r.waiter(ctx, licensePollInterval) {
			return
		}
	}
}

// waitForPoll waits for interval and returns false if ctx ends first. It
// waits up to 10% less, to spread Node writes across nodes.
func waitForPoll(ctx context.Context, interval time.Duration) bool {
	delay := interval - time.Duration(rand.Float64()*float64(interval/10))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// extractLicenseReport returns the license state from smiXMLOutput
func extractLicenseReport(smiData smiXMLOutput) (licenseReport, error) {
	var diagnostics error
	gpus := make([]gpuLicense, 0, len(smiData.GPUs))
	for i, gpu := range smiData.GPUs {
		entry := gpuLicense{Status: gpuLicenseNotApplicable}
		if isLicensingEvaluated(gpu) {
			var err error
			if entry, err = parseLicensedProducts(gpu.LicensedProducts); err != nil {
				diagnostics = errors.Join(diagnostics, fmt.Errorf("GPU %d: %w", i, err))
			}
		}
		entry.UUID, entry.PCIBusID = gpu.UUID, gpu.PCIBusID
		gpus = append(gpus, entry)
	}
	sort.Slice(gpus, func(i, j int) bool { return gpus[i].UUID < gpus[j].UUID })
	return licenseReport{GPUs: gpus}, diagnostics
}

// isLicensingEvaluated reports whether a GPU's license state is evaluated rather
// than reported as NotApplicable. A vGPU always needs a license. A passed-through
// GPU needs one only for a licensed product, such as vWS. nvidia-smi reports a
// licensed product for it only if its driver supports licensing, and then reports
// NVIDIA Virtual Applications unless another product is licensed.
func isLicensingEvaluated(gpu smiXMLGPU) bool {
	switch gpu.VirtualizationMode {
	case "VGPU":
		return true
	case "Pass-Through":
		return len(gpu.LicensedProducts) > 0
	default:
		return false
	}
}

// parseLicensedProducts parses a GPU's vgpu_software_licensed_product elements.
func parseLicensedProducts(products []smiXMLLicensedProduct) (gpuLicense, error) {
	if len(products) > 1 {
		return gpuLicense{Status: gpuLicenseUnknown}, fmt.Errorf("more than one vgpu_software_licensed_product element: %q", products)
	}
	var product smiXMLLicensedProduct
	if len(products) == 1 {
		product = products[0]
	}
	status, expiresAt, err := parseLicenseStatus(product.Status)
	if err != nil {
		return gpuLicense{Status: gpuLicenseUnknown}, err
	}
	return gpuLicense{Status: status, LicensedProduct: product.Name, ExpiresAt: expiresAt}, nil
}

// parseLicenseStatus parses a license_status element. nvidia-smi prints Unlicensed,
// or Licensed (Expiry: X), where X is a date or says that the expiry is permanent,
// not applicable, not available or could not be read.
func parseLicenseStatus(raw string) (gpuLicenseStatus, *time.Time, error) {
	switch raw {
	case "Unlicensed":
		return gpuUnlicensed, nil, nil
	case "Licensed (Expiry: Permanent)", "Licensed (Expiry: N/A)", "Licensed (Expiry: Not available)", "Licensed (Expiry: ERR!)":
		return gpuLicensed, nil, nil
	}
	value, ok := strings.CutPrefix(raw, "Licensed (Expiry: ")
	if !ok {
		return gpuLicenseUnknown, nil, fmt.Errorf("unrecognized license status %q", raw)
	}
	if !strings.HasSuffix(value, " GMT)") {
		return gpuLicenseUnknown, nil, fmt.Errorf("invalid license expiry format %q", raw)
	}
	expiry, err := time.Parse("2006-1-2 15:4:5", strings.TrimSuffix(value, " GMT)"))
	if err != nil {
		return gpuLicenseUnknown, nil, fmt.Errorf("invalid license expiry date: %w", err)
	}
	return gpuLicensed, &expiry, nil
}

const licenseAPITimeout = 10 * time.Second

// publishLicenseReport sets licenseAnnotationKey on the Node to report.
func (r *LicenseReporter) publishLicenseReport(ctx context.Context, report licenseReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to encode license report: %w", err)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{licenseAnnotationKey: string(data)}}})
	if err != nil {
		return fmt.Errorf("failed to encode license annotation patch: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, licenseAPITimeout)
	defer cancel()
	if _, err = r.nodes.Patch(ctx, r.nodeName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("failed to patch node %s: %w", r.nodeName, err)
	}
	return nil
}
