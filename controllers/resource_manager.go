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

package controllers

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"

	secv1 "github.com/openshift/api/security/v1"

	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	pspRemovalAPIVersion    = "v1.25.0-0"
	nodev1MinimumAPIVersion = "v1.20.0"
)

type assetsFromFile []byte

// Resources indicates resources managed by GPU operator
type Resources struct {
	ServiceAccount             corev1.ServiceAccount
	Role                       rbacv1.Role
	RoleBinding                rbacv1.RoleBinding
	ClusterRole                rbacv1.ClusterRole
	ClusterRoleBinding         rbacv1.ClusterRoleBinding
	ConfigMaps                 []corev1.ConfigMap
	DaemonSet                  appsv1.DaemonSet
	Deployment                 appsv1.Deployment
	Pod                        corev1.Pod
	Service                    corev1.Service
	ServiceMonitor             promv1.ServiceMonitor
	PriorityClass              schedv1.PriorityClass
	Taint                      corev1.Taint
	SecurityContextConstraints secv1.SecurityContextConstraints
	RuntimeClasses             []nodev1.RuntimeClass
	PrometheusRule             promv1.PrometheusRule
}

func filePathWalkDir(n *ClusterPolicyController, root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			n.logger.V(1).Info("error in filepath.Walk on %s: %v", root, err)
			return nil
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func getAssetsFrom(n *ClusterPolicyController, path string, openshiftVersion string) []assetsFromFile {
	manifests := []assetsFromFile{}
	files, err := filePathWalkDir(n, path)
	if err != nil {
		panic(err)
	}
	sort.Strings(files)
	for _, file := range files {
		if strings.Contains(file, "openshift") && openshiftVersion == "" {
			continue
		}

		buffer, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		manifests = append(manifests, buffer)
	}
	return manifests
}

func addResourcesControls(n *ClusterPolicyController, path string) (Resources, controlFunc) {
	res := Resources{}
	ctrl := controlFunc{}

	n.logger.Info("Getting assets from: ", "path:", path)
	manifests := getAssetsFrom(n, path, n.openshift)

	s := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme, json.SerializerOptions{Yaml: true, Pretty: false, Strict: false})
	reg := regexp.MustCompile(`\b(\w*kind:\w*)\B.*\b`)

	for _, m := range manifests {
		kind := reg.FindString(string(m))
		slce := strings.Split(kind, ":")
		kind = strings.TrimSpace(slce[1])

		n.logger.V(1).Info("Looking for ", "Kind", kind, "in path:", path)

		switch kind {
		case "ServiceAccount":
			_, _, err := s.Decode(m, nil, &res.ServiceAccount)
			panicIfError(err)
			ctrl = append(ctrl, ServiceAccount)
		case "Role":
			_, _, err := s.Decode(m, nil, &res.Role)
			panicIfError(err)
			ctrl = append(ctrl, Role)
		case "RoleBinding":
			_, _, err := s.Decode(m, nil, &res.RoleBinding)
			panicIfError(err)
			ctrl = append(ctrl, RoleBinding)
		case "ClusterRole":
			_, _, err := s.Decode(m, nil, &res.ClusterRole)
			panicIfError(err)
			ctrl = append(ctrl, ClusterRole)
		case "ClusterRoleBinding":
			_, _, err := s.Decode(m, nil, &res.ClusterRoleBinding)
			panicIfError(err)
			ctrl = append(ctrl, ClusterRoleBinding)
		case "ConfigMap":
			cm := corev1.ConfigMap{}
			_, _, err := s.Decode(m, nil, &cm)
			panicIfError(err)
			res.ConfigMaps = append(res.ConfigMaps, cm)
			// only add the ctrl function when the first ConfigMap is added for this component
			if len(res.ConfigMaps) == 1 {
				ctrl = append(ctrl, ConfigMaps)
			}
		case "DaemonSet":
			_, _, err := s.Decode(m, nil, &res.DaemonSet)
			panicIfError(err)
			ctrl = append(ctrl, DaemonSet)
		case "Deployment":
			_, _, err := s.Decode(m, nil, &res.Deployment)
			panicIfError(err)
			ctrl = append(ctrl, Deployment)
		case "Service":
			_, _, err := s.Decode(m, nil, &res.Service)
			panicIfError(err)
			ctrl = append(ctrl, Service)
		case "ServiceMonitor":
			_, _, err := s.Decode(m, nil, &res.ServiceMonitor)
			panicIfError(err)
			ctrl = append(ctrl, ServiceMonitor)
		case "SecurityContextConstraints":
			_, _, err := s.Decode(m, nil, &res.SecurityContextConstraints)
			panicIfError(err)
			ctrl = append(ctrl, SecurityContextConstraints)
		case "RuntimeClass":
			rt := nodev1.RuntimeClass{}
			_, _, err := s.Decode(m, nil, &rt)
			panicIfError(err)
			res.RuntimeClasses = append(res.RuntimeClasses, rt)
			// only add the ctrl function when the first RuntimeClass is added
			if len(res.RuntimeClasses) == 1 {
				ctrl = append(ctrl, RuntimeClasses)
			}
		case "PrometheusRule":
			_, _, err := s.Decode(m, nil, &res.PrometheusRule)
			panicIfError(err)
			ctrl = append(ctrl, PrometheusRule)
		default:
			n.logger.Info("Unknown Resource", "Manifest", m, "Kind", kind)
		}

	}

	return res, ctrl
}

func panicIfError(err error) {
	if err != nil {
		panic(err)
	}
}
