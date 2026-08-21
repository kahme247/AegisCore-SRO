package model

import (
	"bytes"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestHwanPacketsMatchV188WireContract(t *testing.T) {
	points := BuildHwanPointUpdatePacket(5, 0x11223344)
	if points.MessageID != opcode.EntityUpdatePoints || !bytes.Equal(points.Payload(), []byte{4, 5, 0x44, 0x33, 0x22, 0x11}) {
		t.Fatalf("HWAN point packet = opcode %#x payload %x", points.MessageID, points.Payload())
	}
	title := BuildHwanLevelUpdatePacket(0x11223344, 7)
	if title.MessageID != opcode.EntityUpdateHwanLevel || !bytes.Equal(title.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 7}) {
		t.Fatalf("HWAN title packet = opcode %#x payload %x", title.MessageID, title.Payload())
	}
}

func TestBerserkLifecycleAndCombatModifier(t *testing.T) {
	player := &Player{BodyState: Normal, BaseStats: BaseStats{Str: 20, Int: 20}, RunSpeed: 30}
	base := player.AverageAttackPower(false)
	generation := player.ActivateBerserk(time.Now().Add(time.Minute))
	if generation == 0 || player.GetBodyState() != Berserker {
		t.Fatal("Berserk did not activate")
	}
	if got := player.AverageAttackPower(false); got != base*2 {
		t.Fatalf("Berserk attack = %d, want %d", got, base*2)
	}
	walk, run := player.MovementSpeeds()
	if walk != 0 || run != 60 {
		t.Fatalf("Berserk movement speeds = %.1f/%.1f, want 0/60", walk, run)
	}
	if !player.ExpireBerserk(generation) || player.GetBodyState() != Normal {
		t.Fatal("Berserk did not expire")
	}
	if player.ExpireBerserk(generation) {
		t.Fatal("stale Berserk expiry succeeded")
	}
}

func TestBerserkEffectVariantUsesBlueForLevelFive(t *testing.T) {
	for level := byte(0); level < BlueBerserkHwanLevel; level++ {
		if got := BerserkEffectVariant(level); got != 0 {
			t.Fatalf("HWAN level %d effect = %d, want red variant 0", level, got)
		}
	}
	if got := BerserkEffectVariant(BlueBerserkHwanLevel); got != 1 {
		t.Fatalf("HWAN level %d effect = %d, want blue variant 1", BlueBerserkHwanLevel, got)
	}
}

func TestHwanRewardForMonsterKillMatchesStockGameServer(t *testing.T) {
	tests := []struct {
		name                 string
		playerLevel          int
		monsterLevel         int
		rarity               MonsterRarity
		wantChanceBasisPoint int
		wantPoints           byte
	}{
		{name: "level appropriate normal", playerLevel: 100, monsterLevel: 100, rarity: MonsterNormal, wantChanceBasisPoint: 800, wantPoints: 1},
		{name: "five levels below grace edge", playerLevel: 100, monsterLevel: 95, rarity: MonsterNormal, wantChanceBasisPoint: 800, wantPoints: 1},
		{name: "six levels below", playerLevel: 100, monsterLevel: 94, rarity: MonsterNormal, wantChanceBasisPoint: 775, wantPoints: 1},
		{name: "very low level floor", playerLevel: 100, monsterLevel: 1, rarity: MonsterNormal, wantChanceBasisPoint: 25, wantPoints: 1},
		{name: "giant fivefold", playerLevel: 100, monsterLevel: 100, rarity: MonsterGiant, wantChanceBasisPoint: 4000, wantPoints: 1},
		{name: "party giant uses base class", playerLevel: 100, monsterLevel: 100, rarity: MonsterPartyGiant, wantChanceBasisPoint: 4000, wantPoints: 1},
		{name: "elite twofold", playerLevel: 100, monsterLevel: 100, rarity: MonsterElite, wantChanceBasisPoint: 1600, wantPoints: 1},
		{name: "unique fills gauge", playerLevel: 100, monsterLevel: 1, rarity: MonsterUnique, wantChanceBasisPoint: 10000, wantPoints: MaxHwanPoints},
		{name: "stock special unique fills gauge", playerLevel: 100, monsterLevel: 1, rarity: MonsterRarity(8), wantChanceBasisPoint: 10000, wantPoints: MaxHwanPoints},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := HwanRewardForMonsterKill(test.playerLevel, test.monsterLevel, test.rarity)
			if got.ChanceBasisPoints != test.wantChanceBasisPoint || got.Points != test.wantPoints {
				t.Fatalf("reward = %+v, want chance %d points %d", got, test.wantChanceBasisPoint, test.wantPoints)
			}
		})
	}
}

func TestBerserkDurationScalesWithConsumedPoints(t *testing.T) {
	wants := []time.Duration{0, 12 * time.Second, 24 * time.Second, 36 * time.Second, 48 * time.Second, 60 * time.Second, 60 * time.Second}
	for points, want := range wants {
		if got := BerserkDurationForPoints(byte(points)); got != want {
			t.Fatalf("duration for %d points = %s, want %s", points, got, want)
		}
	}
}

func TestCommitHwanSnapshotClampsPoints(t *testing.T) {
	player := &Player{}
	player.CommitHwanSnapshot(HwanSnapshot{Points: 99, GatheredPoint: 12, Level: 8})
	if got := player.HwanSnapshot(); got != (HwanSnapshot{Points: MaxHwanPoints, GatheredPoint: 12, Level: 8}) {
		t.Fatalf("snapshot = %#v", got)
	}
}
