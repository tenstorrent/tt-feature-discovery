// tt-feature-discovery publishes Kubernetes node labels describing the
// Tenstorrent devices on a node.
//
// Two sources feed the label set:
//
//   - the host PCI sysfs, for tenstorrent.com/device.present
//   - tt-fabric-manager, for ASIC and datacenter-location labels
//
// The fabric manager already owns factory-system-descriptor interpretation, so
// this binary asks it for a structured host summary rather than parsing an FSD
// itself. Fabric labels are optional: without --fabric-manager-address, or on a
// node the fabric manager does not know about, only device.present is
// published.
//
// Labels are published by writing an NFD local-source feature file; the
// node-feature-discovery worker on the same node turns it into node labels.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tenstorrent/tt-feature-discovery/internal/detect"
	"github.com/tenstorrent/tt-feature-discovery/internal/fabric"
	"github.com/tenstorrent/tt-feature-discovery/internal/labels"
)

const labelDevicePresent = "tenstorrent.com/device.present"

// fabricRPCTimeout bounds a single QueryTopologySummary call so a wedged
// fabric manager cannot stall the reconcile loop.
const fabricRPCTimeout = 10 * time.Second

type config struct {
	sysBusPCIDir   string
	featureFile    string
	interval       time.Duration
	once           bool
	logLevel       string
	fabricAddress  string
	nodeName       string
	fabricCacheTTL time.Duration
	fabricMaxStale time.Duration
}

// envOr returns the value of the environment variable key, or def when unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
	fs.StringVar(&cfg.fabricAddress, "fabric-manager-address",
		envOr("TT_FABRIC_MANAGER_ADDRESS", ""),
		"tt-fabric-manager host:port; empty disables fabric-derived labels")
	fs.StringVar(&cfg.nodeName, "node-name", envOr("NODE_NAME", ""),
		"name this node is registered under with tt-fabric-manager (defaults to the OS hostname)")
	fs.DurationVar(&cfg.fabricCacheTTL, "fabric-cache-ttl", fabric.DefaultCacheTTL,
		"minimum interval between fabric-manager queries")
	fs.DurationVar(&cfg.fabricMaxStale, "fabric-max-stale", fabric.DefaultMaxStale,
		"how long to keep publishing the last known fabric labels while fabric-manager is unreachable")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if cfg.nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("resolve node name: %w", err)
		}
		cfg.nodeName = hostname
	}
	return cfg, nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// fabricLabeler is the subset of fabric.Client that runOnce needs, so tests can
// substitute a stub.
type fabricLabeler interface {
	HostSummary(ctx context.Context, hostname string) (*fabric.Summary, error)
}

func runOnce(ctx context.Context, log *slog.Logger, cfg *config, fc fabricLabeler) error {
	accel, err := detect.Scan(cfg.sysBusPCIDir)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}
	present := accel.Count > 0

	out := map[string]string{}
	if present {
		out[labelDevicePresent] = "true"
	}
	for k, v := range labels.FromAccelerators(accel) {
		out[k] = v
	}
	if accel.Mixed() {
		log.Warn("host holds more than one kind of accelerator; publishing mixed",
			slog.Any("families", accel.Families),
			slog.Any("products", accel.Products),
		)
	}

	fabricLabels := 0
	if fc != nil {
		rpcCtx, cancel := context.WithTimeout(ctx, fabricRPCTimeout)
		summary, err := fc.HostSummary(rpcCtx, cfg.nodeName)
		cancel()
		switch {
		case errors.Is(err, fabric.ErrHostNotFound):
			// Normal for a node whose fabric-manager agent has not registered.
			log.Debug("node not known to fabric-manager",
				slog.String("node_name", cfg.nodeName))
		case err != nil:
			// Publish what we do know rather than failing the whole pass.
			log.Warn("fabric-manager query failed; publishing without fabric labels",
				slog.Any("err", err))
		default:
			warnArchMismatch(log, accel, summary.Host.GetArch())
			fl := labels.FromFabricSummary(summary)
			fabricLabels = len(fl)
			for k, v := range fl {
				out[k] = v
			}
			if summary.Stale {
				log.Warn("serving cached fabric labels; fabric-manager unreachable")
			}
		}
	}

	if err := labels.Write(cfg.featureFile, out); err != nil {
		return fmt.Errorf("write feature file: %w", err)
	}
	log.Info("reconciled",
		slog.String("feature_file", cfg.featureFile),
		slog.Bool("device_present", present),
		slog.Int("accelerator_count", accel.Count),
		slog.Int("label_count", len(out)),
		slog.Int("fabric_label_count", fabricLabels),
	)
	return nil
}

// warnArchMismatch flags a host whose PCI-derived family disagrees with the
// arch the fabric manager reports. The two are read from different places —
// the local bus versus the FSD or live discovery — so a disagreement usually
// means the node is described by the wrong FSD entry.
func warnArchMismatch(log *slog.Logger, accel detect.Accelerators, arch string) {
	if arch == "" || len(accel.Families) != 1 {
		return
	}
	family := accel.Families[0]
	if family == detect.Unclassified || strings.HasPrefix(strings.ToLower(arch), family) {
		return
	}
	log.Warn("fabric-manager arch disagrees with the PCI-derived family",
		slog.String("pci_family", family),
		slog.String("fabric_arch", arch),
	)
}

func run(ctx context.Context, log *slog.Logger, cfg *config, fc fabricLabeler) error {
	if err := runOnce(ctx, log, cfg, fc); err != nil {
		log.Error("detection failed", slog.Any("err", err))
		if cfg.once {
			return err
		}
	}
	if cfg.once {
		return nil
	}

	t := time.NewTicker(cfg.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := runOnce(ctx, log, cfg, fc); err != nil {
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
	log.Info("starting",
		slog.String("sys_bus_pci_dir", cfg.sysBusPCIDir),
		slog.String("feature_file", cfg.featureFile),
		slog.Duration("interval", cfg.interval),
		slog.Bool("once", cfg.once),
		slog.String("node_name", cfg.nodeName),
		slog.String("fabric_manager_address", cfg.fabricAddress),
	)

	var fc fabricLabeler
	if cfg.fabricAddress != "" {
		fc = fabric.NewClient(cfg.fabricAddress,
			fabric.WithCacheTTL(cfg.fabricCacheTTL),
			fabric.WithMaxStale(cfg.fabricMaxStale),
		)
	} else {
		log.Info("fabric-manager address unset; publishing device.present only")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log, cfg, fc); err != nil {
		log.Error("exit with error", slog.Any("err", err))
		os.Exit(1)
	}
}
