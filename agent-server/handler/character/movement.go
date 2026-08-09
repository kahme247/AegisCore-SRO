package character

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

type MovementHandler struct {
	// The legacy worker selected all three movement opcodes from one
	// goroutine. Keep that cross-opcode serialization while the registry keeps
	// command order within each session.
	mu           sync.Mutex
	cancelCombat func(*server.Session, uint32)
}

func RegisterMovementHandler(target *server.Server) *MovementHandler {
	handler := &MovementHandler{}
	target.RegisterHandler(opcode.EntityMovementRequest, handler.handleMovementPacket)
	target.RegisterHandler(opcode.EntityMovementForwardRequest, handler.handleForwardPacket)
	target.RegisterHandler(opcode.EntityMovementTurnRequest, handler.handleTurnPacket)
	return handler
}

func (mh *MovementHandler) ConfigureCombatCancellation(cancel func(*server.Session, uint32)) {
	if mh != nil {
		mh.cancelCombat = cancel
	}
}

func (mh *MovementHandler) cancelPlayerCombat(session *server.Session, uniqueID uint32) {
	if mh != nil && mh.cancelCombat != nil {
		mh.cancelCombat(session, uniqueID)
	}
}

func (mh *MovementHandler) handleMovementPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.handleMovement(server.PacketChannelData{Session: session, Packet: packet})
	return nil
}

func (mh *MovementHandler) handleForwardPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.handleForward(server.PacketChannelData{Session: session, Packet: packet})
	return nil
}

func (mh *MovementHandler) handleTurnPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.handleTurn(server.PacketChannelData{Session: session, Packet: packet})
	return nil
}

func (mh *MovementHandler) handleMovement(data server.PacketChannelData) {
	movementService := service.GetMovementServiceInstance()
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(data.UserContext.UniqueID)
	if err != nil {
		logrus.Warnf("movement request for unknown player %d: %v", data.UserContext.UniqueID, err)
		return
	}
	hasDestination, err := data.ReadBool()
	if err != nil {
		mh.rejectMalformed(player, "movement type", err)
		return
	}
	if !hasDestination {
		source, heading, directionErr := parseDirectionalMovementPacket(&data.Packet)
		if directionErr != nil {
			mh.rejectMalformed(player, "direction movement", directionErr)
			return
		}
		if source == 0 {
			mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
			movementService.Turn(player, float32(heading))
		} else {
			mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
			movementService.WalkToDirection(player, float32(heading))
		}
		return
	}

	regionID, offset, err := parseDestinationMovementPacket(&data.Packet)
	if err != nil {
		mh.rejectMalformed(player, "destination movement", err)
		return
	}
	region, err := world.GetRegion(regionID)
	if err != nil {
		mh.rejectMalformed(player, "destination region lookup", err)
		return
	}
	targetCell, err := region.ResolveCell(offset)
	if err != nil {
		mh.rejectMalformed(player, "destination cell", err)
		return
	}
	// The client and character record use the calibrated world Y coordinate.
	// Navmesh height samples are useful for topology/collision, but their scale
	// is not interchangeable with the client protocol's vertical coordinate.
	// Replacing this value makes a moving character sink below the rendered map.
	targetPosition := navmeshv2.RtNavmeshPosition{Offset: offset, Region: region.Region, Cell: &targetCell}
	mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
	movementService.MoveToPosition(player, targetPosition)
	logrus.Tracef("moving %s to position %d (%g|%g|%g)", player.CharName, regionID, offset.X, offset.Y, offset.Z)
}

// parseDestinationMovementPacket reads the binary-confirmed normal-world
// payload after the leading true destination flag of 0x7021. Dungeon regions
// remain unsupported until their navmesh and movement authority are loaded.
func parseDestinationMovementPacket(packet *network.Packet) (int16, *math32.Vector3, error) {
	if packet == nil {
		return 0, nil, fmt.Errorf("missing destination packet")
	}
	regionID, err := packet.ReadInt16()
	if err != nil {
		return 0, nil, err
	}
	if regionID < 0 {
		return 0, nil, fmt.Errorf("dungeon region %d is unsupported", regionID)
	}
	if packet.Remaining() != 6 {
		return 0, nil, fmt.Errorf("region %d has destination payload length %d, want 6", regionID, packet.Remaining())
	}
	x, xErr := packet.ReadInt16()
	y, yErr := packet.ReadInt16()
	z, zErr := packet.ReadInt16()
	if err := firstError(xErr, yErr, zErr); err != nil {
		return 0, nil, err
	}
	return regionID, math32.NewVector3(float32(x), float32(y), float32(z)), nil
}

