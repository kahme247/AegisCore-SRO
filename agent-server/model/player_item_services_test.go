package model

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestItemServiceFromReferenceUsesMediaTimeTicketParameters(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	ref := RefItem{Param1: 86400, Param3: 10800000, Param4: 100, Param5: 100}
	ref.ID = 3883
	ref.IsUseable = true
	ref.TypeInfo = TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 4}

	effect, err := itemServiceFromReference(ref, now)
	if err != nil {
		t.Fatalf("itemServiceFromReference() error = %v", err)
	}
	if effect.Kind != ItemServiceGold || effect.ExperienceBonusPercent != 100 || effect.SkillExperienceBonusPercent != 100 {
		t.Fatalf("effect = %+v", effect)
	}
	if effect.DailyBudget != 3*time.Hour || !effect.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("duration effect = %+v", effect)
	}
}

func TestItemPerkPacketsCarryPremiumIconAndDuration(t *testing.T) {
	premium := ItemServiceEffect{SourceRefItemID: 35567}
	if ItemServiceUsesItemPerk(premium) {
		t.Fatal("Premium must not publish the crash-prone inferred item-perk packet")
	}
	added := BuildItemPerkAdd(7, 35567, 9, 3*time.Hour, 28*24*time.Hour)
	if added.MessageID != opcode.ItemPerkAdd || len(added.Payload()) != 20 {
		t.Fatalf("add packet opcode=0x%04X payload=%x", added.MessageID, added.Payload())
	}
	fields := added.Payload()
	if binary.LittleEndian.Uint32(fields[0:4]) != 7 || binary.LittleEndian.Uint32(fields[4:8]) != 35567 || binary.LittleEndian.Uint32(fields[8:12]) != 9 || binary.LittleEndian.Uint32(fields[12:16]) != 10800000 || binary.LittleEndian.Uint32(fields[16:20]) != 2419200 {
		t.Fatalf("add payload = %x", fields)
	}
	removed := BuildItemPerkRemove(7, 35567, 9)
	if removed.MessageID != opcode.ItemPerkRemove || len(removed.Payload()) != 12 {
		t.Fatalf("remove packet opcode=0x%04X payload=%x", removed.MessageID, removed.Payload())
	}
	buff, exists := BuildItemServiceBuffAdd(7, 9, premium, 28*24*time.Hour)
	if !exists || buff.MessageID != opcode.EntityBuffAdd || len(buff.Payload()) != 16 {
		t.Fatalf("premium buff opcode=0x%04X payload=%x exists=%v", buff.MessageID, buff.Payload(), exists)
	}
	if binary.LittleEndian.Uint32(buff.Payload()[4:8]) != 5412 {
		t.Fatalf("premium skill ID payload=%x", buff.Payload())
	}
}

func TestItemPerkRemainingTimeRoundsUpToSeconds(t *testing.T) {
	added := BuildItemPerkAdd(7, 35567, 9, 3*time.Hour, time.Millisecond)
	if got := binary.LittleEndian.Uint32(added.Payload()[16:20]); got != 1 {
		t.Fatalf("remaining seconds = %d, want 1", got)
	}
}

func TestPremiumAndSkillGoldRatesStackAndExpire(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	player := &Player{}
	player.ApplyItemService(ItemServiceEffect{Kind: ItemServiceGold, ExperienceBonusPercent: 100, SkillExperienceBonusPercent: 100, ExpiresAt: now.Add(time.Hour)})
	player.ApplyItemService(ItemServiceEffect{Kind: ItemServiceSkillGold, SkillExperienceBonusPercent: 100, ExpiresAt: now.Add(time.Hour)})

	experience, skill := player.ItemServiceRates(now)
	if experience != 100 || skill != 200 {
		t.Fatalf("rates = %d/%d, want 100/200", experience, skill)
	}
	experience, skill = player.ItemServiceRates(now.Add(2 * time.Hour))
	if experience != 0 || skill != 0 {
		t.Fatalf("expired rates = %d/%d, want 0/0", experience, skill)
	}
}

func TestVisiblePremiumPlusUsesThreeHourHundredPercentService(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	ref := RefItem{Param1: 2419200}
	ref.ID = 35567
	ref.IsUseable = true
	ref.TypeInfo = TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 14}

	effect, err := itemServiceFromReference(ref, now)
	if err != nil {
		t.Fatalf("itemServiceFromReference() error = %v", err)
	}
	if effect.DailyBudget != 3*time.Hour || effect.ExperienceBonusPercent != 100 || effect.SkillExperienceBonusPercent != 100 || !effect.ExpiresAt.Equal(now.Add(28*24*time.Hour)) {
		t.Fatalf("premium plus effect = %+v", effect)
	}
}
