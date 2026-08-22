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

package images

import (
	"strings"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func FromCSV(csv *v1alpha1.ClusterServiceVersion) []string {
	var images []string

	for _, image := range csv.Spec.RelatedImages {
		images = append(images, image.Image)
	}

	deployment := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0]
	ctr := deployment.Spec.Template.Spec.Containers[0]
	images = append(images, ctr.Image)

	for _, env := range ctr.Env {
		if !strings.HasSuffix(env.Name, "_IMAGE") {
			continue
		}
		images = append(images, env.Value)
	}

	return images
}
