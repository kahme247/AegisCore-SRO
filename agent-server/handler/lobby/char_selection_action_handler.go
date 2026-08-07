package lobby

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/protocol/vsro188"
)

const (
	DeletionTimeInDays                       = 7
	OpcodeCharSelectionActionRequest  uint16 = 0x7007
	OpcodeCharSelectionActionResponse uint16 = 0xB007

	// Actions
	CharActionCreate    uint8 = 1
	CharActionList      uint8 = 2
	CharActionDelete    uint8 = 3
	CharActionCheckName uint8 = 4
	CharActionRestore   uint8 = 5

	// Errors
	ACTION_MSG_ERROR_CHARACTER_OVER_3       uint16 = 0x405
	ACTION_MSG_ERROR_CHARACTER_SELECTWEAPON uint16 = 0x404
	ACTION_MSG_ERROR_CHARACTER_NAME_STRING  uint16 = 0x40C
	ACTION_SMERR_NOT_ALLOWED_CHARNAME       uint16 = 0x40D
	ACTION_MSG_ERROR_ID                     uint16 = 0x410
	ACTION_MSG_ERROR_OVERLAP                uint16 = 0x411
	ACTION_SMERR_MAX_USER_EXCEEDED          uint16 = 0x414
	ACTION_SMERR_INVALID_CHARGEN_INFO       uint16 = 0x403
	ACTION_SMERR_FAILED_TO_CREATE_CHARACTER uint16 = 0x406
	ACTION_SMERR_CANT_FIND_GAMESERVER       uint16 = 0x409
	ACTION_SMERR_CANT_ACCESS_PARENT_SERVER  uint16 = 0x40F
	ACTION_SMERR_FAILED_TO_CREATE_NEW_USER  uint16 = 0x412
	ACTION_SMERR_FAILED_TO_ENTERLOBBY       uint16 = 0x415

	// Memberclass
	Member uint8 = 1
	Master uint8 = 2

	ResultTrue  uint8 = 0x01
	ResultFalse uint8 = 0x02
)

func (h *CharacterSelectionHandler) handleAction(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	owner, ownerErr := characterOwner(session)
	if ownerErr != nil {
		return sendCharacterActionResult(session, 0, ResultFalse, 0)
	}
	if !session.EnterLobby() {
		return fmt.Errorf("character action is invalid while session is %s", session.LifecyclePhase())
	}

	action, err := packet.ReadByte()
	if err != nil {
		return sendCharacterActionResult(session, 0, ResultFalse, 0)
	}

	switch action {
	case CharActionCreate:
		return h.handleCreateAction(ctx, session, packet, owner, action)
	case CharActionList:
		if packet.Remaining() != 0 {
			return sendCharacterActionResult(session, action, ResultFalse, ACTION_MSG_ERROR_ID)
		}
		return h.handleListAction(ctx, session, owner, action)
	case CharActionDelete:
		return h.handleDeletionAction(ctx, session, packet, owner, action, true)
	case CharActionRestore:
		return h.handleDeletionAction(ctx, session, packet, owner, action, false)
	case CharActionCheckName:
		return h.handleCheckNameAction(ctx, session, packet, action)
	default:
		// Unknown actions are rejected without touching persistence or other
		// player state.
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_MSG_ERROR_ID)
	}
}

func (h *CharacterSelectionHandler) handleCreateAction(ctx context.Context, session *server.Session, packet network.Packet, owner characterOwnerScope, action byte) error {
	name, err := packet.ReadString()
	if err != nil || vsro188.ValidateText(name, characterNameRules()) != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_MSG_ERROR_CHARACTER_NAME_STRING)
	}
	charModel, err := packet.ReadUInt32()
	if err != nil || charModel == 0 {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	requestedScale, err := packet.ReadByte()
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	equippedChest, err := packet.ReadUInt32()
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	equippedPants, err := packet.ReadUInt32()
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	equippedBoots, err := packet.ReadUInt32()
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	equippedWeapon, err := packet.ReadUInt32()
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	if packet.Remaining() != 0 {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}

	if h == nil || h.creator == nil || h.creationPolicy == nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_FAILED_TO_CREATE_CHARACTER)
	}
	defaults, starterItems, err := h.creationPolicy.Resolve(ctx, model.CharacterCreationSelection{
		ModelID:        charModel,
		RequestedScale: requestedScale,
		Items: map[byte]uint32{
			model.SlotPrimaryWeapon: equippedWeapon,
			model.SlotChest:         equippedChest,
			model.SlotBoots:         equippedBoots,
			model.SlotPants:         equippedPants,
		},
	})
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_INVALID_CHARGEN_INFO)
	}
	char := model.Char{
		RefObjID:         int(charModel),
		User:             int(owner.UserID),
		Shard:            int(owner.ShardID),
		Name:             name,
		Scale:            defaults.Scale,
		Level:            defaults.Level,
		Exp:              defaults.Exp,
		SkillExp:         defaults.SkillExp,
		Str:              defaults.Strength,
		Int:              defaults.Intellect,
		StatPoints:       defaults.StatPoints,
		HP:               defaults.HP,
		MP:               defaults.MP,
		PosX:             defaults.PosX,
		PosY:             defaults.PosY,
		PosZ:             defaults.PosZ,
		Region:           defaults.Region,
		ReturnTeleportID: defaults.ReturnTeleportID,
		InventorySize:    defaults.InventorySize,
		IsDeleting:       false,
	}
	if _, err := h.creator.CreateCharacter(ctx, char, starterItems); err != nil {
		if errors.Is(err, model.ErrCharacterCreationLimitReached) {
			return sendCharacterActionResult(session, action, ResultFalse, ACTION_MSG_ERROR_CHARACTER_OVER_3)
		}
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_FAILED_TO_CREATE_CHARACTER)
	}
	return sendCharacterActionResult(session, action, ResultTrue, 0)
}

