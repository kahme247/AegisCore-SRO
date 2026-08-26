package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/aggregate"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/g3n/engine/math32"
)

type disconnectSaveResult int64

func (r disconnectSaveResult) LastInsertId() (int64, error) { return 0, nil }
func (r disconnectSaveResult) RowsAffected() (int64, error) { return int64(r), nil }

type disconnectSaveDB struct {
	calls  int
	failAt int
}

func (d *disconnectSaveDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.calls++
	if d.calls == d.failAt {
		return nil, errors.New("scripted repository failure")
	}
	return disconnectSaveResult(1), nil
}
func (*disconnectSaveDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*disconnectSaveDB) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

type disconnectSaveTransaction struct {
	db       frameworkdb.DBTX
	messages []aggregate.OutboxMessage
}

func (t *disconnectSaveTransaction) DB() frameworkdb.DBTX            { return t.db }
func (*disconnectSaveTransaction) CurrentVersion() aggregate.Version { return 0 }
func (t *disconnectSaveTransaction) Enqueue(message aggregate.OutboxMessage) error {
	if err := message.Validate(); err != nil {
		return err
	}
	t.messages = append(t.messages, message)
	return nil
}

type disconnectSaveCoordinator struct {
	db       *disconnectSaveDB
	results  map[aggregate.IdempotencyKey]aggregate.Result
	commands []aggregate.Command
	block    bool
}

func (c *disconnectSaveCoordinator) ExecuteLatest(ctx context.Context, command aggregate.Command, mutation aggregate.Mutation) (aggregate.Result, error) {
	c.commands = append(c.commands, command)
	if c.block {
		<-ctx.Done()
		return aggregate.Result{}, ctx.Err()
	}
	if result, exists := c.results[command.IdempotencyKey]; exists {
		result.Duplicate = true
		return result, nil
	}
	transaction := &disconnectSaveTransaction{db: c.db}
	payload, err := mutation(ctx, transaction)
	if err != nil {
		return aggregate.Result{}, err
	}
	result := aggregate.Result{Version: 1, Payload: payload}
	if c.results == nil {
		c.results = make(map[aggregate.IdempotencyKey]aggregate.Result)
	}
	c.results[command.IdempotencyKey] = result
	return result, nil
}

func disconnectSnapshot() DisconnectSaveSnapshot {
	return DisconnectSaveSnapshot{
		SessionID: "2f9cfec7-6ec4-4c20-a4dd-28a762cd2460", CharacterID: 42,
		HP: 100, MP: 80, RegionID: 25000, X: 1, Y: 2, Z: 3,
		COS: []model.COSRecord{{ID: 7, OwnerCharID: 42, RefObjectID: 9002, Kind: model.COSGrowthPet, Level: 1}},
	}
}

func TestDisconnectSaverPersistsOneIdempotentBoundary(t *testing.T) {
	database := &disconnectSaveDB{}
	coordinator := &disconnectSaveCoordinator{db: database}
	saver, _ := NewDisconnectSaver(coordinator, time.Second)
	first, err := saver.Save(context.Background(), disconnectSnapshot())
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	second, err := saver.Save(context.Background(), disconnectSnapshot())
	if err != nil {
		t.Fatalf("duplicate Save() error = %v", err)
	}
	if database.calls != 3 || len(coordinator.commands) != 2 || !second.Duplicate || first.Version != second.Version {
		t.Fatalf("calls=%d commands=%d first=%+v second=%+v", database.calls, len(coordinator.commands), first, second)
	}
	var payload map[string]int
	if err := json.Unmarshal(first.Payload, &payload); err != nil || payload["character_id"] != 42 || payload["cos_count"] != 1 {
		t.Fatalf("payload=%s error=%v", first.Payload, err)
	}
}

func TestDisconnectSaverStopsOnPartialRepositoryFailure(t *testing.T) {
	database := &disconnectSaveDB{failAt: 2}
	coordinator := &disconnectSaveCoordinator{db: database}
	saver, _ := NewDisconnectSaver(coordinator, time.Second)
	if _, err := saver.Save(context.Background(), disconnectSnapshot()); err == nil {
		t.Fatal("Save() error = nil, want repository failure")
	}
	if database.calls != 2 || len(coordinator.results) != 0 {
		t.Fatalf("calls=%d completed=%d, want stopped mutation without completion", database.calls, len(coordinator.results))
	}
}

func TestDisconnectSaverEnforcesOverallTimeout(t *testing.T) {
	coordinator := &disconnectSaveCoordinator{block: true}
	saver, _ := NewDisconnectSaver(coordinator, time.Millisecond)
	if _, err := saver.Save(context.Background(), disconnectSnapshot()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Save() error = %v, want deadline exceeded", err)
	}
}

func TestNewDisconnectSaveSnapshotCopiesLiveState(t *testing.T) {
	player := &model.Player{ID: 42, BaseStats: model.BaseStats{HP: 100, MP: 80}, SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 2, 3)}}}
	records := []model.COSRecord{{ID: 7, OwnerCharID: 42}}
	snapshot, err := NewDisconnectSaveSnapshot("session", player, records)
	if err != nil {
		t.Fatal(err)
	}
	records[0].ID = 99
	if snapshot.HP != 100 || snapshot.MP != 80 || snapshot.RegionID != 25000 || snapshot.COS[0].ID != 7 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
