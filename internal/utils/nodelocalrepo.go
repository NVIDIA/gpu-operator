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

package utils

import (
	"fmt"
	"path/filepath"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// NodeLocalRepoVolumePrefix is the volume-name prefix used for node-local package
// repository volumes mounted into the NVIDIA driver container.
const NodeLocalRepoVolumePrefix = "node-local-repo"

// ValidateNodeLocalRepoPaths validates and normalizes a list of node-local package repository
// host paths. It returns the paths deduplicated and sorted, so that callers construct volumes
// deterministically. An error is returned if any path is not a clean, absolute, non-root
// directory path.
//
// Paths are never rewritten on the user's behalf: a silently normalized path would no longer
// match the file:// URI in the repository configuration, which is the exact failure this
// feature exists to remove.
func ValidateNodeLocalRepoPaths(paths []string) ([]string, error) {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("node-local repository path %q is not an absolute path", p)
		}
		if filepath.Clean(p) != p {
			return nil, fmt.Errorf("node-local repository path %q is not a clean path (did you mean %q?)", p, filepath.Clean(p))
		}
		if p == "/" {
			return nil, fmt.Errorf("node-local repository path %q is not allowed: the host root filesystem cannot be used as a package repository path", p)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// NodeLocalRepoVolumes builds the read-only host path volumes and corresponding volume mounts
// which expose node-local package repositories to the NVIDIA driver container. Each host path
// is mounted at the same path inside the container so that file:// URIs in the repository
// configuration resolve identically on the host and in the container.
//
// The mounts are read-only to prevent the driver install scripts from mutating the node's
// package repository. This is a guardrail and not a security boundary: the driver container is
// privileged and can remount it writable.
func NodeLocalRepoVolumes(paths []string) ([]corev1.Volume, []corev1.VolumeMount, error) {
	validated, err := ValidateNodeLocalRepoPaths(paths)
	if err != nil {
		return nil, nil, err
	}

	volumes := make([]corev1.Volume, 0, len(validated))
	mounts := make([]corev1.VolumeMount, 0, len(validated))
	for i, p := range validated {
		name := fmt.Sprintf("%s-%d", NodeLocalRepoVolumePrefix, i)
		volumes = append(volumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: p,
					Type: ptr.To(corev1.HostPathDirectory),
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      name,
			MountPath: p,
			ReadOnly:  true,
		})
	}
	return volumes, mounts, nil
}
