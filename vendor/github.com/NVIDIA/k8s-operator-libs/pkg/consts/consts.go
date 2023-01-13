/*
Copyright 2020 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package consts

/*
  This package contains constants used throughout the projects and does not fall into a particular package
*/

const (
	// Note: if a different logger is used than zap (operator-sdk default), these values would probably need to change.
	LogLevelError = iota - 2
	LogLevelWarning
	LogLevelInfo
	LogLevelDebug
)

const (
	NicClusterPolicyResourceName = "nic-cluster-policy"
)
