package handler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	"github.com/Harvi8/AegisCore/gateway-server/db"
)

const (
	shardStatusOffline   = 0
	shardStatusOperating = 1
)

// ShardlistRequestHandler retains the established farm/shard wire layout, but
// validates operator-provided values before narrowing them into uint16/byte
// fields. Invalid configuration therefore cannot wrap into a misleading list.
type ShardlistRequestHandler struct {
	LoadFarm    func() (config.Farm, error)
	LoadProfile func() (config.ProtocolProfile, error)
	Shards      db.ShardCatalog
	Readiness   *cachedAgentReadiness
}

type cachedAgentReadiness struct {
	probe    auth.AdmissionReadinessProbe
	ttl      time.Duration
	mu       sync.Mutex
	checked  time.Time
	ready    bool
	inFlight chan struct{}
}

func newCachedAgentReadiness(probe auth.AdmissionReadinessProbe, ttl time.Duration) *cachedAgentReadiness {
	if probe == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Second
	}
	return &cachedAgentReadiness{probe: probe, ttl: ttl}
}

func (c *cachedAgentReadiness) isReady(ctx context.Context) bool {
	if c == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if !c.checked.IsZero() && time.Since(c.checked) < c.ttl {
		ready := c.ready
		c.mu.Unlock()
		return ready
	}
	if c.inFlight == nil {
		c.inFlight = make(chan struct{})
		go c.refresh()
	}
	done := c.inFlight
	c.mu.Unlock()

	select {
	case <-done:
		c.mu.Lock()
		ready := c.ready
		c.mu.Unlock()
		return ready
	case <-ctx.Done():
		return false
	}
}

func (c *cachedAgentReadiness) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), auth.DefaultAdmissionTicketInstallTimeout)
	defer cancel()
	ready := c.probe.Ready(ctx) == nil

	c.mu.Lock()
	done := c.inFlight
	c.ready = ready
	c.checked = time.Now()
	c.inFlight = nil
	close(done)
	c.mu.Unlock()
}

func NewShardlistRequestHandler(
	loadFarm func() (config.Farm, error),
	shards db.ShardCatalog,
) *ShardlistRequestHandler {
	if loadFarm == nil {
		loadFarm = config.LoadFarm
	}
	if shards == nil {
		shards = db.LegacyShardCatalog()
	}
	return &ShardlistRequestHandler{LoadFarm: loadFarm, LoadProfile: config.LoadProtocolProfile, Shards: shards}
}

// RegisterShardlistRequestHandlerWithRepository installs a shard-list handler
// backed by an injected context-aware catalog. Gateway startup should pass a
// ShardRepository constructed from its long-lived account pool here.
func RegisterShardlistRequestHandlerWithRepository(target *server.Server, shards db.ShardCatalog) *ShardlistRequestHandler {
	return RegisterShardlistRequestHandlerWithReadiness(target, shards, nil)
}

func RegisterShardlistRequestHandlerWithReadiness(target *server.Server, shards db.ShardCatalog, probe auth.AdmissionReadinessProbe) *ShardlistRequestHandler {
	if target == nil {
		return nil
	}
	handler := NewShardlistRequestHandler(config.LoadFarm, shards)
	handler.Readiness = newCachedAgentReadiness(probe, time.Second)
	target.RegisterHandler(opcode.ShardlistRequest, handler.Handle)
	return handler
}

// RegisterShardlistRequestHandler is retained for old startup wiring. New
// startup code should call RegisterShardlistRequestHandlerWithRepository so
// the handler does not allocate a short-lived connection for each request.
func RegisterShardlistRequestHandler(target *server.Server) {
	RegisterShardlistRequestHandlerWithRepository(target, db.LegacyShardCatalog())
}

func (h *ShardlistRequestHandler) Handle(ctx context.Context, session *server.Session, packet network.Packet) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateEmptyGatewayRequest(packet, "shard-list"); err != nil {
		return err
	}
	loadFarm := h.LoadFarm
	if loadFarm == nil {
		loadFarm = config.LoadFarm
	}
	farm, err := loadFarm()
	if err != nil {
		return fmt.Errorf("load gateway farm: %w", err)
	}
	shards := h.Shards
	if shards == nil {
		shards = db.LegacyShardCatalog()
	}
	loadedShards, err := shards.List(ctx)
	if err != nil {
		return fmt.Errorf("load gateway shards: %w", err)
	}
	loadProfile := h.LoadProfile
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	profile, err := loadProfile()
	if err != nil {
		return fmt.Errorf("load gateway shard-list profile: %w", err)
	}
	compatibleShards := shardsForContentID(loadedShards, profile.ContentID)
	if !h.Readiness.isReady(ctx) {
		compatibleShards = markShardsOffline(compatibleShards)
	}
	response, err := buildShardlistResponse(farm, compatibleShards)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("send shard-list response: missing session")
	}
	return session.Send(response)
}
