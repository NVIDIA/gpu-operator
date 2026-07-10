# node-feature-discovery

![Version: 0.2.1](https://img.shields.io/badge/Version-0.2.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.19.0](https://img.shields.io/badge/AppVersion-v0.19.0-informational?style=flat-square)
Node Feature Discovery (NFD) is a Kubernetes add-on for detecting hardware
features and system configuration. Detected features are advertised as node
labels. NFD provides flexible configuration and extension points for a wide
range of vendor and application specific node labeling needs.

**Homepage:** <https://github.com/kubernetes-sigs/node-feature-discovery>

## Source Code

* <https://github.com/kubernetes-sigs/node-feature-discovery>

## Installing the chart

### OCI Helm repository

The NFD project provides Helm charts in an OCI compliant repository. Install
NFD with the default configuration

```bash
helm install nfd --namespace node-feature-discovery --create-namespace oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0
```

See the [configuration](#configuration) section below for instructions how to
alter the deployment parameters.

### Legacy Helm repository

For the time being, the NFD project still provides Helm charts in a legacy
(HTTP) Helm repository, too.

> **NOTE:** This repository will be deprecated in the future. It is recommended
> for users to switch to use the OCI Helm repository.

#### Stable version

To install the latest stable version:

```bash
helm repo add nfd https://kubernetes-sigs.github.io/node-feature-discovery/charts
helm repo update
helm install nfd nfd/node-feature-discovery --namespace node-feature-discovery --create-namespace
```

See the [configuration](#configuration) section below for instructions how to
alter the deployment parameters.

### Latest development version

To install the latest development version you need to clone the NFD Git
repository and install from there.

```bash
git clone https://github.com/kubernetes-sigs/node-feature-discovery/
cd node-feature-discovery/deployment/helm
helm install nfd ./node-feature-discovery/ --namespace node-feature-discovery --create-namespace
```

## Configuration

You can override values from `values.yaml` and provide a file with custom values:

```bash
helm install -f <path/to/custom/values.yaml> --namespace nfd --create-namespace nfd oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0
```

To specify each parameter separately you can provide them to helm install command:

```bash
helm install --set nameOverride=NFDinstance --set master.replicaCount=2 --namespace nfd --create-namespace nfd oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0
```

## Upgrading the chart

To upgrade the `node-feature-discovery` deployment to v0.19.0 via Helm.

### Rolling-update pace on large clusters

The nfd-worker DaemonSet rolls out updates with `maxUnavailable: "10%"` by
default, so an upgrade completes in roughly ten waves regardless of cluster
size. nfd-worker is stateless and node labels persist while a worker pod
restarts, so tuning the pace only affects how quickly feature updates resume
on each node. Adjust it via `worker.updateStrategy.rollingUpdate.maxUnavailable`
(e.g. `1` restores the Kubernetes default of one node at a time; to use
`type: OnDelete`, also set `rollingUpdate: null` as Helm deep-merges maps).

When driving upgrades through `helm upgrade --wait` or GitOps health checks
(Flux `HelmRelease`, Argo CD), size the timeout to cover the full rollout:
roughly `ceil(1 / maxUnavailable) × per-wave pod-ready time`. With the serial
Kubernetes default the rollout time grows linearly with node count and easily
exceeds a 5-minute timeout on large clusters.

### From v0.7 and older

Please see
the [uninstallation guide](https://kubernetes-sigs.github.io/node-feature-discovery/v0.7/get-started/deployment-and-usage.html#uninstallation).
And then follow the standard [installation instructions](#installing-the-chart).

### From v0.8 - v0.11

Helm deployment of NFD was introduced in v0.8.0.

```bash
export NFD_NS=node-feature-discovery
export HELM_INSTALL_NAME=nfd
# Uninstall the old NFD deployment
helm uninstall $HELM_INSTALL_NAME --namespace $NFD_NS
# Install the new NFD deployment
helm install $HELM_INSTALL_NAME oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0 --namespace $NFD_NS --set master.enable=false
# Wait for NFD Worker to be ready
kubectl wait --timeout=-1s --for=condition=ready pod -l app.kubernetes.io/name=node-feature-discovery --namespace $NFD_NS
# Enable the NFD Master
helm upgrade $HELM_INSTALL_NAME oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0 --namespace $NFD_NS --set master.enable=true
```

### From v0.12 - v0.13

In v0.12 the `NodeFeature` CRD was introduced as experimental.
The API was not enabled by default.

```bash
export NFD_NS=node-feature-discovery
export HELM_INSTALL_NAME=nfd
# Install and upgrade CRD's
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.19.0/deployment/base/nfd-crds/nfd-api-crds.yaml
# Install the new NFD deployment
helm upgrade $HELM_INSTALL_NAME oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0 --namespace $NFD_NS --set master.enable=false
# Wait for NFD Worker to be ready
kubectl wait --timeout=-1s --for=condition=ready pod -l app.kubernetes.io/name=node-feature-discovery --namespace $NFD_NS
# Enable the NFD Master
helm upgrade $HELM_INSTALL_NAME oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0 --namespace $NFD_NS --set master.enable=true
```

### From v0.14+

As of version v0.14 the Helm chart is the primary deployment method for NFD,
and the CRD `NodeFeature` is enabled by default.

```bash
export NFD_NS=node-feature-discovery
export HELM_INSTALL_NAME=nfd
# Install and upgrade CRD's
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.19.0/deployment/base/nfd-crds/nfd-api-crds.yaml
# Install the new NFD deployment
helm upgrade $HELM_INSTALL_NAME oci://registry.k8s.io/nfd/charts/node-feature-discovery --version 0.19.0 --namespace $NFD_NS
```

## Uninstalling the chart

To uninstall the `node-feature-discovery` deployment:

```bash
helm uninstall nfd --namespace node-feature-discovery
```

The command removes all the Kubernetes components associated with the chart and
deletes the release. It also runs a post-delete hook that cleans up the nodes
of all labels, annotations, taints and extended resources that were created by
NFD.

## Values

### General

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.repository | string | `"registry.k8s.io/nfd/node-feature-discovery"` | NFD image repository |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.tag | string | `nil` | NFD image tag. If not specified Chart.AppVersion will be used. |
| imagePullSecrets | list | `[]` | Image pull secrets. [More info](https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod). |
| nameOverride | string | `""` | Override the name of the chart |
| fullnameOverride | string | `""` | Override a default fully qualified app name |
| namespaceOverride | string | `""` | Override the namespace to install the chart into. By default, the namespace is determined by Helm. |
| featureGates | object | `{}` | [Feature gates](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/feature-gates) to enable/disable specific features. |
| checksumValuesOnly | bool | `false` | Only checksum config values (not the full rendered ConfigMap template) for the rollout annotation. When enabled, pods will only restart when config values actually change, not on unrelated template changes. |
| priorityClassName | string | `""` | The name of the PriorityClass to be used for the NFD pods. |
| postDeleteCleanup | bool | `true` | Enable/disable the Helm post-delete hook. |

### Global

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| global.imagePullSecrets | list | `[]` | If `imagePullSecrets` is specified, it takes precedence over `global.imagePullSecrets`. [More info](https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod). |

### NFD-Master

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| master.enable | bool | `true` | Specifies whether nfd-master should be deployed |
| master.extraArgs | list | `[]` | Additional [command line arguments](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/master-commandline-reference) to pass to nfd-master. |
| master.extraEnvs | list | `[]` | Additional environment variables to set in the nfd-master container. |
| master.hostNetwork | bool | `false` | Run the container in the host's network namespace. |
| master.hostUsers | bool | `nil` | Run the container with host user ids. NOTE: if hostNetwork is true, hostUsers should be true. |
| master.dnsPolicy | string | `"ClusterFirstWithHostNet"` | NFD master pod [dnsPolicy](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-s-dns-policy). |
| master.config | string | `nil` | NFD master [configuration](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/master-configuration-reference). |
| master.port | int | `8080` | Port on which to serve http for metrics and healthz endpoints. |
| master.instance | string | `nil` | Instance name. Used to separate annotation namespaces for multiple parallel deployments. |
| master.resyncPeriod | int | `nil` | NFD API controller resync period. Time duration string (e.g. "5m", "1h", "2h45m"). |
| master.denyLabelNs | list | `[]` | Label namespaces to deny. Labels with these prefixes will not be published to the nodes. |
| master.extraLabelNs | list | `[]` | Additional label namespaces to publish. Labels with these prefixes are allowed even if otherwise denied by `master.denyLabelNs`. |
| master.enableTaints | bool | `false` | Enable node tainting. |
| master.nfdApiParallelism | int | `nil` | The maximum number of concurrent node updates. |
| master.deploymentAnnotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-master Deployment. |
| master.replicaCount | int | `1` | Number of the desired replicas for the nfd-master Deployment. |
| master.podSecurityContext | object | `{}` | [Pod SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod) of the nfd-master pods. |
| master.initContainers | list | `[]` | [Init containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to add to the nfd-master pods. |
| master.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true}` | [SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-container) of the nfd-master container. |
| master.serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| master.serviceAccount.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the service account |
| master.serviceAccount.name | string | `nil` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template |
| master.revisionHistoryLimit | int | `nil` | Specifies the number of old ReplicaSets for the Deployment to retain. [revisionHistoryLimit](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#revision-history-limit) |
| master.rbac.create | bool | `true` | Create [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) configuration for nfd-master. |
| master.resources.limits | object | `{"memory":"4Gi"}` | Resource [limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-master container. |
| master.resources.requests | object | `{"cpu":"100m","memory":"128Mi"}` | Resource [requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-master container. |
| master.nodeSelector | object | `{}` | [Node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) for the nfd-master pods. |
| master.tolerations | list | `[{"effect":"NoSchedule","key":"node-role.kubernetes.io/control-plane","operator":"Equal","value":""}]` | [Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for the nfd-master pods. |
| master.podDisruptionBudget.enable | bool | `false` | Configure PodDisruptionBudget for the nfd-master Deployment. |
| master.podDisruptionBudget.minAvailable | int | `1` | Minimum number (or percentage) of pods that must be available after the eviction. |
| master.podDisruptionBudget.unhealthyPodEvictionPolicy | string | `"AlwaysAllow"` | Policy to evict unhealthy pods when a PodDisruptionBudget is defined. |
| master.networkPolicy.enabled | bool | `false` | Should a networkPolicy be deployed for the nfd-master pods |
| master.networkPolicy.egress | list | `[{"ports":[{"port":80,"protocol":"TCP"},{"port":443,"protocol":"TCP"},{"port":53,"protocol":"TCP"},{"port":53,"protocol":"UDP"},{"port":6443,"protocol":"TCP"}]}]` | [Egress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-master pods. The minimum egress ports required to function are: DNS (53/udp, 53/tcp, API server (80/tcp, 443/tcp, 6443/tcp). NOTE: OKD and Openshift use 6443/tcp |
| master.networkPolicy.ingress | list | `[{"ports":[{"port":"http","protocol":"TCP"}]}]` | [Ingress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-master pods. |
| master.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-master pods. |
| master.labels | object | `{}` | [Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) to add to the nfd-master pods. |
| master.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"In","values":[""]}]},"weight":1}]}}` | [Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) for the nfd-master pods. |
| master.startupProbe | object | - | Startup probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-startup-probes). |
| master.startupProbe.initialDelaySeconds | int | `nil` | The number of seconds after the container has started before probe is initiated. |
| master.startupProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| master.startupProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| master.startupProbe.failureThreshold | int | `30` | The number of consecutive failures for the probe before considering the pod as not ready. |
| master.livenessProbe | object | - | Liveness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-liveness-probes). |
| master.livenessProbe.initialDelaySeconds | int | `nil` | The number of seconds after the container has started before probe is initiated. |
| master.livenessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| master.livenessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| master.livenessProbe.failureThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| master.readinessProbe | object | - | Readiness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-readiness-probes). |
| master.readinessProbe.initialDelaySeconds | int | `nil` | The number of seconds after the container has started before probe is initiated. |
| master.readinessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| master.readinessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| master.readinessProbe.successThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| master.readinessProbe.failureThreshold | int | `10` | The number of consecutive failures for the probe before considering the pod as not ready. |

### NFD-Worker

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| worker.enable | bool | `true` | Specifies whether nfd-worker should be deployed |
| worker.ownerRefs | list | `["pod","ds"]` | Objects used as owner references for NodeFeature objects. Valid values are `node`, `pod`, and `ds`. This value is passed through `-owner-refs` and takes precedence over `worker.config.core.ownerRefs`. |
| worker.extraArgs | list | `[]` | Additional [command line arguments](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/worker-commandline-reference) to pass to nfd-worker. |
| worker.extraEnvs | list | `[]` | Additional environment variables to set in the nfd-worker container. |
| worker.hostNetwork | bool | `false` | Run the container in the host's network namespace. |
| worker.hostUsers | bool | `nil` | Run the container with host user ids. NOTE: if hostNetwork is true, hostUsers should be true. |
| worker.dnsPolicy | string | `"ClusterFirstWithHostNet"` | NFD worker pod [dnsPolicy](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-dns-policy). |
| worker.config | string | `nil` | NFD worker [configuration](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/worker-configuration-reference). |
| worker.port | int | `8080` | Port on which to serve http for metrics and healthz endpoints. |
| worker.daemonsetAnnotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-worker DaemonSet. |
| worker.podSecurityContext | object | `{}` | [Pod SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod) of the nfd-worker pods. |
| worker.initContainers | list | `[]` | [Init containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to add to the nfd-worker pods. |
| worker.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true}` | [SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-container) of the nfd-worker container. |
| worker.livenessProbe | object | - | Liveness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-liveness-probes). |
| worker.livenessProbe.initialDelaySeconds | int | `10` | The number of seconds after the container has started before probe is initiated. |
| worker.livenessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| worker.livenessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| worker.livenessProbe.failureThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| worker.readinessProbe | object | - | Readiness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-readiness-probes). |
| worker.readinessProbe.initialDelaySeconds | int | `5` | The number of seconds after the container has started before probe is initiated. |
| worker.readinessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| worker.readinessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| worker.readinessProbe.successThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| worker.readinessProbe.failureThreshold | int | `10` | The number of consecutive failures for the probe before considering the pod as not ready. |
| worker.serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| worker.serviceAccount.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the service account |
| worker.serviceAccount.name | string | `nil` | Name of the service account to use. If not set and create is true, a name is generated using the fullname template |
| worker.revisionHistoryLimit | int | `nil` | Specifies the number of old history for the DaemonSet to retain to allow rollback. |
| worker.rbac.create | bool | `true` | Create [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) configuration for nfd-worker. |
| worker.mountUsrSrc | bool | `false` | Mount host path /user/src inside the container. Does not work on systems without /usr/src AND a read-only /usr. |
| worker.resources.limits | object | `{"memory":"512Mi"}` | Resource [limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-worker container. |
| worker.resources.requests | object | `{"cpu":"5m","memory":"64Mi"}` | Resource [requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-worker container. |
| worker.nodeSelector | object | `{}` | [Node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) for the nfd-worker pods. |
| worker.tolerations | list | `[]` | [Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for the nfd-worker pods. |
| worker.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-worker pods. |
| worker.labels | object | `{}` | [Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) to add to the nfd-worker pods. |
| worker.affinity | object | `{}` | [Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) for the nfd-worker pods. |
| worker.priorityClassName | string | `nil` | The name of the PriorityClass to be used for the nfd-worker pods. |
| worker.updateStrategy | object | `{"rollingUpdate":{"maxUnavailable":"10%"}}` | Update strategy for the nfd-worker DaemonSet. Defaults to a rolling update with `maxUnavailable: "10%"` so upgrades complete in a bounded number of waves on clusters of any size (the Kubernetes default of `maxUnavailable: 1` rolls one node at a time and makes Helm wait and Flux HelmRelease timeouts likely on large clusters). nfd-worker is stateless and node labels persist while a worker pod restarts, so a faster roll is safe. Set `maxUnavailable: 1` to restore the Kubernetes default. To use `type: OnDelete`, also set `rollingUpdate: null` (Helm deep-merges maps). [More info](https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set) |
| worker.networkPolicy.enabled | bool | `false` | Should a networkPolicy be deployed for the nfd-worker pods |
| worker.networkPolicy.egress | list | `[{"ports":[{"port":80,"protocol":"TCP"},{"port":443,"protocol":"TCP"},{"port":53,"protocol":"TCP"},{"port":53,"protocol":"UDP"},{"port":6443,"protocol":"TCP"}]}]` | [Egress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-worker pods. The minimum egress ports required to function are: DNS (53/udp, 53/tcp, API server (80/tcp, 443/tcp, 6443/tcp). NOTE: OKD and Openshift use 6443/tcp |
| worker.networkPolicy.ingress | list | `[{"ports":[{"port":"http","protocol":"TCP"}]}]` | [Ingress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-worker pods. |

### NFD-Topology-Updater

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| topologyUpdater.config | string | `nil` | Configuration for the topology updater. See the [configuration reference](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/topology-updater-configuration-reference) for details. |
| topologyUpdater.enable | bool | `false` | Specifies whether nfd-topology-updater should be deployed. |
| topologyUpdater.createCRDs | bool | `false` | Create the NodeResourceTopology CRD. This MUST be set to true when 'enable' is true, unless the CRD is installed separately (e.g., by another Helm release or external tool). If the CRD is missing, the topology-updater pods will fail with "NodeResourceTopology CRD is not installed" error. |
| topologyUpdater.extraArgs | list | `[]` | Additional [command line arguments](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/topology-updater-commandline-reference) to pass to nfd-topology-updater. |
| topologyUpdater.extraEnvs | list | `[]` | Additional environment variables to set in the nfd-topology-updater container. |
| topologyUpdater.hostNetwork | bool | `false` | Run the container in the host's network namespace. |
| topologyUpdater.hostUsers | bool | `nil` | Run the container with host user ids. NOTE: if hostNetwork is true, hostUsers should be true. |
| topologyUpdater.dnsPolicy | string | `"ClusterFirstWithHostNet"` | NFD topology updater pod [dnsPolicy](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-dns-policy). |
| topologyUpdater.serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| topologyUpdater.serviceAccount.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the service account. |
| topologyUpdater.serviceAccount.name | string | `nil` | Name or the service account to use. If not set and create is true, a name is generated using the fullname template. |
| topologyUpdater.revisionHistoryLimit | int | `nil` | Specifies the number of old history for the DaemonSet to retain to allow rollback. |
| topologyUpdater.rbac.create | bool | `true` | Create [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) configuration for nfd-topology-updater. |
| topologyUpdater.port | int | `8080` | Port on which to serve http for metrics and healthz endpoints. |
| topologyUpdater.kubeletConfigPath | string | `nil` | Host path for the kubelet config file. |
| topologyUpdater.kubeletPodResourcesSockPath | string | `nil` | Host path for the kubelet socket for podresources endpoint. |
| topologyUpdater.updateInterval | string | `"60s"` | Time to sleep between CR updates. Non-positive value implies no CR update. |
| topologyUpdater.watchNamespace | string | `"*"` | Namespace to watch pods, `*` for all namespaces. |
| topologyUpdater.kubeletStateDir | string | `"/var/lib/kubelet"` | The kubelet state directory path for watching state and checkpoint files. Empty value disables kubelet state tracking. |
| topologyUpdater.podSecurityContext | object | `{}` | [Pod SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod) of the nfd-topology-updater pods. |
| topologyUpdater.initContainers | list | `[]` | [Init containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to add to the nfd-topology-updater pods. |
| topologyUpdater.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsUser":0}` | [SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-container) of the nfd-topology-updater container. |
| topologyUpdater.livenessProbe | object | - | Liveness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-liveness-probes). |
| topologyUpdater.livenessProbe.initialDelaySeconds | int | `10` | The number of seconds after the container has started before probe is initiated. |
| topologyUpdater.livenessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| topologyUpdater.livenessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| topologyUpdater.livenessProbe.failureThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| topologyUpdater.readinessProbe | object | - | Readiness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-readiness-probes). |
| topologyUpdater.readinessProbe.initialDelaySeconds | int | `5` | The number of seconds after the container has started before probe is initiated. |
| topologyUpdater.readinessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| topologyUpdater.readinessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| topologyUpdater.readinessProbe.successThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| topologyUpdater.readinessProbe.failureThreshold | int | `10` | The number of consecutive failures for the probe before considering the pod as not ready. |
| topologyUpdater.resources.limits | object | `{"memory":"60Mi"}` | Resource [limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-topology-updater container. |
| topologyUpdater.resources.requests | object | `{"cpu":"50m","memory":"40Mi"}` | Resource [requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-topology-updater container. |
| topologyUpdater.nodeSelector | object | `{}` | [Node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) for the nfd-topology-updater pods. |
| topologyUpdater.tolerations | list | `[]` | [Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for the nfd-topology-updater pods. |
| topologyUpdater.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-topology-updater pods. |
| topologyUpdater.labels | object | `{}` | [Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) to add to the nfd-topology-updater pods. |
| topologyUpdater.daemonsetAnnotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-topology-updater DaemonSet. |
| topologyUpdater.affinity | object | `{}` | [Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) for the nfd-topology-updater pods. |
| topologyUpdater.podSetFingerprint | bool | `true` | Enables compute and report of pod fingerprint in NRT objects. |
| topologyUpdater.networkPolicy.enabled | bool | `false` | Should a networkPolicy be deployed for the nfd-topology pods |
| topologyUpdater.networkPolicy.egress | list | `[{"ports":[{"port":80,"protocol":"TCP"},{"port":443,"protocol":"TCP"},{"port":53,"protocol":"TCP"},{"port":53,"protocol":"UDP"},{"port":6443,"protocol":"TCP"}]}]` | [Egress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-topology pods. The minimum egress ports required to function are: DNS (53/udp, 53/tcp, API server (80/tcp, 443/tcp, 6443/tcp). NOTE: OKD and Openshift use 6443/tcp |
| topologyUpdater.networkPolicy.ingress | list | `[{"ports":[{"port":"http","protocol":"TCP"}]}]` | [Ingress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-topology pods. |

### NFD-GC

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| gc.enable | bool | `true` | Specifies whether nfd-gc should be deployed. |
| gc.extraArgs | list | `[]` | Additional [command line arguments](https://kubernetes-sigs.github.io/node-feature-discovery/v0.19/reference/gc-commandline-reference) to pass to nfd-gc. |
| gc.extraEnvs | list | `[]` | Additional environment variables to set in the nfd-gc container. |
| gc.hostNetwork | bool | `false` | Run the container in the host's network namespace. |
| gc.hostUsers | bool | `nil` | Run the container with host user ids. NOTE: if hostNetwork is true, hostUsers should be true. |
| gc.replicaCount | int | `1` | The number of desired replicas for the nfd-gc Deployment. |
| gc.dnsPolicy | string | `"ClusterFirstWithHostNet"` | NFD gc pod [dnsPolicy](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-dns-policy). |
| gc.serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| gc.serviceAccount.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the service account. |
| gc.serviceAccount.name | string | `nil` | Name of the service account to use. If not set and create is true, a name is generated using the fullname template. |
| gc.rbac.create | bool | `true` | Create [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) configuration for nfd-gc. |
| gc.interval | string | `"1h"` | Time between periodic garbage collector runs. |
| gc.podSecurityContext | object | `{}` | [Pod SecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod) of the nfd-gc pods. |
| gc.initContainers | list | `[]` | [Init containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to add to the nfd-gc pods. |
| gc.livenessProbe | object | - | Liveness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-liveness-probes). |
| gc.livenessProbe.initialDelaySeconds | int | `10` | The number of seconds after the container has started before probe is initiated. |
| gc.livenessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| gc.livenessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| gc.livenessProbe.failureThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| gc.readinessProbe | object | - | Readiness probe configuration. [More information](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#configure-readiness-probes). |
| gc.readinessProbe.initialDelaySeconds | int | `5` | The number of seconds after the container has started before probe is initiated. |
| gc.readinessProbe.timeoutSeconds | int | `nil` | The number of seconds after which the probe times out. |
| gc.readinessProbe.periodSeconds | int | `nil` | How often (in seconds) to perform the probe. |
| gc.readinessProbe.successThreshold | int | `nil` | Minimum consecutive successes for the probe before considering the pod as ready. |
| gc.readinessProbe.failureThreshold | int | `nil` | The number of consecutive failures for the probe before considering the pod as not ready. |
| gc.resources.limits | object | `{"memory":"1Gi"}` | Resource [limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-gc container. |
| gc.resources.requests | object | `{"cpu":"10m","memory":"128Mi"}` | Resource [requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#requests-and-limits) for the nfd-gc container. |
| gc.port | int | `8080` | Port on which to serve http for metrics and healthz endpoints. |
| gc.nodeSelector | object | `{}` | [Node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) for the nfd-gc pods. |
| gc.tolerations | list | `[]` | [Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for the nfd-gc pods. |
| gc.annotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-gc pods. |
| gc.labels | object | `{}` | [Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) to add to the nfd-gc pods. |
| gc.deploymentAnnotations | object | `{}` | [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) to add to the nfd-gc  Deployment. |
| gc.affinity | object | `{}` | [Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) for the nfd-gc pods. |
| gc.podDisruptionBudget.enable | bool | `false` | Configure PodDisruptionBudget for the nfd-master Deployment. |
| gc.podDisruptionBudget.minAvailable | int | `1` | Minimum number (or percentage) of pods that must be available after the eviction. |
| gc.podDisruptionBudget.unhealthyPodEvictionPolicy | string | `"AlwaysAllow"` | Policy to evict unhealthy pods when a PodDisruptionBudget is defined. |
| gc.revisionHistoryLimit | int | `nil` | Specifies the number of old history for the Deployment to retain to allow rollback. |
| gc.networkPolicy.enabled | bool | `false` | Should a networkPolicy be deployed for the nfd-gc pods |
| gc.networkPolicy.egress | list | `[{"ports":[{"port":80,"protocol":"TCP"},{"port":443,"protocol":"TCP"},{"port":53,"protocol":"TCP"},{"port":53,"protocol":"UDP"},{"port":6443,"protocol":"TCP"}]}]` | [Egress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-gc pods. The minimum egress ports required to function are: DNS (53/udp, 53/tcp, API server (80/tcp, 443/tcp, 6443/tcp). NOTE: OKD and Openshift use 6443/tcp |
| gc.networkPolicy.ingress | list | `[{"ports":[{"port":"http","protocol":"TCP"}]}]` | [Ingress](https://kubernetes.io/docs/concepts/services-networking/network-policies/#network-traffic-filtering) for the nfd-gc pods. |

### Prometheus

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| prometheus.enable | bool | `false` | Create PodMonitor object for enabling metrics collection by Prometheus Operator. |
| prometheus.scrapeInterval | string | `"10s"` | Interval at which metrics are scraped. |
| prometheus.labels | object | `{}` | Labels to add to the PodMonitor object. |
