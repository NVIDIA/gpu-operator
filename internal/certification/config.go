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

package certification

import (
	"os"
	"strings"
	"time"
)

const DefaultTimeout = 15 * time.Minute

type Config struct {
	Namespace              string
	Timeout                string
	TestSets               []string
	TestWorkloadImage      string
	TargetDriverRepository string
	TargetDriverImage      string
	TargetDriverVersion    string
	RDMABaseImage          string
}

// LoadConfigFromEnv builds a Config from environment variables.
func LoadConfigFromEnv() Config {
	var testSets []string
	if raw := os.Getenv("TEST_SETS"); raw != "" {
		for s := range strings.SplitSeq(raw, ",") {
			if t := strings.TrimSpace(s); t != "" {
				testSets = append(testSets, t)
			}
		}
	}

	return Config{
		Namespace:              os.Getenv("NAMESPACE"),
		Timeout:                os.Getenv("TEST_TIMEOUT"),
		TestSets:               testSets,
		TestWorkloadImage:      os.Getenv("TEST_WORKLOAD_IMAGE"),
		TargetDriverRepository: os.Getenv("TARGET_DRIVER_REPOSITORY"),
		TargetDriverImage:      os.Getenv("TARGET_DRIVER_IMAGE"),
		TargetDriverVersion:    os.Getenv("TARGET_DRIVER_VERSION"),
		RDMABaseImage:          os.Getenv("RDMA_BASE_IMAGE"),
	}
}

func (c Config) ParseTimeout() time.Duration {
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return DefaultTimeout
	}
	return timeout
}