// parseDirectionalMovementPacket reads the payload after the leading false
// destination flag of 0x7021. The pinned client uses the compact form;
// the sourced variant is retained for older emulator clients.
func parseDirectionalMovementPacket(packet *network.Packet) (byte, uint16, error) {
	if packet == nil {
		return 0, 0, fmt.Errorf("missing directional packet")
	}
	source := byte(1) // `00 | heading:uint16` means walk in that direction.
	switch packet.Remaining() {
	case 2:
		// Exact stock-client form.
	case 3:
		var err error
		source, err = packet.ReadByte()
		if err != nil {
			return 0, 0, err
		}
	default:
		return 0, 0, fmt.Errorf("invalid directional payload length %d", packet.Remaining())
	}
	heading, err := packet.ReadUInt16()
	if err != nil {
		return 0, 0, err
	}
	return source, heading, nil
}

func (mh *MovementHandler) handleForward(data server.PacketChannelData) {
	movementService := service.GetMovementServiceInstance()
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(data.UserContext.UniqueID)
	if err != nil {
		logrus.Warnf("forward request for unknown player %d: %v", data.UserContext.UniqueID, err)
		return
	}
	action, heading, err := parseForwardPacket(&data.Packet, uint16(player.GetNavmeshPosition().Heading))
	if err != nil {
		mh.rejectMalformed(player, "forward action", err)
		return
	}
	if action == 0 {
		mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
		movementService.Stop(player, "forward-release")
	} else {
		mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
		movementService.WalkToDirection(player, float32(heading))
	}
}

func (mh *MovementHandler) handleTurn(data server.PacketChannelData) {
	movementService := service.GetMovementServiceInstance()
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(data.UserContext.UniqueID)
	if err != nil {
		logrus.Warnf("turn request for unknown player %d: %v", data.UserContext.UniqueID, err)
		return
	}
	heading, err := parseTurnPacket(&data.Packet)
	if err != nil {
		mh.rejectMalformed(player, "turn heading", err)
		return
	}
	mh.cancelPlayerCombat(data.Session, player.GetUniqueID())
	movementService.Turn(player, float32(heading))
}

func parseForwardPacket(packet *network.Packet, fallbackHeading uint16) (byte, uint16, error) {
	if packet.DataSize != 1 && packet.DataSize != 3 {
		return 0, 0, fmt.Errorf("invalid forward payload length %d", packet.DataSize)
	}
	action, err := packet.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	heading := fallbackHeading
	if packet.DataSize >= 3 {
		heading, err = packet.ReadUInt16()
		if err != nil {
			return 0, 0, err
		}
	}
	return action, heading, nil
}

func parseTurnPacket(packet *network.Packet) (uint16, error) {
	if packet.DataSize != 2 && packet.DataSize != 3 {
		return 0, fmt.Errorf("invalid turn payload length %d", packet.DataSize)
	}
	if packet.DataSize == 3 {
		if _, err := packet.ReadByte(); err != nil {
			return 0, err
		}
	}
	return packet.ReadUInt16()
}