func (h *CharacterSelectionHandler) handleDeletionAction(ctx context.Context, session *server.Session, packet network.Packet, owner characterOwnerScope, action byte, deleting bool) error {
	name, err := packet.ReadString()
	if err != nil || vsro188.ValidateText(name, characterNameRules()) != nil || packet.Remaining() != 0 {
		return sendCharacterActionResult(session, action, ResultFalse, 0)
	}
	updated, err := h.characters.SetDeletionForOwner(ctx, name, owner.UserID, owner.ShardID, deleting)
	if err != nil || !updated {
		// Do not reveal whether the name exists but belongs to another account.
		return sendCharacterActionResult(session, action, ResultFalse, 0)
	}
	return sendCharacterActionResult(session, action, ResultTrue, 0)
}

func (h *CharacterSelectionHandler) handleCheckNameAction(ctx context.Context, session *server.Session, packet network.Packet, action byte) error {
	name, err := packet.ReadString()
	if err != nil || vsro188.ValidateText(name, characterNameRules()) != nil || packet.Remaining() != 0 {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_MSG_ERROR_CHARACTER_NAME_STRING)
	}
	exists, err := h.characters.CharacterNameExists(ctx, name)
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, 0)
	}
	if exists {
		return sendCharacterActionResult(session, action, ResultFalse, ACTION_SMERR_NOT_ALLOWED_CHARNAME)
	}
	return sendCharacterActionResult(session, action, ResultTrue, 0)
}

func characterNameRules() vsro188.TextRules {
	return vsro188.TextRules{Field: "character name", MaxRunes: 32, RequireTrimmed: true}
}

func (h *CharacterSelectionHandler) handleListAction(ctx context.Context, session *server.Session, owner characterOwnerScope, action byte) error {
	chars, err := h.characters.CharactersForOwner(ctx, owner.UserID, owner.ShardID)
	if err != nil {
		return sendCharacterActionResult(session, action, ResultFalse, 0)
	}

	p := network.EmptyPacket()
	p.MessageID = OpcodeCharSelectionActionResponse
	p.WriteByte(action)
	p.WriteByte(ResultTrue)
	p.WriteByte(byte(len(chars)))

	for _, char := range chars {
		if err := h.writeCharacterListEntry(ctx, &p, char); err != nil {
			return sendCharacterActionResult(session, action, ResultFalse, 0)
		}
	}
	return session.Send(p)
}

func (h *CharacterSelectionHandler) writeCharacterListEntry(ctx context.Context, p *network.Packet, char model.Char) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("load character inventory: %v", recovered)
		}
	}()

	p.WriteUInt32(uint32(char.RefObjID))
	p.WriteString(char.Name)
	p.WriteByte(byte(char.Scale))
	p.WriteByte(byte(char.Level))
	p.WriteUInt64(uint64(char.Exp))
	p.WriteUInt16(uint16(char.Str))
	p.WriteUInt16(uint16(char.Int))
	p.WriteUInt16(uint16(char.StatPoints))
	p.WriteUInt32(uint32(char.HP))
	p.WriteUInt32(uint32(char.MP))
	if char.IsDeleting {
		p.WriteByte(ResultTrue)
		diff := char.Utime.Add(DeletionTimeInDays * 24 * time.Hour).Sub(time.Now().UTC())
		p.WriteUInt32(uint32(int(diff.Minutes())))
	} else {
		p.WriteByte(0)
	}
	// Guild and academy are not implemented yet.
	p.WriteByte(0)
	p.WriteByte(0)
	p.WriteByte(0)

	inv, err := h.loadPlayerInventory(ctx, int64(char.ID))
	if err != nil {
		return err
	}
	appearanceSlots := make([]byte, 0, model.SlotRightRing+1)
	for _, slot := range model.SortedInventorySlots(inv.Items) {
		if slot <= model.SlotRightRing {
			appearanceSlots = append(appearanceSlots, slot)
		}
	}
	p.WriteByte(byte(len(appearanceSlots)))
	// This legacy appearance list does not carry a slot field, so its wire
	// order is explicitly the ascending equipped-slot order. Bag items are
	// serialized later in the full in-game inventory, never as lobby equipment.
	for _, slot := range appearanceSlots {
		item := inv.Items[slot]
		p.WriteUInt32(item.GetRefObjectID())
		p.WriteByte(item.OptLevel)
	}
	p.WriteByte(0) // Avatar item count
	return nil
}

func sendCharacterActionResult(session *server.Session, action, result byte, errorCode uint16) error {
	if session == nil {
		return fmt.Errorf("character action has no session")
	}
	p := network.EmptyPacket()
	p.MessageID = OpcodeCharSelectionActionResponse
	p.WriteByte(action)
	p.WriteByte(result)
	if result == ResultFalse && errorCode != 0 {
		p.WriteUInt16(errorCode)
	}
	return session.Send(p)
}
