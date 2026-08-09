package handler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

func markShardsOffline(shards []db.Shard) []db.Shard {
	offline := append([]db.Shard(nil), shards...)
	for index := range offline {
		offline[index].Status = shardStatusOffline
	}
	return offline
}

func shardsForContentID(shards []db.Shard, contentID byte) []db.Shard {
	if contentID == 0 {
		return nil
	}
	filtered := make([]db.Shard, 0, len(shards))
	for _, shard := range shards {
		if shard.ContentId == int(contentID) {
			filtered = append(filtered, shard)
		}
	}
	return filtered
}

func validateEmptyGatewayRequest(packet network.Packet, name string) error {
	if packet.Remaining() != 0 {
		return fmt.Errorf("malformed %s request: unexpected payload", name)
	}
	return nil
}

// buildShardlistResponse serializes the vSRO 0xA101 server-list response.
// The farm loop followed by the shard loop is intentionally unchanged from
// the documented layout; only value validation and checked writes were added.
func buildShardlistResponse(farm config.Farm, shards []db.Shard) (network.Packet, error) {
	if err := config.ValidateFarm(farm); err != nil {
		return network.Packet{}, fmt.Errorf("invalid gateway farm: %w", err)
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.ShardlistResponse
	if err := p.WriteByte(1); err != nil { // GlobalOperationFlag: next farm
		return network.Packet{}, err
	}
	if err := p.WriteByte(farm.ID); err != nil { // GlobalOperation.Type / Farm.ID
		return network.Packet{}, err
	}
	if err := p.WriteString(farm.Name); err != nil { // GlobalOperation.Name / Farm.Name
		return network.Packet{}, err
	}
	if err := p.WriteByte(0); err != nil { // GlobalOperationFlag: end farms
		return network.Packet{}, err
	}

	orderedShards := append([]db.Shard(nil), shards...)
	sort.SliceStable(orderedShards, func(left, right int) bool {
		return orderedShards[left].Id < orderedShards[right].Id
	})
	previousID := 0
	for index, shard := range orderedShards {
		if err := validateShard(shard, farm.ID); err != nil {
			return network.Packet{}, fmt.Errorf("invalid shard %d: %w", index, err)
		}
		if shard.Id == previousID {
			return network.Packet{}, fmt.Errorf("invalid shard %d: duplicate ID %d", index, shard.Id)
		}
		previousID = shard.Id
		if err := p.WriteByte(1); err != nil { // ShardFlag: next shard
			return network.Packet{}, err
		}
		if err := p.WriteUInt16(uint16(shard.Id)); err != nil {
			return network.Packet{}, err
		}
		if err := p.WriteString(shard.Name); err != nil {
			return network.Packet{}, err
		}
		if err := p.WriteUInt16(uint16(shard.OnlinePlayers)); err != nil {
			return network.Packet{}, err
		}
		if err := p.WriteUInt16(uint16(shard.Capacity)); err != nil {
			return network.Packet{}, err
		}
		if err := p.WriteByte(byte(shard.Status)); err != nil {
			return network.Packet{}, err
		}
		if err := p.WriteByte(farm.ID); err != nil { // GlobalOperationID / Farm.ID
			return network.Packet{}, err
		}
	}

	if err := p.WriteByte(0); err != nil { // ShardFlag: end shards
		return network.Packet{}, err
	}
	return p, nil
}

func validateShard(shard db.Shard, farmID byte) error {
	if farmID == 0 {
		return fmt.Errorf("missing farm ID")
	}
	if shard.Id <= 0 || shard.Id > math.MaxUint16 {
		return fmt.Errorf("ID must be 1..%d", math.MaxUint16)
	}
	if shard.ContentId <= 0 || shard.ContentId > math.MaxUint8 {
		return fmt.Errorf("content ID must be 1..%d", math.MaxUint8)
	}
	if shard.Name == "" || strings.TrimSpace(shard.Name) != shard.Name || strings.IndexByte(shard.Name, 0) >= 0 {
		return fmt.Errorf("name must be a non-blank, NUL-free value")
	}
	if !utf8.ValidString(shard.Name) || len(shard.Name) > math.MaxUint16 {
		return fmt.Errorf("name is not serializable")
	}
	if shard.Capacity <= 0 || shard.Capacity > math.MaxUint16 {
		return fmt.Errorf("capacity must be 1..%d", math.MaxUint16)
	}
	if shard.OnlinePlayers < 0 || shard.OnlinePlayers > shard.Capacity || shard.OnlinePlayers > math.MaxUint16 {
		return fmt.Errorf("online player count must be 0..capacity")
	}
	if shard.Status != shardStatusOffline && shard.Status != shardStatusOperating {
		return fmt.Errorf("status must be %d (offline) or %d (operating)", shardStatusOffline, shardStatusOperating)
	}
	return nil
}
