package character

import (
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type questMarkPublisher struct {
	send func(network.Packet) error
}

func (p questMarkPublisher) add(player *model.Player, definition model.QuestMark) (model.QuestMark, error) {
	if player == nil || p.send == nil {
		return model.QuestMark{}, fmt.Errorf("quest mark publisher is unavailable")
	}
	mark, err := player.AddQuestMark(definition)
	if err != nil {
		return model.QuestMark{}, err
	}
	packet, err := vsro188.BuildQuestMarkAdd(vsro188.QuestMark{
		RuntimeID: mark.RuntimeID, Flags: mark.Flags, Kind: mark.Kind, AuthoredValue: mark.AuthoredValue,
		X: mark.X, Y: mark.Y, Z: mark.Z, TargetID: mark.TargetID,
	})
	if err != nil {
		_, _ = player.RemoveQuestMark(mark.RuntimeID)
		return model.QuestMark{}, err
	}
	if err = p.send(packet); err != nil {
		_, _ = player.RemoveQuestMark(mark.RuntimeID)
		return model.QuestMark{}, err
	}
	return mark, nil
}

// remove sends 0x30D7 before unlinking the runtime record, matching the
// recovered GameServer ordering. A failed send leaves the mark registered.
func (p questMarkPublisher) remove(player *model.Player, runtimeID uint32) error {
	if player == nil || p.send == nil {
		return fmt.Errorf("quest mark publisher is unavailable")
	}
	if _, exists := player.QuestMarkByRuntimeID(runtimeID); !exists {
		return fmt.Errorf("quest mark %d is not registered", runtimeID)
	}
	packet, err := vsro188.BuildQuestMarkRemove(runtimeID)
	if err != nil {
		return err
	}
	if err = p.send(packet); err != nil {
		return err
	}
	if _, exists := player.RemoveQuestMark(runtimeID); !exists {
		return fmt.Errorf("quest mark %d disappeared during removal", runtimeID)
	}
	return nil
}

// removeByQuest emits one 0x30D7 for every owned mark in stable handle order.
func (p questMarkPublisher) removeByQuest(player *model.Player, questID int) error {
	if player == nil || questID <= 0 {
		return fmt.Errorf("invalid quest mark owner")
	}
	for _, mark := range player.QuestMarksSnapshot() {
		if mark.QuestID == questID {
			if err := p.remove(player, mark.RuntimeID); err != nil {
				return err
			}
		}
	}
	return nil
}
