/*
 * Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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

package config

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

// Config defines the configuration for the kata-manager
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type Config struct {
	// ArtifactsDir is the directory where kata artifacts (e.g. kernel / guest images, configuration, etc.)
	// are placed on the local filesystem.
	// +kubebuilder:default=/opt/nvidia-gpu-operator/artifacts/runtimeclasses
	ArtifactsDir string `json:"artifactsDir,omitempty"    yaml:"artifactsDir,omitempty"`

	// RuntimeClasses is a list of kata runtime classes to configure.
	// +optional
	RuntimeClasses []RuntimeClass `json:"runtimeClasses,omitempty"  yaml:"runtimeClasses,omitempty"`
}

// RuntimeClass defines the configuration for a kata RuntimeClass
// +kubebuilder:object:generate=true
type RuntimeClass struct {
	// Name is the name of the kata runtime class.
	Name string `json:"name"                   yaml:"name"`

	// NodeSelector specifies the nodeSelector for the RuntimeClass object.
	// This ensures pods running with the RuntimeClass only get scheduled
	// onto nodes which support it.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`

	// Artifacts are the kata artifacts associated with the runtime class.
	Artifacts Artifacts `json:"artifacts"              yaml:"artifacts"`
}

// Artifacts defines the path to an OCI artifact (payload) containing all artifacts
// associated with a kata RuntimeClass (e.g. kernel, guest image, initrd, kata configuration)
// +kubebuilder:object:generate=true
type Artifacts struct {
	// URL is the path to the OCI artifact (payload) containing all artifacts
	// associated with a kata runtime class.
	URL string `json:"url"                  yaml:"url"`

	// PullSecret is the secret used to pull the OCI artifact.
	// +optional
	PullSecret string `json:"pullSecret,omitempty" yaml:"pullSecret,omitempty"`
}

// NewDefaultConfig returns a new default config.
func NewDefaultConfig() *Config {
	return &Config{
		ArtifactsDir: DefaultKataArtifactsDir,
	}
}

// GetObjectKind
func (c *Config) GetObjectKind() schema.ObjectKind { return nil }

// SanitizeConfig sanitizes the config struct and removes any invalid runtime class entries
func SanitizeConfig(c *Config) {
	i := 0
	for idx, rc := range c.RuntimeClasses {
		if rc.Name == "" {
			klog.Warningf("empty RuntimeClass name, skipping entry at index %d", idx)
			continue
		}
		if rc.Artifacts.URL == "" {
			klog.Warningf("empty artifacts url for runtime class %s, skipping entry at index %d", rc.Name, idx)
			continue
		}
		c.RuntimeClasses[i] = rc
		i++
	}

	c.RuntimeClasses = c.RuntimeClasses[:i]
}
