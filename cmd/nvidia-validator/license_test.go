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
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// singleGPUExpiry is the expiry in testdata/smi/licensed-single.xml.
var singleGPUExpiry = time.Date(2026, 10, 30, 19, 27, 30, 0, time.UTC)

// noGPUsXML is the nvidia-smi output of a node without GPUs.
var noGPUsXML = []byte("<nvidia_smi_log><attached_gpus>0</attached_gpus></nvidia_smi_log>")

func smiFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/smi/" + name)
	require.NoError(t, err)
	return data
}

// singleGPUXML is licensed-single.xml with its license status replaced.
func singleGPUXML(t *testing.T, rawLicense string) []byte {
	t.Helper()
	const licensed = "Licensed (Expiry: 2026-10-30 19:27:30 GMT)"
	return []byte(strings.ReplaceAll(string(smiFixture(t, "licensed-single.xml")), licensed, rawLicense))
}

// notApplicableXML is licensed-single.xml with its GPU outside vGPU mode.
func notApplicableXML(t *testing.T) []byte {
	t.Helper()
	return []byte(strings.ReplaceAll(string(smiFixture(t, "licensed-single.xml")), ">VGPU<", ">None<"))
}

// vcs is the licensed product of the GPUs in licensed-single.xml and
// licensed-multiple.xml.
const vcs = "NVIDIA Virtual Compute Server"

// makeTestReport is the report of the one GPU in licensed-single.xml. Its licensed
// product is reported unless the status is NotApplicable or Unknown.
func makeTestReport(status gpuLicenseStatus, expiresAt *time.Time) licenseReport {
	product := vcs
	if status == gpuLicenseNotApplicable || status == gpuLicenseUnknown {
		product = ""
	}
	return licenseReport{GPUs: []gpuLicense{{
		UUID: "GPU-00000000-0000-0000-0000-000000000001", PCIBusID: "00000000:03:00.0", Status: status, LicensedProduct: product, ExpiresAt: expiresAt,
	}}}
}

func makeTestLicensed(expiry time.Time) licenseReport {
	return makeTestReport(gpuLicensed, &expiry)
}

func makeTestUnlicensed() licenseReport {
	return makeTestReport(gpuUnlicensed, nil)
}

func makeTestNotApplicable() licenseReport {
	return makeTestReport(gpuLicenseNotApplicable, nil)
}

// mustParseSMIOutput parses nvidia-smi output that must be valid.
func mustParseSMIOutput(t *testing.T, data []byte) smiXMLOutput {
	t.Helper()
	var out smiXMLOutput
	require.NoError(t, xml.Unmarshal(data, &out))
	return out
}

// decodeTestLicense decodes the license state in nvidia-smi output.
func decodeTestLicense(t *testing.T, data []byte) (licenseReport, error) {
	t.Helper()
	return extractLicenseReport(mustParseSMIOutput(t, data))
}

func mustDecodeLicense(t *testing.T, data []byte) licenseReport {
	t.Helper()
	report, err := decodeTestLicense(t, data)
	require.NoError(t, err)
	return report
}

