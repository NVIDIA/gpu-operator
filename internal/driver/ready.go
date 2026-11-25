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

package driver

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// These names match the ones nvidia-validator and the driver container use.
const (
	// ReadyFile is written by every driver-validation step once the driver is
	// ready.
	ReadyFile = "driver-ready"
	// CtrReadyFile is written by the operator's driver container when its
	// install is done, and removed by the driver pod's preStop hook.
	CtrReadyFile = ".driver-ctr-ready"

	// DaemonSetSelector selects the driver DaemonSets.
	DaemonSetSelector = "app.kubernetes.io/component=nvidia-driver"
)

// Info is the content of ReadyFile.
type Info struct {
	IsHostDriver bool
	// DriverRoot is the driver's install location on the host.
	DriverRoot string
	// DriverRootCtrPath is where the driver root is mounted in the container:
	// /host for a host driver, /driver-root for a containerized one.
	DriverRootCtrPath string
	DevRoot           string
	DevRootCtrPath    string
}

// WriteInfo writes info to ReadyFile in dir. The write is atomic, so a reader
// never sees a partial file.
func WriteInfo(dir string, info *Info) error {
	return utils.WriteFileAtomically(filepath.Join(dir, ReadyFile), info.marshal())
}

func (i *Info) marshal() string {
	return strings.Join([]string{
		fmt.Sprintf("IS_HOST_DRIVER=%t", i.IsHostDriver),
		fmt.Sprintf("NVIDIA_DRIVER_ROOT=%s", i.DriverRoot),
		fmt.Sprintf("DRIVER_ROOT_CTR_PATH=%s", i.DriverRootCtrPath),
		fmt.Sprintf("NVIDIA_DEV_ROOT=%s", i.DevRoot),
		fmt.Sprintf("DEV_ROOT_CTR_PATH=%s", i.DevRootCtrPath),
	}, "\n") + "\n"
}

// readInfo reads ReadyFile in dir. ok is false when the file doesn't exist,
// which includes the moment between a validation step deleting it and writing
// it again.
func readInfo(dir string) (info *Info, ok bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, ReadyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", ReadyFile, err)
	}
	info, err = parseInfo(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", ReadyFile, err)
	}
	return info, true, nil
}

func parseInfo(data []byte) (*Info, error) {
	info := &Info{}
	sawHostDriver := false
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		key, value, found := strings.Cut(s.Text(), "=")
		if !found {
			continue
		}
		switch key {
		case "IS_HOST_DRIVER":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("IS_HOST_DRIVER: %w", err)
			}
			info.IsHostDriver = b
			sawHostDriver = true
		case "NVIDIA_DRIVER_ROOT":
			info.DriverRoot = value
		case "DRIVER_ROOT_CTR_PATH":
			info.DriverRootCtrPath = value
		case "NVIDIA_DEV_ROOT":
			info.DevRoot = value
		case "DEV_ROOT_CTR_PATH":
			info.DevRootCtrPath = value
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scanning: %w", err)
	}
	// Telling a host driver from a containerized one decides whether
	// CtrReadyFile is needed, so its absence can't default to either.
	if !sawHostDriver {
		return nil, errors.New("missing IS_HOST_DRIVER")
	}
	return info, nil
}

// Ready reports whether the driver is ready and returns its ReadyFile content.
// A containerized driver the operator manages (see ManagedByOperator) also
// needs CtrReadyFile.
func Ready(dir string, operatorManaged bool) (*Info, bool, error) {
	info, ok, err := readInfo(dir)
	if err != nil || !ok {
		return nil, false, err
	}
	// A driver container the operator doesn't manage never writes
	// CtrReadyFile, so waiting for it there would wait forever.
	if info.IsHostDriver || !operatorManaged {
		return info, true, nil
	}
	_, err = os.Stat(filepath.Join(dir, CtrReadyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", CtrReadyFile, err)
	}
	return info, true, nil
}

// ManagedByOperator reports whether the operator manages the driver: one of
// dss, the driver DaemonSets in the operator namespace (see DaemonSetSelector),
// has a ClusterPolicy or NVIDIADriver as its controller. It makes the same
// check as nvidia-validator's driver step.
func ManagedByOperator(dss []appsv1.DaemonSet) bool {
	for _, ds := range dss {
		owner := metav1.GetControllerOf(&ds)
		if owner == nil {
			continue
		}
		if strings.HasPrefix(owner.APIVersion, "nvidia.com/") &&
			(owner.Kind == nvidiav1.ClusterPolicyCRDName || owner.Kind == nvidiav1alpha1.NVIDIADriverCRDName) {
			return true
		}
	}
	return false
}
