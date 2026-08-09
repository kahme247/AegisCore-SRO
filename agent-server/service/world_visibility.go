package service

import (
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/utils"
)

// VisibilityDeltaKind describes a domain visibility transition. It deliberately
// has no protocol meaning: captured fixtures must establish the eventual spawn
// and despawn packet encodings before an outbound adapter is added.
type VisibilityDeltaKind uint8

const (
	VisibilityAdded VisibilityDeltaKind = iota + 1
	VisibilityRemoved
)

// VisibilityDelta is a caller-owned, UID-only snapshot of one directed
// visibility change. ObserverUID is the player whose known list changed and
// SubjectUID is the player added to or removed from that list. It contains no
// live object reference, packet, or socket so consumers cannot mutate world
// state or accidentally turn this reconciliation path into an unverified wire
// encoder.
type VisibilityDelta struct {
	ObserverUID uint32
	SubjectUID  uint32
	Kind        VisibilityDeltaKind
}

const (
	visibilityRange           = float32(model.RegionHeight) / 3
	visibilityExitRange       = visibilityRange + 100
	portalVisibilityRange     = float32(model.RegionHeight)
	portalVisibilityExitRange = portalVisibilityRange + 100
)

func objectVisibilityRange(object model.ISRObject, known bool) float32 {
	if model.IsTeleportGateObject(object) {
		if known {
			return portalVisibilityExitRange
		}
		return portalVisibilityRange
	}
	if known {
		return visibilityExitRange
	}
	return visibilityRange
}

// GetNPCsKnownToPlayers returns the deduplicated NPCs currently visible to an
// active player. Mob AI uses this bounded set instead of scanning the world.
func (w *WorldService) GetNPCsKnownToPlayers() []*model.NPC {
	if w == nil || w.mutex == nil {
		return nil
	}

	w.mutex.Lock()
	players := make([]*model.Player, 0, len(w.playersByUniqueId))
	for _, player := range w.playersByUniqueId {
		if player != nil {
			players = append(players, player)
		}
	}
	w.mutex.Unlock()

	byUID := make(map[uint32]*model.NPC)
	for _, player := range players {
		if !visibilityEligible(player) || player.GetKnownObjectList() == nil {
			continue
		}
		for uid, object := range player.GetKnownObjectList().GetKnownObjects() {
			if npc, ok := object.(*model.NPC); ok && npc != nil {
				byUID[uid] = npc
			}
		}
	}

	w.mutex.Lock()
	npcs := make([]*model.NPC, 0, len(byUID))
	for uid, npc := range byUID {
		if w.npcsByUniqueId[uid] == npc {
			npcs = append(npcs, npc)
		}
	}
	w.mutex.Unlock()
	sort.Slice(npcs, func(i, j int) bool { return npcs[i].GetUniqueID() < npcs[j].GetUniqueID() })
	return npcs
}

// ActivatePlayer is the one-way transition from staged/spawning world
// membership to visible gameplay membership. GameReady calls this only after
// the session has atomically entered InWorld. The returned deltas are sorted
// and caller-owned; this method does not emit a packet.
func (w *WorldService) ActivatePlayer(player *model.Player) []VisibilityDelta {
	if w == nil || w.mutex == nil || player == nil {
		return nil
	}

	w.visibilityMutex.Lock()
	defer w.visibilityMutex.Unlock()
	if !w.isConnectedPlayer(player) {
		return nil
	}

	if player.CurrentHP() == 0 {
		player.SetLifeState(model.Dead)
	} else {
		player.SetLifeState(model.Alive)
	}
	return w.reconcilePlayerVisibilityLocked(player)
}

