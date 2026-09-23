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

func TestExperienceScrollServicesUseCatalogDurationMarkerAndPercentage(t *testing.T) {
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		marker    int
		kind      ItemServiceKind
		wantEXP   uint32
		wantSkill uint32
	}{
		{name: "experience", marker: itemServiceExperienceMarker, kind: ItemServiceExperience, wantEXP: 150},
		{name: "skill experience", marker: itemServiceSkillExperienceMarker, kind: ItemServiceSkillExperience, wantSkill: 150},
		{name: "event skill experience", marker: itemServiceSkillExperienceEventMarker, kind: ItemServiceSkillExperience, wantSkill: 30},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			percent := 150
			if test.marker == itemServiceSkillExperienceEventMarker {
				percent = 30
			}
			ref := RefItem{Param1: 3600, Param2: test.marker, Param3: percent}
			ref.ID, ref.IsUseable = 24170, true
			ref.TypeInfo = TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 10}
			effect, err := itemServiceFromReference(ref, now)
			if err != nil {
				t.Fatalf("itemServiceFromReference() error = %v", err)
			}
			if effect.Kind != test.kind || effect.ExperienceBonusPercent != test.wantEXP || effect.SkillExperienceBonusPercent != test.wantSkill || !effect.ExpiresAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("effect = %+v", effect)
			}
		})
	}
}

func TestItemPerkPacketsUseAuthoredActionAndValue(t *testing.T) {
	previous := RefItems
	t.Cleanup(func() { RefItems = previous })
	RefItems = map[uint32]RefItem{
		3790: {
			RefObject: RefObject{ID: 3790},
			Param2:    100663296,
			Param3:    2,
		},
	}
	effect := ItemServiceEffect{Kind: ItemServiceReinforceWeapon, SourceRefItemID: 3790}
	added := BuildItemPerkAdd(7, effect, 30*time.Minute)
	if added.MessageID != opcode.ItemPerkAdd || len(added.Payload()) != 20 {
		t.Fatalf("add packet opcode=0x%04X payload=%x", added.MessageID, added.Payload())
	}
	fields := added.Payload()
	if binary.LittleEndian.Uint32(fields[0:4]) != 7 ||
		binary.LittleEndian.Uint32(fields[4:8]) != 3790 ||
		binary.LittleEndian.Uint32(fields[8:12]) != 100663296 ||
		binary.LittleEndian.Uint32(fields[12:16]) != 2 ||
		binary.LittleEndian.Uint32(fields[16:20]) != 1800 {
		t.Fatalf("add payload = %x", fields)
	}
	removed := BuildItemPerkRemove(7, effect)
	if removed.MessageID != opcode.ItemPerkRemove || len(removed.Payload()) != 12 ||
		binary.LittleEndian.Uint32(removed.Payload()[8:12]) != 100663296 {
		t.Fatalf("remove packet opcode=0x%04X payload=%x", removed.MessageID, removed.Payload())
	}
}

func TestEnergyOfLifePerkUsesPairedIconAndConsumableParameters(t *testing.T) {
	previous := RefItems
	t.Cleanup(func() { RefItems = previous })
	RefItems = map[uint32]RefItem{
		23941: {
			RefObject: RefObject{ID: 23941, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 10}},
			Param2:    7366772,
			Param3:    14,
		},
		23947: {
			RefObject: RefObject{ID: 23947, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 14}},
			Param2:    1,
			Param3:    -1,
		},
	}
	effect := ItemServiceEffect{Kind: ItemServiceHwanDuration, SourceRefItemID: 23947}
	added := BuildItemPerkAdd(7, effect, time.Hour)
	fields := added.Payload()
	if binary.LittleEndian.Uint32(fields[4:8]) != 23947 ||
		binary.LittleEndian.Uint32(fields[8:12]) != 7366772 ||
		binary.LittleEndian.Uint32(fields[12:16]) != 14 ||
		binary.LittleEndian.Uint32(fields[16:20]) != 3600 {
		t.Fatalf("Energy of Life add payload = %x", fields)
	}
}

