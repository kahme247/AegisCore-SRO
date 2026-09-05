package service

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"sync"
	"time"
)

type MovementService struct {
}

var movementServiceInstance *MovementService
var movementServiceOnce sync.Once

func GetMovementServiceInstance() *MovementService {
	movementServiceOnce.Do(func() {
		movementServiceInstance = &MovementService{}
	})

	return movementServiceInstance
}

func (m *MovementService) MoveToPosition(player *model.Player, newPosition navmeshv2.RtNavmeshPosition) {
	currentTime := time.Now()
	world := GetWorldServiceInstance()
	movementEntity, speedOverride := mountedMovementEntity(world, player)
	startPosition := player.GetNavmeshPosition()
	movementData := model.MovementData{
		StartTime:      currentTime,
		UpdateTime:     currentTime,
		TargetPosition: newPosition,
		HasDestination: true,
		DirectionAngle: 0,
		SpeedOverride:  speedOverride,
	}
	registered := world.RegisterMovingCharacter(player, movementData)
	if movementEntity != player {
		movementEntity.SetMotionState(player.GetMotionState())
	}
	packet := model.BuildMovementUpdatePacket(movementEntity, registered, startPosition)
	player.SendToSelfAndKnown(&packet)
	traceMovementFor(player, "start uid=%d movement_uid=%d generation=%d from=%s target=%s", player.GetUniqueID(), movementEntity.GetUniqueID(), registered.Generation, startPosition.String(), newPosition.String())
}

func (m *MovementService) WalkToDirection(player *model.Player, heading float32) {
	currentTime := time.Now()
	world := GetWorldServiceInstance()
	movementEntity, speedOverride := mountedMovementEntity(world, player)
	startPosition := player.GetNavmeshPosition()
	movementData := model.MovementData{
		StartTime:      currentTime,
		UpdateTime:     currentTime,
		HasDestination: false,
		DirectionAngle: heading,
		SpeedOverride:  speedOverride,
	}

	registered := world.RegisterMovingCharacter(player, movementData)
	if movementEntity != player {
		movementEntity.SetMotionState(player.GetMotionState())
	}
	packet := model.BuildMovementUpdatePacket(movementEntity, registered, startPosition)
	// The initiating client already owns this directional movement request. A
	// B021 direction echo makes the stock client close its session, while
	// nearby observers still need the movement state for their local view.
	player.Broadcast(&packet)
	traceMovementFor(player, "direction uid=%d movement_uid=%d generation=%d heading=%d", player.GetUniqueID(), movementEntity.GetUniqueID(), registered.Generation, uint16(heading))
}

func (m *MovementService) Stop(player *model.Player, reason string) {
	GetWorldServiceInstance().StopMovingCharacter(player, player.GetNavmeshPosition(), reason, true)
}

func (m *MovementService) Turn(player *model.Player, heading float32) {
	world := GetWorldServiceInstance()
	position := player.GetNavmeshPosition()
	position.Heading = heading
	world.StopMovingCharacter(player, position, "turn", false)
	movementEntity, _ := mountedMovementEntity(world, player)
	packet := model.BuildTurnUpdatePacket(movementEntity)
	player.SendToSelfAndKnown(&packet)
}

func mountedMovementEntity(world *WorldService, player *model.Player) (model.ICharacter, float32) {
	if transport, mounted := world.MountedTransport(player); mounted {
		return transport, transport.GetMovementSpeed()
	}
	return player, 0
}
