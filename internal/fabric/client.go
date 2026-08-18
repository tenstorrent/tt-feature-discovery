// Package fabric is a small gRPC client for tt-fabric-manager.
//
// It calls QueryTopologySummary and hands back the HostSummary record for
// this node. The fabric manager already resolves the factory system
// descriptor and merges it with live discovered state, so everything we need
// arrives as structured fields — no FSD parsing on this side.
//
// The client-side subset of the service definition lives in proto/; the
// generated bindings are checked in under orchestratorpb/.
package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

// ErrHostNotFound is returned when the fabric manager answered but does not
// know about the requested host. That is a normal state for a node whose
// fabric-manager agent has not registered (or has no Tenstorrent hardware),
// so callers should treat it as "no fabric labels", not as a failure.
var ErrHostNotFound = errors.New("host not known to fabric-manager")

// Default timings.
const (
	DefaultCacheTTL = 5 * time.Minute
	// DefaultMaxStale bounds how long a cached summary keeps being served
	// after the fabric manager starts failing. Serving slightly stale
	// topology beats dropping every label on a controller restart, but a
	// decommissioned host should stop being labelled eventually.
	DefaultMaxStale = 30 * time.Minute
)

// dialer abstracts grpc.NewClient so tests can substitute a bufconn.
type dialer func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// Summary is the fabric manager's view of one host, plus the response-level
// context needed to derive labels.
type Summary struct {
	Host *orchpb.HostSummary
	// Group is the connectivity group containing this host: its FSD-derived
	// name (empty when the fabric manager could not derive one) and member
	// count (0 when the host is in no reported group).
	Group Grouping
	// Pod is the group's innermost subdivision containing this host. A host
	// that belongs to no pod leaves this zero.
	Pod Grouping
	// Stale is true when the fabric manager could not be reached and this
	// summary came from the cache.
	Stale bool
}

// Grouping names a set of hosts and records how many there are.
type Grouping struct {
	Name string
	Size int
}

// Client wraps the fabric-manager gRPC client and caches the last successful
// response. A single Client is safe for concurrent use.
type Client struct {
	address  string
	cacheTTL time.Duration
	maxStale time.Duration
	dial     dialer
	now      func() time.Time

	mu         sync.Mutex
	cache      *orchpb.QueryTopologySummaryResponse
	cacheStamp time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithCacheTTL sets the minimum interval between fabric-manager RPCs.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) { c.cacheTTL = ttl }
}

// WithMaxStale sets how long a cached response keeps being served once the
// fabric manager becomes unreachable. Zero disables stale serving.
func WithMaxStale(d time.Duration) Option {
	return func(c *Client) { c.maxStale = d }
}

// withDialer and withClock are test hooks.
func withDialer(d dialer) Option          { return func(c *Client) { c.dial = d } }
func withClock(f func() time.Time) Option { return func(c *Client) { c.now = f } }

// NewClient constructs a Client. address is a host:port suitable for
// grpc.NewClient (e.g. "tt-fabric-manager.tenstorrent.svc:50051").
func NewClient(address string, opts ...Option) *Client {
	c := &Client{
		address:  address,
		cacheTTL: DefaultCacheTTL,
		maxStale: DefaultMaxStale,
		dial:     grpc.NewClient,
		now:      time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// HostSummary returns the fabric manager's record for hostname. It reuses the
// cached response while it is younger than the cache TTL, refreshes it
// otherwise, and falls back to a stale cached response (Summary.Stale set) if
// the refresh fails within the stale window.
func (c *Client) HostSummary(ctx context.Context, hostname string) (*Summary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp, stale, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}

	host, ok := findHost(resp, hostname)
	if !ok {
		return nil, ErrHostNotFound
	}
	group, pod := grouping(resp, host.GetHostName())
	return &Summary{
		Host:  host,
		Group: group,
		Pod:   pod,
		Stale: stale,
	}, nil
}

func (c *Client) snapshot(ctx context.Context) (*orchpb.QueryTopologySummaryResponse, bool, error) {
	if c.cache != nil && c.now().Sub(c.cacheStamp) < c.cacheTTL {
		return c.cache, false, nil
	}

	resp, err := c.fetch(ctx)
	if err != nil {
		if c.cache != nil && c.maxStale > 0 && c.now().Sub(c.cacheStamp) < c.maxStale {
			return c.cache, true, nil
		}
		return nil, false, err
	}
	c.cache = resp
	c.cacheStamp = c.now()
	return resp, false, nil
}

func (c *Client) fetch(ctx context.Context) (*orchpb.QueryTopologySummaryResponse, error) {
	conn, err := c.dial(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial fabric-manager %q: %w", c.address, err)
	}
	defer func() { _ = conn.Close() }()

	// COMBINED_PSD_FSD is the only source that carries both live ASIC counts
	// and the FSD's datacenter location metadata, and the controller falls
	// back to PSD-only when no FSD is loaded.
	resp, err := orchpb.NewFabricManagerServiceClient(conn).QueryTopologySummary(ctx,
		&orchpb.QueryTopologySummaryRequest{
			TopoSource: orchpb.TopologySource_COMBINED_PSD_FSD,
		})
	if err != nil {
		return nil, fmt.Errorf("QueryTopologySummary: %w", err)
	}
	return resp, nil
}

// findHost matches hostname against the response, preferring an exact match
// and falling back to a case-insensitive comparison of the leading DNS label.
// Kubernetes node names and the agent's gethostname() usually agree, but one
// side may be FQDN-qualified or differently cased.
func findHost(resp *orchpb.QueryTopologySummaryResponse, hostname string) (*orchpb.HostSummary, bool) {
	for _, h := range resp.GetHosts() {
		if h.GetHostName() == hostname {
			return h, true
		}
	}
	needle := shortName(hostname)
	if needle == "" {
		return nil, false
	}
	for _, h := range resp.GetHosts() {
		if shortName(h.GetHostName()) == needle {
			return h, true
		}
	}
	return nil, false
}

func shortName(s string) string {
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	if i := strings.Index(s, "."); i >= 0 {
		s = s[:i]
	}
	return s
}

// grouping returns the connectivity group containing hostName and, within it,
// the pod containing hostName. Pods are only reported when they sit deeper
// than the group itself, and not every member of a group belongs to one, so a
// zero pod is normal rather than an error.
func grouping(resp *orchpb.QueryTopologySummaryResponse, hostName string) (group, pod Grouping) {
	for _, g := range resp.GetHostGroups() {
		if !contains(g, hostName) {
			continue
		}
		group = Grouping{Name: g.GetName(), Size: len(g.GetHostNames())}
		for _, p := range g.GetPods() {
			if contains(p, hostName) {
				pod = Grouping{Name: p.GetName(), Size: len(p.GetHostNames())}
				break
			}
		}
		return group, pod
	}
	return Grouping{}, Grouping{}
}

func contains(g *orchpb.HostGrouping, hostName string) bool {
	for _, n := range g.GetHostNames() {
		if n == hostName {
			return true
		}
	}
	return false
}
