package manager

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/g3n/engine/math32"
)

func TestMobAIAggressiveMobPursuesNearestKnownPlayer(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}

	manager := mobAIManagerForTest()
	manager.tick(world, now)
	if world.moves != 1 || npc.GetMotionState() != model.Running {
		t.Fatalf("pursuit = moves %d state %d", world.moves, npc.GetMotionState())
	}
	_, _, _, targetUID := npc.AIProfile()
	if targetUID != player.GetUniqueID() {
		t.Fatalf("target UID = %d, want %d", targetUID, player.GetUniqueID())
	}
	if got := world.DistanceTo(world.lastMovement.TargetPosition, player.GetNavmeshPosition()); got < 22.9 || got > 23.1 {
		t.Fatalf("pursuit stand-off = %.2f, want 23 reference units", got)
	}
}

func TestMobAIRejectsObstructedPursuitBeforeMovement(t *testing.T) {
	now := time.Now()
	npc, _ := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}, blocked: true}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 0 {
		t.Fatalf("obstructed pursuit registered %d movements, want 0", world.moves)
	}
	if npc.GetMotionState() != model.NoMotion {
		t.Fatalf("obstructed pursuit motion state = %d, want stopped", npc.GetMotionState())
	}
}

func TestMobAIStopsExistingMovementWhenPursuitBecomesObstructed(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npc.SetAggroTarget(player.GetUniqueID())
	npc.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: player.GetNavmeshPosition()})
	npc.SetMotionState(model.Running)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}, blocked: true}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 0 || world.stops != 1 {
		t.Fatalf("obstructed replacement pursuit = moves %d stops %d, want 0/1", world.moves, world.stops)
	}
	if npc.GetMotionState() != model.NoMotion {
		t.Fatalf("obstructed replacement pursuit motion state = %d, want stopped", npc.GetMotionState())
	}
}

func TestMobAIPassiveMobRetaliatesOnlyAfterHit(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, false, 150)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager := mobAIManagerForTest()

	manager.tick(world, now)
	if world.moves != 0 {
		t.Fatal("passive mob acquired an unprovoked target")
	}
	npc.SetAggroTarget(player.GetUniqueID())
	manager.tick(world, now.Add(time.Second))
	if world.moves != 1 {
		t.Fatal("passive mob did not retaliate after being attacked")
	}
}

func TestMobAIUsesAuthoredNestRadiusForLeash(t *testing.T) {
	now := time.Now()
	npc, _ := mobAIFixture(10)
	npc.ConfigureAI(5, true, 115)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{
		Position: mobAIPosition(600, 0),
		Radius:   700,
	}})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager := mobAIManagerForTest()

	manager.tick(world, now)
	if world.moves != 0 {
		t.Fatalf("mob inside authored nest radius made %d leash-return moves", world.moves)
	}
	if _, exists, _ := manager.pendingAttack(npc.GetUniqueID(), now); !exists {
		t.Fatal("mob inside authored nest radius did not attack its nearby target")
	}
}

func TestMobAIRetaliationReplacesExistingWander(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, false, 150)
	npc.SetMotionState(model.Running)
	npc.SetAggroTarget(player.GetUniqueID())
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 1 {
		t.Fatalf("replacement pursuit moves = %d, want 1", world.moves)
	}
}

func TestMobAIAttackDamagesPlayerOnCooldown(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(10)
	npc.ConfigureAI(5, true, 150)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager := mobAIManagerForTest()

	manager.tick(world, now)
	if got := player.CurrentHP(); got != 100 {
		t.Fatalf("HP at cast start = %d, want 100 before the reference delay", got)
	}
	manager.tick(world, now.Add(2*time.Second))
	if got := player.CurrentHP(); got != 100 {
		t.Fatalf("HP before delayed hit = %d, want 100", got)
	}
	manager.tick(world, now.Add(2400*time.Millisecond))
	if got := player.CurrentHP(); got != 90 {
		t.Fatalf("HP after delayed hit = %d, want 90", got)
	}
	manager.tick(world, now.Add(2500*time.Millisecond))
	if got := player.CurrentHP(); got != 90 {
		t.Fatalf("cooldown attack changed HP to %d", got)
	}
}

func TestMobAICombatRangeIgnoresClientFacingHeight(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(10)
	npc.ConfigureAI(5, true, 150)
	playerPosition := player.GetNavmeshPosition()
	playerPosition.Offset.Y = 800
	player.SetNavmeshPosition(playerPosition)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager := mobAIManagerForTest()

	manager.tick(world, now)
	if world.moves != 0 {
		t.Fatalf("vertically offset adjacent target triggered %d pursuit moves, want 0", world.moves)
	}
	if _, exists, _ := manager.pendingAttack(npc.GetUniqueID(), now); !exists {
		t.Fatal("vertically offset adjacent target did not start its reference attack")
	}
}

