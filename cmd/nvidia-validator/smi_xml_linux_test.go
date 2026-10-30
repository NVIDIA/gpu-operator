//go:build linux

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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubChroot replaces chroot, which needs privileges, with a script that runs the
// binary under the root.
func stubChroot(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	writeTestFile(t, bin, "chroot", "#!/bin/sh\nroot=$1\nbinary=$2\nshift 2\nexec \"$root$binary\" \"$@\"\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func writeStatusFile(t *testing.T, isHostDriver bool) string {
	t.Helper()
	return writeTestFile(t, t.TempDir(), "driver-ready", fmt.Sprintf("IS_HOST_DRIVER=%t\n", isHostDriver))
}

// Each case has a working query on the other driver, which must not be run.
func TestSMIXMLReaderDoesNotFallBack(t *testing.T) {
	const working = "#!/bin/sh\nprintf '<nvidia_smi_log/>'\n"
	for name, tc := range map[string]struct {
		isHostDriver bool
		hostSMI      string
		driverSMI    string
		wantError    string
	}{
		"host query fails":        {isHostDriver: true, hostSMI: "not a valid executable", driverSMI: working, wantError: "exit status"},
		"no host nvidia-smi":      {isHostDriver: true, driverSMI: working, wantError: "failed to find an executable"},
		"container query fails":   {hostSMI: working, driverSMI: "#!/bin/sh\nexit 1\n", wantError: "exit status"},
		"container not installed": {hostSMI: working, wantError: "failed to locate driver"},
	} {
		t.Run(name, func(t *testing.T) {
			stubChroot(t)
			host, root := t.TempDir(), t.TempDir()
			if tc.hostSMI != "" {
				writeTestFile(t, host, "usr/bin/nvidia-smi", tc.hostSMI)
			}
			if tc.driverSMI != "" {
				writeTestFile(t, root, "usr/bin/nvidia-smi", tc.driverSMI)
				writeTestFile(t, root, "usr/lib64/libnvidia-ml.so.1", "")
			}
			reader := &smiXMLReader{statusFile: writeStatusFile(t, tc.isHostDriver), hostRoot: host, driverRoot: root}
			out, err := reader.Read(t.Context())
			require.ErrorContains(t, err, tc.wantError)
			require.Zero(t, out)
		})
	}
}

// The data is stdout only. A failure reports stderr, and a deadline or a
// cancellation is reported as such, not as the signal that stopped the command.
func TestSMIXMLReaderCommandOutcome(t *testing.T) {
	for name, tc := range map[string]struct {
		script    string
		timeout   time.Duration
		cancelled bool
		wantUUID  string
		wantError string
		wantCause error
	}{
		// A warning on stderr in the middle of the UUID would corrupt it if stderr were
		// part of the data.
		"stderr stays out of the data": {script: "printf '<nvidia_smi_log><gpu><uuid>GPU-'; echo 'a warning' >&2; printf '1</uuid></gpu></nvidia_smi_log>'", wantUUID: "GPU-1"},
		"failure reports stderr":       {script: "echo 'NVML mismatch' >&2; exit 1", wantError: "NVML mismatch"},
		"deadline":                     {script: "exec sleep 30", timeout: 100 * time.Millisecond, wantCause: context.DeadlineExceeded},
		"cancelled":                    {script: "exec sleep 30", cancelled: true, wantCause: context.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			root := t.TempDir()
			writeTestFile(t, root, "usr/bin/nvidia-smi", "#!/bin/sh\n"+tc.script+"\n")
			writeTestFile(t, root, "usr/lib64/libnvidia-ml.so.1", "")
			reader := &smiXMLReader{statusFile: writeStatusFile(t, false), hostRoot: t.TempDir(), driverRoot: root}
			out, err := reader.Read(ctx)
			switch {
			case tc.wantCause != nil:
				require.ErrorIs(t, err, tc.wantCause)
				require.Zero(t, out)
			case tc.wantError != "":
				require.ErrorContains(t, err, tc.wantError)
				require.Zero(t, out)
			default:
				require.NoError(t, err)
				require.Len(t, out.GPUs, 1)
				require.Equal(t, tc.wantUUID, out.GPUs[0].UUID)
			}
		})
	}
}

// Read picks the driver from the status file and returns the parsed output.
func TestSMIXMLReaderRead(t *testing.T) {
	fixturePath := func(fixture string) string {
		path, err := filepath.Abs(filepath.Join("testdata", "smi", fixture))
		require.NoError(t, err)
		return path
	}
	cat := func(fixture string) string { return "#!/bin/sh\ncat " + fixturePath(fixture) + "\n" }
	for name, tc := range map[string]struct {
		status       string // the status file's content, or empty for no file
		driverSMI    string
		wantGPUs     int
		wantError    string
		wantNotReady bool
	}{
		"host driver":          {status: "IS_HOST_DRIVER=true\n", driverSMI: cat("licensed-multiple.xml"), wantGPUs: 1},
		"containerized driver": {status: "IS_HOST_DRIVER=false\n", driverSMI: cat("licensed-multiple.xml"), wantGPUs: 2},
		"driver not ready":     {driverSMI: cat("licensed-multiple.xml"), wantNotReady: true},
		"output is not XML":    {status: "IS_HOST_DRIVER=false\n", driverSMI: "#!/bin/sh\necho not xml\n", wantError: "parse nvidia-smi XML"},
		// A warning on stdout, such as one about the infoROM, does not fail the read.
		"text around the XML": {status: "IS_HOST_DRIVER=false\n", driverSMI: "#!/bin/sh\necho 'WARNING: infoROM is corrupted'\ncat " + fixturePath("licensed-multiple.xml") + "\necho 'trailing text'\n", wantGPUs: 2},
	} {
		t.Run(name, func(t *testing.T) {
			stubChroot(t)
			dir, host, root := t.TempDir(), t.TempDir(), t.TempDir()
			// The host's nvidia-smi reports one GPU and the driver container's two, so the
			// number of GPUs shows which one ran.
			writeTestFile(t, host, "usr/bin/nvidia-smi", cat("licensed-single.xml"))
			writeTestFile(t, root, "usr/bin/nvidia-smi", tc.driverSMI)
			writeTestFile(t, root, "usr/lib64/libnvidia-ml.so.1", "")
			reader := &smiXMLReader{statusFile: filepath.Join(dir, "driver-ready"), hostRoot: host, driverRoot: root}
			if tc.status != "" {
				writeTestFile(t, dir, "driver-ready", tc.status)
			}
			out, err := reader.Read(t.Context())
			switch {
			case tc.wantNotReady:
				require.ErrorIs(t, err, errDriverNotReady)
			case tc.wantError != "":
				require.ErrorContains(t, err, tc.wantError)
			default:
				require.NoError(t, err)
				require.Len(t, out.GPUs, tc.wantGPUs)
			}
		})
	}
}
