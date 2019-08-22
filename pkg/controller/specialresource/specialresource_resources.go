package specialresource

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"

	secv1 "github.com/openshift/api/security/v1"

	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

type assetsFromFile []byte

var manifests []assetsFromFile

type Resources struct {
	ServiceAccount             corev1.ServiceAccount
	Role                       rbacv1.Role
	RoleBinding                rbacv1.RoleBinding
	ClusterRole                rbacv1.ClusterRole
	ClusterRoleBinding         rbacv1.ClusterRoleBinding
	ConfigMap                  corev1.ConfigMap
	DaemonSet                  appsv1.DaemonSet
	Pod                        corev1.Pod
	Service                    corev1.Service
	ServiceMonitor             promv1.ServiceMonitor
	PriorityClass              schedv1.PriorityClass
	Taint                      corev1.Taint
	SecurityContextConstraints secv1.SecurityContextConstraints
}

func filePathWalkDir(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Info("DEBUG: error in filepath.Walk on %s: %v", root, err)
			return nil
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func matchKubeFlavor(path string) bool {
	// e.g. KUBE_UNSUPPORTED_FLAVOR=openshift
	// then the manifest path will be skipped if it contains that name
	// TODO: make the value of env var be a list (e.g. "openshift,nutanix,pks" will ensure specific files get ignored)
	flavor := os.Getenv("KUBE_UNSUPPORTED_FLAVOR")
	if flavor == "" {
		return true
	}
	matched := strings.Contains(path, flavor)
	if matched {
		log.Info("OpenShift is not supported. Skipping", "PATH", path)
		return false
	}
	return true
}

func getAssetsFrom(path string) []assetsFromFile {

	manifests := []assetsFromFile{}
	files, err := filePathWalkDir(path)
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		if !matchKubeFlavor(file) {
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

func addResourcesControls(path string) (Resources, controlFunc) {

	res := Resources{}
	ctrl := controlFunc{}

	manifests := getAssetsFrom(path)

	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme)
	reg, _ := regexp.Compile(`\b(\w*kind:\w*)\B.*\b`)

	for _, m := range manifests {
		kind := reg.FindString(string(m))
		slce := strings.Split(kind, ":")
		kind = strings.TrimSpace(slce[1])

		log.Info("DEBUG: Looking for ", "Kind", kind)

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
			_, _, err := s.Decode(m, nil, &res.ConfigMap)
			panicIfError(err)
			ctrl = append(ctrl, ConfigMap)
		case "DaemonSet":
			_, _, err := s.Decode(m, nil, &res.DaemonSet)
			panicIfError(err)
			ctrl = append(ctrl, DaemonSet)
		case "Service":
			_, _, err := s.Decode(m, nil, &res.Service)
			panicIfError(err)
			ctrl = append(ctrl, Service)
		case "Pod":
			_, _, err := s.Decode(m, nil, &res.Pod)
			panicIfError(err)
			ctrl = append(ctrl, Pod)
		case "ServiceMonitor":
			_, _, err := s.Decode(m, nil, &res.ServiceMonitor)
			panicIfError(err)
			ctrl = append(ctrl, ServiceMonitor)
		case "SecurityContextConstraints":
			_, _, err := s.Decode(m, nil, &res.SecurityContextConstraints)
			panicIfError(err)
			ctrl = append(ctrl, SecurityContextConstraints)
		default:
			log.Info("Unknown Resource", "Manifest", m, "Kind", kind)
		}

	}

	return res, ctrl
}

func panicIfError(err error) {
	if err != nil {
		panic(err)
	}
}