// mock-nvml-states.xml is the output of the real nvidia-smi 580.65.06 binary run
// against the mock NVML of NVIDIA/k8s-test-infra, so its formatting is real while
// the license states are simulated. shim-nvml-states.xml is from the same binary
// with license values injected into the mock NVML, for states the mock cannot
// simulate.
func TestLicenseDecodeCaptured(t *testing.T) {
	single := makeTestLicensed(singleGPUExpiry)
	multiple := makeTestLicensed(time.Date(2026, 10, 30, 21, 26, 9, 0, time.UTC))
	multiple.GPUs = append(multiple.GPUs, gpuLicense{UUID: "GPU-00000000-0000-0000-0000-000000000002", PCIBusID: "00000000:03:01.0", Status: gpuLicensed, LicensedProduct: vcs, ExpiresAt: multiple.GPUs[0].ExpiresAt})
	mockExpiry := new(time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC))
	mockGPU := func(i int, status gpuLicenseStatus, product string, expiresAt *time.Time) gpuLicense {
		busIDs := []string{"00000000:07:00.0", "00000000:0F:00.0", "00000000:47:00.0", "00000000:4E:00.0", "00000000:87:00.0", "00000000:90:00.0", "00000000:B7:00.0", "00000000:BD:00.0"}
		return gpuLicense{UUID: fmt.Sprintf("GPU-12345678-1234-1234-1234-12345678000%d", i), PCIBusID: busIDs[i], Status: status, LicensedProduct: product, ExpiresAt: expiresAt}
	}
	mockStates := licenseReport{GPUs: []gpuLicense{
		mockGPU(0, gpuUnlicensed, vcs, nil),
		mockGPU(1, gpuLicenseNotApplicable, "", nil), // passed through, without licensing support
		mockGPU(2, gpuLicensed, vcs, nil),            // permanent
		mockGPU(3, gpuLicensed, vcs, mockExpiry),
	}}
	shimStates := licenseReport{GPUs: []gpuLicense{
		mockGPU(0, gpuLicensed, vcs, nil),                                     // expiry not available
		mockGPU(1, gpuLicensed, vcs, nil),                                     // expiry could not be read
		mockGPU(2, gpuLicensed, vcs, nil),                                     // expiry not applicable
		mockGPU(3, gpuLicensed, "NVIDIA Virtual Applications", nil),           // passed through, without another license
		mockGPU(4, gpuLicensed, "NVIDIA RTX Virtual Workstation", mockExpiry), // passed through, with vWS
		mockGPU(5, gpuLicenseNotApplicable, "", nil),                          // passed through, without licensing support
		mockGPU(6, gpuUnlicensed, "", nil),                                    // vGPU without a licensed feature
		mockGPU(7, gpuLicenseNotApplicable, "", nil),                          // bare metal
	}}
	for name, want := range map[string]licenseReport{"licensed-single.xml": single, "licensed-multiple.xml": multiple, "mock-nvml-states.xml": mockStates, "shim-nvml-states.xml": shimStates} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want, mustDecodeLicense(t, smiFixture(t, name)))
		})
	}
}

