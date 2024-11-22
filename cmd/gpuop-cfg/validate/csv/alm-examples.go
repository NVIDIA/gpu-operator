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
	"fmt"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/util/json"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func validateALMExample(csv *v1alpha1.ClusterServiceVersion) error {
	cpList := []v1.ClusterPolicy{}
	example := csv.Annotations["alm-examples"]
	err := json.Unmarshal([]byte(example), &cpList)
	if err != nil {
		return err
	}
	if len(cpList) == 0 {
		return fmt.Errorf("no example clusterpolicy found")
	}

	if cpList[0].Kind != "ClusterPolicy" {
		return fmt.Errorf("invalid example clusterpolicy")
	}

	return nil
}