// ResetPlayerVisibilityForTeleport mirrors the client's SERVER_AGENT_GAME_RESET
// transition. It removes reciprocal world visibility, then clears the
// teleporting client's known/delivery state without queuing despawns for the
// object table that the client has already discarded.
func (w *WorldService) ResetPlayerVisibilityForTeleport(player *model.Player) {
	if w == nil || w.mutex == nil || player == nil {
		return
	}

	w.visibilityMutex.Lock()
	defer w.visibilityMutex.Unlock()
	if !w.isConnectedPlayer(player) {
		return
	}

	known := player.GetCharKnownObjectList()
	if known == nil {
		return
	}
	for _, object := range known.GetKnownObjects() {
		if object == nil {
			continue
		}
		if reciprocal := object.GetKnownObjectList(); reciprocal != nil {
			reciprocal.RemoveObject(player)
		}
	}
	known.ResetClientWorld()
}

// ReconcileCharacterVisibility applies a bounded, event-driven visibility
// reconciliation for one changed character. The candidate set is limited to
// the character's neighbouring regions plus the character's existing known
// player entries, so this never recreates the old full-world polling pass.
//
// Player pairs are reconciled reciprocally and produce UID-only deltas. Other
// visible world objects (including NPCs) are also reconciled reciprocally so
// the SpawnManager can serialize their queued spawn/despawn packets.
func (w *WorldService) ReconcileCharacterVisibility(character model.ICharacter) []VisibilityDelta {
	if character == nil || w == nil || w.mutex == nil {
		return nil
	}

	w.visibilityMutex.Lock()
	defer w.visibilityMutex.Unlock()
	if player, ok := character.(*model.Player); ok {
		return w.reconcilePlayerVisibilityLocked(player)
	}
	w.reconcileObjectVisibilityLocked(character)
	return nil
}

