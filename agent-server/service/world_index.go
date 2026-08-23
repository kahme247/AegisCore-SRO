package service

import (
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/pkg/errors"
)

// worldIndex owns the invariants that must be updated atomically when a player
// enters the world. WorldService remains the public facade and retains the
// backing maps so existing focused tests and hot paths do not change layout.
type worldIndex struct {
	world *WorldService
}

func (w *WorldService) index() worldIndex {
	return worldIndex{world: w}
}

func (i worldIndex) objectByUniqueID(uniqueID uint32) (model.ISRObject, error) {
	i.world.mutex.Lock()
	defer i.world.mutex.Unlock()
	if object, exists := i.world.visibleObjects[uniqueID]; exists {
		return object, nil
	}
	return nil, errors.New(fmt.Sprintf("object with uniqueId %d does not exist", uniqueID))
}

func (i worldIndex) playerByUniqueID(uniqueID uint32) (*model.Player, error) {
	i.world.mutex.Lock()
	defer i.world.mutex.Unlock()
	if player, exists := i.world.playersByUniqueId[uniqueID]; exists {
		return player, nil
	}
	return nil, errors.New(fmt.Sprintf("player with uniqueId %d does not exist", uniqueID))
}

func (i worldIndex) playerByCharacterID(characterID int) (*model.Player, error) {
	i.world.mutex.Lock()
	defer i.world.mutex.Unlock()
	if player, exists := i.world.playersByCharacterID[characterID]; exists {
		return player, nil
	}
	return nil, errors.New(fmt.Sprintf("player with character ID %d does not exist", characterID))
}

func (i worldIndex) npcs() []*model.NPC {
	i.world.mutex.Lock()
	defer i.world.mutex.Unlock()
	npcs := make([]*model.NPC, 0, len(i.world.npcsByUniqueId))
	for _, npc := range i.world.npcsByUniqueId {
		if npc != nil {
			npcs = append(npcs, npc)
		}
	}
	return npcs
}

func (i worldIndex) addPlayer(player *model.Player) error {
	if player == nil || player.ID <= 0 || player.CharName == "" {
		return ErrPlayerIdentityInvalid
	}
	i.world.mutex.Lock()
	defer i.world.mutex.Unlock()
	if _, exists := i.world.playersByCharName[player.CharName]; exists {
		return ErrPlayerAlreadyConnected
	}
	if _, exists := i.world.playersByCharacterID[player.ID]; exists {
		return ErrPlayerAlreadyConnected
	}
	uid, err := i.world.nextUniqueIDLocked()
	if err != nil {
		return err
	}
	player.SetUniqueID(uid)
	i.world.visibleObjects[uid] = player
	regionID := player.GetNavmeshPosition().Region.ID
	i.world.visibleObjectsByRegion[regionID] = append(i.world.visibleObjectsByRegion[regionID], player)
	i.world.playersByUniqueId[uid] = player
	i.world.playersByCharName[player.CharName] = player
	i.world.playersByCharacterID[player.ID] = player
	return nil
}
