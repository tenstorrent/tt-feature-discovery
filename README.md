# tt-feature-discovery

Tenstorrent Feature Discovery automatically publishes Kubernetes node labels
describing the Tenstorrent devices on a node.

It runs as a DaemonSet alongside [node-feature-discovery][nfd] (NFD) and
publishes labels by writing files into NFD's local feature-file directory.
NFD's worker on the same node turns those files into node labels — no
direct contact with the API server, no service account.

[nfd]: https://github.com/kubernetes-sigs/node-feature-discovery

## Labels

### From the host PCI bus

| Label                            | Value  | Meaning                            |
| -------------------------------- | ------ | ---------------------------------- |
| `tenstorrent.com/device.present` | `true` | Host has ≥1 Tenstorrent PCI device |

Detection scans the PCI sysfs (`/sys/bus/pci/devices/*/vendor`) for the
Tenstorrent vendor ID `0x1e52`. Nodes without a Tenstorrent device publish
no `tenstorrent.com/*` labels.

### From tt-fabric-manager

Set `--fabric-manager-address` to enable these. tt-feature-discovery calls
[tt-fabric-manager][ttfm]'s `QueryTopologySummary` RPC, which returns a
structured per-host record. The fabric manager is the component that owns
factory-system-descriptor (FSD) interpretation, so these labels are a direct
projection of its answer rather than a second, independent FSD parser.

| Label                                  | Example       | Source                                   |
| -------------------------------------- | ------------- | ---------------------------------------- |
| `tenstorrent.com/asic.count`           | `8`           | ASICs the fabric manager sees on the host |
| `tenstorrent.com/asic.arch`            | `wormhole_b0` | ASIC architecture                        |
| `tenstorrent.com/datacenter.hall`      | `aus2`        | FSD physical location                    |
| `tenstorrent.com/datacenter.aisle`     | `a1`          | FSD physical location                    |
| `tenstorrent.com/datacenter.rack`      | `12`          | FSD physical location                    |
| `tenstorrent.com/datacenter.shelf-u`   | `34`          | FSD physical location                    |
| `tenstorrent.com/fabric.connected-hosts` | `2`         | Hosts reachable via this host's exit nodes |
| `tenstorrent.com/fabric.group`         | `110-sc36_4x4_c1-10` | Name of this host's connectivity group |
| `tenstorrent.com/fabric.group-size`    | `3`           | Hosts in this host's connectivity group  |
| `tenstorrent.com/fabric.pod`           | `bh_galaxy_sp_0` | Name of this host's pod within the group |
| `tenstorrent.com/fabric.pod-size`      | `2`           | Hosts in this host's pod                 |
| `tenstorrent.com/fabric.source`        | `psd-fsd`     | Which view the fabric manager had: `psd`, `fsd`, or `psd-fsd` |

A group is a connected component over cross-host fabric links. Its name, and
the pods it subdivides into, come from the FSD cabling hierarchy: a pod is the
deepest instance-path prefix that still covers more than one host. Both names
are best-effort — the fabric manager leaves a group unnamed when no member is
FSD-described, omits pods when the hierarchy bottoms out at the group, and a
host can belong to a group but to no pod. Names carry `/` separators and an
occasional ` (2)` disambiguating suffix, which are folded to `-` to fit the
Kubernetes label value grammar.

The query uses `COMBINED_PSD_FSD`, the only source carrying both live ASIC
counts (PSD) and datacenter location (FSD). A cluster with no FSD loaded gets
PSD-only records, so the `datacenter.*` labels are simply absent — which is
also why `fabric.source` is published, to make that distinction visible on the
node.

[ttfm]: https://github.com/tenstorrent/tt-fabric-manager

### Degradation

Fabric labels are best-effort and never block `device.present`:

- No `--fabric-manager-address` → only `device.present` is published.
- Node unknown to the fabric manager (agent not registered) → no fabric labels.
- Fabric manager unreachable → the last known summary keeps being published for
  `--fabric-max-stale` (default 30m), so a controller restart does not strip
  every node's labels. Past that window the fabric labels drop.

Node identity is matched on the name the fabric-manager agent registered under
(its `gethostname()`). Pass it via `--node-name` or `NODE_NAME`; the DaemonSet
sets `NODE_NAME` from `spec.nodeName`. Matching falls back to a
case-insensitive comparison of the leading DNS label.

## Prerequisites

- A cluster with NFD installed (the [tt-operator][tt-operator] umbrella chart
  installs NFD with the right config).
- NFD worker configured with the `local` feature source (default).
- For fabric labels: tt-fabric-manager installed, with its agent running on the
  nodes to be labelled.

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
make proto        # regenerate the fabric-manager gRPC bindings
make docker-build IMAGE=... TAG=...
```

CI workflows (`.github/workflows/`):

- `ci.yaml` — runs on every push/PR: `go vet`, `go test -race`, `go build`,
  `golangci-lint`, and a no-push container build for `linux/amd64`.
- `release.yaml` — on push to `main` or a `v*` tag: builds and pushes
  `ghcr.io/tenstorrent/tt-feature-discovery` with semantic tags.

`proto/orchestrator.proto` is a client-side subset of tt-fabric-manager's
proto of the same name, carrying only `QueryTopologySummary`. Field numbers are
the wire contract — keep them in sync with upstream. The generated bindings
under `internal/fabric/orchestratorpb/` are checked in.

## License

Apache-2.0 — see [LICENSE](./LICENSE).
