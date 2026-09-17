package character

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type TAPHandler struct {
	repository model.TAPRepository
	world      selectEntityWorld
	send       func(*server.Session, network.Packet) error
	now        func() time.Time
	mu         sync.Mutex
	snapshots  map[*server.Session][]uint64
}

func RegisterTAPHandler(target *server.Server, repository model.TAPRepository, world selectEntityWorld) (*TAPHandler, error) {
	if target == nil || repository == nil || world == nil {
		return nil, fmt.Errorf("configure TAP handler: dependencies are required")
	}
	h := &TAPHandler{repository: repository, world: world, now: time.Now, snapshots: make(map[*server.Session][]uint64)}
	target.RegisterHandler(opcode.TapInfoRequest, h.handleInfo)
	target.RegisterHandler(opcode.TapUpdateRequest, h.handleUpdate)
	return h, nil
}

func (h *TAPHandler) handleInfo(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeTAPInfoRequest(&packet)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("invalid TAP session")
	}
	var npcRefObjectID uint32
	if !request.Icon {
		player, resolveErr := resolveSelectingPlayer(h.world, session)
		if resolveErr != nil {
			return fmt.Errorf("resolve TAP player: %w", resolveErr)
		}
		if validateErr := validateNPCSelection(h.world, player, request.Reference); validateErr != nil {
			return fmt.Errorf("validate TAP NPC: %w", validateErr)
		}
		npc, lookupErr := h.world.GetObjectByUniqueId(request.Reference)
		if lookupErr != nil || npc == nil || npc.GetRefObjectID() == 0 {
			return fmt.Errorf("resolve TAP NPC reference")
		}
		npcRefObjectID = npc.GetRefObjectID()
	}
	records, err := h.repository.Snapshot(ctx, npcRefObjectID)
	if err != nil {
		return err
	}
	entries := make([]vsro188.TAPInfoEntry, len(records))
	ids := make([]uint64, len(records))
	active := false
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	for index, record := range records {
		ids[index] = record.ID
		entries[index] = vsro188.TAPInfoEntry{
			Type:              vsro188.TAPEntryType(record.EntryType),
			From:              tapWireTime(record.From),
			To:                tapWireTime(record.To),
			TraderUnionPoints: record.TraderUnionPoints,
			ThiefUnionPoints:  record.ThiefUnionPoints,
		}
		active = active || model.TAPRecordActive(record, now)
	}
	response, err := vsro188.BuildTAPInfoResponse(entries)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.snapshots[session] = ids
	h.mu.Unlock()
	if err = h.sendPacket(session, response); err != nil {
		return err
	}
	return h.sendPacket(session, vsro188.BuildTAPIcon(active))
}

func (h *TAPHandler) handleUpdate(ctx context.Context, session *server.Session, packet network.Packet) error {
	if err := vsro188.DecodeTAPUpdateRequest(&packet); err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("invalid TAP session")
	}
	h.mu.Lock()
	ids, exists := h.snapshots[session]
	ids = append([]uint64(nil), ids...)
	h.mu.Unlock()
	if !exists {
		return fmt.Errorf("TAP update requested before info snapshot")
	}
	points, err := h.repository.Points(ctx, ids)
	if err != nil {
		return err
	}
	entries := make([]vsro188.TAPUpdateEntry, len(points))
	for index, value := range points {
		entries[index] = vsro188.TAPUpdateEntry{TraderUnionPoints: value.Trader, ThiefUnionPoints: value.Thief}
	}
	return h.sendPacket(session, vsro188.BuildTAPUpdateResponse(entries))
}

func tapWireTime(value model.TAPClock) vsro188.TAPTime {
	return vsro188.TAPTime{Month: value.Month, Day: value.Day, Hour: value.Hour, Minute: value.Minute}
}

func (h *TAPHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