func TestMobAIPursuitUsesHorizontalStandOffAndPreservesHeight(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npcPosition := npc.GetNavmeshPosition()
	npcPosition.Offset.Y = 120
	npc.SetNavmeshPosition(npcPosition)
	playerPosition := player.GetNavmeshPosition()
	playerPosition.Offset.Y = 900
	player.SetNavmeshPosition(playerPosition)
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 1 {
		t.Fatalf("pursuit moves = %d, want 1", world.moves)
	}
	destination := world.lastMovement.TargetPosition
	if destination.Offset.Y != npcPosition.Offset.Y {
		t.Fatalf("pursuit height = %g, want mob height %g", destination.Offset.Y, npcPosition.Offset.Y)
	}
	if got := navmeshv2.HorizontalDistance(destination, player.GetNavmeshPosition()); got < 22.9 || got > 23.1 {
		t.Fatalf("horizontal pursuit stand-off = %.2f, want 23", got)
	}
}

func TestMobAIAttackUsesReferenceSkillDelayAndB071Damage(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(10)
	npc.ConfigureAI(5, true, 150)
	npc.ConfigureAttackSkill(160)
	conn := &mobAICaptureConn{}
	player.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "mob-ai-test")
	defer player.Session.Close()
	spawned := player.GetCharKnownObjectList().GetObjectsToSpawn()
	player.GetCharKnownObjectList().MarkSpawned(spawned)

	manager := mobAIManagerForTest()
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager.tick(world, now)

	packets := readCapturedPackets(t, conn, 1)
	if len(packets) != 1 || packets[0].MessageID != opcode.EntitySkillCastBegin {
		t.Fatalf("mob packets before reference delay = %#v, want one B070", packets)
	}
	if got := player.CurrentHP(); got != 100 {
		t.Fatalf("HP before reference delay = %d, want 100", got)
	}

	manager.tick(world, now.Add(2400*time.Millisecond))
	packets = readCapturedPackets(t, conn, 3)
	if len(packets) != 3 || packets[1].MessageID != opcode.EntitySkillCastEnd || packets[2].MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("completed mob attack sequence = %#v, want B070, B071, then 3057", packetOpcodes(packets))
	}
	begin := packets[0]
	for range 3 {
		_, _ = begin.ReadByte()
	}
	_, _ = begin.ReadUInt32() // skill ID
	_, _ = begin.ReadUInt32() // source UID
	beginActionUID, _ := begin.ReadUInt32()
	end := packets[1]
	_, _ = end.ReadByte()
	endActionUID, _ := end.ReadUInt32()
	endTargetUID, _ := end.ReadUInt32()
	if beginActionUID == 0 || endActionUID != beginActionUID || endTargetUID != player.GetUniqueID() {
		t.Fatalf("delayed action IDs = begin:%d end:%d target:%d", beginActionUID, endActionUID, endTargetUID)
	}
	if got := player.CurrentHP(); got != 90 {
		t.Fatalf("HP at B071 = %d, want 90", got)
	}
}

