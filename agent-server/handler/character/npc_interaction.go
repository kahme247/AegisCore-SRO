package character

import (
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// NPCInteraction is the server-validated context shared by NPC-backed
// services. It prevents a later shop, repair, teleport, or quest packet from
// naming an arbitrary NPC that the player did not just interact with.
type NPCInteraction struct {
	PlayerUniqueID         uint32
	NPCUniqueID            uint32
	NPCRefObjectID         uint32
	Action                 byte
	PendingQuestID         int
	OfferedQuestIDs        []int
	PendingDialogQuestID   int
	DialogQuestIDs         []int
	DialogKeys             []string
	PendingDeliveryQuestID int
	DeliveryQuestIDs       []int
	DeliveryKeys           []string
	PendingInstanceQuestID int
	PendingNoticeClose     bool
	OpenedAt               time.Time
}

func (r *NPCInteractionRegistry) SetNoticeClose(playerUID uint32) bool {
	if r == nil || playerUID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingQuestID = 0
	interaction.OfferedQuestIDs = nil
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = nil
	interaction.DialogKeys = nil
	interaction.PendingDeliveryQuestID = 0
	interaction.DeliveryQuestIDs = nil
	interaction.DeliveryKeys = nil
	interaction.PendingInstanceQuestID = 0
	interaction.PendingNoticeClose = true
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) SetInstanceEntry(playerUID uint32, questID int) bool {
	if r == nil || playerUID == 0 || questID <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingInstanceQuestID = questID
	interaction.PendingQuestID = 0
	interaction.OfferedQuestIDs = nil
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = nil
	interaction.DialogKeys = nil
	r.entries[playerUID] = interaction
	return true
}

// NPCInteractionRegistry deliberately keeps only the most-recent interaction
// per player. A new NPC action atomically supersedes the old context.
type NPCInteractionRegistry struct {
	mu      sync.RWMutex
	entries map[uint32]NPCInteraction
	now     func() time.Time
}

func NewNPCInteractionRegistry() *NPCInteractionRegistry {
	return &NPCInteractionRegistry{entries: make(map[uint32]NPCInteraction), now: time.Now}
}

func (r *NPCInteractionRegistry) Record(playerUID, npcUID, refObjectID uint32, action byte) {
	if r == nil || playerUID == 0 || npcUID == 0 || refObjectID == 0 || action == 0 {
		return
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	r.mu.Lock()
	if r.entries == nil {
		r.entries = make(map[uint32]NPCInteraction)
	}
	r.entries[playerUID] = NPCInteraction{
		PlayerUniqueID: playerUID,
		NPCUniqueID:    npcUID,
		NPCRefObjectID: refObjectID,
		Action:         action,
		OpenedAt:       now(),
	}
	r.mu.Unlock()
}

func (r *NPCInteractionRegistry) Get(playerUID uint32) (NPCInteraction, bool) {
	if r == nil || playerUID == 0 {
		return NPCInteraction{}, false
	}
	r.mu.RLock()
	interaction, ok := r.entries[playerUID]
	r.mu.RUnlock()
	return interaction, ok
}

func (r *NPCInteractionRegistry) SelectQuest(playerUID uint32, questID int) bool {
	if r == nil || playerUID == 0 || questID <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingQuestID = questID
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) SetQuestOffers(playerUID uint32, questIDs []int) bool {
	if r == nil || playerUID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.OfferedQuestIDs = append([]int(nil), questIDs...)
	interaction.PendingQuestID = 0
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = nil
	interaction.DialogKeys = nil
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) SetDialogs(playerUID uint32, prompts []model.QuestDialogPrompt) bool {
	if r == nil || playerUID == 0 || len(prompts) == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingQuestID = 0
	interaction.OfferedQuestIDs = nil
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = make([]int, len(prompts))
	interaction.DialogKeys = make([]string, len(prompts))
	for index, prompt := range prompts {
		interaction.DialogQuestIDs[index] = prompt.QuestID
		interaction.DialogKeys[index] = prompt.DialogueKey
	}
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) SetDeliveries(playerUID uint32, prompts []model.QuestDeliveryPrompt) bool {
	if r == nil || playerUID == 0 || len(prompts) == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingQuestID = 0
	interaction.OfferedQuestIDs = nil
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = nil
	interaction.DialogKeys = nil
	interaction.PendingDeliveryQuestID = 0
	interaction.DeliveryQuestIDs = make([]int, len(prompts))
	interaction.DeliveryKeys = make([]string, len(prompts))
	for index, prompt := range prompts {
		interaction.DeliveryQuestIDs[index] = prompt.QuestID
		interaction.DeliveryKeys[index] = prompt.DialogueKey
	}
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) SelectDelivery(playerUID uint32, questID int) bool {
	if r == nil || playerUID == 0 || questID <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingDeliveryQuestID = questID
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) ClearSelectedDelivery(playerUID uint32) {
	if r == nil || playerUID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return
	}
	interaction.PendingDeliveryQuestID = 0
	interaction.DeliveryQuestIDs = nil
	interaction.DeliveryKeys = nil
	r.entries[playerUID] = interaction
}

func (r *NPCInteractionRegistry) SelectDialog(playerUID uint32, questID int) bool {
	if r == nil || playerUID == 0 || questID <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return false
	}
	interaction.PendingDialogQuestID = questID
	r.entries[playerUID] = interaction
	return true
}

