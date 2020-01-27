# NVIDIA GPU Operator

The GPU operator manages NVIDIA GPU resources in a Kubernetes cluster and automates tasks related to bootstrapping GPU nodes. Since the GPU is a special resource in the cluster, it requires a few components to be installed before application workloads can be deployed onto the GPU. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin, container runtime and others such as automatic node labelling, monitoring etc.

## Project Status
This is a technical preview release of the GPU operator. The operator can be deployed using a Helm chart. 


## Platform Support
- Pascal+ GPUs are supported (incl. Tesla V100 and T4)
- Kubernetes v1.13+
- Helm 2
- Ubuntu 18.04.3 LTS
- The GPU operator has been validated with the following NVIDIA components:
  - Docker CE 19.03.2
  - NVIDIA Container Toolkit 1.0.5
  - NVIDIA Kubernetes Device Plugin 1.0.0-beta4
  - NVIDIA Tesla Driver 418.87.01


## Prerequisites
- Nodes must not be pre-configured with NVIDIA components (driver, container runtime, device plugin).
- i2c_core and ipmi_msghandler kernel modules need to be loaded (Use the following command to ensure these modules are loaded with the following command)
  - $ sudo modprobe -a i2c_core ipmi_msghandler
  - Note that this step is not persistent across reboots. To make this persistent across reboots, add the modules to the configuration file as shown:
    - $ echo -e "i2c_core\nipmi_msghandler" | sudo tee /etc/modules-load.d/driver.conf