func TestPremiumDailyPacketsUseLinkedServiceAndSeconds(t *testing.T) {
	premium := ItemServiceEffect{SourceRefItemID: 35567}
	added, exists := BuildPremiumDailyAdd(9, premium, 28*24*time.Hour)
	if !exists || added.MessageID != opcode.PremiumDailyAdd || len(added.Payload()) != 13 {
		t.Fatalf("Premium daily add opcode=0x%04X payload=%x exists=%v", added.MessageID, added.Payload(), exists)
	}
	fields := added.Payload()
	if binary.LittleEndian.Uint32(fields[0:4]) != 9278 || binary.LittleEndian.Uint32(fields[4:8]) != 2419200 || binary.LittleEndian.Uint32(fields[8:12]) != 9 || fields[12] != 1 {
		t.Fatalf("Premium daily add payload = %x", fields)
	}
	premium.DailyUsedOn = time.Now().UTC()
	used, exists := BuildPremiumDailyAdd(9, premium, 28*24*time.Hour)
	if !exists || used.Payload()[12] != 0 {
		t.Fatalf("used Premium daily payload = %x, exists=%v", used.Payload(), exists)
	}
	removed, exists := BuildPremiumDailyRemove(9, premium)
	if !exists || removed.MessageID != opcode.PremiumDailyRemove || len(removed.Payload()) != 8 || binary.LittleEndian.Uint32(removed.Payload()[0:4]) != 9278 || binary.LittleEndian.Uint32(removed.Payload()[4:8]) != 9 {
		t.Fatalf("Premium daily remove opcode=0x%04X payload=%x exists=%v", removed.MessageID, removed.Payload(), exists)
	}
	response := BuildPremiumDailyUseSuccess(9278, 9)
	if response.MessageID != opcode.PremiumDailyUseResponse || len(response.Payload()) != 9 || response.Payload()[0] != 1 || binary.LittleEndian.Uint32(response.Payload()[1:5]) != 9278 || binary.LittleEndian.Uint32(response.Payload()[5:9]) != 9 {
		t.Fatalf("Premium daily response opcode=0x%04X payload=%x", response.MessageID, response.Payload())
	}
}

func TestItemPerkRemainingTimeRoundsUpToSeconds(t *testing.T) {
	previous := RefItems
	t.Cleanup(func() { RefItems = previous })
	RefItems = map[uint32]RefItem{3790: {RefObject: RefObject{ID: 3790}}}
	added := BuildItemPerkAdd(7, ItemServiceEffect{SourceRefItemID: 3790}, time.Millisecond)
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

func TestExperienceAndSkillExperienceScrollRatesStackWithPremium(t *testing.T) {
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	player := &Player{}
	player.ApplyItemService(ItemServiceEffect{Kind: ItemServiceGold, ExperienceBonusPercent: 100, SkillExperienceBonusPercent: 100, ExpiresAt: now.Add(time.Hour)})
	player.ApplyItemService(ItemServiceEffect{Kind: ItemServiceExperience, ExperienceBonusPercent: 150, ExpiresAt: now.Add(time.Hour)})
	player.ApplyItemService(ItemServiceEffect{Kind: ItemServiceSkillExperience, SkillExperienceBonusPercent: 50, ExpiresAt: now.Add(time.Hour)})
	if experience, skill := player.ItemServiceRates(now); experience != 250 || skill != 150 {
		t.Fatalf("stacked scroll rates = %d/%d, want 250/150", experience, skill)
	}
}

func TestPremiumRatesRequireDailyActivation(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	player := &Player{}
	token, _ := player.ApplyItemService(ItemServiceEffect{
		Kind:                        ItemServiceGold,
		SourceRefItemID:             35567,
		ExperienceBonusPercent:      100,
		SkillExperienceBonusPercent: 100,
		ExpiresAt:                   now.Add(28 * 24 * time.Hour),
	})
	if experience, skill := player.ItemServiceRates(now); experience != 0 || skill != 0 {
		t.Fatalf("inactive Premium rates = %d/%d, want 0/0", experience, skill)
	}
	if !player.ActivateItemServiceDaily(ItemServiceGold, token, now, now.Add(3*time.Hour)) {
		t.Fatal("ActivateItemServiceDaily() = false")
	}
	if experience, skill := player.ItemServiceRates(now.Add(time.Hour)); experience != 100 || skill != 100 {
		t.Fatalf("active Premium rates = %d/%d, want 100/100", experience, skill)
	}
	if experience, skill := player.ItemServiceRates(now.Add(4 * time.Hour)); experience != 0 || skill != 0 {
		t.Fatalf("elapsed Premium rates = %d/%d, want 0/0", experience, skill)
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
