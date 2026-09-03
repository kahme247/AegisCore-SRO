package manager

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

func TestMobAIAggressiveMobPursuesNearestKnownPlayer(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{Position: npc.GetNavmeshPosition(), Radius: 500}})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}

	manager := mobAIManagerForTest()
	manager.tick(world, now)
	if world.moves != 0 {
		t.Fatalf("ordinary mob pursued during awareness delay: %d moves", world.moves)
	}
	manager.tick(world, now.Add(2*time.Second))
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

func TestSourceTherapyPreventsPreemptiveAggro(t *testing.T) {
	npc, player := mobAIFixture(50)
	npc.ConfigureAI(5, true, 150)
	if _, ok := player.CommitActiveBuff(8390, 77, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("commit source therapy")
	}
	manager := mobAIManagerForTest()
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		8390: {ID: 8390, AggroSuppression: true},
	}}))
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager.tick(world, time.Now())
	_, _, _, targetUID := npc.AIProfile()
	if targetUID != 0 {
		t.Fatalf("source therapy target UID = %d, want none", targetUID)
	}
}

func TestMobAIStationaryPlantDoesNotPursueOrWander(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.ConfigureMovementSpeeds(0, 0)
	npc.ConfigureAI(46, true, 150)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{
		Position:       npc.GetNavmeshPosition(),
		GenerateRadius: 50,
	}})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager := mobAIManagerForTest()

	manager.tick(world, now.Add(20*time.Second))
	if world.moves != 0 {
		t.Fatalf("stationary plant registered %d pursuit movements, want 0", world.moves)
	}
	if npc.GetMotionState() != model.NoMotion || npc.GetMovementData() != nil {
		t.Fatal("stationary plant entered a moving state while pursuing")
	}

	npc.ClearAggroTarget(player.GetUniqueID())
	npc.ConfigureAI(46, false, 150)
	manager.tick(world, now.Add(40*time.Second))
	if world.moves != 0 {
		t.Fatalf("stationary plant registered %d movements after wander tick, want 0", world.moves)
	}
}

