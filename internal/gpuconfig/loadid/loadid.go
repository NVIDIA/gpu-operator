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

// Package loadid identifies the current load of the NVIDIA kernel module, so
// that a decision made for one driver load is never reused after a reload,
// and reports whether the driver is ready for NVML to be opened.
package loadid

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// ModulePath is the sysfs directory of the nvidia kernel module.
const ModulePath = "/sys/module/nvidia"

// Read returns the load ID of the current driver load: the inode number of
// path, normally ModulePath. The kernel gives the directory a new inode at
// every module load, so the load ID changes on every reload and never repeats
// within a boot. ok is false while the module isn't loaded.
func Read(path string) (l uint64, ok bool, err error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("stat %s: %w", path, err)
	}
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, false, fmt.Errorf("stat %s: no inode number", path)
	}
	return st.Ino, true, nil
}
