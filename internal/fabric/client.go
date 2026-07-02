// Package fabric is a small gRPC client for tt-fabric-manager. It calls
// QueryFactorySystemDescriptor and returns the deserialized FSD so the
// caller can derive node labels from physical-location fields (hall,
// aisle, rack, shelf_u, motherboard) plus the per-host positional index.
//
// The FSD schema and orchestrator service definitions live under proto/;
// the generated Go bindings are checked in under fsdpb/ and orchestratorpb/.
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
	"google.golang.org/protobuf/proto"

	fsdpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/fsdpb"
	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

// ErrHostNotFound is returned by FindHost when the given hostname is not
// present in the FSD.
var ErrHostNotFound = errors.New("host not found in FSD")

// dialer abstracts grpc.NewClient so tests can substitute a bufconn.
type dialer func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// Client is a thin wrapper around the fabric-manager gRPC client that
// caches the last-fetched FSD for cachTTL to avoid hammering the server.
// A single Client is safe for concurrent use; Snapshot serializes fetches
// through a mutex.
type Client struct {
	address  string
	cacheTTL time.Duration
	dial     dialer

	mu         sync.Mutex
	cache      *fsdpb.FactorySystemDescriptor
	cacheStamp time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithCacheTTL sets the minimum interval between fabric-manager RPCs. A
// call to Snapshot within ttl of the last successful fetch returns the
// cached FSD without a network round trip.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) { c.cacheTTL = ttl }
}

// withDialer is a test hook.
func withDialer(d dialer) Option {
	return func(c *Client) { c.dial = d }
}

// NewClient constructs a Client. address is a host:port suitable for
// grpc.NewClient (e.g. "fabric-manager.tt.svc:50051").
func NewClient(address string, opts ...Option) *Client {
	c := &Client{
		address:  address,
		cacheTTL: 5 * time.Minute,
		dial:     grpc.NewClient,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Snapshot returns the current FSD, either from the cache (if still fresh)
// or by issuing a fresh QueryFactorySystemDescriptor RPC.
func (c *Client) Snapshot(ctx context.Context) (*fsdpb.FactorySystemDescriptor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache != nil && time.Since(c.cacheStamp) < c.cacheTTL {
		return c.cache, nil
	}

	fsd, err := c.fetch(ctx)
	if err != nil {
		return nil, err
	}
	c.cache = fsd
	c.cacheStamp = time.Now()
	return fsd, nil
}

func (c *Client) fetch(ctx context.Context) (*fsdpb.FactorySystemDescriptor, error) {
	conn, err := c.dial(c.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial fabric-manager %q: %w", c.address, err)
	}
	defer func() { _ = conn.Close() }()

	client := orchpb.NewFabricManagerServiceClient(conn)
	resp, err := client.QueryFactorySystemDescriptor(ctx,
		&orchpb.QueryFactorySystemDescriptorRequest{})
	if err != nil {
		return nil, fmt.Errorf("QueryFactorySystemDescriptor: %w", err)
	}
	if len(resp.GetFsdFile()) == 0 {
		return nil, errors.New("fabric-manager returned no FSDs")
	}

	// A fabric-manager can host multiple FSDs (one per scaleout config
	// file). For per-host labeling, all we need is the FSD that contains
	// our host, so parse each until we find one and fall back to the first.
	// We defer host resolution to the caller — return the first FSD here
	// and let FindHost handle "not this one". If callers need to search
	// across files, they can iterate GetFsdFile themselves.
	fsd := &fsdpb.FactorySystemDescriptor{}
	if err := proto.Unmarshal(resp.GetFsdFile()[0].GetTextprotoBytes(), fsd); err != nil {
		return nil, fmt.Errorf("unmarshal FSD: %w", err)
	}
	return fsd, nil
}

// FindHost looks up hostname in fsd.Hosts and returns the matching Host
// plus its positional (0-based) index. Matching is case-insensitive.
// Returns ErrHostNotFound if no host matches.
func FindHost(fsd *fsdpb.FactorySystemDescriptor, hostname string) (*fsdpb.FactorySystemDescriptor_Host, int, error) {
	if fsd == nil {
		return nil, 0, ErrHostNotFound
	}
	needle := strings.ToLower(hostname)
	for i, h := range fsd.GetHosts() {
		if strings.ToLower(h.GetHostname()) == needle {
			return h, i, nil
		}
	}
	return nil, 0, ErrHostNotFound
}