func TestMobAIKnockbackInterruptsPendingAttackAndResumesPursuit(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(10)
	npc.ConfigureAI(5, true, 150)
	npc.SetAggroTarget(player.GetUniqueID())
	manager := mobAIManagerForTest()
	manager.setPendingAttack(npc.GetUniqueID(), mobPendingAttack{
		npc: npc, targetUID: player.GetUniqueID(), skillID: 160,
		castUID: 77, damage: 100, readyAt: now.Add(time.Second),
	})

	manager.InterruptAttack(npc)
	if _, exists, _ := manager.pendingAttack(npc.GetUniqueID(), now); exists {
		t.Fatal("pending attack survived forced displacement")
	}

	// Model the authoritative endpoint written by the combat displacement path.
	npc.SetNavmeshPosition(mobAIPosition(40, 100))
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	manager.tick(world, now)
	if world.moves != 1 || npc.GetMotionState() != model.Running {
		t.Fatalf("post-knockback pursuit = moves %d state %d, want 1/running", world.moves, npc.GetMotionState())
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
	npc, player := mobAIFixture(10)
	npc.ConfigureAI(5, true, 115)
	npc.BindSpawnArea(&model.SpawnArea{Spawn: model.Spawn{
		Position: mobAIPosition(600, 0),
		Radius:   700,
	}})
	npc.SetAggroTarget(player.GetUniqueID())
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
	manager.tick(world, now.Add(799*time.Millisecond))
	if got := player.CurrentHP(); got != 100 {
		t.Fatalf("HP before delayed hit = %d, want 100", got)
	}
	manager.tick(world, now.Add(800*time.Millisecond))
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

	manager.tick(world, now.Add(799*time.Millisecond))
	packets = readCapturedPackets(t, conn, 1)
	if len(packets) != 1 {
		t.Fatalf("mob packets before cast impact = %#v, want one B070", packetOpcodes(packets))
	}

	manager.tick(world, now.Add(800*time.Millisecond))
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
	_, _ = end.ReadBool()
	_, _ = end.ReadByte()
	_, _ = end.ReadByte()
	if beginActionUID == 0 || endActionUID != beginActionUID || endTargetUID != player.GetUniqueID() {
		t.Fatalf("delayed action IDs = begin:%d end:%d target:%d", beginActionUID, endActionUID, endTargetUID)
	}
	if got := player.CurrentHP(); got != 90 {
		t.Fatalf("HP at B071 = %d, want 90", got)
	}
}

func TestMobAIPhysicalCriticalAndPlayerBlockUseStockDamageStates(t *testing.T) {
	criticalNPC, criticalTarget := mobAIFixture(10)
	criticalTarget.BaseStats.HP = 1000
	criticalConn := &mobAICaptureConn{}
	criticalTarget.Session = server.NewSession(criticalConn, network.EncodingOptions{}, nil, nil, "mob-critical-test")
	defer criticalTarget.Session.Close()
	criticalSpawned := criticalTarget.GetCharKnownObjectList().GetObjectsToSpawn()
	criticalTarget.GetCharKnownObjectList().MarkSpawned(criticalSpawned)
	manager := mobAIManagerForTest()
	// Attack/parry rating must not turn a normal SRO attack into a synthetic
	// miss. Only the independent shield block roll suppresses damage.
	manager.hitRollPercent = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return true }
	manager.blockRoll = func(int) bool { return false }
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		1933: {RefObject: model.RefObject{ID: 1933}, CriticalHitRate: 100},
	}}))
	manager.applyAttackDamage(&mobAIWorldFake{}, criticalNPC, criticalTarget, 10, 100, false, false)
	criticalPackets := readCapturedPackets(t, criticalConn, 2)
	criticalPayload := criticalPackets[0].Payload()
	if criticalTarget.CurrentHP() != 800 || len(criticalPayload) < 31 || criticalPayload[27] != model.DamageStateCritical || !bytes.Equal(criticalPayload[28:31], []byte{200, 0, 0}) {
		t.Fatalf("critical attack = hp:%d payload:% X", criticalTarget.CurrentHP(), criticalPayload)
	}

	blockedNPC, blockedTarget := mobAIFixture(10)
	blockedTarget.BaseDefenseStats.BlockRate = 100
	blockedConn := &mobAICaptureConn{}
	blockedTarget.Session = server.NewSession(blockedConn, network.EncodingOptions{}, nil, nil, "mob-block-test")
	defer blockedTarget.Session.Close()
	blockedSpawned := blockedTarget.GetCharKnownObjectList().GetObjectsToSpawn()
	blockedTarget.GetCharKnownObjectList().MarkSpawned(blockedSpawned)
	blockedManager := mobAIManagerForTest()
	blockedManager.hitRollPercent = func(int) bool { return true }
	blockedManager.criticalRoll = func(int) bool { return true }
	blockedManager.blockRoll = func(int) bool { return true }
	blockedManager.applyAttackDamage(&mobAIWorldFake{}, blockedNPC, blockedTarget, 11, 100, false, false)
	blockedPackets := readCapturedPackets(t, blockedConn, 2)
	blockedPayload := blockedPackets[0].Payload()
	if blockedTarget.CurrentHP() != 100 || len(blockedPayload) != 27 || blockedPayload[26] != model.DamageAttackStateBlock {
		t.Fatalf("blocked attack = hp:%d payload:% X", blockedTarget.CurrentHP(), blockedPayload)
	}
}

func TestMobAITiscFinalFactorAppliesToPhysicalAndMagicalDamage(t *testing.T) {
	for _, test := range []struct {
		name       string
		magical    bool
		critical   bool
		wantDamage int
	}{
		{name: "physical", wantDamage: 135},
		{name: "physical critical", critical: true, wantDamage: 270},
		{name: "magical", magical: true, critical: true, wantDamage: 135},
	} {
		t.Run(test.name, func(t *testing.T) {
			npc, target := mobAIFixture(10)
			target.BaseStats.HP = 1000
			status := model.CombatStatus{
				Kind: model.CombatStatusDamageTakenUp, Token: 1, SkillID: 8414,
				Power: 35, ExpiresAt: time.Now().Add(time.Minute),
			}
			if !target.ApplyCombatStatus(status) {
				t.Fatal("failed to apply tisc status")
			}
			conn := &mobAICaptureConn{}
			target.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "mob-tisc-test")
			defer target.Session.Close()
			spawned := target.GetCharKnownObjectList().GetObjectsToSpawn()
			target.GetCharKnownObjectList().MarkSpawned(spawned)

			manager := mobAIManagerForTest()
			manager.criticalRoll = func(int) bool { return test.critical }
			manager.blockRoll = func(int) bool { return false }
			manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
				npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}, CriticalHitRate: 100},
			}}))
			manager.applyAttackDamage(&mobAIWorldFake{}, npc, target, 10, 100, false, test.magical)

			if got := 1000 - target.CurrentHP(); got != test.wantDamage {
				t.Fatalf("damage = %d, want %d", got, test.wantDamage)
			}
		})
	}
}

