// tt-feature-discovery publishes Kubernetes node labels describing the
// Tenstorrent devices on a node.
//
// v1 scope: a single label, tenstorrent.com/npu.present=true, set when at
// least one Tenstorrent PCI device (vendor 0x1e52) is visible on the host.
// The label is published by writing an NFD local-source feature file; the
// node-feature-discovery worker on the same node converts it into a real
// node label.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tenstorrent/tt-feature-discovery/internal/detect"
	"github.com/tenstorrent/tt-feature-discovery/internal/labels"
)

const labelNPUPresent = "tenstorrent.com/npu.present"

type config struct {
	sysBusPCIDir string
	featureFile  string
	interval     time.Duration
	once         bool
	logLevel     string
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
	if err := fs.Parse(args); err != nil {
		return nil, err
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

func runOnce(log *slog.Logger, cfg *config) error {
	present, err := detect.NPUPresent(cfg.sysBusPCIDir)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}

	out := map[string]string{}
	if present {
		out[labelNPUPresent] = "true"
	}

	if err := labels.Write(cfg.featureFile, out); err != nil {
		return fmt.Errorf("write feature file: %w", err)
	}
	log.Info("reconciled",
		slog.String("feature_file", cfg.featureFile),
		slog.Bool("npu_present", present),
	)
	return nil
}

func run(ctx context.Context, log *slog.Logger, cfg *config) error {
	if err := runOnce(log, cfg); err != nil {
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
			if err := runOnce(log, cfg); err != nil {
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
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log, cfg); err != nil {
		log.Error("exit with error", slog.Any("err", err))
		os.Exit(1)
	}
}
