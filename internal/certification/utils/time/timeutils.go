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

package time

import "time"

// GetElapsedTime returns the difference in time between the endTime and startTime, and returns a human-readable duration
// as the output.
func GetElapsedTime(startTime, endTime time.Time) string {
	d := endTime.Sub(startTime).Truncate(time.Second)
	return d.String()
}