func TestMobAIShockDoesNotBehaveLikeStun(t *testing.T) {
	now := time.Now()
	npc, _ := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npc.SetWalkSpeed(8)
	npc.SetRunSpeed(22)
	npc.SetMotionState(model.Running)
	npc.ConfigureCombat(100, 10)
	npc.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusShock, Token: 1, SkillID: 8129, Power: 240, ExpiresAt: now.Add(time.Second)})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	mobAIManagerForTest().tick(world, now)
	if world.stops != 0 {
		t.Fatalf("Shock stopped movement %d times; Shock must impair parry rather than stun", world.stops)
	}
}

func TestMobAINPCSkillAppliesDirectStatusActor(t *testing.T) {
	npc, target := mobAIFixture(10)
	const skillID = 161
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.hitRollPercent = func(int) bool { return true }
	var scheduled []func()
	manager.schedule = func(_ time.Duration, callback func()) { scheduled = append(scheduled, callback) }
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}},
		},
		Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusStun, Chance: 100, Level: 2, DurationMS: 1000}}},
		},
	}))
	manager.applyAttackDamageWithSkill(&mobAIWorldFake{}, npc, target, 12, 1, false, skillID, true, false)
	if status, active := target.CombatStatus(model.CombatStatusStun, time.Now()); !active || status.SkillID != skillID || status.Level != 2 {
		t.Fatalf("NPC skill status = %+v, active=%t", status, active)
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled NPC status callbacks = %d, want 1", len(scheduled))
	}
	scheduled[0]()
	if _, active := target.CombatStatus(model.CombatStatusStun, time.Now().Add(2*time.Second)); active {
		t.Fatal("expired NPC status remained active after scheduler callback")
	}
}

func TestMobAINPCSkillAppliesAuthoredKnockbackAndInterruptsPlayer(t *testing.T) {
	npc, target := mobAIFixture(10)
	const skillID = 165
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.hitRollPercent = func(chance int) bool { return chance == 30 }
	interrupted := uint32(0)
	manager.ConfigurePlayerAttackInterrupter(func(player *model.Player) { interrupted = player.GetUniqueID() })
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}}},
		Skills:     map[uint32]model.RefSkill{skillID: {ID: skillID, KnockbackChance: 30, KnockbackDistance: 20}},
	}))
	world := &mobAIWorldFake{}
	targets := manager.applyAttackDamageWithSkillResult(world, npc, target, 16, 1, false, skillID, true, false, false)
	if len(targets) != 1 || len(targets[0].Records) != 1 {
		t.Fatalf("mob knockback targets = %+v", targets)
	}
	record := targets[0].Records[0]
	if record.AttackState != model.DamageResultKnockback || record.EndpointX != 130 {
		t.Fatalf("mob knockback record = %+v, want endpoint x=130", record)
	}
	if target.GetNavmeshPosition().Offset.X != 130 || interrupted != target.GetUniqueID() || world.stops != 1 {
		t.Fatalf("mob knockback target x=%v interrupted=%d stops=%d", target.GetNavmeshPosition().Offset.X, interrupted, world.stops)
	}
}