func TestLicenseWireFormat(t *testing.T) {
	for name, tc := range map[string]struct {
		report licenseReport
		want   string
	}{
		"licensed GPU": {
			report: makeTestLicensed(singleGPUExpiry),
			want:   `{"gpus":[{"uuid":"GPU-00000000-0000-0000-0000-000000000001","pciBusID":"00000000:03:00.0","status":"Licensed","licensedProduct":"NVIDIA Virtual Compute Server","expiresAt":"2026-10-30T19:27:30Z"}]}`,
		},
		// An empty list must encode as [], not null.
		"no GPUs": {
			report: mustDecodeLicense(t, noGPUsXML),
			want:   `{"gpus":[]}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(tc.report)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(data))
		})
	}
}

func TestLicenseDecodeSyntheticStates(t *testing.T) {
	// The XML structure is captured; these license strings are synthetic.
	for name, tc := range map[string]struct {
		rawLicense     string
		wantGPUStatus  gpuLicenseStatus
		wantExpiry     *time.Time
		wantDiagnostic string // empty if the state decodes cleanly
	}{
		"unpadded date":           {rawLicense: "Licensed (Expiry: 2026-11-1 1:2:3 GMT)", wantGPUStatus: gpuLicensed, wantExpiry: new(time.Date(2026, 11, 1, 1, 2, 3, 0, time.UTC))},
		"permanent":               {rawLicense: "Licensed (Expiry: Permanent)", wantGPUStatus: gpuLicensed},
		"expiry not applicable":   {rawLicense: "Licensed (Expiry: N/A)", wantGPUStatus: gpuLicensed},
		"expiry not available":    {rawLicense: "Licensed (Expiry: Not available)", wantGPUStatus: gpuLicensed},
		"expiry not read":         {rawLicense: "Licensed (Expiry: ERR!)", wantGPUStatus: gpuLicensed},
		"licensed without expiry": {rawLicense: "Licensed", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `unrecognized license status "Licensed"`},
		"unlicensed":              {rawLicense: "Unlicensed", wantGPUStatus: gpuUnlicensed},
		"unrecognized text":       {rawLicense: "Something new", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `unrecognized license status "Something new"`},
		"N/A":                     {rawLicense: "N/A", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `unrecognized license status "N/A"`},
		"empty license_status":    {rawLicense: "", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `unrecognized license status ""`},
		"impossible date":         {rawLicense: "Licensed (Expiry: 2026-2-30 1:2:3 GMT)", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `invalid license expiry date: parsing time "2026-2-30 1:2:3": day out of range`},
		"past expiry":             {rawLicense: "Licensed (Expiry: 2020-1-1 0:0:0 GMT)", wantGPUStatus: gpuLicensed, wantExpiry: new(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))},
		"wrong zone":              {rawLicense: "Licensed (Expiry: 2026-11-1 1:2:3 PST)", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `invalid license expiry format "Licensed (Expiry: 2026-11-1 1:2:3 PST)"`},
		"control characters":      {rawLicense: "bad\nvalue", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `unrecognized license status "bad\nvalue"`},
		"trailing text":           {rawLicense: "Licensed (Expiry: 2026-11-1 1:2:3 GMT) junk", wantGPUStatus: gpuLicenseUnknown, wantDiagnostic: `invalid license expiry format "Licensed (Expiry: 2026-11-1 1:2:3 GMT) junk"`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeTestLicense(t, singleGPUXML(t, tc.rawLicense))
			if tc.wantDiagnostic == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantDiagnostic)
			}
			require.Equal(t, makeTestReport(tc.wantGPUStatus, tc.wantExpiry), got)
		})
	}
}

// A second licensed product makes the GPU Unknown, because it is not clear which
// one applies.
func TestLicenseDecodeAmbiguousLicense(t *testing.T) {
	const second = "<vgpu_software_licensed_product><licensed_product_name>NVIDIA RTX Virtual Workstation</licensed_product_name><license_status>Unlicensed</license_status></vgpu_software_licensed_product>"
	input := strings.Replace(string(smiFixture(t, "licensed-single.xml")), "</vgpu_software_licensed_product>", "</vgpu_software_licensed_product>"+second, 1)
	got, err := decodeTestLicense(t, []byte(input))
	require.ErrorContains(t, err, "more than one vgpu_software_licensed_product element")
	require.Equal(t, makeTestReport(gpuLicenseUnknown, nil), got)
}

func TestLicenseDecodeNotApplicable(t *testing.T) {
	for name, input := range map[string]string{
		"mode None":       string(notApplicableXML(t)),
		"no mode element": strings.ReplaceAll(string(smiFixture(t, "licensed-single.xml")), "<virtualization_mode>VGPU</virtualization_mode>", ""),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, makeTestNotApplicable(), mustDecodeLicense(t, []byte(input)))
		})
	}
}

// A passed-through GPU is evaluated only if nvidia-smi reports a licensed product.
func TestLicenseDecodePassThrough(t *testing.T) {
	fixture := strings.ReplaceAll(string(smiFixture(t, "licensed-single.xml")), ">VGPU<", ">Pass-Through<")
	before, rest, _ := strings.Cut(fixture, "<vgpu_software_licensed_product>")
	_, after, _ := strings.Cut(rest, "</vgpu_software_licensed_product>")
	for name, tc := range map[string]struct {
		input string
		want  licenseReport
	}{
		"licensed":            {input: fixture, want: makeTestLicensed(singleGPUExpiry)},
		"no licensed product": {input: before + after, want: makeTestNotApplicable()},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, mustDecodeLicense(t, []byte(tc.input)))
		})
	}
}

func TestLicenseDecodeMixedModes(t *testing.T) {
	fixture := string(smiFixture(t, "licensed-multiple.xml"))
	// Only the first GPU leaves vGPU mode.
	got := mustDecodeLicense(t, []byte(strings.Replace(fixture, ">VGPU<", ">None<", 1)))
	require.Len(t, got.GPUs, 2)
	require.Equal(t, gpuLicenseNotApplicable, got.GPUs[0].Status)
	require.Nil(t, got.GPUs[0].ExpiresAt)
	require.Equal(t, gpuLicensed, got.GPUs[1].Status)
	require.NotNil(t, got.GPUs[1].ExpiresAt)
}

func TestLicenseDecodeStableOrder(t *testing.T) {
	fixture := string(smiFixture(t, "licensed-multiple.xml"))
	fixture = strings.ReplaceAll(fixture, "GPU-00000000-0000-0000-0000-000000000001", "GPU-z")
	fixture = strings.ReplaceAll(fixture, "GPU-00000000-0000-0000-0000-000000000002", "GPU-a")
	got := mustDecodeLicense(t, []byte(fixture))
	require.Len(t, got.GPUs, 2)
	require.Equal(t, "GPU-a", got.GPUs[0].UUID)
	require.Equal(t, "00000000:03:01.0", got.GPUs[0].PCIBusID)
}

// licenseReportingStep is one poll of the reporter.
type licenseReportingStep struct {
	data       []byte // the nvidia-smi output
	readErr    error
	publishErr error
	// want is the report the poll should publish, or nil if it should not
	// publish.
	want *licenseReport
}

// runLicenseScript runs the reporter through steps, one poll per step. It checks
// that each step publishes what its want says, and that the reporter keeps
// polling until the last step, waiting licensePollInterval after every poll.
func runLicenseScript(t *testing.T, steps []licenseReportingStep) {
	t.Helper()
	var (
		polls     int
		published = map[int]licenseReport{} // step index to what that step published
	)
	nodes := &licenseTestNodes{
		// read runs first in each poll, so polls-1 is the step in onPatch.
		onPatch: func(data []byte) error {
			published[polls-1] = decodeLicensePatch(t, data)
			return steps[polls-1].publishErr
		},
	}
	reporter := LicenseReporter{
		nodes: nodes,
		readSMIOutput: func(ctx context.Context) (smiXMLOutput, error) {
			polls++
			if steps[polls-1].readErr != nil {
				return smiXMLOutput{}, steps[polls-1].readErr
			}
			var out smiXMLOutput
			return out, xml.Unmarshal(steps[polls-1].data, &out)
		},
		waiter: func(ctx context.Context, interval time.Duration) bool {
			require.Equal(t, licensePollInterval, interval, "poll %d", polls)
			return polls < len(steps)
		},
	}
	reporter.Run(t.Context())
	require.Equal(t, len(steps), polls, "polling must continue after a failed poll")
	for i, step := range steps {
		if step.want == nil {
			require.NotContains(t, published, i, "step %d must not publish", i)
			continue
		}
		require.Contains(t, published, i, "step %d must publish", i)
		require.Equal(t, *step.want, published[i], "step %d", i)
	}
}

// Every poll that decodes is published, and a poll that fails publishes nothing.
func TestLicenseReportingLoop(t *testing.T) {
	licensedXML := smiFixture(t, "licensed-single.xml")
	licensed := new(makeTestLicensed(singleGPUExpiry))
	runLicenseScript(t, []licenseReportingStep{
		{data: licensedXML, want: licensed},
		// The same content is published again. The API server ignores a patch that
		// changes nothing.
		{data: licensedXML, want: licensed},
		// A failed publish does not stop the next poll.
		{data: singleGPUXML(t, "Unlicensed"), publishErr: fmt.Errorf("API unavailable"), want: new(makeTestUnlicensed())},
		{data: licensedXML, want: licensed},
		{data: singleGPUXML(t, "Licensed (Expiry: 2026-10-31 19:27:30 GMT)"), want: new(makeTestLicensed(singleGPUExpiry.Add(24 * time.Hour)))},
		// A node without GPUs, or with a GPU outside vGPU mode, is published like any
		// other state.
		{data: noGPUsXML, want: new(mustDecodeLicense(t, noGPUsXML))},
		{data: notApplicableXML(t), want: new(makeTestNotApplicable())},
		// A failed query or undecodable output publishes nothing, so the Node keeps
		// the earlier content.
		{readErr: fmt.Errorf("query failed")},
		{readErr: errDriverNotReady},
		{data: []byte("not XML")},
		// A GPU with an unknown license state is published with the others.
		{data: singleGPUXML(t, "Something new"), want: new(makeTestReport(gpuLicenseUnknown, nil))},
		{data: licensedXML, want: licensed},
	})
}

func TestLicenseReportingCancellation(t *testing.T) {
	for name, tc := range map[string]struct {
		cancelBeforeRun bool
		wantReads       int
	}{
		"cancelled before startup": {cancelBeforeRun: true, wantReads: 0},
		"cancelled during query":   {cancelBeforeRun: false, wantReads: 1},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelBeforeRun {
				cancel()
			}
			reads := 0
			nodes := &licenseTestNodes{}
			reporter := LicenseReporter{
				readSMIOutput: func(ctx context.Context) (smiXMLOutput, error) { reads++; cancel(); return smiXMLOutput{}, ctx.Err() },
				nodes:         nodes,
			}
			reporter.Run(ctx)
			require.Equal(t, tc.wantReads, reads)
			require.Empty(t, nodes.patches)
		})
	}
}

func TestLicensePollWaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, waitForPoll(ctx, licensePollInterval), "cancellation must stop the reporting loop")
}

func TestLicensePollWaitElapsed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.True(t, waitForPoll(ctx, time.Millisecond), "elapsed timer must allow the next poll")
}

type licensePatch struct {
	name         string
	kind         types.PatchType
	data         []byte
	subresources []string
}
type licenseTestNodes struct {
	typedcorev1.NodeInterface
	patches []licensePatch
	failure error
	// onPatch, if set, is called with each patch's data. A non-nil result fails the patch.
	onPatch func(data []byte) error
}

func (n *licenseTestNodes) Patch(ctx context.Context, name string, kind types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*corev1.Node, error) {
	n.patches = append(n.patches, licensePatch{name, kind, data, subresources})
	if n.onPatch != nil {
		if err := n.onPatch(data); err != nil {
			return nil, err
		}
	}
	return &corev1.Node{}, n.failure
}

// decodeLicensePatch returns the licenseReport in the annotation set by a patch.
func decodeLicensePatch(t *testing.T, data []byte) licenseReport {
	t.Helper()
	var payload map[string]map[string]map[string]string
	require.NoError(t, json.Unmarshal(data, &payload))
	var decoded licenseReport
	require.NoError(t, json.Unmarshal([]byte(payload["metadata"]["annotations"][licenseAnnotationKey]), &decoded))
	return decoded
}

func TestLicensePublication(t *testing.T) {
	nodes := &licenseTestNodes{}
	reporter := &LicenseReporter{nodes: nodes, nodeName: "local"}
	report := mustDecodeLicense(t, smiFixture(t, "licensed-multiple.xml"))
	require.NoError(t, reporter.publishLicenseReport(t.Context(), report))
	require.Len(t, nodes.patches, 1)
	patch := nodes.patches[0]
	require.Equal(t, "local", patch.name)
	require.Equal(t, types.MergePatchType, patch.kind)
	require.Empty(t, patch.subresources)
	var payload map[string]map[string]map[string]string
	require.NoError(t, json.Unmarshal(patch.data, &payload))
	require.Len(t, payload, 1)
	require.Len(t, payload["metadata"], 1)
	require.Len(t, payload["metadata"]["annotations"], 1)
	require.Equal(t, report, decodeLicensePatch(t, patch.data))

	// Each patch replaces the entire inventory, including with an empty list.
	for _, report = range []licenseReport{
		mustDecodeLicense(t, smiFixture(t, "licensed-single.xml")),
		mustDecodeLicense(t, noGPUsXML),
	} {
		require.NoError(t, reporter.publishLicenseReport(t.Context(), report))
		require.Equal(t, report, decodeLicensePatch(t, nodes.patches[len(nodes.patches)-1].data))
	}
}

func TestLicensePublicationFailure(t *testing.T) {
	report := mustDecodeLicense(t, smiFixture(t, "licensed-single.xml"))
	for name, failure := range map[string]error{
		"forbidden":   apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "local", fmt.Errorf("denied")),
		"not found":   apierrors.NewNotFound(schema.GroupResource{Resource: "nodes"}, "local"),
		"conflict":    apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, "local", fmt.Errorf("conflict")),
		"unavailable": apierrors.NewServiceUnavailable("unavailable"),
	} {
		t.Run(name, func(t *testing.T) {
			nodes := &licenseTestNodes{failure: failure}
			reporter := &LicenseReporter{nodes: nodes, nodeName: "local"}
			require.ErrorIs(t, reporter.publishLicenseReport(t.Context(), report), failure)
			nodes.failure = nil
			require.NoError(t, reporter.publishLicenseReport(t.Context(), report))
		})
	}
}
