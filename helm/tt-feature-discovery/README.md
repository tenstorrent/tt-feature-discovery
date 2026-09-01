# tt-feature-discovery

Publishes Kubernetes node labels describing a node's Tenstorrent hardware — accelerator family, board product and count from PCI sysfs, plus ASIC and datacenter-location labels from tt-fabric-manager — via node-feature-discovery's local feature-file source.

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: latest](https://img.shields.io/badge/AppVersion-latest-informational?style=flat-square)

## Install

```bash
helm install tt-feature-discovery oci://ghcr.io/tenstorrent/helm/tt-feature-discovery \
  --namespace tt-feature-discovery --create-namespace
```

Requires node-feature-discovery in the cluster with its `local` feature
source enabled (the default). The [tt-operator][tt-operator] umbrella chart
installs NFD with the right config.

To enable the fabric-derived labels, point the chart at tt-fabric-manager's
controller:

```bash
helm install tt-feature-discovery oci://ghcr.io/tenstorrent/helm/tt-feature-discovery \
  --namespace tt-feature-discovery --create-namespace \
  --set fabricManager.address=tt-fabric-manager-controller.tt-operator-system.svc.cluster.local:50052
```

See the [repository README][repo] for the full label reference.

[tt-operator]: https://github.com/tenstorrent/tt-operator
[repo]: https://github.com/tenstorrent/tt-feature-discovery

## Requirements

Kubernetes: `>=1.27.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for the DaemonSet. |
| extraArgs | list | `[]` | Extra CLI args appended to the container command. |
| extraEnv | list | `[]` | Extra environment variables. |
| fabricManager | object | `{"address":"","cacheTTL":"5m","maxStale":"30m"}` | tt-fabric-manager integration. Fabric-derived labels are opt-in and never block the PCI-derived ones: with no address set, or on a node the fabric manager does not know about, the accelerator and device.present labels are still published. |
| fabricManager.address | string | `""` | Controller gRPC address (host:port). Empty disables fabric labels. The umbrella chart's TTFM renders its controller Service as `tt-fabric-manager-controller` on port 50052. |
| fabricManager.cacheTTL | string | `"5m"` | Minimum interval between fabric-manager queries. |
| fabricManager.maxStale | string | `"30m"` | How long the last known fabric labels keep being published while the fabric manager is unreachable. Bounded so a decommissioned host eventually stops being labelled, but long enough that a controller restart does not strip every node. |
| fullnameOverride | string | `""` | Override the fully-qualified release name. The tt-operator umbrella sets this so resources render as `tt-feature-discovery` rather than `tt-operator-tt-feature-discovery`. |
| hostPaths | object | `{"nfdFeaturesDir":"/etc/kubernetes/node-feature-discovery/features.d","sys":"/sys"}` | Host paths the DaemonSet mounts. |
| hostPaths.nfdFeaturesDir | string | `"/etc/kubernetes/node-feature-discovery/features.d"` | NFD's local-source feature directory. Must match the path NFD's worker reads on this node. |
| hostPaths.sys | string | `"/sys"` | Host /sys, mounted read-only; the PCI bus is scanned under it. |
| image | object | `{"pullPolicy":"IfNotPresent","repository":"ghcr.io/tenstorrent/tt-feature-discovery","tag":""}` | Container image for the DaemonSet. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.repository | string | `"ghcr.io/tenstorrent/tt-feature-discovery"` | Image repository. |
| image.tag | string | `""` | Image tag; falls back to .Chart.AppVersion when empty. |
| imagePullSecrets | list | `[]` | Image pull secrets for the DaemonSet pods. Leave empty for public images or when GHCR creds are injected via a namespace-default ServiceAccount. |
| interval | string | `"60s"` | Interval between detection passes. |
| logLevel | string | `"info"` | Log level: debug, info, warn, error. |
| nameOverride | string | `""` | Override the chart name used in resource names and labels. |
| nodeSelector | object | `{"kubernetes.io/os":"linux"}` | Node selector for the DaemonSet. Runs on every Linux node by default: a node with no Tenstorrent device publishes no tenstorrent.com/* labels, which is also how NFD removes a stale label from a node that lost its hardware. |
| podAnnotations | object | `{}` | Pod-level annotations. |
| podLabels | object | `{}` | Pod-level extra labels. |
| podSecurityContext | object | `{}` | Pod security context. |
| priorityClassName | string | `"system-node-critical"` | Priority class. Node labels gate scheduling for everything else on the node, so this defaults to a system-node-critical class. |
| resources | object | `{"limits":{"cpu":"100m","memory":"64Mi"},"requests":{"cpu":"10m","memory":"16Mi"}}` | Resource requests and limits. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsGroup":65532,"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}}` | Container security context. Unprivileged: the process only reads PCI sysfs and writes one file into NFD's features.d directory. |
| tolerations | list | `[{"operator":"Exists"}]` | Tolerations. Tolerates everything by default so tainted accelerator nodes still get labelled. |
| updateStrategy | object | `{"type":"RollingUpdate"}` | DaemonSet update strategy. |