func TestMobAINPCSkillFailedKnockbackRollDoesNotMoveOrInterruptPlayer(t *testing.T) {
	npc, target := mobAIFixture(10)
	const skillID = 166
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.hitRollPercent = func(int) bool { return false }
	interrupted := false
	manager.ConfigurePlayerAttackInterrupter(func(*model.Player) { interrupted = true })
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}}},
		Skills:     map[uint32]model.RefSkill{skillID: {ID: skillID, KnockbackChance: 30, KnockbackDistance: 20}},
	}))
	world := &mobAIWorldFake{}
	start := target.GetNavmeshPosition()
	manager.applyAttackDamageWithSkill(world, npc, target, 17, 1, false, skillID, true, false)
	position := target.GetNavmeshPosition()
	if position.Region.ID != start.Region.ID || navmeshv2.HorizontalDistance(position, start) > 0.01 || interrupted || world.stops != 0 {
		t.Fatalf("failed mob knockback moved/interrupted target: position=%v interrupted=%t stops=%d", target.GetNavmeshPosition(), interrupted, world.stops)
	}
}

func TestMobAINPCSkillAggregatesOrdinaryTransferTargets(t *testing.T) {
	npc, primary := mobAIFixture(10)
	secondary := &model.Player{
		ID: 2, CharName: "secondary", SRObject: model.NewSRObject(),
		BaseStats: model.BaseStats{HP: 100, Str: 20},
	}
	secondary.SetUniqueID(201)
	secondary.RefObjectID = primary.RefObjectID
	secondary.SetNavmeshPosition(mobAIPosition(115, 100))
	secondary.SetLifeState(model.Alive)
	npc.GetKnownObjectList().AddObject(secondary)
	const skillID = 162
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}},
		},
		Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, TransferType: 2, TransferRange: 20, TransferCount: 1, TransferFalloffPercent: 50},
		},
	}))
	manager.applyMobSkillAttack(&mobAIWorldFake{}, npc, primary, 13, 100, false, skillID, false)
	if primary.CurrentHP() != 0 || secondary.CurrentHP() != 50 {
		t.Fatalf("ordinary transfer damage = primary:%d secondary:%d, want 0/50", primary.CurrentHP(), secondary.CurrentHP())
	}
}

func TestMobAINPCSkillOwnerTransferUsesExplicitProvider(t *testing.T) {
	npc, primary := mobAIFixture(10)
	secondary := &model.Player{ID: 2, CharName: "owner-target", SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 100, Str: 20}}
	secondary.SetUniqueID(202)
	secondary.RefObjectID = primary.RefObjectID
	secondary.SetLifeState(model.Alive)
	const skillID = 163
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}}},
		Skills:     map[uint32]model.RefSkill{skillID: {ID: skillID, TransferType: 5, TransferRange: 1, TransferCount: 1}},
	}))
	manager.ConfigureSkillOwnerTargetProvider(func(_ *model.NPC, _ *model.Player, _ model.RefSkill, _ int) []*model.Player {
		return []*model.Player{primary, secondary, secondary}
	})
	manager.applyMobSkillAttack(&mobAIWorldFake{}, npc, primary, 14, 100, false, skillID, false)
	if secondary.CurrentHP() != 0 {
		t.Fatalf("owner-provided transfer did not apply damage: hp=%d", secondary.CurrentHP())
	}
}

func TestMobAINPCSkillChainTransferUsesProgressiveNearestTargets(t *testing.T) {
	npc, primary := mobAIFixture(10)
	makePlayer := func(id uint32, x float32) *model.Player {
		player := &model.Player{ID: int(id), SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 100, Str: 20}}
		player.SetUniqueID(id)
		player.SetLifeState(model.Alive)
		player.SetNavmeshPosition(mobAIPosition(x, 100))
		npc.GetKnownObjectList().AddObject(player)
		return player
	}
	first := makePlayer(203, 115)
	second := makePlayer(204, 122)
	makePlayer(205, 140)
	manager := mobAIManagerForTest()
	got := manager.mobSkillTransferTargets(npc, primary, model.RefSkill{TransferType: 6, TransferRange: 10, TransferCount: 2})
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("chain targets = %v, want [%d %d]", got, first.GetUniqueID(), second.GetUniqueID())
	}
}

