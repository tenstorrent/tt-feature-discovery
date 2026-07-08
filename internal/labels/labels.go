// Package labels writes node-feature-discovery feature files.
//
// node-feature-discovery's local source reads files from
// /etc/kubernetes/node-feature-discovery/features.d/ and converts each
// "key=value" line into a Kubernetes node label. Writing to that directory
// is how we publish tenstorrent.com/* labels without talking to the API
// server ourselves.
//
// https://kubernetes-sigs.github.io/node-feature-discovery/stable/usage/customization-guide.html#local-feature-source
package labels

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Write atomically writes the given labels to path. Existing contents are
// replaced. The file is rewritten only when contents change, so NFD's
// inotify watcher does not fire on no-op reconciles.
func Write(path string, labels map[string]string) error {
	body := render(labels)

	if existing, err := os.ReadFile(path); err == nil && string(existing) == body {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create feature dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

func render(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte('\n')
	}
	return b.String()
}
