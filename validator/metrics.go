/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	promcli "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// statusFileCheckDelaySeconds indicates the delay between two checks of the status files, in seconds
	statusFileCheckDelaySeconds = 30
	// driverValidationCheckDelaySeconds indicates the delay between two checks of the driver validation, in seconds
	driverValidationCheckDelaySeconds = 60
	// pluginValidationCheckDelaySeconds indicates the delay between two checks of the device plugin validation, in seconds
	pluginValidationCheckDelaySeconds = 30
	// nvidiaPciDevicesCheckDeplaySeconds indicates the deplay between two checks of the number of NVIDIA PCI devices in the local node, in seconds
	nvidiaPciDevicesCheckDeplaySeconds = 60
)

// NodeMetrics contains the port of the metrics server and the
// Prometheus metrics objects
type NodeMetrics struct {
	port int

	metricsReady promcli.Gauge
	driverReady  promcli.Gauge
	toolkitReady promcli.Gauge
	pluginReady  promcli.Gauge
	cudaReady    promcli.Gauge

	driverValidation            promcli.Gauge
	driverValidationLastSuccess promcli.Gauge

	deviceCount                 promcli.Gauge
	pluginValidationLastSuccess promcli.Gauge

	nvidiaPciDevices promcli.Gauge
}

// NewNodeMetrics creates a NodeMetrics with its Prometheus metrics objects initialized (and automatically registered by promauto)
func NewNodeMetrics(port int) NodeMetrics {
	return NodeMetrics{
		port: port,
		metricsReady: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_metrics_ready_ts_seconds",
				Help: "timestamp (in seconds) of the launch time of the GPU Operator node metrics Pod",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		driverReady: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_driver_ready",
				Help: "1 if the driver synchronization barrier of the the local node is open, 0 otherwise",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		toolkitReady: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_toolkit_ready",
				Help: "1 if the container-toolkit synchronization barrier on the local node is open, 0 otherwise",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		pluginReady: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_plugin_ready",
				Help: "1 if the device plugin synchronization barrier on the local is open, 0 otherwise",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		cudaReady: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_cuda_ready",
				Help: "1 if cuda synchronization barrier on the local node is open, 0 otherwise",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		driverValidation: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_driver_validation",
				Help: "1 if the driver validation test passed on the local node, 0 otherwise",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		driverValidationLastSuccess: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_driver_validation_last_success_ts_seconds",
				Help: "timestamp (in seconds) of the last successful driver test validation",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		deviceCount: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_device_plugin_devices_total",
				Help: "number of GPU devices exposed by the DevicePlugin on the local node. -1 if failing to inspect the node spec.",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		pluginValidationLastSuccess: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_node_device_plugin_validation_last_success_ts_seconds",
				Help: "timestamp (in seconds) of the last time GPU devices were found on the local node spec",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),

		nvidiaPciDevices: promauto.NewGaugeVec(
			promcli.GaugeOpts{
				Name: "gpu_operator_nvidia_pci_devices_total",
				Help: "number of NVIDIA devices found in the node. -1 if failing to count",
			},
			[]string{"node"},
		).WithLabelValues(nodeNameFlag),
	}
}

func (nm *NodeMetrics) watchStatusFile(statusFile *promcli.Gauge, statusFileFilename string) {
	log.Printf("metrics: StatusFile: watching %s", statusFileFilename)

	ready := false
	prevReady := false
	(*statusFile).Set(0)
	for {
		_, err := os.Stat(outputDirFlag + "/" + statusFileFilename)
		ready = !os.IsNotExist(err)
		if ready != prevReady {
			prevReady = ready

			if ready {
				log.Printf("metrics: StatusFile: '%s' is ready", statusFileFilename)

				(*statusFile).Set(1)
			} else {
				log.Printf("metrics: StatusFile: '%s' is not ready", statusFileFilename)

				(*statusFile).Set(0)
			}
		}

		time.Sleep(statusFileCheckDelaySeconds * time.Second)
	}
}

func (nm *NodeMetrics) watchDevicePluginValidation() error {
	p := &Plugin{}

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("metrics: DevicePlugin validation: Error getting config cluster - %s\n", err.Error())
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("metrics: DevicePlugin validation: Error getting k8s client - %s\n", err.Error())
		return err
	}

	// update k8s client for the plugin
	p.setKubeClient(kubeClient)
	log.Printf("metrics: DevicePlugin validation: node name is %s", nodeNameFlag)

	prevCount := int64(-2)
	for {
		// enumerate node resources and count K8s GPU devices
		count, err := p.countGPUResources()
		if err != nil {
			nm.deviceCount.Set(-1)
			if prevCount != count {
				log.Errorf("metrics: DevicePlugin validation: could not list the DevicePlugin devices: %v", err)
			}
		} else {
			nm.deviceCount.Set(float64(count))
			if count != 0 {
				nm.pluginValidationLastSuccess.Set(float64(time.Now().Unix()))
			}
			if prevCount != count {
				log.Printf("metrics: DevicePlugin validation: found %d GPUs exposed by the DevicePlugin", count)
			}
		}
		prevCount = count

		time.Sleep(pluginValidationCheckDelaySeconds * time.Second)
	}
}

func (nm *NodeMetrics) watchDriverValidation() {
	driver := &Driver{}

	for {
		err := driver.runValidation(true)
		if err == nil {
			nm.driverValidation.Set(1)
			nm.driverValidationLastSuccess.Set(float64(time.Now().Unix()))
		} else {
			nm.driverValidation.Set(0)
		}
		time.Sleep(driverValidationCheckDelaySeconds * time.Second)
	}
}

func runLsPCI() (string, error) {
	var out bytes.Buffer

	cmd := exec.Command("lspci")
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func countNvidiaDevices(lspciStdout string) int {
	count := 0
	for _, line := range strings.Split(lspciStdout, "\n") {
		if strings.Contains(strings.ToLower(line), "nvidia") {
			count++
		}
	}
	return count
}

func (nm *NodeMetrics) watchNVIDIAPCI() {
	prevDevCount := -2
	for {
		lspciStdout, err := runLsPCI()

		var devCount = -1
		if err != nil {
			if prevDevCount != devCount {
				log.Errorf("metrics: PCI devices: Error running 'lspci': %v", err)
			}
		} else {
			devCount = countNvidiaDevices(lspciStdout)
			if prevDevCount != devCount {
				suffix := ""
				if devCount > 1 {
					suffix = "s"
				}

				log.Printf("metrics: PCI devices: found %d NVIDIA device%s", devCount, suffix)
			}
		}
		prevDevCount = devCount
		nm.nvidiaPciDevices.Set(float64(devCount))
		time.Sleep(driverValidationCheckDelaySeconds * time.Second)
	}
}

// Run launches a Prometheus server and watches for metrics value udates
func (nm *NodeMetrics) Run() error {
	nm.metricsReady.Set(float64(time.Now().Unix()))

	go nm.watchStatusFile(&nm.driverReady, driverStatusFile)
	go nm.watchStatusFile(&nm.toolkitReady, toolkitStatusFile)
	go nm.watchStatusFile(&nm.pluginReady, pluginStatusFile)
	go nm.watchStatusFile(&nm.cudaReady, cudaStatusFile)

	go nm.watchDriverValidation()
	go nm.watchDevicePluginValidation()
	go nm.watchNVIDIAPCI()

	log.Printf("Running the metrics server, listening on :%d/metrics", nm.port)
	http.Handle("/metrics", promhttp.Handler())

	return http.ListenAndServe(fmt.Sprintf(":%d", nm.port), nil)
}