func (w *WorldService) reconcileObjectVisibilityLocked(object model.ISRObject) {
	if object == nil || object.GetUniqueID() == 0 {
		return
	}

	objectUID := object.GetUniqueID()
	position := object.GetNavmeshPosition()
	playersByUID := make(map[uint32]*model.Player)
	w.mutex.Lock()
	if w.visibleObjects[objectUID] != object {
		w.mutex.Unlock()
		return
	}
	for _, regionID := range w.visibilityNeighbourRegionIDsLocked(position.Region.ID) {
		for _, candidate := range w.visibleObjectsByRegion[regionID] {
			if player, ok := candidate.(*model.Player); ok && player != nil {
				playersByUID[player.GetUniqueID()] = player
			}
		}
	}
	w.mutex.Unlock()

	// Previously known players may now be outside the neighbouring region set.
	// Include them so the same movement step also produces reciprocal removals.
	if knownList := object.GetKnownObjectList(); knownList != nil {
		for _, candidate := range knownList.GetKnownObjects() {
			if player, ok := candidate.(*model.Player); ok && player != nil {
				playersByUID[player.GetUniqueID()] = player
			}
		}
	}

	ids := make([]uint32, 0, len(playersByUID))
	for uid := range playersByUID {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, uid := range ids {
		w.reconcilePlayerObject(playersByUID[uid], object)
	}
}

// isConnectedPlayer verifies world membership while visibilityMutex is held.
// PlayerDisconnected takes the same mutex before removing membership, which
// prevents a stale reconciliation snapshot from restoring a disconnected
// player to a peer's known list.
func (w *WorldService) isConnectedPlayer(player *model.Player) bool {
	if w == nil || w.mutex == nil || player == nil {
		return false
	}

	uid := player.GetUniqueID()
	if uid == 0 {
		return false
	}
	w.mutex.Lock()
	connected := w.playersByUniqueId[uid] == player && w.visibleObjects[uid] == player
	w.mutex.Unlock()
	return connected
}

func (w *WorldService) reconcilePlayerVisibilityLocked(player *model.Player) []VisibilityDelta {
	if !w.isConnectedPlayer(player) {
		return nil
	}

	candidates := w.visibilityCandidates(player)
	deltas := make([]VisibilityDelta, 0, len(candidates)*2)
	for _, candidate := range candidates {
		if other, isPlayer := candidate.(*model.Player); isPlayer {
			deltas = append(deltas, w.reconcilePlayerPair(player, other)...)
			continue
		}
		w.reconcilePlayerObject(player, candidate)
	}
	sortVisibilityDeltas(deltas)
	return deltas
}

// visibilityCandidates takes only a membership snapshot while the world mutex
// is held. It does all known-list and position work after release, avoiding a
// lock-order dependency on player/known-list synchronization.
func (w *WorldService) visibilityCandidates(player *model.Player) []model.ISRObject {
	if w == nil || w.mutex == nil || player == nil {
		return nil
	}

	playerUID := player.GetUniqueID()
	playerPosition := player.GetNavmeshPosition()
	candidatesByUID := make(map[uint32]model.ISRObject)

	w.mutex.Lock()
	if w.playersByUniqueId[playerUID] != player || w.visibleObjects[playerUID] != player {
		w.mutex.Unlock()
		return nil
	}
	for _, regionID := range w.visibilityNeighbourRegionIDsLocked(playerPosition.Region.ID) {
		for _, object := range w.visibleObjectsByRegion[regionID] {
			if object == nil {
				continue
			}
			otherUID := object.GetUniqueID()
			if otherUID == 0 || otherUID == playerUID {
				continue
			}
			candidatesByUID[otherUID] = object
		}
	}
	w.mutex.Unlock()

	// A player that leaves range may no longer be in a neighbouring region, but
	// it is still in the current known list. Include that bounded per-player
	// set so one movement event produces the corresponding reciprocal removal.
	if knownList := player.GetKnownObjectList(); knownList != nil {
		for _, object := range knownList.GetKnownObjects() {
			if object == nil {
				continue
			}
			otherUID := object.GetUniqueID()
			if otherUID == 0 || otherUID == playerUID {
				continue
			}
			candidatesByUID[otherUID] = object
		}
	}

	ids := make([]uint32, 0, len(candidatesByUID))
	for uid := range candidatesByUID {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	candidates := make([]model.ISRObject, 0, len(ids))
	for _, uid := range ids {
		candidates = append(candidates, candidatesByUID[uid])
	}
	return candidates
}

// reconcileVisibleObject queues a newly registered NPC/item for every active
// player already in range. Without this event path, objects spawned after a
// stationary player entered the world would remain invisible until movement.
func (w *WorldService) reconcileVisibleObject(object model.ISRObject) {
	if w == nil || w.mutex == nil || object == nil || object.GetUniqueID() == 0 {
		return
	}

	w.visibilityMutex.Lock()
	defer w.visibilityMutex.Unlock()

	position := object.GetNavmeshPosition()
	players := make([]*model.Player, 0)
	w.mutex.Lock()
	if w.visibleObjects[object.GetUniqueID()] != object {
		w.mutex.Unlock()
		return
	}
	for _, regionID := range w.visibilityNeighbourRegionIDsLocked(position.Region.ID) {
		for _, candidate := range w.visibleObjectsByRegion[regionID] {
			if player, ok := candidate.(*model.Player); ok && player != nil {
				players = append(players, player)
			}
		}
	}
	w.mutex.Unlock()

	for _, player := range players {
		w.reconcilePlayerObject(player, object)
	}
}

func (w *WorldService) reconcilePlayerObject(player *model.Player, object model.ISRObject) {
	if player == nil || object == nil || player.GetUniqueID() == object.GetUniqueID() {
		return
	}
	playerList := player.GetKnownObjectList()
	objectList := object.GetKnownObjectList()
	rangeLimit := objectVisibilityRange(object, playerList != nil && playerList.Knows(object))
	inRange := playerList != nil && visibilityEligible(player) &&
		navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), object.GetNavmeshPosition()) <= rangeLimit
	if inRange {
		playerList.AddObject(object)
		if objectList != nil {
			objectList.AddObject(player)
		}
		return
	}
	if playerList != nil {
		playerList.RemoveObject(object)
	}
	if objectList != nil {
		objectList.RemoveObject(player)
	}
}

