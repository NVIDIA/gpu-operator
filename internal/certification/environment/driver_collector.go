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

package environment

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type driverCollector struct{}

func (c *driverCollector) Name() string { return "drivers" }

func (c *driverCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	drivers, err := clients.ListNVIDIADrivers(ctx)
	if err != nil {
		if s.Policy.Config.UseNvidiaDriverCRD {
			s.Warnings = append(s.Warnings, "failed to list NVIDIADriver CRs: "+err.Error())
		}
		return nil
	}
	if len(drivers) == 0 {
		return nil
	}

	driverInfos := make([]DriverInfo, 0, len(drivers))
	for i := range drivers {
		driverInfos = append(driverInfos, extractDriverInfo(&drivers[i]))
	}

	s.Drivers = driverInfos
	return nil
}

func extractDriverInfo(d *nvidiav1alpha1.NVIDIADriver) DriverInfo {
	info := DriverInfo{
		Name:          d.Name,
		State:         string(d.Status.State),
		DriverVersion: d.Spec.Version,
		Repository:    d.Spec.Repository,
		Image:         d.Spec.Image,
	}

	if d.Spec.UsePrecompiled != nil && *d.Spec.UsePrecompiled {
		info.UsePrecompiled = true
	}
	if d.Spec.KernelModuleType == "open" {
		info.UseOpenKernelModules = true
	}
	info.NodeSelector = d.Spec.NodeSelector

	info.Conditions = convertDriverConditions(d.Status.Conditions)

	if specBytes, err := json.Marshal(d.Spec); err == nil {
		info.RawSpec = specBytes
	}

	return info
}

func convertDriverConditions(conditions []metav1.Condition) []PolicyCondition {
	if len(conditions) == 0 {
		return nil
	}
	result := make([]PolicyCondition, len(conditions))
	for i, cond := range conditions {
		result[i] = PolicyCondition{
			Type:    cond.Type,
			Status:  string(cond.Status),
			Reason:  cond.Reason,
			Message: cond.Message,
		}
	}
	return result
}
