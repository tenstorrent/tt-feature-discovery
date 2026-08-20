# tt-feature-discovery

Tenstorrent Feature Discovery automatically publishes Kubernetes node labels
describing the Tenstorrent devices on a node.

It runs as a DaemonSet alongside [node-feature-discovery][nfd] (NFD) and
publishes labels by writing files into NFD's local feature-file directory.
NFD's worker on the same node turns those files into node labels — no
direct contact with the API server, no service account.

[nfd]: https://github.com/kubernetes-sigs/node-feature-discovery

## v1 scope

A single label:

| Label                            | Value  | Meaning                            |
| -------------------------------- | ------ | ---------------------------------- |
| `tenstorrent.com/device.present` | `true` | Host has ≥1 Tenstorrent PCI device |

Detection scans the PCI sysfs (`/sys/bus/pci/devices/*/vendor`) for the
Tenstorrent vendor ID `0x1e52`. Nodes without a Tenstorrent device publish
no `tenstorrent.com/*` labels.

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
# feature.node.kubernetes.io/tenstorrent.com-device.present: "true"
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