// visibilityNeighbourRegionIDsLocked returns at most the 3x3 region window
// around regionID. It intentionally reads the membership index directly: a
// player visibility decision does not require a loaded terrain object, and
// this lets teardown tests use the same safe path before world bootstrap.
// Callers must hold w.mutex.
func (w *WorldService) visibilityNeighbourRegionIDsLocked(regionID int16) []int16 {
	x, z := utils.Int16ToXAndZ(regionID)
	regionIDs := make([]int16, 0, 9)
	for x1 := x - 1; x1 <= x+1; x1++ {
		for z1 := z - 1; z1 <= z+1; z1++ {
			// Normal-world visibility must not wrap a boundary coordinate through
			// byte conversion (for example x=-1 becoming 255). Dungeon/layer
			// addressing is intentionally outside this normal-grid reconciliation
			// until its instance metadata is implemented.
			if x1 < 0 || x1 >= normalWorldRegionColumns || z1 < 0 || z1 >= normalWorldRegionRows {
				continue
			}
			candidateID := utils.XAndZToInt16(byte(x1), byte(z1))
			if _, exists := w.visibleObjectsByRegion[candidateID]; exists {
				regionIDs = append(regionIDs, candidateID)
			}
		}
	}
	sort.Slice(regionIDs, func(i, j int) bool { return regionIDs[i] < regionIDs[j] })
	return regionIDs
}

func (w *WorldService) reconcilePlayerPair(player, other *model.Player) []VisibilityDelta {
	if player == nil || other == nil || player == other {
		return nil
	}

	playerList := player.GetKnownObjectList()
	otherList := other.GetKnownObjectList()
	rangeLimit := visibilityRange
	if playerList != nil && otherList != nil && (playerList.Knows(other) || otherList.Knows(player)) {
		rangeLimit = visibilityExitRange
	}
	inRange := playerList != nil && otherList != nil &&
		visibilityEligible(player) && visibilityEligible(other) &&
		w.playersAreWithinVisibilityRange(player, other, rangeLimit)

	deltas := make([]VisibilityDelta, 0, 2)
	if inRange {
		if playerList.AddObject(other) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: player.GetUniqueID(), SubjectUID: other.GetUniqueID(), Kind: VisibilityAdded})
		}
		if otherList.AddObject(player) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: other.GetUniqueID(), SubjectUID: player.GetUniqueID(), Kind: VisibilityAdded})
		}
		return deltas
	}

	if playerList != nil && playerList.RemoveObject(other) {
		deltas = append(deltas, VisibilityDelta{ObserverUID: player.GetUniqueID(), SubjectUID: other.GetUniqueID(), Kind: VisibilityRemoved})
	}
	if otherList != nil && otherList.RemoveObject(player) {
		deltas = append(deltas, VisibilityDelta{ObserverUID: other.GetUniqueID(), SubjectUID: player.GetUniqueID(), Kind: VisibilityRemoved})
	}
	return deltas
}

func visibilityEligible(player *model.Player) bool {
	return player != nil && player.GetUniqueID() != 0 && player.GetLifeState() == model.Alive && player.GetBodyState() != model.Invisible
}

func (w *WorldService) playersAreWithinVisibilityRange(first, second *model.Player, rangeLimit float32) bool {
	firstPosition := first.GetNavmeshPosition()
	secondPosition := second.GetNavmeshPosition()
	if firstPosition.Offset == nil || secondPosition.Offset == nil {
		return false
	}
	return navmeshv2.HorizontalDistance(firstPosition, secondPosition) <= rangeLimit
}

func sortVisibilityDeltas(deltas []VisibilityDelta) {
	sort.Slice(deltas, func(i, j int) bool {
		if deltas[i].ObserverUID != deltas[j].ObserverUID {
			return deltas[i].ObserverUID < deltas[j].ObserverUID
		}
		if deltas[i].SubjectUID != deltas[j].SubjectUID {
			return deltas[i].SubjectUID < deltas[j].SubjectUID
		}
		return deltas[i].Kind < deltas[j].Kind
	})
}