func TestMobAINPCSkillSchedulesPeriodicAbnormalDamage(t *testing.T) {
	npc, target := mobAIFixture(10)
	const skillID = 164
	manager := mobAIManagerForTest()
	manager.blockRoll = func(int) bool { return false }
	manager.criticalRoll = func(int) bool { return false }
	manager.hitRollPercent = func(int) bool { return true }
	var scheduled []func()
	manager.schedule = func(_ time.Duration, callback func()) { scheduled = append(scheduled, callback) }
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{npc.GetRefObjectID(): {RefObject: model.RefObject{ID: npc.GetRefObjectID()}}},
		Skills:     map[uint32]model.RefSkill{skillID: {ID: skillID, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusBurn, Power: 20, Payload: 20, Chance: 100, DurationMS: 11000}}}},
	}))
	manager.applyAttackDamageWithSkill(&mobAIWorldFake{}, npc, target, 15, 1, false, skillID, true, false)
	if len(scheduled) != 2 {
		t.Fatalf("scheduled callbacks = %d, want expiry and first tick", len(scheduled))
	}
	scheduled[1]()
	if target.CurrentHP() != 79 {
		t.Fatalf("periodic NPC damage hp = %d, want 79", target.CurrentHP())
	}
	if len(scheduled) != 3 {
		t.Fatalf("scheduled callbacks after first tick = %d, want next tick", len(scheduled))
	}
	if status, active := target.CombatStatus(model.CombatStatusBurn, time.Now()); !active || status.Token == 0 {
		t.Fatal("periodic tick removed or failed to preserve parent status")
	}
}

func TestMobAILinkedDamageRedirectsAuthoredShareAndConsumesUseLimit(t *testing.T) {
	npc, target := mobAIFixture(10)
	target.BaseStats.HP = 1000
	linked := &model.Player{ID: 2, SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 1000}}
	linked.SetUniqueID(201)
	linked.SetLifeState(model.Alive)
	linked.CharKnownObjectList = model.NewCharKnownObjectList(linked)
	linked.KnownObjectList = linked.CharKnownObjectList
	npc.KnownObjectList.AddObject(linked)
	const skillID = uint32(901)
	if _, ok := target.CommitActiveBuffFrom(skillID, 77, linked.GetUniqueID(), time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to arrange linked-damage buff")
	}
	manager := &MobAIManager{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		skillID: {ID: skillID, LinkedDamageMask: 15, LinkedDamagePercent: 35, LinkedDamageUseLimit: 2},
	}})}

	remaining, first := manager.interceptLinkedDamage(npc, target, 200, false)
	if remaining != 130 || first.player != linked || first.application.AppliedDamage != 70 || first.removedBuff != 0 || linked.CurrentHP() != 930 {
		t.Fatalf("first linked interception = remaining:%d result:%+v linkedHP:%d", remaining, first, linked.CurrentHP())
	}
	remaining, second := manager.interceptLinkedDamage(npc, target, 200, false)
	if remaining != 130 || second.application.AppliedDamage != 70 || second.removedBuff != 77 || linked.CurrentHP() != 860 {
		t.Fatalf("second linked interception = remaining:%d result:%+v linkedHP:%d", remaining, second, linked.CurrentHP())
	}
	if len(target.ActiveBuffSnapshot()) != 0 {
		t.Fatalf("limited linked buff remained active: %+v", target.ActiveBuffSnapshot())
	}
}

func TestMobAILinkedResultAlignmentPadsStateOnlySuppression(t *testing.T) {
	targets := alignMobDamageTargets([]model.CombatDamageTarget{
		{TargetUID: 1, Records: []model.CombatDamageRecord{{Damage: 50}}},
		{TargetUID: 2, Records: []model.CombatDamageRecord{{AttackState: model.DamageResultBarrier}, {Damage: 25}}},
	})
	if len(targets[0].Records) != 2 || targets[0].Records[1].AttackState != model.DamageResultSuppressed || len(targets[1].Records) != 2 {
		t.Fatalf("aligned linked targets = %+v", targets)
	}
}

func TestMobAIStunStopsMovementAndAttack(t *testing.T) {
	now := time.Now()
	npc, _ := mobAIFixture(100)
	npc.ConfigureAI(5, true, 150)
	npc.SetMotionState(model.Running)
	npc.ConfigureCombat(100, 10)
	npc.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusStun, Token: 1, SkillID: 8416, Power: 1, ExpiresAt: now.Add(5 * time.Second)})
	world := &mobAIWorldFake{npcs: []*model.NPC{npc}}
	mobAIManagerForTest().tick(world, now)
	if npc.GetMotionState() != model.NoMotion || world.stops != 1 || world.moves != 0 {
		t.Fatalf("stunned state=%d stops=%d moves=%d", npc.GetMotionState(), world.stops, world.moves)
	}
}