func TestMobAIClientVisibleChaseKeepsOneRouteUntilAttack(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npc.SetWalkSpeed(8)
	npc.SetRunSpeed(22)
	conn := &mobAICaptureConn{}
	player.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "mob-ai-client-visible-test")
	defer player.Session.Close()

	// Establish the same group-spawn state the stock client sees before it
	// receives movement and combat packets for this mob.
	groupSpawnObjects(player, player.GetCharKnownObjectList().GetObjectsToSpawn(), true)

	manager := mobAIManagerForTest()
	world := &mobAIWorldFake{
		npcs:                    []*model.NPC{npc},
		emitPositionCorrections: true,
	}
	manager.tick(world, now)
	firstRoute := npc.GetMovementData()
	if firstRoute == nil {
		t.Fatal("initial pursuit did not register a route")
	}

	// The authoritative movement loop has advanced the mob while the player has
	// moved farther along the same line. The first route is still useful and
	// must finish instead of being replaced mid-interpolation.
	npc.SetNavmeshPosition(mobAIPosition(111, 100))
	player.SetNavmeshPosition(mobAIPosition(210, 100))
	manager.tick(world, now.Add(mobPursuitRefresh))
	if got := npc.GetMovementGeneration(); got != firstRoute.Generation {
		t.Fatalf("active client-visible route generation = %d, want unchanged %d", got, firstRoute.Generation)
	}

	// Arrival at the first destination permits exactly one follow-up route to
	// the player's new stand-off point.
	if !npc.FinalizeMovement(firstRoute.Generation, firstRoute.TargetPosition, "test-arrived") {
		t.Fatal("could not complete initial pursuit route")
	}
	npc.SetMotionState(model.NoMotion)
	manager.tick(world, now.Add(2*mobPursuitRefresh))
	secondRoute := npc.GetMovementData()
	if secondRoute == nil || secondRoute.Generation == firstRoute.Generation {
		t.Fatalf("follow-up pursuit route = %#v, want a new generation", secondRoute)
	}

	// Once the mob reaches attack range, the client receives the final position,
	// cast start, and delayed hit using one globally unique action ID.
	npc.SetNavmeshPosition(secondRoute.TargetPosition)
	castAt := now.Add(3 * mobPursuitRefresh)
	manager.tick(world, castAt)
	manager.tick(world, castAt.Add(2400*time.Millisecond))

	packets := readCapturedPackets(t, conn, 8)
	wantOpcodes := []uint16{
		opcode.EntityGroupSpawnBegin,
		opcode.EntityGroupSpawnData,
		opcode.EntityGroupSpawnEnd,
		opcode.EntityMovementResponse,
		opcode.EntityMovementResponse,
		opcode.MovementPositionUpdate,
		opcode.EntitySkillCastBegin,
		opcode.EntitySkillCastEnd,
		opcode.EntityUpdateStatus,
	}
	if len(packets) != len(wantOpcodes) {
		t.Fatalf("client-visible chase packet count = %d, want %d; opcodes=%#v", len(packets), len(wantOpcodes), packetOpcodes(packets))
	}
	for index, want := range wantOpcodes {
		if packets[index].MessageID != want {
			t.Fatalf("client-visible chase packet %d opcode = 0x%04X, want 0x%04X; all=%#v", index, packets[index].MessageID, want, packetOpcodes(packets))
		}
	}
	if got := player.CurrentHP(); got != 90 {
		t.Fatalf("HP after client-visible chase = %d, want 90", got)
	}
	if !player.GetKnownObjectList().Knows(npc) || !npc.GetKnownObjectList().Knows(player) {
		t.Fatal("mob/player visibility was lost before the attack resolved")
	}
	if despawns := player.GetCharKnownObjectList().GetObjectsToDespawn(); len(despawns) != 0 {
		t.Fatalf("client-visible chase queued unexpected despawns: %#v", despawns)
	}
}

func packetOpcodes(packets []network.Packet) []uint16 {
	opcodes := make([]uint16, len(packets))
	for index := range packets {
		opcodes[index] = packets[index].MessageID
	}
	return opcodes
}

func TestMobAIWandersInsideSpawnArea(t *testing.T) {
	now := time.Now().Add(4 * time.Second)
	npc, _ := mobAIFixture(100)
	npc.ConfigureAI(5, false, 150)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{
		Position:       mobAIPosition(100, 100),
		GenerateRadius: 50,
	}})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}, newPosition: mobAIPosition(120, 120), useNewPosition: true}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 1 {
		t.Fatalf("wander moves = %d, want 1", world.moves)
	}
}

func TestMobAITickSkipsMobsNotKnownToPlayers(t *testing.T) {
	now := time.Now().Add(4 * time.Second)
	npc, _ := mobAIFixture(100)
	npc.ConfigureAI(5, false, 150)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{
		Position:       mobAIPosition(100, 100),
		GenerateRadius: 50,
	}})
	world := &mobAIWorldFake{
		npcs:           []*model.NPC{npc},
		activeNPCs:     []*model.NPC{},
		newPosition:    mobAIPosition(120, 120),
		useNewPosition: true,
	}

	mobAIManagerForTest().tick(world, now)
	if world.moves != 0 || npc.GetMotionState() != model.NoMotion {
		t.Fatalf("inactive mob AI = moves %d state %d, want 0/%d", world.moves, npc.GetMotionState(), model.NoMotion)
	}
}

type mobAIWorldFake struct {
	npcs                    []*model.NPC
	activeNPCs              []*model.NPC
	moves                   int
	stops                   int
	newPosition             navmeshv2.RtNavmeshPosition
	useNewPosition          bool
	lastMovement            model.MovementData
	blocked                 bool
	emitPositionCorrections bool
}

func (w *mobAIWorldFake) GetNPCs() []*model.NPC { return w.npcs }

func (w *mobAIWorldFake) GetNPCsKnownToPlayers() []*model.NPC {
	if w.activeNPCs != nil {
		return w.activeNPCs
	}
	return w.npcs
}

func (w *mobAIWorldFake) DistanceTo(a, b navmeshv2.RtNavmeshPosition) float32 {
	return a.GetGlobalCoordinates().DistanceTo(b.GetGlobalCoordinates())
}

