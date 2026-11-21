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

package main

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	nvidiawebhook "github.com/NVIDIA/gpu-operator/internal/webhook"
)

func main() {
	ctrl.SetLogger(zap.New())
	logger := ctrl.Log.WithName("webhook")

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(nvidiav1alpha1.AddToScheme(scheme))

	apiClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "unable to create Kubernetes API reader")
		os.Exit(1)
	}

	server := webhook.NewServer(webhook.Options{
		Port:    9443,
		CertDir: "/etc/gpu-operator/webhook/certs",
	})
	server.Register(nvidiawebhook.NVIDIADriverPath, admission.WithValidator(scheme, &nvidiawebhook.NVIDIADriverValidator{
		Reader: apiClient,
	}))

	if err := server.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to serve admission requests")
		os.Exit(1)
	}
}