func (r *NPCInteractionRegistry) ClearSelectedQuest(playerUID uint32) {
	if r == nil || playerUID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return
	}
	interaction.PendingQuestID = 0
	r.entries[playerUID] = interaction
}

func (r *NPCInteractionRegistry) ClearSelectedDialog(playerUID uint32) {
	if r == nil || playerUID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	interaction, ok := r.entries[playerUID]
	if !ok {
		return
	}
	interaction.PendingDialogQuestID = 0
	interaction.DialogQuestIDs = nil
	interaction.DialogKeys = nil
	r.entries[playerUID] = interaction
}

func (r *NPCInteractionRegistry) Clear(playerUID uint32) {
	if r == nil || playerUID == 0 {
		return
	}
	r.mu.Lock()
	delete(r.entries, playerUID)
	r.mu.Unlock()
}

var npcInteractions = NewNPCInteractionRegistry()

// ClearNPCInteraction removes the transient interaction context for a player
// that has left the world. Runtime unique IDs can be reused after a disconnect,
// so keeping the old context would allow a later session to inherit an NPC
// action it never validated.
func ClearNPCInteraction(playerUID uint32) {
	npcInteractions.Clear(playerUID)
}

// HasRecentNPCInteraction lets an NPC-backed service prove that the client
// completed the validated 0x7046 interaction before accepting a follow-up
// packet. The short lifetime prevents a previously opened window from being
// replayed after the player has moved on to another service.
func HasRecentNPCInteraction(playerUID, npcUID uint32, action byte, maxAge time.Duration) bool {
	interaction, ok := npcInteractions.Get(playerUID)
	if !ok || interaction.NPCUniqueID != npcUID || interaction.Action != action {
		return false
	}
	if maxAge <= 0 {
		return true
	}
	now := time.Now
	if npcInteractions.now != nil {
		now = npcInteractions.now
	}
	return !interaction.OpenedAt.IsZero() && now().Sub(interaction.OpenedAt) <= maxAge
}

// RecentNPCInteraction returns the latest server-validated interaction for an
// empty follow-up request whose stock wire layout does not repeat the NPC UID.
func RecentNPCInteraction(playerUID uint32, action byte, maxAge time.Duration) (NPCInteraction, bool) {
	interaction, ok := npcInteractions.Get(playerUID)
	if !ok || interaction.Action != action {
		return NPCInteraction{}, false
	}
	if maxAge <= 0 {
		return interaction, true
	}
	now := time.Now
	if npcInteractions.now != nil {
		now = npcInteractions.now
	}
	if interaction.OpenedAt.IsZero() || now().Sub(interaction.OpenedAt) > maxAge {
		return NPCInteraction{}, false
	}
	return interaction, true
}
