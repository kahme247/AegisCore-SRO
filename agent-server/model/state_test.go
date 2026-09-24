package model

import (
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
)

func TestAuthoritativeModelStateValues(t *testing.T) {
	tests := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"life spawning", uint8(Spawning), 0},
		{"life alive", uint8(Alive), 1},
		{"life dead", uint8(Dead), 2},
		{"life gone", uint8(Gone), 3},
		{"body normal", uint8(Normal), 0},
		{"body hwan", uint8(Hwan), 1},
		{"body untouchable", uint8(Untouchable), 2},
		{"body gm invincible", uint8(GmInvincible), 3},
		{"body gm untouchable", uint8(GmUntouchable), 4},
		{"body gm invisible", uint8(GmInvisible), 5},
		{"body berserker", uint8(Berserker), 1},
		{"body stealth", uint8(Stealth), 6},
		{"body invisible", uint8(Invisible), 7},
		{"motion stand", uint8(Stand), 0},
		{"motion walk", uint8(Walk), 1},
		{"motion run", uint8(Run), 2},
		{"motion sit", uint8(Sit), 3},
		{"battle peace", uint8(BattlePeace), 0},
		{"battle", uint8(BattleBattle), 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("state value = %d, want %d", test.got, test.want)
			}
		})
	}

	// These assignments prove the model aliases are the profile's pure domain
	// types rather than a second, potentially divergent, value table.
	var _ vsro188.LifeState = Spawning
	var _ vsro188.BodyMode = Berserker
	var _ vsro188.MotionState = Run
	var _ vsro188.BattleState = BattleBattle
}

func TestLegacyStateNamesRemainCompatibleAliases(t *testing.T) {
	if NoStatus != Normal {
		t.Fatalf("NoStatus = %d, want Normal = %d", NoStatus, Normal)
	}
	if Berserk != Berserker {
		t.Fatalf("Berserk = %d, want Berserker = %d", Berserk, Berserker)
	}
	if NoMotion != Stand || Walking != Walk || Running != Run || Sitting != Sit {
		t.Fatalf("legacy motion aliases = (%d, %d, %d, %d), want (%d, %d, %d, %d)", NoMotion, Walking, Running, Sitting, Stand, Walk, Run, Sit)
	}
}

func TestBattleStateDoesNotConflatePvPState(t *testing.T) {
	if reflect.TypeOf(BattlePeace) == reflect.TypeOf(PvpWhite) {
		t.Fatalf("battle state must remain a distinct type from PvP state")
	}

	player := &Player{PKState: PvpRed}
	if got, want := player.GetBattleState(), BattlePeace; got != want {
		t.Fatalf("default battle state = %d, want %d", got, want)
	}
	player.SetBattleState(BattleBattle)
	if got, want := player.GetBattleState(), BattleBattle; got != want {
		t.Fatalf("battle state = %d, want %d", got, want)
	}
	if got, want := player.GetPKState(), PvpRed; got != want {
		t.Fatalf("setting battle state changed PvP state to %d, want %d", got, want)
	}
}

func TestBerserkerMovementAndCanonicalSpawnSerializationUseAuthoritativeValue(t *testing.T) {
	player := &Player{
		SRObject: SRObject{
			TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 1},
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.NewRegionFromInt16(25000),
				Offset: math32.NewVector3(100, 0, 200),
			},
		},
		LifeState:   Gone,
		MotionState: Run,
		BodyState:   Berserker,
		RunSpeed:    30,
		HwanLevel:   7,
	}

	if got, want := player.GetMovementSpeed(), float32(60); got != want {
		t.Fatalf("berserker run speed = %f, want %f", got, want)
	}

	player.SetMotionState(Sit)
	packet := network.EmptyPacket()
	WriteEntitySpawn(&packet, player)
	payload := packet.Payload()
	if len(payload) <= 5 || payload[5] != 7 {
		t.Fatalf("serialized HWAN level = %x, want 7", payload)
	}

	// This is the existing spawn writer's state block: life, legacy unknown,
	// motion, and body. The test locks in the corrected values without
	// declaring a new client-facing packet layout.
	const stateOffset = 38
	if len(payload) < stateOffset+4 {
		t.Fatalf("spawn payload too short for its existing state block: %d", len(payload))
	}
	want := []byte{byte(Gone), 0, byte(Sit), byte(Berserker)}
	if got := payload[stateOffset : stateOffset+4]; !reflect.DeepEqual(got, want) {
		t.Fatalf("serialized state block = %v, want %v", got, want)
	}
}
