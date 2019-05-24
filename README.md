# Special Resource Operator (SRO)

The special resource operator is an orchestrator for resources in a cluster specifically designed for resources that need extra management. This reference implementation shows how GPUs can be deployed on a Kubernetes/OpenShift cluster. 

There is a general problem when trying to configure a cluster with a special resource. One does not know which node has a special resource or not. To circumvent this bootstrap problem, the SRO relies on the [NFD operator](https://github.com/openshift/cluster-nfd-operator) and its node feature discovery capabilities. NFD will label the host with node specific attributes, like PCI cards, kernel or OS version and many more, see ([Upstream NFD](https://github.com/kubernetes-sigs/node-feature-discovery)) fore more info. 

Here is a sample excerpt of NFD labels that are applied to the node. 
```
Name:               ip-xx-x-xxx-xx
Roles:              worker
Labels:             beta.kubernetes.io/arch=amd64
                    beta.kubernetes.io/instance-type=g3.4xlarge
                    beta.kubernetes.io/os=linux
                                ... 
                    feature.node.kubernetes.io/cpuid-AVX=true
                    feature.node.kubernetes.io/cpuid-AVX2=true
                    feature.node.kubernetes.io/kernel-version.full=3.10.0-957.1.3.el7.x86_64
                                ...
                    feature.node.kubernetes.io/pci-0300_10de.present=true
                    feature.node.kubernetes.io/system-os_release.VERSION_ID=4.0
                    feature.node.kubernetes.io/system-os_release.VERSION_ID.major=4
                    feature.node.kubernetes.io/system-os_release.VERSION_ID.minor=0
```


## Operation Breakdown
The special resource operator implements a simple state machine, where each state has a validation step. The validation step for each state is different and relies on the functionality to be tested of the previous state. 

The following descriptions of the states will describe how e.g. the SRO handles GPUs in a cluster. 

### Namespace Separation
The operator will run in its own namespace (`openshift-sro-operator`) and the operands that the operator orchestrates in another (`openshift-sro`). The operator will watch the operands namespace for changes and act up on accordingly. 


### General State Breakdown
Assets like ServiceAccount, RBAC, DaemonSet, ConfigMap yaml files for each state are saved in the container under `/opt/sro/state-{driver,device-plugin,monitoring}`. The SRO will take each of these assets and assign a control function to each of them. Those control functions have hooks for preprocessing the yaml files or hooks to preprocess the decoded API runtime objects. Those hooks are used to add runtime information from the cluster like kernel-version, and nodeSelectors based on the discovered hardware etc. 

After the assests were decoded preprocessed and transformed into API runtime objects, the control funcs take care of CRUD operations on those. 

The SRO is easily extended just by creating another directory under `/opt/sro/state-new` and adding this new state to the operator [addState(...)](https://github.com/zvonkok/special-resource-operator/blob/012020bb04922737d1f9eb5e703d3b931a053bd4/pkg/controller/specialresource/specialresource_state.go#L79). The SRO operator will scan this new directory and automatically assign the corresponding control functions. 

#### State Driver
This state will deploy a DaemonSet with a driver container. The driver container holds all userspace and kernelspace parts to make the special resource (GPU) work. It will configure the host and tell cri-o where to look for the GPU hook ([upstream nvidia-driver-container](https://gitlab.com/nvidia/driver/tree/centos7)). 

The DaemonSet will use the PCI label from NFD to schedule the DaemonSet only on nodes that have a special resource (0x0300 is a display class and 0x10DE is the vendor id for NVIDIA). 
```
      nodeSelector:
        feature.node.kubernetes.io/pci-0300_10de.present: "true"
```

To schedule the correct version of the compiled kernel modules, the operator will fetch the kernel-version label from the special resource nodes and preprocess the driver container DaemonSet in such a way that the `nodeSelector` and the pulled image have the kernel-version in their name: 
```
      nodeSelector:
        feature.node.kubernetes.io/pci-0300_10de.present: "true"
        feature.node.kubernetes.io/kernel-version.full: "KERNEL_FULL_VERSION"
```
```
      - image: quay.io/zvonkok/nvidia-driver:v410.79-KERNEL_FULL_VERSION
```

This way one can be sure that only the correct driver version is scheduled on the node with that specific kernel-version. 


#### State Driver Validation
To check if the driver and the hook are correctly deployed, the operator will schedule a simple GPU workload and check if the Pod status is `Success`, which means the application returned succesfully without an error. The GPU workload will exit with an error, it the driver or the userspace part are not working correctly. This Pod will not allocate an extended resource, only checking if the GPU is working. 

#### State Device Plugin
As the name already suggests, this state will deploy a special resource DevicePlugin with all its dependencies, see [state-device-plugin](https://github.com/zvonkok/special-resource-operator/tree/master/assets/state-device-plugin) for a complete list. 

#### State Device Plugin Validation
One will use the same GPU workload as before for validation but this time the Pod will request a extended resource (1) to check if the DevicePlugin has correctly advertised the GPUs to the cluster and (2) to check if userspace and kernelspace are working correclty. 

#### State Monitoring
This state uses a custom metrics exporter DaemonSet to export metrics for Prometheus. A ServiceMonitor adds this exporter as a new scrape target. 


## Hard and Soft Partitioning
The operator has example CR's how to create a hard or soft partitioning scheme for the worker nodes where one has special resources. Hard partitioning is realized with taints and tolerations where soft partitioning is priority and preemption. 

### Hard Partitioning
If one wants to repel Pods from nodes that have special resources without the corresponding toleration, the following CR can be used to instantiate the operator with taints for the nodes: [sro_cr_sched_taints_tolerations.yaml](https://github.com/zvonkok/special-resource-operator/blob/5973d6fea1985c425f5c36733fbc8e693e2c3821/manifests/sro_cr_sched_taints_tolerations.yaml#L1). The CR accepts an array of taints. 

The `nvidia.com/gpu` is an extended resource, which is exposed by the DevicePlugin, there is no need to add a  toleration to Pods that request extended resources. The ExtendedResourcesAdmissionController will add a toleration to each Pod that tries to allocate an extended resource on a node with the corresponding taint.

A Pod that does not request a extended resource e.g. a CPU only Pod will be repelled from the node. The taint will make sure that only special resource workloads are deployed to those specific nodes.

### Soft Partitioning
Compared to the hard partioning scheme with taints, soft partitioning will allow any Pod on the node but will preempt low priority Pods with high priority Pods. High priority Pods could be with special resource workloads and low priority CPU only Pods. The following CR can be used to instantiate the operator with priority and preemption: [sro_cr_sched_priority_preemption.yaml](https://github.com/zvonkok/special-resource-operator/blob/052d7ad0cd4255ab9b0595f17d4914b61927d18f/manifests/sro_cr_sched_priority_preemption.yaml#L1). The CR accepts an array of priorityclasses, here the operator creates two classes, namely: `gpu-high-priority` and `gpu-low-priority`. 

One can use the low priority class to keep the node busy and as soon as a high priority class Pod is created that allocates a extended resource, the scheduler will try to preempt the low priority Pod to make schedulig of the pending Pod possible. 