func (mh *MovementHandler) rejectMalformed(player interface {
	GetNavmeshPosition() navmeshv2.RtNavmeshPosition
	GetUniqueID() uint32
}, field string, err error) {
	logrus.Warnf("rejected malformed movement packet uid=%d field=%s: %v", player.GetUniqueID(), field, err)
	if concretePlayer, lookupErr := service.GetWorldServiceInstance().GetPlayerByUniqueId(player.GetUniqueID()); lookupErr == nil {
		service.GetMovementServiceInstance().Stop(concretePlayer, "malformed-packet")
	}
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

//func walkToDestination(player *model.Player, target model.Position, spawnEngine *spawn.SpawnEngine) {
//	if player.MovementTimer != nil {
//		player.MovementTimer.Stop()
//	}
//	if player.MovementTicker != nil {
//		player.MovementTicker.Stop()
//	}
//
//	distance := player.Position.DistanceTo(target)
//	walkTimeInSeconds := distance / player.GetRunSpeed()
//
//	logrus.Tracef("MovementSpeed %f\n", player.GetRunSpeed())
//	logrus.Tracef("Distance %f\n", distance)
//
//	startPosWorldX, _, startPosWorldZ := player.Position.ToWorldCoordinates()
//	endPosWorldX, _, endPosWorldZ := target.ToWorldCoordinates()
//
//	startPos := math32.NewVector3(startPosWorldX, 0, startPosWorldZ)
//	endPos := math32.NewVector3(endPosWorldX, 0, endPosWorldZ)
//	diffPos := endPos.Sub(startPos)
//	angle := math.AngleToEastInDeg(*diffPos)
//	heading := (angle / 360) * 0xFFFF
//	player.MovementTicker = time.NewTicker(time.Second / 60)
//	player.MovementTimer = time.NewTimer(time.Duration(walkTimeInSeconds*1000) * time.Millisecond)
//	go func() {
//		logrus.Tracef("Starting Movement. Walking for %f seconds\n", walkTimeInSeconds)
//		curNumOfTicks := 0
//		maxNumOfTicks := int(walkTimeInSeconds * 60)
//		walkStepVector := diffPos.MultiplyScalar(1 / float32(maxNumOfTicks))
//		world := model.GetSroWorldInstance()
//		for {
//			select {
//			case <-player.MovementTimer.C:
//				player.MovementTicker.Stop()
//				player.MovementTimer.Stop()
//				player.MotionState = model.NoMotion
//				logrus.Tracef("Stopping Movement")
//				break
//			case <-player.MovementTicker.C:
//				curNumOfTicks++
//				if curNumOfTicks <= maxNumOfTicks {
//					player.MotionState = model.Running
//					worldX, _, worldZ := player.Position.ToWorldCoordinates()
//					curPosVec := math32.NewVector3(worldX, player.Position.Y, worldZ)
//
//					newPosVec := curPosVec.Clone().Add(walkStepVector)
//					newPos := model.NewPosFromWorldCoordinates(newPosVec.X, newPosVec.Z)
//					newPos.Heading = heading
//					curCell := player.Position.Region.GetCellAtOffset(player.Position.X, player.Position.Z)
//					newCell := newPos.Region.GetCellAtOffset(newPos.X, newPos.Z)
//
//					objects := make([]*navmesh.Object, 0)
//					for _, objId := range curCell.Objects {
//						obj := player.Position.Region.Objects[objId]
//						objects = append(objects, obj)
//					}
//
//					if curCell.ID != newCell.ID {
//						for _, objId := range newCell.Objects {
//							obj := newPos.Region.Objects[objId]
//							objects = append(objects, obj)
//						}
//					}
//
//					if player.Position.Region.ID != newPos.Region.ID {
//						logrus.Tracef("new position is in new region (%d) -> (%d)\n", player.Position.Region.ID, newPos.Region.ID)
//						if !player.Position.Region.CanEnter(curCell, newCell) {
//							player.MotionState = model.NoMotion
//							player.MovementTicker.Stop()
//							player.MovementTimer.Stop()
//							player.SendPositionUpdate()
//							logrus.Tracef("Cell collision between R(%d)[%d] and R(%d)[%d]\n", curCell.RegionID, curCell.ID, newCell.RegionID, newCell.ID)
//							return
//						}
//					}
//					hasCollision, _, inObj, objPos := geo.FindCollisions(player.Position, newPos)
//					if hasCollision {
//						player.MovementTimer.Stop()
//						player.MovementTicker.Stop()
//						player.MotionState = model.NoMotion
//						player.SetPosition(player.GetPosition())
//						player.SendPositionUpdate()
//						spawnEngine.PositionChanged(player)
//						spawnEngine.PlayerObjectCollision(player)
//						return
//					}
//
//					if inObj && objPos != nil && !IsNextPositionTooHigh(player.GetPosition(), model.Position{
//						X:       objPos.X,
//						Y:       objPos.Y,
//						Z:       objPos.Z,
//						Heading: heading,
//						Region:  world.regions[newCell.RegionID],
//					}) {
//						newPos.Y = objPos.Y
//						logrus.Tracef("Changing position to obj position: %v", newPos)
//						objPos = nil
//					}
//
//					if curCell.ID != newCell.ID && !inObj {
//						logrus.Tracef("cell %d has %d objects\n", curCell.ID, curCell.ObjCount)
//						if !player.Position.Region.CanEnter(curCell, newCell) {
//							player.MovementTicker.Stop()
//							player.MovementTimer.Stop()
//							player.MotionState = model.NoMotion
//							logrus.Debugf("Cell collision between R(%d)[%d] and R(%d)[%d]\n", curCell.RegionID, curCell.ID, newCell.RegionID, newCell.ID)
//							player.SendPositionUpdate()
//							spawnEngine.PlayerObjectCollision(player)
//							return
//						}
//					}
//					inObj = false
//					objPos = nil
//					logrus.Tracef("setting new position to %v\n", newPos)
//					if diff := math32.Abs(player.GetPosition().Y - newPos.Y); diff > 10 {
//						logrus.Tracef("y-pos difference greater 10: %v\n", diff)
//					}
//					player.SetPosition(newPos)
//					spawnEngine.PositionChanged(player)
//				}
//			}
//
//		}
//	}()
//}
