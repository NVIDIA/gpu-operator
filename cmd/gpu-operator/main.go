/*
Copyright 2021.

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

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	apiconfigv1 "github.com/openshift/api/config/v1"
	apiimagev1 "github.com/openshift/api/image/v1"
	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	_ "go.uber.org/automaxprocs"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	clusterpolicyv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/info"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterpolicyv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nvidiav1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	utilruntime.Must(secv1.Install(scheme))
	utilruntime.Must(apiconfigv1.Install(scheme))
	utilruntime.Must(apiimagev1.Install(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var renewDeadline time.Duration

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&renewDeadline, "leader-lease-renew-deadline", 0,
		"Set the leader lease renew deadline duration (e.g. \"10s\") of the controller manager. "+
			"Only enabled when the --leader-elect flag is set. "+
			"If undefined, the renew deadline defaults to the controller-runtime manager's default RenewDeadline. "+
			"By setting this option, the LeaseDuration is also set as RenewDealine + 5s.")

	opts := zap.Options{
		StacktraceLevel: zapcore.PanicLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctrl.Log.Info(fmt.Sprintf("version: %s", info.GetVersionString()))

	metricsOptions := metricsserver.Options{
		BindAddress: metricsAddr,
	}

	webhookServer := webhook.NewServer(webhook.Options{
		Port: 9443,
	})

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	openshiftNamespace := consts.OpenshiftNamespace
	cacheOptions := cache.Options{
		DefaultNamespaces: map[string]cache.Config{
			operatorNamespace: {},
			// Also cache resources in the openshift namespace to retrieve ImageStreams when on an openshift  cluster
			openshiftNamespace: {},
		},
	}

	options := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "53822513.nvidia.com",
		WebhookServer:          webhookServer,
		Cache:                  cacheOptions,
	}

	if enableLeaderElection && int(renewDeadline) != 0 {
		leaseDuration := renewDeadline + 5*time.Second

		options.RenewDeadline = &renewDeadline
		options.LeaseDuration = &leaseDuration
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	if err = (&controllers.ClusterPolicyReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ClusterPolicy"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterPolicy")
		os.Exit(1)
	}

	// setup upgrade controller
	upgrade.SetDriverName("gpu")
	upgradeLogger := ctrl.Log.WithName("controllers").WithName("Upgrade")
	clusterUpgradeStateManager, err := upgrade.NewClusterUpgradeStateManager(
		upgradeLogger,
		mgr.GetConfig(),
		mgr.GetEventRecorderFor("nvidia-gpu-operator"),
	)
	if err != nil {
		setupLog.Error(err, "unable to create new ClusterUpdateStateManager", "controller", "Upgrade")
		os.Exit(1)
	}
	clusterUpgradeStateManager = clusterUpgradeStateManager.WithPodDeletionEnabled(gpuPodSpecFilter).WithValidationEnabled("app=nvidia-operator-validator")

	if err = (&controllers.UpgradeReconciler{
		Client:       mgr.GetClient(),
		Log:          upgradeLogger,
		Scheme:       mgr.GetScheme(),
		StateManager: clusterUpgradeStateManager,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Upgrade")
		os.Exit(1)
	}

	clusterInfo, err := clusterinfo.New(
		ctx,
		clusterinfo.WithKubernetesConfig(mgr.GetConfig()),
		clusterinfo.WithOneShot(false),
	)
	if err != nil {
		setupLog.Error(err, "failed to get cluster wide information needed by controllers")
		os.Exit(1)
	}

	if err = (&controllers.NVIDIADriverReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ClusterInfo: clusterInfo,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NVIDIADriver")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder
	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func gpuPodSpecFilter(pod corev1.Pod) bool {
	gpuInResourceList := func(rl corev1.ResourceList) bool {
		for resourceName := range rl {
			str := string(resourceName)
			if strings.HasPrefix(str, "nvidia.com/gpu") || strings.HasPrefix(str, "nvidia.com/mig-") {
				return true
			}
		}
		return false
	}

	//  ignore pods other than in running and pending state
	if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
		return false
	}

	for _, c := range pod.Spec.Containers {
		if gpuInResourceList(c.Resources.Limits) || gpuInResourceList(c.Resources.Requests) {
			return true
		}
	}
	return false
}
