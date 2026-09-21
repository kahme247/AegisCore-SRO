package handler

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	"github.com/Harvi8/AegisCore/gateway-server/db"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

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
	if _, err := (vsro188.GatewayShardListRequestCodec{}).Decode(packet); err != nil {
		return fmt.Errorf("malformed %s request: %w", name, err)
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