- Node Feature Discovery (NFD) is required on each node. By default, NFD master and worker are automatically deployed . If NFD is already running in the cluster prior to the deployment of the operator, follow this step:
  - Set the variable nfd.enabled=false at the helm install step:
    - $ helm install --devel --set nfd.enabled=false nvidia/gpu-operator -n test-operator
  - See notes on [NFD setup](https://github.com/kubernetes-sigs/node-feature-discovery)
- For monitoring in Kubernetes <= 1.13 and > 1.15, enable the kubelet "KubeletPodResources" feature gate. From Kubernetes 1.15 onwards, its enabled by default.
  - $ echo -e "KUBELET_EXTRA_ARGS=--feature-gates=KubeletPodResources=true" | sudo tee /etc/default/kubelet

## Installation

#### Install Helm
```sh
$ curl -L https://git.io/get_helm.sh | bash

# Create service-account for helm
$ kubectl create serviceaccount -n kube-system tiller
$ kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller

# Initialize Helm
$ helm init --service-account tiller --wait

# Note that if you have helm already deployed in your cluster and you are adding a new node, run this instead
$ helm init --client-only

# Additional step required for Kubernetes v1.16. See: https://github.com/helm/helm/issues/6374
$ helm init --service-account tiller --override spec.selector.matchLabels.'name'='tiller',spec.selector.matchLabels.'app'='helm' --output yaml | sed 's@apiVersion: extensions/v1beta1@apiVersion: apps/v1@' | kubectl apply -f -
$ kubectl wait --for=condition=available -n kube-system deployment tiller-deploy
```

#### Install GPU Operator
```sh
# Before running this, make sure helm is installed and initialized:
$ helm repo add nvidia https://nvidia.github.io/gpu-operator
$ helm repo update

# Note that after running this command, NFD will be automatically deployed. If you have NFD already setup, follow the NFD instruction from the Prerequisites.
$ helm install --devel nvidia/gpu-operator -n test-operator --wait
$ kubectl apply -f https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/manifests/cr/sro_cr_sched_none.yaml

# To check the gpu-operator version
$ helm ls
```

#### Uninstall GPU Operator
```sh
$ helm del --purge test-operator
$ sudo reboot

# Check if the operator got uninstalled properly
$ kubectl get pods -n gpu-operator-resources
No resources found.
```

#### Running a Sample GPU Application
```sh
# Create a tensorflow notebook example
$ kubectl apply -f https://nvidia.github.io/gpu-operator/notebook-example.yml

# Grab the token from the pod once it is created
$ kubectl get pod tf-notebook
$ kubectl logs tf-notebook
...
[I 23:20:42.891 NotebookApp] jupyter_tensorboard extension loaded.
[I 23:20:42.926 NotebookApp] JupyterLab alpha preview extension loaded from /opt/conda/lib/python3.6/site-packages/jupyterlab
JupyterLab v0.24.1
Known labextensions:
[I 23:20:42.933 NotebookApp] Serving notebooks from local directory: /home/jovyan

   Copy/paste this URL into your browser when you connect for the first time,
       to login with a token:
          http://localhost:8888/?token=MY_TOKEN
You can now access the notebook on http://localhost:30001/?token=MY_TOKEN
```

#### GPU Monitoring
```sh
# Check if the dcgm-exporter is successufully deployed
$ kubectl get pods -n gpu-operator-resources | grep dcgm

# Check gpu metrics locally
$ dcgm_pod_ip=$(kubectl get pods -n gpu-operator-resources -lapp=nvidia-dcgm-exporter -ojsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' -o wide | tail -n 1 | awk '{print $6}')
$ curl $dcgm_pod_ip:9400/gpu/metrics

# To scrape gpu metrics from Prometheus, add dcgm endpoint to Prometheus via a configmap

$ tee dcgmScrapeConfig.yaml <<EOF
- job_name: gpu-metrics
  scrape_interval: 1s
  metrics_path: /gpu/metrics
  scheme: http

  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - gpu-operator-resources

  relabel_configs:
  - source_labels: [__meta_kubernetes_pod_node_name]
    action: replace 
    target_label: kubernetes_node 
EOF

# Deploy Prometheus
$ helm install --name prom-monitoring --set-file extraScrapeConfigs=./dcgmScrapeConfig.yaml stable/prometheus

# Alternatively, if you find your prometheus pod pending and get this error "no persistent volumes available...", disable persistentVolumes. [Refer this](https://stackoverflow.com/questions/47235014/why-prometheus-pod-pending-after-setup-it-by-helm-in-kubernetes-cluster-on-ranch).
$ helm install --name prom-monitoring --set-file extraScrapeConfigs=./dcgmScrapeConfig.yaml --set alertmanager.persistentVolume.enabled=false --set server.persistentVolume.enabled=false stable/prometheus

# To check the metrics in browser
$ kubectl port-forward $(kubectl get pods -lapp=prometheus -lcomponent=server -ojsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}') 9090 &
# Open in browser http://localhost:9090

# Deploy Grafana
$ helm install --name grafana-gpu-dashboard stable/grafana

# Decode the admin user and password to login in the dashboard
$ kubectl get secret grafana-test -o jsonpath="{.data.admin-user}" | base64 --decode ; echo
$ kubectl get secret grafana-test -o jsonpath="{.data.admin-password}" | base64 --decode ; echo

# To open dashboard in browser
$ kubectl port-forward $(kubectl get pods --namespace default -l "app=grafana,release=grafana-test" -o jsonpath="{.items[0].metadata.name}") 3000 &
# In browser: http://localhost:3000
# On AWS: ssh -L 3000:localhost:3000 -i YOUR_SECRET_KEY INSTANCE_NAME@PUBLIC_IP

# Login in the dashboard with the decoded credentials and add Promethues datasource 
# Get Promethues IP to add to the Grafana datasource
$ prom_server_ip=$(kubectl get pods -lapp=prometheus -lcomponent=server -ojsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' -o wide | tail -n 1 | awk '{print $6}')
# Check if Prometheus is reachable
$ curl $prom_server_ip:9090

# Import this GPU metrics dashboard from Grafana https://grafana.com/grafana/dashboards/11578
```

### Known Limitations
- With Kubernetes v1.16, Helm may fail to initialize. See [this issue](https://github.com/helm/helm/issues/6374) for more details. A workaround has already been included in the Helm installation steps above in this document.
- GPU Operator will fail on nodes already setup with NVIDIA components (driver, runtime, device plugin).
- Removing the GPU Operator will require you to reboot your nodes.


### Contributions
[Read the document on contributions](https://github.com/NVIDIA/gpu-operator/blob/master/CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

### Getting Help
Please open [an issue on the GitHub project](https://github.com/NVIDIA/gpu-operator/issues/new) for any questions. Your feedback is appreciated.

