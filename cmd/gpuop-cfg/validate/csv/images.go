/**
# Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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

package csv

import (
	"context"
	"fmt"
	"strings"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/regclient/regclient"
	"github.com/regclient/regclient/types/ref"
)

var client = regclient.New()

func validateImages(csv *v1alpha1.ClusterServiceVersion) error {
	// validate all 'relatedImages'
	images := csv.Spec.RelatedImages
	for _, image := range images {
		err := validateImage(image.Image)
		if err != nil {
			return fmt.Errorf("failed to validate image %s: %v", image.Name, err)
		}
	}

	// get the gpu-operator deployment spec
	deployment := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0]
	ctr := deployment.Spec.Template.Spec.Containers[0]

	// validate the gpu-operator image
	err := validateImage(ctr.Image)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", ctr.Image, err)
	}

	// validate all operand images configured as env vars
	for _, env := range ctr.Env {
		if !strings.HasSuffix(env.Name, "_IMAGE") {
			continue
		}
		err = validateImage(env.Value)
		if err != nil {
			return fmt.Errorf("failed to validate image %s: %v", env.Name, err)
		}
	}

	return nil
}

func validateImage(path string) error {
	ref, err := ref.New(path)
	if err != nil {
		return fmt.Errorf("failed to construct an image reference: %v", err)
	}

	_, err = client.ManifestGet(context.TODO(), ref)
	if err != nil {
		return fmt.Errorf("failed to get image manifest: %v", err)
	}

	return nil
}
