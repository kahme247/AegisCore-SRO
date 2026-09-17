package character

import (
	"testing"
	"time"
)

func TestNPCInteractionRegistryRetainsOnlyLatestValidatedInteraction(t *testing.T) {
	registry := NewNPCInteractionRegistry()
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(100, 200, 300, 2)
	registry.Record(100, 201, 301, 4)
	interaction, ok := registry.Get(100)
	if !ok {
		t.Fatal("Get did not return recorded interaction")
	}
	if interaction.NPCUniqueID != 201 || interaction.NPCRefObjectID != 301 || interaction.Action != 4 || !interaction.OpenedAt.Equal(now) {
		t.Fatalf("interaction = %#v", interaction)
	}
	registry.Clear(100)
	if _, ok := registry.Get(100); ok {
		t.Fatal("Clear retained interaction")
	}
}

func TestClearNPCInteractionRemovesGlobalPlayerContext(t *testing.T) {
	const playerUID uint32 = 9881
	npcInteractions.Record(playerUID, 200, 300, 2)
	ClearNPCInteraction(playerUID)

	if _, ok := npcInteractions.Get(playerUID); ok {
		t.Fatal("interaction remained after clear")
	}
}

func TestRecentNPCInteractionSupportsEmptyFollowUpPackets(t *testing.T) {
	const playerUID uint32 = 9882
	npcInteractions.Record(playerUID, 201, 301, 0x0D)
	t.Cleanup(func() { ClearNPCInteraction(playerUID) })
	interaction, ok := RecentNPCInteraction(playerUID, 0x0D, time.Minute)
	if !ok || interaction.NPCUniqueID != 201 {
		t.Fatalf("interaction = %#v, ok=%t", interaction, ok)
	}
	if _, ok = RecentNPCInteraction(playerUID, 0x0E, time.Minute); ok {
		t.Fatal("accepted a different NPC action")
	}
}
