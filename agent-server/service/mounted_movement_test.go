package service

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/g3n/engine/math32"
)

func TestMountedMovementUsesTransportIdentitySpeedAndPosition(t *testing.T) {
	start := visibilityTestPosition(25000, 100, 100)
	owner := newVisibilityTestPlayer(41, "mounted-owner", start)
	world := newVisibilityTestWorld(t, owner)
	world.positionPersistence = NewPositionPersistenceService(&fakePositionRepository{}, 30*time.Second)

	transport := &model.COS{
		NPC: model.NPC{
			SRObject: model.NewSRObject(),
			Type:     "COS",
			Mutex:    &sync.Mutex{},
		},
		OwnerCharID:   owner.ID,
		OwnerUniqueID: owner.GetUniqueID(),
		Kind:          model.COSRideTransport,
	}
	transport.SetUniqueID(9001)
	transport.SetNavmeshPosition(start)
	transport.SetRunSpeed(135)
	transport.KnownObjectList = model.NewKnownObjectList(transport)
	world.visibleObjects[transport.GetUniqueID()] = transport
	world.visibleObjectsByRegion[start.Region.ID] = append(world.visibleObjectsByRegion[start.Region.ID], transport)

	runtime := NewCOSService(nil, world)
	runtime.byUID[transport.GetUniqueID()] = transport
	runtime.byOwnerKind[owner.ID] = map[model.COSKind]*model.COS{model.COSRideTransport: transport}
	if err := owner.COSState.ActivateTransport(transport.GetUniqueID(), 2001, true); err != nil {
		t.Fatalf("ActivateTransport: %v", err)
	}
	world.ConfigureMountedTransportResolver(runtime.ResolveMountedTransport)

	movementEntity, speed := mountedMovementEntity(world, owner)
	if movementEntity != transport || speed != transport.GetMovementSpeed() {
		t.Fatalf("mounted authority = %T uid=%d speed=%v, want transport uid=%d speed=%v", movementEntity, movementEntity.GetUniqueID(), speed, transport.GetUniqueID(), transport.GetMovementSpeed())
	}

	target := visibilityTestPosition(25000, 450, 475)
	wire := model.BuildMovementUpdatePacket(movementEntity, model.MovementData{HasDestination: true, TargetPosition: target}, start)
	if got := binary.LittleEndian.Uint32(wire.Payload()[:4]); got != transport.GetUniqueID() {
		t.Fatalf("mounted B021 entity uid = %d, want transport %d", got, transport.GetUniqueID())
	}
	movement := world.RegisterMovingCharacter(owner, model.MovementData{
		StartTime:      time.Now(),
		UpdateTime:     time.Now(),
		TargetPosition: target,
		HasDestination: true,
		SpeedOverride:  speed,
	})
	if !world.commitMovementStep(owner, movement.Generation, target, time.Now(), true, "mounted-test", false) {
		t.Fatal("mounted owner movement did not commit")
	}
	ownerPosition := owner.GetNavmeshPosition()
	transportPosition := transport.GetNavmeshPosition()
	if math32.Abs(ownerPosition.Offset.X-transportPosition.Offset.X) > 0.001 || math32.Abs(ownerPosition.Offset.Z-transportPosition.Offset.Z) > 0.001 {
		t.Fatalf("mounted positions diverged: owner=%s transport=%s", ownerPosition.String(), transportPosition.String())
	}
	if transportPosition.Offset.X != target.Offset.X || transportPosition.Offset.Z != target.Offset.Z {
		t.Fatalf("transport remained at spawn: got=%s target=%s", transportPosition.String(), target.String())
	}
}
