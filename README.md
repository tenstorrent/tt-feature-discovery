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

Two more sysfs attributes say what the cards are:

| Label                                 | Example      | Meaning                            |
| ------------------------------------- | ------------ | ---------------------------------- |
| `tenstorrent.com/accelerator.count`   | `8`          | Tenstorrent PCI endpoints on the host |
| `tenstorrent.com/accelerator.family`  | `wormhole`   | ASIC generation, from the PCI device ID |
| `tenstorrent.com/accelerator.product` | `n150`       | Board type, from the board UPI in `subsystem_device` |

`device` gives the generation (`0x401e` Wormhole, `0xb140` Blackhole) and
`subsystem_device` carries the board's UPI code — the same value UMD looks up
in `board_upi_map`, and what it reads off PCI to recognise a Wormhole UBB.
Known products: `n150`, `n300`, `galaxy`, `ubb`, `p100`, `p150`, `p300`,
`ubb_blackhole`.

This classification is local: no ioctl, no UMD, no driver and no fabric
manager, so a node is classifiable before anything Tenstorrent-specific is
installed on it. A card whose IDs this build does not recognise is still
counted and labelled `unknown`, rather than dropped or guessed at.

`accelerator.count` counts PCI endpoints, which is not always the ASIC count.
On an N300 only one of the two ASICs is PCIe-attached, so the fabric manager's
`asic.count` is higher. On a UBB Galaxy every ASIC has MMIO, so the two agree
at 32 — see below.

### Galaxy systems

Galaxy needs spelling out, because three different products carry the name and
`accelerator.count` means something different on each.

| Label value      | UPI      | System                | ASICs | PCI endpoints |
| ---------------- | -------- | --------------------- | ----- | ------------- |
| `ubb`            | `0x0035` | Wormhole Galaxy       | 32    | 32            |
| `ubb_blackhole`  | `0x0047` | Blackhole Galaxy      | 32    | 32            |
| `galaxy`         | `0x000b` | Older Galaxy module    | 1 per board | 1 per MMIO board |

A **UBB Galaxy** (`ubb` / `ubb_blackhole`) is one 32-ASIC universal baseboard
built as 4 trays of 8. Every ASIC has MMIO, so all 32 appear as separate PCI
endpoints and a Galaxy host publishes:

```
tenstorrent.com/accelerator.count=32
tenstorrent.com/accelerator.family=wormhole
tenstorrent.com/accelerator.product=ubb
```

So `count=32` here is one physical baseboard with 32 endpoints, not 32 cards —
whereas `count=8` on a host of N150s really is 8 separate cards. The label
names the board type, and every endpoint on the baseboard reports the same
`subsystem_device`, which is why a Galaxy reads as homogeneous rather than
mixed. On a UBB, `accelerator.count` and the fabric manager's `asic.count`
agree; a disagreement there is worth investigating.

`galaxy` (`0x000b`) is the older, unrelated thing: a single-ASIC board, where a
shelf reaches the host through its closest MMIO chip and the rest are remote
over Ethernet. On those, most ASICs are not PCIe-visible at all, so
`accelerator.count` sits well below `asic.count`. **`product=galaxy` does not
mean "a Galaxy system"** — a Galaxy system is `ubb`.

Which tray an endpoint sits in is encoded in its PCI bus ID (UMD's
`get_ubb_id` masks `bus & 0xf0` against a per-architecture tray table, and
`bus & 0x0f` gives the ASIC within the tray). Wormhole and Blackhole use
different tray orderings — the same rev-A/B versus rev-C layout the fabric
manager reports as `is_bh_glx_rev_c`. Trays are not labelled here: a tray is a
sub-node detail, and per-device placement belongs in DRA.

Product names match UMD's `board_type_name_map` verbatim, so a label value can
be compared directly against `tt-smi` or cluster-descriptor output.

### Mixed hosts

Tenstorrent systems are homogeneous in practice, but a host with two card
types cannot be described by one family or product. Rather than pick a winner
— which would let a `family=wormhole` selector place a Wormhole-only workload
on a node that is half Blackhole — the scalar label becomes `mixed` and the
full sorted set is published beside it:

```
tenstorrent.com/accelerator.count=2
tenstorrent.com/accelerator.family=mixed
tenstorrent.com/accelerator.families=blackhole.wormhole
tenstorrent.com/accelerator.product=mixed
tenstorrent.com/accelerator.products=n150.p150
```

So the safe case is the default: a selector for a specific family does not
match a mixed node. The two axes are independent — a host of N150s and N300s
keeps `family=wormhole` and goes mixed only on `product`. Values are joined
with `.` because `,` is not a legal label value character. Anything needing
per-device placement should go through DRA, which resolves devices
individually, rather than through node labels.

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
| `tenstorrent.com/fabric.pod`           | `sc36-blah-sc20_0-sc16_0-bh_glx_pod_2` | Path of this host's pod in the cabling hierarchy |
| `tenstorrent.com/fabric.pod-size`      | `2`           | Hosts in this host's pod                 |
| `tenstorrent.com/fabric.source`        | `psd-fsd`     | Which view the fabric manager had: `psd`, `fsd`, or `psd-fsd` |

A group is a connected component over cross-host fabric links. Its name, and
the pods it subdivides into, come from the FSD cabling hierarchy: a pod is the
deepest instance-path prefix that still covers more than one host. Both names
are best-effort — the fabric manager leaves a group unnamed when no member is
FSD-described, omits pods when the hierarchy bottoms out at the group, and a
host can belong to a group but to no pod.

The fabric manager names a pod *relative to its group*, so `fabric.pod` is the
group path and the pod name joined: a bare leaf like `bh_glx_pod_2` repeats
across groups and would collide. One segment is still missing from the front —
the fabric manager drops the hierarchy's outermost (cluster) segment when
naming a group that spans more than one segment, and the summary API exposes no
raw instance path to recover it from.

Names carry `/` separators and an occasional ` (2)` disambiguating suffix, both
folded to `-` to fit the Kubernetes label value grammar. A path too long for
the 63-character limit drops leading segments rather than trailing ones, since
the deepest segments are what identify the pod.

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
