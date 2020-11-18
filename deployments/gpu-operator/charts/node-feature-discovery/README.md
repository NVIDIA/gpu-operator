# node-feature-discovery helm chart

This is a helm chart for [node-feature-discovery](https://github.com/kubernetes-sigs/node-feature-discovery) using the master/worker pattern.

## TL;DR

```shell
helm repo add k8s-at-home https://k8s-at-home.com/charts/
helm install k8s-at-home/node-feature-discovery
```

## Installing the Chart

To install the chart with the release name `my-release`:

```shell
helm install my-release k8s-at-home/node-feature-discovery
```

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```shell
helm delete my-release --purge
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Configuration

The following tables lists the configurable parameters of the Sentry chart and their default values.
Read through the [values.yaml](https://github.com/k8s-at-home/charts/blob/master/charts/node-feature-discovery/values.yaml) file. It has several commented out suggested values.

| Parameter                                   | Description                                                                                  | Default                                               |
| ------------------------------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `image.repository`                          | Image repository                                                                             | `quay.io/kubernetes_incubator/node-feature-discovery` |
| `image.tag`                                 | Image tag. Possible values listed [here](https://github.com/kubernetes-sigs/node-feature-discovery/releases).     | `v0.6.0`                         |
| `image.pullPolicy`                          | Image pull policy                                                                            | `IfNotPresent`                                        |
| `strategyType`                              | Specifies the strategy used to replace old Pods by new ones                                  | `Recreate`                                            |
| `sources`                                   | List of sources to consider when labeling - see [documentation](https://github.com/kubernetes-sigs/node-feature-discovery#feature-sources) for info  | `[]`                                                  |
| `config`                                    | node-feature-discovery configuration - see [nfd-worker.conf.example](https://github.com/kubernetes-sigs/node-feature-discovery/blob/master/nfd-worker.conf.example) for example  | `{}` |
| `service.type`                              | Kubernetes service type for the GUI                                                          | `ClusterIP`                                           |
| `service.port`                              | Kubernetes port where the GUI is exposed                                                     | `8080`                                                |
| `service.annotations`                       | Service annotations for the GUI                                                              | `{}`                                                  |
| `service.labels`                            | Custom labels                                                                                | `{}`                                                  |
| `service.loadBalancerIP`                    | Loadbalancer IP for the GUI                                                                  | `{}`                                                  |
| `service.loadBalancerSourceRanges`          | List of IP CIDRs allowed access to load balancer (if supported)                              | `nil`                                                 |
| `podAnnotations`                            | Key-value pairs to add as pod annotations                                                    | `{}`                                                  |
| `master.replicaCount`                       | Number of replicas to scale the master component to                                          | `1`                                                   |
| `master.resources`                          | CPU/Memory resource requests/limits for master component                                     | `{}`                                                  |
| `master.nodeSelector`                       | Node labels for master component pod assignment                                              | `{}`                                                  |
| `master.tolerations`                        | Toleration labels for master component pod assignment                                        | See [values.yaml](https://github.com/k8s-at-home/charts/blob/master/charts/node-feature-discovery/values.yaml)                                                  |
| `master.affinity`                           | Affinity settings for master component pod assignment                                        | See [values.yaml](https://github.com/k8s-at-home/charts/blob/master/charts/node-feature-discovery/values.yaml)                                                  |
| `worker.resources`                          | CPU/Memory resource requests/limits for worker component                                     | `{}`                                                  |
| `worker.nodeSelector`                       | Node labels for worker component pod assignment                                              | `{}`                                                  |
| `worker.tolerations`                        | Toleration labels for worker component pod assignment                                        | `[]`                                                  |
| `worker.affinity`                           | Affinity settings for worker component pod assignment                                        | `{}`                                                  |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. For example,

```console
helm install my-release \
  --set image.pullPolicy="Always" \
    k8s-at-home/node-feature-discovery
```

Alternatively, a YAML file that specifies the values for the above parameters can be provided while installing the chart. For example,

```console
helm install my-release -f values.yaml k8s-at-home/node-feature-discovery
```
