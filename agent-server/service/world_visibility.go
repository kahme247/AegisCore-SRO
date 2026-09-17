package service

import (
	"math"
	"sort"
	"time"

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

type visibilityCellKey struct {
	x int
	z int
}

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
	if model.IsTeleportGateObject(object) || model.IsDimensionHolePortalObject(object) {
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
	w.updateVisibilityCellLocked(object, position)
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
	var deltas []VisibilityDelta
	for _, candidate := range candidates {
		if other, isPlayer := candidate.(*model.Player); isPlayer {
			deltas = w.reconcilePlayerPairInto(deltas, player, other)
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
	if w.visibilityCandidatesByUID == nil {
		w.visibilityCandidatesByUID = make(map[uint32]model.ISRObject)
	}
	candidatesByUID := w.visibilityCandidatesByUID
	for uid := range candidatesByUID {
		delete(candidatesByUID, uid)
	}

	w.mutex.Lock()
	if w.playersByUniqueId[playerUID] != player || w.visibleObjects[playerUID] != player {
		w.mutex.Unlock()
		return nil
	}
	w.updateVisibilityCellLocked(player, playerPosition)
	cell, validCell := visibilityCellForPosition(playerPosition)
	if validCell {
		for x := cell.x - 1; x <= cell.x+1; x++ {
			for z := cell.z - 1; z <= cell.z+1; z++ {
				for _, object := range w.visibilityObjectsByCell[visibilityCellKey{x: x, z: z}] {
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
		}
	} else {
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
	}
	w.mutex.Unlock()

	// A player that leaves range may no longer be in a neighbouring region, but
	// it is still in the current known list. Include that bounded per-player
	// set so one movement event produces the corresponding reciprocal removal.
	if knownList := player.GetKnownObjectList(); knownList != nil {
		w.visibilityKnownObjects = w.visibilityKnownObjects[:0]
		if appender, ok := knownList.(interface {
			AppendKnownObjects([]model.ISRObject) []model.ISRObject
		}); ok {
			w.visibilityKnownObjects = appender.AppendKnownObjects(w.visibilityKnownObjects)
		} else {
			for _, object := range knownList.GetKnownObjects() {
				w.visibilityKnownObjects = append(w.visibilityKnownObjects, object)
			}
		}
		for _, object := range w.visibilityKnownObjects {
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

	w.visibilityCandidateIDs = w.visibilityCandidateIDs[:0]
	ids := w.visibilityCandidateIDs
	for uid := range candidatesByUID {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	w.visibilityCandidateIDs = ids
	w.visibilityCandidateObjects = w.visibilityCandidateObjects[:0]
	candidates := w.visibilityCandidateObjects
	for _, uid := range ids {
		candidates = append(candidates, candidatesByUID[uid])
	}
	w.visibilityCandidateObjects = candidates
	return candidates
}

func visibilityCellForPosition(position navmeshv2.RtNavmeshPosition) (visibilityCellKey, bool) {
	if position.Offset == nil {
		return visibilityCellKey{}, false
	}
	x := float64(float32(position.Region.X)*navmeshv2.RegionWidth + position.Offset.X)
	z := float64(float32(position.Region.Y)*navmeshv2.RegionHeight + position.Offset.Z)
	return visibilityCellKey{
		x: int(math.Floor(x / float64(visibilityExitRange))),
		z: int(math.Floor(z / float64(visibilityExitRange))),
	}, true
}

// updateVisibilityCellLocked keeps the coarse normal-world spatial index in
// sync. The cell width equals the visibility exit range, so only the 3x3 cell
// neighbourhood can contain a player that should remain visible.
// The caller holds w.mutex.
func (w *WorldService) updateVisibilityCellLocked(object model.ISRObject, position navmeshv2.RtNavmeshPosition) {
	if w == nil || object == nil {
		return
	}
	if w.visibilityObjectsByCell == nil {
		w.visibilityObjectsByCell = make(map[visibilityCellKey][]model.ISRObject)
		w.visibilityCellByUID = make(map[uint32]visibilityCellKey)
		for uid, visible := range w.visibleObjects {
			if visible == nil {
				continue
			}
			cell, ok := visibilityCellForPosition(visible.GetNavmeshPosition())
			if !ok {
				continue
			}
			w.visibilityObjectsByCell[cell] = append(w.visibilityObjectsByCell[cell], visible)
			w.visibilityCellByUID[uid] = cell
		}
	}
	uid := object.GetUniqueID()
	cell, ok := visibilityCellForPosition(position)
	if !ok || uid == 0 {
		return
	}
	if previous, exists := w.visibilityCellByUID[uid]; exists {
		if previous == cell {
			return
		}
		objects := w.visibilityObjectsByCell[previous]
		for index, candidate := range objects {
			if candidate == object {
				objects[index] = objects[len(objects)-1]
				w.visibilityObjectsByCell[previous] = objects[:len(objects)-1]
				break
			}
		}
	}
	w.visibilityObjectsByCell[cell] = append(w.visibilityObjectsByCell[cell], object)
	w.visibilityCellByUID[uid] = cell
}

// removeVisibilityCellLocked removes terminal membership from the coarse
// spatial index. The caller holds w.mutex.
func (w *WorldService) removeVisibilityCellLocked(uid uint32) {
	if w == nil || w.visibilityCellByUID == nil {
		return
	}
	cell, exists := w.visibilityCellByUID[uid]
	if !exists {
		return
	}
	target := w.visibleObjects[uid]
	objects := w.visibilityObjectsByCell[cell]
	for index, object := range objects {
		if object == target {
			objects[index] = objects[len(objects)-1]
			w.visibilityObjectsByCell[cell] = objects[:len(objects)-1]
			break
		}
	}
	delete(w.visibilityCellByUID, uid)
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
		player.GetGameWorldID() == object.GetGameWorldID() &&
		navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), object.GetNavmeshPosition()) <= rangeLimit
	if inRange {
		playerList.AddObject(object)
		if objectList != nil {
			if player.GetBodyState() == model.Stealth || player.GetBodyState() == model.Invisible || player.GetBodyState() == model.GmInvisible {
				objectList.RemoveObject(player)
			} else {
				objectList.AddObject(player)
			}
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
	return w.reconcilePlayerPairInto(nil, player, other)
}

func (w *WorldService) reconcilePlayerPairInto(deltas []VisibilityDelta, player, other *model.Player) []VisibilityDelta {
	if player == nil || other == nil || player == other {
		return deltas
	}

	playerList := player.GetKnownObjectList()
	otherList := other.GetKnownObjectList()
	rangeLimit := visibilityRange
	if playerList != nil && otherList != nil && (playerList.Knows(other) || otherList.Knows(player)) {
		rangeLimit = visibilityExitRange
	}
	inRange := playerList != nil && otherList != nil &&
		player.GetGameWorldID() == other.GetGameWorldID() &&
		visibilityObserverEligible(player) && visibilityObserverEligible(other) &&
		w.playersAreWithinVisibilityRange(player, other, rangeLimit)

	if inRange {
		if w.playerCanSee(player, other) && playerList.AddObject(other) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: player.GetUniqueID(), SubjectUID: other.GetUniqueID(), Kind: VisibilityAdded})
		}
		if !w.playerCanSee(player, other) && playerList.RemoveObject(other) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: player.GetUniqueID(), SubjectUID: other.GetUniqueID(), Kind: VisibilityRemoved})
		}
		if w.playerCanSee(other, player) && otherList.AddObject(player) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: other.GetUniqueID(), SubjectUID: player.GetUniqueID(), Kind: VisibilityAdded})
		}
		if !w.playerCanSee(other, player) && otherList.RemoveObject(player) {
			deltas = append(deltas, VisibilityDelta{ObserverUID: other.GetUniqueID(), SubjectUID: player.GetUniqueID(), Kind: VisibilityRemoved})
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
	return visibilityObserverEligible(player)
}

func visibilityObserverEligible(player *model.Player) bool {
	return player != nil && player.GetUniqueID() != 0 && player.GetLifeState() == model.Alive
}

func (w *WorldService) playerCanSee(observer, subject *model.Player) bool {
	if !visibilityObserverEligible(observer) || !visibilityObserverEligible(subject) {
		return false
	}
	switch subject.GetBodyState() {
	case model.GmInvisible:
		return false
	case model.Stealth, model.Invisible:
		if w.referenceData == nil {
			return false
		}
		now := time.Now()
		state, level, hidden := subject.HiddenBodyState(w.referenceData, now)
		if !hidden || state != subject.GetBodyState() {
			return false
		}
		hideMask := 1
		if state == model.Invisible {
			hideMask = 2
		}
		return observer.DetectsHidden(w.referenceData, hideMask, level, now)
	default:
		return true
	}
}

func (w *WorldService) playersAreWithinVisibilityRange(first, second *model.Player, rangeLimit float32) bool {
	firstX, firstZ, firstOK := first.NavmeshHorizontalCoordinates()
	secondX, secondZ, secondOK := second.NavmeshHorizontalCoordinates()
	if !firstOK || !secondOK {
		return false
	}
	deltaX := firstX - secondX
	deltaZ := firstZ - secondZ
	return deltaX*deltaX+deltaZ*deltaZ <= rangeLimit*rangeLimit
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
