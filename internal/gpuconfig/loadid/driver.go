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

package loadid

import (
	"bufio"
	"bytes"
	"context"
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
)

// These names match the ones nvidia-validator and the driver container use.
const (
	// DriverReadyFile is written by every driver-validation step once the
	// driver is ready.
	DriverReadyFile = "driver-ready"
	// DriverCtrReadyFile is written by the operator's driver container when
	// its install is done, and removed by the driver pod's preStop hook.
	DriverCtrReadyFile = ".driver-ctr-ready"

	// DriverDaemonSetSelector selects the driver DaemonSets.
	DriverDaemonSetSelector = "app.kubernetes.io/component=nvidia-driver"
)

// DriverInfo is the content of DriverReadyFile.
type DriverInfo struct {
	IsHostDriver bool
	// DriverRoot is the driver's install location on the host.
	DriverRoot string
	// DriverRootCtrPath is where the driver root is mounted in the container:
	// /host for a host driver, /driver-root for a containerized one.
	DriverRootCtrPath string
	DevRoot           string
	DevRootCtrPath    string
}

// readDriverInfo reads DriverReadyFile in dir. ok is false when the file
// doesn't exist, which includes the moment between a validation step
// deleting it and writing it again.
func readDriverInfo(dir string) (info *DriverInfo, ok bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, DriverReadyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", DriverReadyFile, err)
	}
	info, err = parseDriverInfo(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", DriverReadyFile, err)
	}
	return info, true, nil
}

func parseDriverInfo(data []byte) (*DriverInfo, error) {
	info := &DriverInfo{}
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
	// DriverCtrReadyFile is needed, so its absence can't default to either.
	if !sawHostDriver {
		return nil, errors.New("missing IS_HOST_DRIVER")
	}
	return info, nil
}

// Ready reports whether the driver is ready and returns its DriverReadyFile
// content. A containerized driver the operator manages (see
// ManagedByOperator) also needs DriverCtrReadyFile.
func Ready(dir string, operatorManaged bool) (*DriverInfo, bool, error) {
	info, ok, err := readDriverInfo(dir)
	if err != nil || !ok {
		return nil, false, err
	}
	// A driver container the operator doesn't manage never writes
	// DriverCtrReadyFile, so waiting for it there would wait forever.
	if info.IsHostDriver || !operatorManaged {
		return info, true, nil
	}
	_, err = os.Stat(filepath.Join(dir, DriverCtrReadyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", DriverCtrReadyFile, err)
	}
	return info, true, nil
}

// DaemonSetLister lists DaemonSets in a namespace by label selector.
type DaemonSetLister interface {
	ListDaemonSets(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error)
}

// ManagedByOperator reports whether the operator manages the driver: a driver
// DaemonSet in namespace has a ClusterPolicy or NVIDIADriver as its
// controller. It makes the same check as nvidia-validator's driver step.
func ManagedByOperator(ctx context.Context, l DaemonSetLister, namespace string) (bool, error) {
	dss, err := l.ListDaemonSets(ctx, namespace, DriverDaemonSetSelector)
	if err != nil {
		return false, fmt.Errorf("listing driver DaemonSets: %w", err)
	}
	for i := range dss {
		owner := metav1.GetControllerOf(&dss[i])
		if owner == nil {
			continue
		}
		if strings.HasPrefix(owner.APIVersion, "nvidia.com/") &&
			(owner.Kind == nvidiav1.ClusterPolicyCRDName || owner.Kind == nvidiav1alpha1.NVIDIADriverCRDName) {
			return true, nil
		}
	}
	return false, nil
}
