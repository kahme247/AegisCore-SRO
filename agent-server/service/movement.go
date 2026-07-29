package service

import (
	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
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
	startPosition := player.GetNavmeshPosition()
	movementData := model.MovementData{
		StartTime:      currentTime,
		UpdateTime:     currentTime,
		TargetPosition: newPosition,
		HasDestination: true,
		DirectionAngle: 0,
	}
	registered := GetWorldServiceInstance().RegisterMovingCharacter(player, movementData)
	packet := model.BuildMovementUpdatePacket(player, registered, startPosition)
	player.SendToSelfAndKnown(&packet)
	traceMovementFor(player, "start uid=%d generation=%d from=%s target=%s", player.GetUniqueID(), registered.Generation, startPosition.String(), newPosition.String())
}

func (m *MovementService) WalkToDirection(player *model.Player, heading float32) {
	currentTime := time.Now()
	startPosition := player.GetNavmeshPosition()
	movementData := model.MovementData{
		StartTime:      currentTime,
		UpdateTime:     currentTime,
		HasDestination: false,
		DirectionAngle: heading,
	}

	registered := GetWorldServiceInstance().RegisterMovingCharacter(player, movementData)
	packet := model.BuildMovementUpdatePacket(player, registered, startPosition)
	// The initiating client already owns this directional movement request. A
	// B021 direction echo makes the stock client close its session, while
	// nearby observers still need the movement state for their local view.
	player.Broadcast(&packet)
	traceMovementFor(player, "direction uid=%d generation=%d heading=%d", player.GetUniqueID(), registered.Generation, uint16(heading))
}

func (m *MovementService) Stop(player *model.Player, reason string) {
	GetWorldServiceInstance().StopMovingCharacter(player, player.GetNavmeshPosition(), reason, true)
}

func (m *MovementService) Turn(player *model.Player, heading float32) {
	position := player.GetNavmeshPosition()
	position.Heading = heading
	GetWorldServiceInstance().StopMovingCharacter(player, position, "turn", true)
}
