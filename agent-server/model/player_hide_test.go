package model

import (
	"testing"
	"time"
)

func TestHiddenBodyStateUsesStrongestLiveHideDescriptor(t *testing.T) {
	now := time.Now()
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		10: {ID: 10, HideType: 1, HideLevel: 8},
		11: {ID: 11, HideType: 2, HideLevel: 8},
		12: {ID: 12, HideType: 1, HideLevel: 12},
	}})
	player := &Player{ActiveBuffs: map[uint32]ActiveBuff{
		1: {SkillID: 10, Token: 1, ExpiresAt: now.Add(time.Minute)},
		2: {SkillID: 11, Token: 2, ExpiresAt: now.Add(time.Minute)},
		3: {SkillID: 12, Token: 3, ExpiresAt: now.Add(-time.Second)},
	}}
	state, level, ok := player.HiddenBodyState(catalog, now)
	if !ok || state != Invisible || level != 8 {
		t.Fatalf("hidden state = state:%d level:%d ok:%t", state, level, ok)
	}
}

func TestDetectsHiddenUsesMaskAndLevel(t *testing.T) {
	now := time.Now()
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		20: {ID: 20, DetectionType: 5, DetectionLevel: 8, ReferenceRadius: 100},
		21: {ID: 21, DamageTypeFamily: 6, DamageTypePrecedence: 10, ReferenceRadius: 150},
	}})
	player := &Player{ActiveBuffs: map[uint32]ActiveBuff{1: {SkillID: 20, Token: 1, ExpiresAt: now.Add(time.Minute)}}}
	if !player.DetectsHidden(catalog, 1, 8, now) {
		t.Fatal("dtt mask 5 did not reveal compatible hide mask 1")
	}
	if player.DetectsHidden(catalog, 2, 8, now) {
		t.Fatal("dtt mask 5 revealed incompatible hide mask 2")
	}
	if player.DetectsHidden(catalog, 1, 9, now) {
		t.Fatal("dtt reveal ignored level")
	}
	player.ActiveBuffs = map[uint32]ActiveBuff{2: {SkillID: 21, Token: 2, ExpiresAt: now.Add(time.Minute)}}
	if !player.DetectsHidden(catalog, 2, 10, now) {
		t.Fatal("dttp mask 6 did not reveal compatible hide mask 2")
	}
}