func (w *mobAIWorldFake) RegisterMovingCharacter(character model.ICharacter, data model.MovementData) model.MovementData {
	w.moves++
	w.lastMovement = data
	registered := character.BeginMovement(data)
	character.SetMotionState(model.Running)
	return registered
}

func (w *mobAIWorldFake) StopMovingCharacter(character model.ICharacter, position navmeshv2.RtNavmeshPosition, reason string, correction bool) bool {
	w.stops++
	character.CancelMovement(position, reason)
	character.SetMotionState(model.NoMotion)
	if correction && w.emitPositionCorrections {
		character.SendPositionUpdate()
	}
	return true
}

func (w *mobAIWorldFake) NewPosFromGlobalCoordinates(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	if w.useNewPosition {
		return w.newPosition, nil
	}
	regionX := byte(int(global.X / navmeshv2.RegionWidth))
	regionZ := byte(int(global.Z / navmeshv2.RegionHeight))
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromXAndY(regionX, regionZ),
		Offset: math32.NewVector3(
			global.X-float32(regionX)*navmeshv2.RegionWidth,
			global.Y,
			global.Z-float32(regionZ)*navmeshv2.RegionHeight,
		),
	}, nil
}

func (w *mobAIWorldFake) CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool {
	return !w.blocked
}

func mobAIFixture(distance float32) (*model.NPC, *model.Player) {
	npc := &model.NPC{
		SRObject: model.NewSRObject(),
		Type:     "NPC",
		Mutex:    &sync.Mutex{},
	}
	npc.SetUniqueID(100)
	npc.RefObjectID = 1933
	npc.SetTypeInfo(model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1})
	npc.SetNavmeshPosition(mobAIPosition(100, 100))
	npc.KnownObjectList = model.NewKnownObjectList(npc)
	npc.SetLifeState(model.Alive)
	npc.ConfigureAttackSkill(160)

	player := &model.Player{
		ID:        1,
		CharName:  "target",
		SRObject:  model.NewSRObject(),
		BaseStats: model.BaseStats{HP: 100, Str: 20},
	}
	player.SetUniqueID(200)
	player.RefObjectID = 1907
	player.SetNavmeshPosition(mobAIPosition(100+distance, 100))
	player.CharKnownObjectList = model.NewCharKnownObjectList(player)
	player.KnownObjectList = player.CharKnownObjectList
	player.SetLifeState(model.Alive)
	npc.KnownObjectList.AddObject(player)
	player.KnownObjectList.AddObject(npc)
	return npc, player
}

func mobAIManagerForTest() *MobAIManager {
	manager := &MobAIManager{pending: make(map[uint32]mobPendingAttack)}
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			1907: {RefObject: model.RefObject{ID: 1907, BCRadius: 4}},
			1933: {RefObject: model.RefObject{ID: 1933, BCRadius: 6}},
		},
		Skills: map[uint32]model.RefSkill{
			160: {ID: 160, ActionDuration: 2400, ReuseDelay: 3000, CoolTime: 3000, ActionRange: 10, AttackMin: 17, AttackMax: 19},
		},
	}))
	return manager
}

type mobAICaptureConn struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *mobAICaptureConn) Read([]byte) (int, error) { return 0, net.ErrClosed }
func (c *mobAICaptureConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.Write(data)
}
func (c *mobAICaptureConn) Close() error                     { return nil }
func (c *mobAICaptureConn) LocalAddr() net.Addr              { return mobAIAddr("local") }
func (c *mobAICaptureConn) RemoteAddr() net.Addr             { return mobAIAddr("remote") }
func (c *mobAICaptureConn) SetDeadline(time.Time) error      { return nil }
func (c *mobAICaptureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *mobAICaptureConn) SetWriteDeadline(time.Time) error { return nil }
func (c *mobAICaptureConn) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...)
}

func readCapturedPackets(t *testing.T, conn *mobAICaptureConn, want int) []network.Packet {
	t.Helper()
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		packets, err := network.NewPacketReader(bytes.NewReader(conn.Bytes())).ReadPackets()
		if err == nil && len(packets) >= want {
			return packets
		}
		if time.Now().After(deadline) {
			t.Fatalf("captured packets did not reach %d: count=%d error=%v", want, len(packets), err)
		}
		time.Sleep(time.Millisecond)
	}
}

type mobAIAddr string

func (a mobAIAddr) Network() string { return string(a) }
func (a mobAIAddr) String() string  { return string(a) }

func mobAIPosition(x, z float32) navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000),
		Offset: math32.NewVector3(x, 0, z),
	}
}