func TestMobAISleepBypassIsLimitedToThreeBinaryConfirmedSkills(t *testing.T) {
	manager := &MobAIManager{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		1: {ID: 1, CodeName: "MSKILL_SD_HAROERIS_ATTACK04"},
		2: {ID: 2, CodeName: "MSKILL_SD_SETH_ATTACK04"},
		3: {ID: 3, CodeName: "MSKILL_SD_SETH_ATTACK10"},
		4: {ID: 4, CodeName: "MSKILL_SD_SETH_ATTACK11"},
	}})}
	for _, skillID := range []uint32{1, 2, 3} {
		if !manager.skillBypassesSleepRejection(skillID) {
			t.Fatalf("confirmed Sand Dungeon skill %d did not bypass sleep", skillID)
		}
	}
	if manager.skillBypassesSleepRejection(4) || manager.skillBypassesSleepRejection(999) {
		t.Fatal("unconfirmed skill received broad sleep bypass")
	}
}

func TestMobAISealStoneSleepBypassUsesExactActorCode(t *testing.T) {
	manager := &MobAIManager{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		100: {RefObject: model.RefObject{ID: 100, CodeName: "MOB_RM_SEALSTONE"}},
		101: {RefObject: model.RefObject{ID: 101, CodeName: "MOB_RM_SEALSTONE_2"}},
	}})}
	exact := &model.NPC{SRObject: model.SRObject{RefObjectID: 100}}
	variant := &model.NPC{SRObject: model.SRObject{RefObjectID: 101}}
	if !manager.isExactSealStone(exact) {
		t.Fatal("binary-confirmed seal stone actor did not receive exception")
	}
	if manager.isExactSealStone(variant) {
		t.Fatal("numbered seal-stone variant received prefix-based exception")
	}
}

func TestVitalSpotHitDownChangesMonsterHitChance(t *testing.T) {
	now := time.Now()
	npc, player := mobAIFixture(100)
	npc.SRObject.RefObjectID = 9000
	npc.ConfigureCombat(100, 1)
	var chances []int
	manager := &MobAIManager{
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
			9000: {RefObject: model.RefObject{ID: 9000}, HitRate: 99},
		}}),
		hitRollPercent: func(chance int) bool { chances = append(chances, chance); return true },
	}
	if !manager.mobAttackLands(npc, player, now) {
		t.Fatal("baseline monster hit rejected")
	}
	npc.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusHitDown, Token: 1, SkillID: 1617, Power: 88, ExpiresAt: now.Add(time.Minute)})
	if !manager.mobAttackLands(npc, player, now) {
		t.Fatal("hit-down monster hit rejected")
	}
	if len(chances) != 2 || chances[0] != 98 || chances[1] != 90 {
		t.Fatalf("monster hit chances = %v, want [98 90]", chances)
	}
}

func TestVitalSpotAttackDownReducesMonsterDamage(t *testing.T) {
	now := time.Now()
	npc, _ := mobAIFixture(100)
	npc.ConfigureCombat(100, 1)
	npc.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusAttackDown, Token: 1, SkillID: 8410, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if got := mobDamageAfterAttackDown(npc, 200, now); got != 130 {
		t.Fatalf("attack-down damage = %d, want 130", got)
	}
}

func TestMobAIZeroImpactTimingWaitsForClientActionRegistration(t *testing.T) {
	npc, player := mobAIFixture(10)
	manager := &MobAIManager{pending: make(map[uint32]mobPendingAttack)}
	manager.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Skills: map[uint32]model.RefSkill{
			160: {ID: 160, ActionRange: 10},
		},
	}))

	profile, ok := manager.attackProfile(npc, player, 160)
	if !ok {
		t.Fatal("zero-timing attack profile was not found")
	}
	if profile.delay != mobClientActionRegistrationDelay {
		t.Fatalf("zero-timing impact delay = %s, want %s", profile.delay, mobClientActionRegistrationDelay)
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
			160: {ID: 160, ActionPreparingTime: 200, ActionCastingTime: 600, ActionDuration: 2400, ReuseDelay: 3000, CoolTime: 3000, ActionRange: 10, AttackMin: 17, AttackMax: 19},
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
