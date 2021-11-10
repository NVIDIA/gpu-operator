# NVIDIA GPU Operator for ppc64le by Rocket Software

The GPU operator manages NVIDIA GPU resources in a Openshift cluster and automates tasks related to bootstrapping GPU nodes. Since the GPU is a special resource in the cluster, it requires a few components to be installed before application workloads can be deployed onto the GPU. These components include the NVIDIA drivers (to enable CUDA), Openshift device plugin, container runtime and others such as automatic node labelling, monitoring etc.

Red Hat OpenShift Container Platform is a security-centric and enterprise-grade hardened Kubernetes platform for deploying and managing Kubernetes clusters at scale, developed and supported by Red Hat. Red Hat OpenShift Container Platform includes enhancements to Kubernetes so users can easily configure and use GPU resources for accelerating workloads such as deep learning.

The GPU operator manages NVIDIA GPU resources in a Openshift cluster and automates tasks related to bootstrapping GPU nodes. Since the GPU is a special resource in the cluster, it requires a few components to be installed before application workloads can be deployed onto the GPU. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin, container runtime and others such as automatic node labelling, monitoring etc. 

The NVIDIA GPU Operator uses the operator framework within Kubernetes to automate the management of all NVIDIA software components needed to provision GPU. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin for GPUs, the NVIDIA Container Toolkit, automatic node labelling using GFD, DCGM based monitoring and others.

## Project Status
This is a technical preview release of the GPU operator. The operator can be deployed using a Helm chart. 

## Prerequisites and Platform Support 
- ppc64le GPUs are only supported 
- A working OpenShift cluster up and running with a GPU worker node. See https://docs.openshift.com/container-platform/latest/installing/index.html for guidance on installing. Refer to https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/platform-support.html#container-platforms for the support matrix of the GPU Operator releases and the supported container platforms for more information.
- Access to the OpenShift cluster as a cluster-admin to perform the necessary steps.
- OpenShift CLI (oc) installed.
- RedHat Enterprise Linux (RHEL) 8.X 
- Ensure that the appropriate Red Hat subscriptions and entitlements for OpenShift are properly enabled.
- API Key to access images from icr.io. If you dont have one, request api key to access icr.io to pull images at following email address rocketce@rocketsoftware.com. For more details refer https://cloud.ibm.com/docs/openshift?topic=openshift-registry .

#### Quickstart
```sh
# If this is your first run of helm
$ helm init --client-only

# Install helm https://docs.helm.sh/using_helm/ then run:
$ helm repo add rocketgpu https://rocketsoftware.github.io/gpu-operator/
$ helm repo update

# Create the secret for API Key obtained from Rocket to access the images in icr.io:    
$ oc --namespace <project> create secret docker-registry icrlogin --docker-server=<registry_URL> --docker-username=iamapikey --docker-password=<api_key_value> --docker-email=<docker_email>

# Verify the secret creation, below command gives list of secrets in the namespace. The name "icrlogin" must be present there.
$ oc get secrets --namespace <project>


#Store the image pull secret in the Kubernetes service account for the selected project. Every OpenShift project has a Kubernetes service account that is named default. Within the project, you can add the image pull secret to this service account to grant access for pods to pull images from your registry. Deployments that do not specify a service account automatically use the default service account for this OpenShift project.

#Check if an image pull secret already exists for your default service account.
$ oc describe serviceaccount default -n <project_name>
#When <none> is displayed in the Image pull secrets entry, no image pull secret exists.

# Add the image pull secret to your default service account.
# Example command to add the image pull secret when no image pull secret is defined.
$ oc patch -n <project_name> serviceaccount/default -p '{"imagePullSecrets":[{"name": "icrlogin"}]}'
# Example command to add the image pull secret when an image pull secret is already defined.
$ oc patch -n <project_name> serviceaccount/default --type='json' -p='[{"op":"add","path":"/imagePullSecrets/-","value":{"name":"icrlogin"}}]'

# Verify that your image pull secret was added to your default service account.

$ oc describe serviceaccount default -n <project_name>


$ helm install --devel rocketgpu/gpu-operator -n test-operator --wait
$ oc apply -f https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/manifests/cr/sro_cr_sched_none.yaml

# Create a tensorflow notebook example
$ oc apply -f https://rocketsoftware.github.io/gpu-operator/notebook-example.yml

# Grab the token from the pod once it is created
$ oc get pod tf-notebook
$ oc logs tf-notebook
...
[I 23:20:42.891 NotebookApp] jupyter_tensorboard extension loaded.
[I 23:20:42.926 NotebookApp] JupyterLab alpha preview extension loaded from /opt/conda/lib/python3.6/site-packages/jupyterlab
JupyterLab v0.24.1
Known labextensions:
[I 23:20:42.933 NotebookApp] Serving notebooks from local directory: /home/jovyan
    
        Copy/paste this URL into your browser when you connect for the first time,
            to login with a token:
                    http://localhost:8888/?token=MY_TOKEN
```

You can now access the notebook on http://localhost:30001/?token=MY_TOKEN

#### Install Helm
```sh
curl -L https://git.io/get_helm.sh | bash
kubectl create serviceaccount -n kube-system tiller
kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller

# See: https://github.com/helm/helm/issues/6374
helm init --service-account tiller --override spec.selector.matchLabels.'name'='tiller',spec.selector.matchLabels.'app'='helm' --output yaml | sed 's@apiVersion: extensions/v1beta1@apiVersion: apps/v1@' | kubectl apply -f -
kubectl wait --for=condition=available -n kube-system deployment tiller-deploy
```

## Known Limitations
  - With Kubernetes v1.16, Helm may fail to initialize. See [this issue](https://github.com/helm/helm/issues/6374) for more details.
  - GPU Operator will fail on nodes already setup with NVIDIA Components (driver, runtime, device plugin)
  - Removing the GPU Operator will require you to reboot your nodes

## Contributions
  [Read the document on contributions](https://github.com/rocketsoftware/gpu-operator/blob/master/CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

## Getting Help
  Please open [an issue on the GitHub project](https://github.com/rocketsoftware/gpu-operator/issues/new) for any questions. Your feedback is appreciated.
