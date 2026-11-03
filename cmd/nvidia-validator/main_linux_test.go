//go:build linux

/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateHostDriverReportsHostNvidiaSMIFailure(t *testing.T) {
	errNotExitStatus := errors.New("fake runner failure")

	testCases := map[string]struct {
		// runErr returns the error the fake runner reports for the host nvidia-smi.
		runErr func(t *testing.T) error
		// exitCode is the exit status expected to survive wrapping, or -1 if
		// the runner error is not an *exec.ExitError.
		exitCode int
	}{
		"nvidia-smi exits with status 9": {
			runErr:   exitStatus(9),
			exitCode: 9,
		},
		"nvidia-smi exits with status 18": {
			runErr:   exitStatus(18),
			exitCode: 18,
		},
		"runner fails without an exit status": {
			runErr:   func(*testing.T) error { return errNotExitStatus },
			exitCode: -1,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			hostRoot := t.TempDir()
			target := filepath.Join(hostRoot, "usr/bin/nvidia-smi")
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
			require.NoError(t, os.WriteFile(target, []byte("fake nvidia-smi"), 0755)) //nolint:gosec // resolveHostNvidiaSMI only accepts an executable nvidia-smi

			runErr := tc.runErr(t)
			var gotCommand string
			var gotArgs []string
			run := func(command string, args []string, silent bool) error {
				gotCommand, gotArgs = command, args
				return runErr
			}

			err := validateHostDriverInRoot(hostRoot, run, true)
			require.Error(t, err)

			require.Equal(t, "chroot", gotCommand)
			require.Equal(t, []string{hostRoot, "/usr/bin/nvidia-smi"}, gotArgs)

			require.Contains(t, err.Error(), "'/usr/bin/nvidia-smi'")
			require.Contains(t, err.Error(), runErr.Error())
			require.ErrorIs(t, err, runErr)

			var exitErr *exec.ExitError
			if tc.exitCode < 0 {
				require.False(t, errors.As(err, &exitErr))
				return
			}
			require.ErrorAs(t, err, &exitErr)
			require.Equal(t, tc.exitCode, exitErr.ExitCode())
		})
	}
}

// exitStatus returns a real *exec.ExitError with the given exit status, the
// same type runCommand returns when the host nvidia-smi exits non-zero.
func exitStatus(code int) func(t *testing.T) error {
	return func(t *testing.T) error {
		t.Helper()
		err := exec.Command("/bin/sh", "-c", "exit \"$0\"", strconv.Itoa(code)).Run() //nolint:gosec // fixed script; code is passed as an argument, not interpolated
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		require.Equal(t, code, exitErr.ExitCode())
		return err
	}
}
