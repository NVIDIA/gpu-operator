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

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type sharingConfigCollector struct{}

func (c *sharingConfigCollector) Name() string { return "sharing-config" }

func (c *sharingConfigCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	// Only collect if config names are set
	if s.Policy.Config.TimeSlicingConfigName == "" && s.Policy.Config.MIGConfigName == "" {
		return nil
	}

	sharingCfg := &SharingConfig{}

	// Collect time-slicing config
	if s.Policy.Config.TimeSlicingConfigName != "" {
		tsCfg, err := c.collectTimeSlicingConfig(ctx, clients, namespace, s.Policy.Config.TimeSlicingConfigName)
		if err == nil {
			sharingCfg.TimeSlicing = tsCfg
		}
	}

	// Collect MIG config
	if s.Policy.Config.MIGConfigName != "" {
		migCfg, err := c.collectMIGConfig(ctx, clients, namespace, s.Policy.Config.MIGConfigName)
		if err == nil {
			sharingCfg.MIG = migCfg
		}
	}

	if sharingCfg.TimeSlicing != nil || sharingCfg.MIG != nil {
		s.SharingConfig = sharingCfg
	}

	return nil
}

func (c *sharingConfigCollector) collectTimeSlicingConfig(ctx context.Context, clients *k8s.Clients, namespace, name string) (*TimeSlicingConfig, error) {
	cm, err := clients.GetConfigMap(ctx, namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	tsCfg := &TimeSlicingConfig{
		ConfigMapName: name,
		Profiles:      make(map[string]TimeSlicingProfile),
	}

	// Parse the config data
	for key, data := range cm.Data {
		var profile timeSlicingConfigData
		if err := yaml.Unmarshal([]byte(data), &profile); err != nil {
			continue
		}

		if profile.Sharing != nil && profile.Sharing.TimeSlicing != nil {
			tsCfg.Profiles[key] = TimeSlicingProfile{
				Replicas:        profile.Sharing.TimeSlicing.Replicas,
				RenameResources: profile.Sharing.TimeSlicing.RenameByDefault,
			}
		}
	}

	return tsCfg, nil
}

func (c *sharingConfigCollector) collectMIGConfig(ctx context.Context, clients *k8s.Clients, namespace, name string) (*MIGConfig, error) {
	cm, err := clients.GetConfigMap(ctx, namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	migCfg := &MIGConfig{
		ConfigMapName: name,
		Profiles:      make(map[string][]MIGDevice),
	}

	// Parse the config data
	for key, data := range cm.Data {
		var config migConfigData
		if err := yaml.Unmarshal([]byte(data), &config); err != nil {
			continue
		}

		var devices []MIGDevice
		for _, device := range config.MIGDevices {
			devices = append(devices, MIGDevice{
				Profile: device.Profile,
				Count:   device.Count,
			})
		}
		if len(devices) > 0 {
			migCfg.Profiles[key] = devices
		}
	}

	return migCfg, nil
}

// Internal types for parsing ConfigMap data

type timeSlicingConfigData struct {
	Sharing *struct {
		TimeSlicing *struct {
			Replicas        int  `yaml:"replicas"`
			RenameByDefault bool `yaml:"renameByDefault"`
		} `yaml:"timeSlicing"`
	} `yaml:"sharing"`
}

type migConfigData struct {
	MIGDevices []struct {
		Profile string `yaml:"profile"`
		Count   int    `yaml:"count"`
	} `yaml:"mig-devices"`
}
