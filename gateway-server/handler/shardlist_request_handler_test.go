package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/gateway-server/config"
	"github.com/ferdoran/go-sro/gateway-server/db"
)

type readinessProbeFunc func(context.Context) error

func (f readinessProbeFunc) Ready(ctx context.Context) error { return f(ctx) }

func TestAgentReadinessIsCachedAndOfflineProjectionDoesNotMutateCatalog(t *testing.T) {
	calls := 0
	cache := newCachedAgentReadiness(readinessProbeFunc(func(context.Context) error {
		calls++
		return errors.New("Agent unavailable")
	}), time.Minute)
	if cache.isReady(context.Background()) || cache.isReady(context.Background()) {
		t.Fatal("unavailable Agent reported ready")
	}
	if calls != 1 {
		t.Fatalf("readiness probe calls = %d, want 1 cached call", calls)
	}
	original := []db.Shard{{Id: 1, Status: shardStatusOperating}}
	offline := markShardsOffline(original)
	if offline[0].Status != shardStatusOffline || original[0].Status != shardStatusOperating {
		t.Fatal("offline projection mutated the repository result")
	}
}

func TestAgentReadinessDoesNotHoldLockOrUseRequestContextDuringProbe(t *testing.T) {
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	cache := newCachedAgentReadiness(readinessProbeFunc(func(ctx context.Context) error {
		close(probeStarted)
		select {
		case <-releaseProbe:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}), time.Minute)

	requestContext, cancelRequest := context.WithCancel(context.Background())
	firstResult := make(chan bool, 1)
	go func() { firstResult <- cache.isReady(requestContext) }()
	<-probeStarted
	cancelRequest()
	if <-firstResult {
		t.Fatal("canceled request reported Agent ready before the probe completed")
	}

	secondResult := make(chan bool, 1)
	go func() { secondResult <- cache.isReady(context.Background()) }()
	close(releaseProbe)
	if !<-secondResult {
		t.Fatal("request cancellation poisoned the shared readiness result")
	}
	if !cache.isReady(context.Background()) {
		t.Fatal("successful readiness result was not cached")
	}
}

func TestBuildShardlistResponseUsesVSRONetworkLayout(t *testing.T) {
	packet, err := buildShardlistResponse(config.Farm{ID: 1, Name: "SRO_DEUTSCHLAND"}, []db.Shard{{
		Id:            1,
		ContentId:     122,
		Name:          "GoSRO",
		Capacity:      1000,
		OnlinePlayers: 0,
		Status:        1,
	}})
	if err != nil {
		t.Fatalf("buildShardlistResponse() error = %v", err)
	}

	if packet.MessageID != opcode.ShardlistResponse {
		t.Fatalf("message ID = 0x%04X, want 0x%04X", packet.MessageID, opcode.ShardlistResponse)
	}

	globalOperationFlag, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if globalOperationFlag != 1 {
		t.Fatalf("global operation flag = %d, want 1", globalOperationFlag)
	}

	globalOperationType, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if globalOperationType != 1 {
		t.Fatalf("global operation type = %d, want 1", globalOperationType)
	}

	globalOperationName, err := packet.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if globalOperationName != "SRO_DEUTSCHLAND" {
		t.Fatalf("global operation name = %q, want SRO_DEUTSCHLAND", globalOperationName)
	}

	operationFlag, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if operationFlag != 0 {
		t.Fatalf("operation flag = %d, want 0", operationFlag)
	}

	shardFlag, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if shardFlag != 1 {
		t.Fatalf("shard flag = %d, want 1", shardFlag)
	}

	shardID, err := packet.ReadUInt16()
	if err != nil {
		t.Fatal(err)
	}
	if shardID != 1 {
		t.Fatalf("shard ID = %d, want 1", shardID)
	}

	shardName, err := packet.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if shardName != "GoSRO" {
		t.Fatalf("shard name = %q, want GoSRO", shardName)
	}

	if _, err = packet.ReadUInt16(); err != nil {
		t.Fatal(err)
	}
	if _, err = packet.ReadUInt16(); err != nil {
		t.Fatal(err)
	}
	if _, err = packet.ReadByte(); err != nil {
		t.Fatal(err)
	}

	globalOperationID, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if globalOperationID != 1 {
		t.Fatalf("global operation ID = %d, want 1", globalOperationID)
	}

	finalShardFlag, err := packet.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if finalShardFlag != 0 {
		t.Fatalf("final shard flag = %d, want 0", finalShardFlag)
	}
}

func TestBuildShardlistResponseRejectsValuesThatWouldWrapOnWire(t *testing.T) {
	farm := config.Farm{ID: 1, Name: "Farm"}
	tests := []struct {
		name  string
		shard db.Shard
	}{
		{name: "zero id", shard: db.Shard{ContentId: 22, Name: "GoSRO", Capacity: 1, Status: 1}},
		{name: "zero content", shard: db.Shard{Id: 1, Name: "GoSRO", Capacity: 1, Status: 1}},
		{name: "unknown status", shard: db.Shard{Id: 1, ContentId: 22, Name: "GoSRO", Capacity: 1, Status: 2}},
		{name: "negative players", shard: db.Shard{Id: 1, ContentId: 22, Name: "GoSRO", Capacity: 1, OnlinePlayers: -1, Status: 1}},
		{name: "over capacity", shard: db.Shard{Id: 1, ContentId: 22, Name: "GoSRO", Capacity: 1, OnlinePlayers: 2, Status: 1}},
		{name: "uint16 overflow", shard: db.Shard{Id: 1, ContentId: 22, Name: "GoSRO", Capacity: 65536, Status: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildShardlistResponse(farm, []db.Shard{test.shard}); err == nil {
				t.Fatal("buildShardlistResponse() error = nil, want validation failure")
			}
		})
	}
}

func TestBuildShardlistResponseOrdersShardsByIDAndRejectsDuplicates(t *testing.T) {
	farm := config.Farm{ID: 1, Name: "Farm"}
	shard := func(id int, name string) db.Shard {
		return db.Shard{Id: id, ContentId: 22, Name: name, Capacity: 100, Status: shardStatusOperating}
	}
	packet, err := buildShardlistResponse(farm, []db.Shard{shard(2, "Second"), shard(1, "First")})
	if err != nil {
		t.Fatalf("buildShardlistResponse() error = %v", err)
	}
	if _, err := packet.ReadByte(); err != nil { // farm flag
		t.Fatal(err)
	}
	if _, err := packet.ReadByte(); err != nil { // farm ID
		t.Fatal(err)
	}
	if _, err := packet.ReadString(); err != nil { // farm name
		t.Fatal(err)
	}
	if _, err := packet.ReadByte(); err != nil { // farm terminator
		t.Fatal(err)
	}
	for _, wantID := range []uint16{1, 2} {
		flag, err := packet.ReadByte()
		if err != nil || flag != 1 {
			t.Fatalf("shard flag = %d, err = %v", flag, err)
		}
		gotID, err := packet.ReadUInt16()
		if err != nil || gotID != wantID {
			t.Fatalf("shard ID = %d, err = %v, want %d", gotID, err, wantID)
		}
		if _, err := packet.ReadString(); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 2; index++ {
			if _, err := packet.ReadUInt16(); err != nil {
				t.Fatal(err)
			}
		}
		for index := 0; index < 2; index++ {
			if _, err := packet.ReadByte(); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, err := buildShardlistResponse(farm, []db.Shard{shard(1, "First"), shard(1, "Duplicate")}); err == nil {
		t.Fatal("duplicate shard IDs accepted")
	}
}

func TestValidateEmptyGatewayRequestRejectsTrailingBytes(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(1); err != nil {
		t.Fatal(err)
	}
	if err := validateEmptyGatewayRequest(packet, "shard-list"); err == nil {
		t.Fatal("validateEmptyGatewayRequest() error = nil, want trailing data rejection")
	}
}

func TestShardlistRequestHandlerUsesInjectedCatalogAndPropagatesFailures(t *testing.T) {
	want := errors.New("catalog unavailable")
	contextKey := struct{}{}
	requestContext := context.WithValue(context.Background(), contextKey, "request")
	var receivedContext context.Context
	handler := NewShardlistRequestHandler(
		func() (config.Farm, error) { return config.Farm{ID: 1, Name: "Farm"}, nil },
		db.ShardCatalogFunc(func(ctx context.Context) ([]db.Shard, error) {
			receivedContext = ctx
			return nil, want
		}),
	)
	err := handler.Handle(requestContext, nil, network.EmptyClientPacket())
	if !errors.Is(err, want) {
		t.Fatalf("Handle error = %v, want wrapped %v", err, want)
	}
	if receivedContext != requestContext {
		t.Fatal("handler did not pass the session context to the injected catalog")
	}
}

func TestShardsForContentIDDoesNotAdvertiseIncompatibleShards(t *testing.T) {
	shards := []db.Shard{
		{Id: 1, ContentId: 22, Name: "Vietnam"},
		{Id: 2, ContentId: 56, Name: "Other"},
		{Id: 3, ContentId: 22, Name: "Vietnam Two"},
	}
	filtered := shardsForContentID(shards, 22)
	if len(filtered) != 2 || filtered[0].Id != 1 || filtered[1].Id != 3 {
		t.Fatalf("filtered shards = %#v", filtered)
	}
	if got := shardsForContentID(shards, 0); len(got) != 0 {
		t.Fatalf("zero-content filter returned %#v", got)
	}
}
