// tt-feature-discovery publishes Kubernetes node labels describing the
// Tenstorrent devices and physical placement of the node.
//
// Labels are published by writing an NFD local-source feature file; the
// node-feature-discovery worker on the same node converts each key=value
// line into a real node label. This program never talks to the API server.
//
// v1 scope:
//   - tenstorrent.com/present=true  (from PCI sysfs scan)
//   - tenstorrent.com/datacenter.*, host-id, systems.* — optional,
//     enabled by passing --fabric-manager-address. Values come from the
//     Factory System Descriptor served by tt-fabric-manager.
//
// FSD-derived labels are v0 — upstream FSD schema changes will require a
// coordinated update to the vendored proto under proto/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tenstorrent/tt-feature-discovery/internal/detect"
	"github.com/tenstorrent/tt-feature-discovery/internal/fabric"
	"github.com/tenstorrent/tt-feature-discovery/internal/labels"
)

const labelPresent = "tenstorrent.com/present"

type config struct {
	sysBusPCIDir string
	featureFile  string
	interval     time.Duration
	once         bool
	logLevel     string

	fabricAddr    string
	fabricPoll    time.Duration
	fabricTimeout time.Duration
	nodeName      string
}

func parseFlags(args []string, out *os.File) (*config, error) {
	fs := flag.NewFlagSet("tt-feature-discovery", flag.ContinueOnError)
	fs.SetOutput(out)
	cfg := &config{}
	fs.StringVar(&cfg.sysBusPCIDir, "sys-bus-pci-dir", detect.DefaultSysBusPCIDir,
		"PCI sysfs root to scan for Tenstorrent devices")
	fs.StringVar(&cfg.featureFile, "feature-file", "/etc/kubernetes/node-feature-discovery/features.d/tt-fd",
		"NFD local-source feature file to write")
	fs.DurationVar(&cfg.interval, "interval", 60*time.Second,
		"interval between detection passes (ignored with --once)")
	fs.BoolVar(&cfg.once, "once", false, "run a single detection pass and exit")
	fs.StringVar(&cfg.logLevel, "log-level", "info", "log level: debug, info, warn, error")

	fs.StringVar(&cfg.fabricAddr, "fabric-manager-address", "",
		"tt-fabric-manager gRPC endpoint (host:port). If empty, FSD-derived labels are disabled.")
	fs.DurationVar(&cfg.fabricPoll, "fabric-manager-poll-interval", 5*time.Minute,
		"minimum interval between fabric-manager RPCs. The FSD is stable, so this is deliberately long.")
	fs.DurationVar(&cfg.fabricTimeout, "fabric-manager-timeout", 10*time.Second,
		"per-RPC timeout for fabric-manager calls.")
	fs.StringVar(&cfg.nodeName, "node-name", "",
		"override for this node's hostname when matching FSD.Host.hostname. Defaults to $NODE_NAME then os.Hostname().")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return cfg, nil
}

func resolveNodeName(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if v := os.Getenv("NODE_NAME"); v != "" {
		return v, nil
	}
	return os.Hostname()
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// reconciler holds the runtime dependencies for a single reconcile pass.
// Wired at startup so runOnce doesn't have to rebuild them each tick.
type reconciler struct {
	cfg      *config
	nodeName string
	fabric   *fabric.Client // may be nil when --fabric-manager-address is empty
}

func (r *reconciler) runOnce(ctx context.Context, log *slog.Logger) error {
	present, err := detect.NPUPresent(r.cfg.sysBusPCIDir)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}

	out := map[string]string{}
	if present {
		out[labelPresent] = "true"
	}

	fsdLabels := r.fetchFSDLabels(ctx, log)
	for k, v := range fsdLabels {
		out[k] = v
	}

	if err := labels.Write(r.cfg.featureFile, out); err != nil {
		return fmt.Errorf("write feature file: %w", err)
	}
	log.Info("reconciled",
		slog.String("feature_file", r.cfg.featureFile),
		slog.Bool("npu_present", present),
		slog.Int("label_count", len(out)),
	)
	return nil
}

// fetchFSDLabels returns FSD-derived labels for this node, or an empty
// map if fabric integration is disabled or the RPC fails. Errors are
// logged and swallowed: a stale/unreachable fabric-manager must not tear
// down the whole reconcile loop or drop the PCI-detected labels.
func (r *reconciler) fetchFSDLabels(ctx context.Context, log *slog.Logger) map[string]string {
	if r.fabric == nil {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, r.cfg.fabricTimeout)
	defer cancel()
	fsd, err := r.fabric.Snapshot(fetchCtx)
	if err != nil {
		log.Warn("fabric-manager snapshot failed; FSD labels skipped",
			slog.String("err", err.Error()))
		return nil
	}

	host, idx, err := fabric.FindHost(fsd, r.nodeName)
	if err != nil {
		if errors.Is(err, fabric.ErrHostNotFound) {
			log.Info("node not found in FSD; no datacenter labels emitted",
				slog.String("node_name", r.nodeName),
				slog.Int("fsd_host_count", len(fsd.GetHosts())))
			return nil
		}
		log.Warn("FSD host lookup failed", slog.String("err", err.Error()))
		return nil
	}
	log.Debug("matched node in FSD",
		slog.String("node_name", r.nodeName),
		slog.String("fsd_hostname", host.GetHostname()),
		slog.Int("host_index", idx),
		slog.String("raw_hall", host.GetHall()),
		slog.String("raw_aisle", host.GetAisle()),
		slog.String("raw_motherboard", host.GetMotherboard()),
	)
	out := labels.FromFSDHost(fsd, host, idx)
	for k, v := range out {
		log.Info("fsd label",
			slog.String("key", k),
			slog.String("value", v),
		)
	}
	return out
}

func run(ctx context.Context, log *slog.Logger, r *reconciler) error {
	if err := r.runOnce(ctx, log); err != nil {
		log.Error("detection failed", slog.Any("err", err))
		if r.cfg.once {
			return err
		}
	}
	if r.cfg.once {
		return nil
	}

	t := time.NewTicker(r.cfg.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := r.runOnce(ctx, log); err != nil {
				log.Error("detection failed", slog.Any("err", err))
			}
		}
	}
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	log := newLogger(cfg.logLevel)

	nodeName, err := resolveNodeName(cfg.nodeName)
	if err != nil {
		log.Error("resolve node name", slog.Any("err", err))
		os.Exit(1)
	}

	r := &reconciler{cfg: cfg, nodeName: nodeName}
	if cfg.fabricAddr != "" {
		r.fabric = fabric.NewClient(cfg.fabricAddr, fabric.WithCacheTTL(cfg.fabricPoll))
	}

	log.Info("starting",
		slog.String("sys_bus_pci_dir", cfg.sysBusPCIDir),
		slog.String("feature_file", cfg.featureFile),
		slog.Duration("interval", cfg.interval),
		slog.Bool("once", cfg.once),
		slog.String("node_name", nodeName),
		slog.String("fabric_manager_address", cfg.fabricAddr),
		slog.Duration("fabric_manager_poll_interval", cfg.fabricPoll),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log, r); err != nil {
		log.Error("exit with error", slog.Any("err", err))
		os.Exit(1)
	}
}
