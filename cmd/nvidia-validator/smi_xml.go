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
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// errDriverNotReady is a normal state until driver validation has finished.
var errDriverNotReady = errors.New("driver is not ready")

// smiQueryTimeout bounds one run of nvidia-smi.
const smiQueryTimeout = 10 * time.Second

// smiXMLOutput is the part of the nvidia-smi -q -x document that this package
// reads.
type smiXMLOutput struct {
	XMLName xml.Name    `xml:"nvidia_smi_log"`
	GPUs    []smiXMLGPU `xml:"gpu"`
}

// smiXMLGPU is a gpu element of smiXMLOutput.
type smiXMLGPU struct {
	PCIBusID           string `xml:"id,attr"`
	UUID               string `xml:"uuid"`
	VirtualizationMode string `xml:"gpu_virtualization_mode>virtualization_mode"`
	// LicensedProducts is a slice so that a second element is visible.
	LicensedProducts []smiXMLLicensedProduct `xml:"vgpu_software_licensed_product"`
}

// smiXMLLicensedProduct is a vgpu_software_licensed_product element of smiXMLGPU.
type smiXMLLicensedProduct struct {
	Name   string `xml:"licensed_product_name"`
	Status string `xml:"license_status"`
}

// smiXMLReader runs nvidia-smi -q -x from wherever the driver is installed and
// parses its output.
type smiXMLReader struct {
	statusFile string // the driver-ready file that driver validation writes
	hostRoot   string // where the host's root filesystem is mounted
	driverRoot string // where a containerized driver's root filesystem is mounted
}

// Read runs nvidia-smi and returns its parsed output. Text around the XML, such as
// a warning, is ignored. It returns errDriverNotReady until driver validation has
// written the status file.
func (r *smiXMLReader) Read(ctx context.Context) (smiXMLOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, smiQueryTimeout)
	defer cancel()

	cmd, err := r.buildCommand(ctx)
	if err != nil {
		return smiXMLOutput{}, err
	}
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return smiXMLOutput{}, context.Cause(ctx)
		}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return smiXMLOutput{}, fmt.Errorf("%w; stderr: %q", err, exitErr.Stderr)
		}
		return smiXMLOutput{}, err
	}
	var out smiXMLOutput
	if err := xml.Unmarshal(data, &out); err != nil {
		return smiXMLOutput{}, fmt.Errorf("failed to parse nvidia-smi XML: %w", err)
	}
	return out, nil
}

// buildCommand returns nvidia-smi -q -x for the host driver or the containerized
// driver, as the status file says.
func (r *smiXMLReader) buildCommand(ctx context.Context) (*exec.Cmd, error) {
	isHostDriver, err := r.isHostDriver()
	if err != nil {
		return nil, err
	}
	if !isHostDriver {
		return nvidiaSMICommand(ctx, r.driverRoot, "-q", "-x")
	}
	binary, err := resolveHostNvidiaSMI(r.hostRoot)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, "chroot", r.hostRoot, binary, "-q", "-x"), nil //nolint:gosec // The host root is a fixed mount point and the binary is resolved under it.
}

// isHostDriver reports whether the status file says the driver is installed on
// the host.
func (r *smiXMLReader) isHostDriver() (bool, error) {
	data, err := os.ReadFile(r.statusFile)
	if errors.Is(err, fs.ErrNotExist) {
		return false, errDriverNotReady
	}
	if err != nil {
		return false, fmt.Errorf("failed to read driver status: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "IS_HOST_DRIVER="); ok {
			isHost, err := strconv.ParseBool(value)
			if err != nil {
				return false, fmt.Errorf("failed to parse IS_HOST_DRIVER in %s: %w", r.statusFile, err)
			}
			return isHost, nil
		}
	}
	return false, fmt.Errorf("IS_HOST_DRIVER not found in %s", r.statusFile)
}
