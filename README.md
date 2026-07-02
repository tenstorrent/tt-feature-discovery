# tt-feature-discovery

Tenstorrent Feature Discovery automatically publishes Kubernetes node labels
describing the Tenstorrent devices on a node.

It runs as a DaemonSet alongside [node-feature-discovery][nfd] (NFD) and
publishes labels by writing files into NFD's local feature-file directory.
NFD's worker on the same node turns those files into node labels — no
direct contact with the API server, no service account.

[nfd]: https://github.com/kubernetes-sigs/node-feature-discovery

## v1 scope

Two label families:

### Presence (always on)

| Label                          | Value  | Meaning                                  |
| ------------------------------ | ------ | ---------------------------------------- |
| `tenstorrent.com/npu.present`  | `true` | Host has ≥1 Tenstorrent PCI device       |

Detection scans the PCI sysfs (`/sys/bus/pci/devices/*/vendor`) for the
Tenstorrent vendor ID `0x1e52`. Nodes without a Tenstorrent device publish
no `tenstorrent.com/*` labels.

### Datacenter placement (opt-in, via tt-fabric-manager)

When `--fabric-manager-address` is set, tt-feature-discovery calls
`FabricManagerService.QueryFactorySystemDescriptor` on tt-fabric-manager,
looks the node up in the FSD by hostname, and publishes:

| Label                                          | Source                                        |
| ---------------------------------------------- | --------------------------------------------- |
| `tenstorrent.com/datacenter.hall`              | `FSD.Host.hall` (lowercased, sanitized)       |
| `tenstorrent.com/datacenter.aisle`             | `FSD.Host.aisle`                              |
| `tenstorrent.com/datacenter.rack`              | `FSD.Host.rack` (integer)                     |
| `tenstorrent.com/datacenter.shelf-u`           | `FSD.Host.shelf_u` (integer)                  |
| `tenstorrent.com/datacenter.motherboard`       | `FSD.Host.motherboard`                        |
| `tenstorrent.com/host-id`                      | Positional index of this host in `FSD.hosts`  |
| `tenstorrent.com/systems.host-count`           | `len(FSD.hosts)` in the containing mesh       |
| `tenstorrent.com/systems.eth-connection-count` | `len(FSD.eth_connections.connection)`         |

Empty FSD fields are skipped rather than emitted with an empty value.
Nodes not present in the FSD publish no datacenter labels but still get
the presence label.

This is v0 of FSD-derived labeling. The FSD schema is mirrored under
`proto/factory_system_descriptor.proto` for Go binding generation; any
change to the upstream schema (tt-metal / tt-fabric-manager) requires a
coordinated update to this repo's copy.

## Prerequisites

- A cluster with NFD installed (the [tt-operator][tt-operator] umbrella chart
  installs NFD with the right config).
- NFD worker configured with the `local` feature source (default).

[tt-operator]: https://github.com/tenstorrent/tt-operator

## Install

```bash
kubectl apply -f deploy/tt-feature-discovery.yaml
```

Verify on a node with Tenstorrent hardware:

```bash
kubectl get node <node> -o jsonpath='{.metadata.labels}' | tr ',' '\n' | grep tenstorrent
# feature.node.kubernetes.io/tenstorrent.com-npu.present: "true"
```

NFD prefixes locally-sourced labels with `feature.node.kubernetes.io/`. To
expose them on a bare `tenstorrent.com/` prefix, enable the `tenstorrent.com`
extra label namespace in NFD's master config.

## Development

```bash
make build        # go build ./...
make test         # go test ./...
make vet          # go vet ./...
make lint         # golangci-lint run
make docker-build IMAGE=... TAG=...
```

CI workflows (`.github/workflows/`):

- `ci.yaml` — runs on every push/PR: `go vet`, `go test -race`, `go build`,
  `golangci-lint`, and a no-push container build for `linux/amd64`.
- `release.yaml` — on push to `main` or a `v*` tag: builds and pushes
  `ghcr.io/tenstorrent/tt-feature-discovery` with semantic tags.

## License

Apache-2.0 — see [LICENSE](./LICENSE).
