package controllers

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"

	secv1 "github.com/openshift/api/security/v1"

	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

type assetsFromFile []byte

var manifests []assetsFromFile

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
	PodSecurityPolicy          policyv1beta1.PodSecurityPolicy
	RuntimeClass               nodev1.RuntimeClass
	PrometheusRule             promv1.PrometheusRule
}

func filePathWalkDir(n *ClusterPolicyController, root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			n.rec.Log.Info("DEBUG: error in filepath.Walk on %s: %v", root, err)
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

		buffer, err := ioutil.ReadFile(file)
		if err != nil {
			panic(err)
		}
		manifests = append(manifests, buffer)
	}
	return manifests
}

func addResourcesControls(n *ClusterPolicyController, path string, openshiftVersion string) (Resources, controlFunc) {
	res := Resources{}
	ctrl := controlFunc{}

	n.rec.Log.Info("Getting assets from: ", "path:", path)
	manifests := getAssetsFrom(n, path, openshiftVersion)

	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme)
	reg, _ := regexp.Compile(`\b(\w*kind:\w*)\B.*\b`)

	for _, m := range manifests {
		kind := reg.FindString(string(m))
		slce := strings.Split(kind, ":")
		kind = strings.TrimSpace(slce[1])

		n.rec.Log.Info("DEBUG: Looking for ", "Kind", kind, "in path:", path)

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
			_, _, err := s.Decode(m, nil, &res.RuntimeClass)
			panicIfError(err)
			ctrl = append(ctrl, RuntimeClass)
		case "PodSecurityPolicy":
			_, _, err := s.Decode(m, nil, &res.PodSecurityPolicy)
			panicIfError(err)
			ctrl = append(ctrl, PodSecurityPolicy)
		case "PrometheusRule":
			_, _, err := s.Decode(m, nil, &res.PrometheusRule)
			panicIfError(err)
			ctrl = append(ctrl, PrometheusRule)
		default:
			n.rec.Log.Info("Unknown Resource", "Manifest", m, "Kind", kind)
		}

	}

	return res, ctrl
}

func panicIfError(err error) {
	if err != nil {
		panic(err)
	}
}
