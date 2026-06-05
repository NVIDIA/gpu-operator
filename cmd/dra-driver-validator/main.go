/*
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
*/

// Command dra-driver-validator is the init container for the DRA driver
// kubelet-plugin DaemonSet. It validates that an NVIDIA driver is installed and
// writes /run/nvidia/validations/driver-ready with the env contract that the
// kubelet-plugin containers source on startup (NVIDIA_DRIVER_ROOT, DRIVER_ROOT_CTR_PATH).
//
// Unlike nvidia-validator, it validates with `nvidia-smi --version` only (never
// full nvidia-smi), so validation is safe when GPUs are bound to vfio-pci for
// passthrough: full nvidia-smi would initialize the GPUs and fail or hang.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	pathrs "github.com/cyphar/filepath-securejoin/pathrs-lite"
	log "github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v3"

	"github.com/NVIDIA/gpu-operator/internal/info"
)

const (
	// defaultStatusPath is the directory where validation status files are written.
	defaultStatusPath = "/run/nvidia/validations"
	// driverStatusFile is the status file signalling driver readiness; its content
	// is the env contract sourced by the kubelet-plugin containers.
	driverStatusFile = "driver-ready"
	// hostRootCtrPath is where the host root filesystem is mounted in the container.
	hostRootCtrPath = "/host"
	// defaultDriverInstallDirCtrPath is where a containerized driver installation is
	// mounted in the container. The DRA stack mounts it at the same path it occupies
	// on the host, and the kubelet-plugin containers mount the same path and source
	// DRIVER_ROOT_CTR_PATH from driver-ready.
	defaultDriverInstallDirCtrPath = "/run/nvidia/driver"
	// hostNvidiaSMIPath is the expected location of nvidia-smi within the host root.
	hostNvidiaSMIPath = "/usr/bin/nvidia-smi"
	// defaultSleepIntervalSeconds is the retry interval between validation attempts.
	defaultSleepIntervalSeconds = 10
)

var (
	outputDirFlag               string
	hostRootFlag                string
	driverInstallDirFlag        string
	driverInstallDirCtrPathFlag string
	sleepIntervalSecondsFlag    int
	debugFlag                   bool
)

func main() {
	c := cli.Command{}
	c.Name = "dra-driver-validator"
	c.Usage = "Validate the NVIDIA driver for the DRA driver kubelet plugin"
	c.Version = info.GetVersionString()

	c.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:        "debug",
			Aliases:     []string{"d"},
			Usage:       "Enable debug-level logging",
			Destination: &debugFlag,
			Sources:     cli.EnvVars("DEBUG"),
		},
		&cli.StringFlag{
			Name:        "output-dir",
			Usage:       "Directory where the driver-ready status file is written",
			Value:       defaultStatusPath,
			Destination: &outputDirFlag,
			Sources:     cli.EnvVars("OUTPUT_DIR"),
		},
		&cli.StringFlag{
			Name:        "host-root",
			Usage:       "Root path of the host filesystem (mounted at /host in the container)",
			Value:       "/",
			Destination: &hostRootFlag,
			Sources:     cli.EnvVars("HOST_ROOT"),
		},
		&cli.StringFlag{
			Name:        "driver-install-dir",
			Usage:       "Path on the host where a containerized driver installation is made available",
			Value:       "/run/nvidia/driver",
			Destination: &driverInstallDirFlag,
			Sources:     cli.EnvVars("DRIVER_INSTALL_DIR"),
		},
		&cli.StringFlag{
			Name:        "driver-install-dir-ctr-path",
			Usage:       "Path where the containerized driver installation is mounted in this container",
			Value:       defaultDriverInstallDirCtrPath,
			Destination: &driverInstallDirCtrPathFlag,
			Sources:     cli.EnvVars("DRIVER_INSTALL_DIR_CTR_PATH"),
		},
		&cli.IntFlag{
			Name:        "sleep-interval-seconds",
			Usage:       "Seconds to wait between validation attempts",
			Value:       defaultSleepIntervalSeconds,
			Destination: &sleepIntervalSecondsFlag,
			Sources:     cli.EnvVars("SLEEP_INTERVAL_SECONDS"),
		},
	}

	c.Before = func(ctx context.Context, cli *cli.Command) (context.Context, error) {
		if debugFlag {
			log.SetLevel(log.DebugLevel)
		}
		return ctx, nil
	}
	c.Action = run

	log.Infof("version: %s", c.Version)

	// DS pods may be terminated (SIGTERM) and re-created when the operator's driver
	// container creates a mount under /run/nvidia. Exit cleanly so Kubernetes retries.
	go handleSignal()

	if err := c.Run(context.Background(), os.Args); err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, _ *cli.Command) error {
	if err := os.MkdirAll(outputDirFlag, 0755); err != nil {
		return fmt.Errorf("failed to create status directory %q: %w", outputDirFlag, err)
	}

	for {
		info, err := runValidation()
		if err != nil {
			log.Warningf("driver not ready, retrying in %ds: %v", sleepIntervalSecondsFlag, err)
			time.Sleep(time.Duration(sleepIntervalSecondsFlag) * time.Second)
			continue
		}
		if err := writeDriverReady(info); err != nil {
			return fmt.Errorf("failed to write %s: %w", driverStatusFile, err)
		}
		log.Infof("driver is ready; wrote %s/%s", outputDirFlag, driverStatusFile)
		return nil
	}
}

func handleSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt,
		syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	s := <-stop
	log.Fatalf("Exiting due to signal [%v] notification for pid [%d]", s.String(), os.Getpid())
}

// runValidation mirrors nvidia-validator's host-then-container probe, but validates
// with `nvidia-smi --version` only. It returns the env-contract driverInfo on success.
func runValidation() (driverInfo, error) {
	hostErr := validateHostDriver()
	if hostErr == nil {
		log.Info("Detected a pre-installed driver on the host")
		return getDriverInfo(true, hostRootFlag, hostRootFlag, hostRootCtrPath), nil
	}
	log.Infof("No pre-installed driver detected on the host: %v", hostErr)

	log.Info("Validating containerized driver installation")
	if err := validateContainerDriver(); err != nil {
		return driverInfo{}, err
	}
	return getDriverInfo(false, hostRootFlag, driverInstallDirFlag, driverInstallDirCtrPathFlag), nil
}

// validateHostDriver checks for a driver installed directly on the host root by
// running `nvidia-smi --version` inside the mounted host filesystem.
func validateHostDriver() error {
	fileInfo, err := resolveHostNvidiaSMI(hostRootCtrPath)
	if err != nil {
		return err
	}
	if fileInfo.Size() == 0 {
		return fmt.Errorf("empty 'nvidia-smi' file found on the host")
	}
	// chroot into the host root so nvidia-smi resolves its libraries from the host.
	return runStreaming(nvidiaSMIVersionCommand("chroot", hostRootCtrPath, "nvidia-smi"))
}

// validateContainerDriver checks a containerized driver installation mounted at
// driverInstallDirCtrPath by running `nvidia-smi --version` against it.
func validateContainerDriver() error {
	driverRoot := root(driverInstallDirCtrPathFlag)

	driverLibraryPath, err := driverRoot.getDriverLibraryPath()
	if err != nil {
		return fmt.Errorf("failed to locate driver libraries: %w", err)
	}
	nvidiaSMIPath, err := driverRoot.getNvidiaSMIPath()
	if err != nil {
		return fmt.Errorf("failed to locate nvidia-smi: %w", err)
	}

	cmd := nvidiaSMIVersionCommand(nvidiaSMIPath)
	// nvidia-smi needs libnvidia-ml.so.1 on its load path.
	cmd.Env = setEnvVar(os.Environ(), "LD_PRELOAD", prependPathListEnvvar("LD_PRELOAD", driverLibraryPath))
	return runStreaming(cmd)
}

// resolveHostNvidiaSMI opens and stats nvidia-smi within the mounted host root.
func resolveHostNvidiaSMI(hostRoot string) (os.FileInfo, error) {
	f, err := pathrs.OpenInRoot(hostRoot, hostNvidiaSMIPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open 'nvidia-smi' on the host: %w", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat 'nvidia-smi' on the host: %w", err)
	}
	return fileInfo, nil
}

// nvidiaSMIVersionCommand builds the nvidia-smi invocation. It always appends
// "--version": full nvidia-smi would initialize GPUs and fail/hang when GPUs are
// bound to vfio-pci for passthrough.
func nvidiaSMIVersionCommand(nvidiaSMIPath string, prefixArgs ...string) *exec.Cmd {
	args := append(slices.Clone(prefixArgs), "--version")
	return exec.Command(nvidiaSMIPath, args...)
}

// runStreaming runs cmd with its output forwarded to the validator's own streams,
// so nvidia-smi output (and errors) land in the init container's logs.
func runStreaming(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeDriverReady writes the driver-ready env contract sourced by the
// kubelet-plugin containers. Only NVIDIA_DRIVER_ROOT and DRIVER_ROOT_CTR_PATH are
// emitted: those are the only values the DRA kubelet plugins read (see
// cmd/gpu-kubelet-plugin/main.go in k8s-dra-driver-gpu). The plugin derives its
// dev root from DRIVER_ROOT_CTR_PATH itself.
func writeDriverReady(info driverInfo) error {
	content := fmt.Sprintf("NVIDIA_DRIVER_ROOT=%s\nDRIVER_ROOT_CTR_PATH=%s\n",
		info.driverRoot, info.driverRootCtrPath)

	return writeStatusFile(filepath.Join(outputDirFlag, driverStatusFile), content)
}

// writeStatusFile atomically writes content to statusFile via a temp file + rename.
func writeStatusFile(statusFile, content string) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(statusFile), filepath.Base(statusFile)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary status file: %w", err)
	}
	// Best-effort cleanup; on success the rename moves the temp file away first.
	defer func() { _ = os.Remove(tmpFile.Name()) }() //nolint:gosec

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temporary status file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary status file: %w", err)
	}
	if err := os.Rename(tmpFile.Name(), statusFile); err != nil {
		return fmt.Errorf("error moving temporary file to %q: %w", statusFile, err)
	}
	return nil
}

func prependPathListEnvvar(envvar string, prepend ...string) string {
	if len(prepend) == 0 {
		return os.Getenv(envvar)
	}
	current := filepath.SplitList(os.Getenv(envvar))
	return strings.Join(append(prepend, current...), string(filepath.ListSeparator))
}

// setEnvVar adds or updates an envar to the list of specified envvars and returns it.
func setEnvVar(envvars []string, key, value string) []string {
	var updated []string
	for _, envvar := range envvars {
		pair := strings.SplitN(envvar, "=", 2)
		if pair[0] == key {
			continue
		}
		updated = append(updated, envvar)
	}
	return append(updated, fmt.Sprintf("%s=%s", key, value))
}
