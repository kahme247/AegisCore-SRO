package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/aggregate"
)

const DefaultDisconnectSaveTimeout = 2 * time.Second

// DisconnectSaveSnapshot is captured only after the session's packet workers
// have drained. It contains every durable value currently owned by the live
// character that must cross the disconnect boundary together.
type DisconnectSaveSnapshot struct {
	SessionID   string
	CharacterID int
	HP          int
	MP          int
	RegionID    int16
	X           float32
	Y           float32
	Z           float32
	COS         []model.COSRecord
}

func NewDisconnectSaveSnapshot(sessionID string, player *model.Player, cosRecords []model.COSRecord) (DisconnectSaveSnapshot, error) {
	if sessionID == "" || player == nil || player.ID <= 0 {
		return DisconnectSaveSnapshot{}, errors.New("disconnect save requires a session and character")
	}
	hp, mp := player.CurrentVitals()
	position := player.GetNavmeshPosition()
	return DisconnectSaveSnapshot{
		SessionID: sessionID, CharacterID: player.ID, HP: hp, MP: mp,
		RegionID: position.Region.ID, X: position.Offset.X, Y: position.Offset.Y, Z: position.Offset.Z,
		COS: append([]model.COSRecord(nil), cosRecords...),
	}, nil
}

type DisconnectSaver struct {
	coordinator aggregate.LatestCoordinator
	timeout     time.Duration
}

func NewDisconnectSaver(coordinator aggregate.LatestCoordinator, timeout time.Duration) (*DisconnectSaver, error) {
	if coordinator == nil {
		return nil, errors.New("disconnect save coordinator is required")
	}
	if timeout <= 0 {
		timeout = DefaultDisconnectSaveTimeout
	}
	return &DisconnectSaver{coordinator: coordinator, timeout: timeout}, nil
}

// Save owns one overall deadline and one SQL transaction for vitals,
// position, COS records, idempotency, aggregate version, and outbox record.
func (s *DisconnectSaver) Save(parent context.Context, snapshot DisconnectSaveSnapshot) (aggregate.Result, error) {
	if s == nil || s.coordinator == nil {
		return aggregate.Result{}, errors.New("disconnect saver is not configured")
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	aggregateID := aggregate.ID{Kind: aggregate.Character, Value: strconv.Itoa(snapshot.CharacterID)}
	command := aggregate.Command{
		Aggregate: aggregateID, IdempotencyKey: aggregate.IdempotencyKey("disconnect:" + snapshot.SessionID),
		Name: "character.disconnect-save",
	}
	return s.coordinator.ExecuteLatest(ctx, command, func(ctx context.Context, transaction aggregate.Transaction) (json.RawMessage, error) {
		vitals, err := model.NewCharacterVitalsRepository(transaction.DB())
		if err != nil {
			return nil, err
		}
		positions, err := model.NewCharacterPositionRepository(transaction.DB())
		if err != nil {
			return nil, err
		}
		cosRepository, err := model.NewCOSRepository(transaction.DB())
		if err != nil {
			return nil, err
		}
		if err := vitals.SaveVitals(ctx, snapshot.CharacterID, snapshot.HP, snapshot.MP); err != nil {
			return nil, err
		}
		if err := positions.UpdateCharacterPosition(ctx, snapshot.CharacterID, snapshot.RegionID, snapshot.X, snapshot.Y, snapshot.Z); err != nil {
			return nil, err
		}
		for _, record := range snapshot.COS {
			if record.OwnerCharID != snapshot.CharacterID {
				return nil, fmt.Errorf("COS %d does not belong to disconnecting character", record.ID)
			}
			if err := cosRepository.Save(ctx, record); err != nil {
				return nil, err
			}
		}
		payload, err := json.Marshal(struct {
			CharacterID int `json:"character_id"`
			COSCount    int `json:"cos_count"`
		}{snapshot.CharacterID, len(snapshot.COS)})
		if err != nil {
			return nil, err
		}
		if err := transaction.Enqueue(aggregate.OutboxMessage{
			ID: "disconnect:" + snapshot.SessionID, Aggregate: aggregateID,
			Topic: "character.disconnect-saved", Payload: payload, OccurredAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		return payload, nil
	})
}
