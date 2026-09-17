package model

import (
	"time"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/sirupsen/logrus"
)

func entityHwanLevel(character any) byte {
	if titled, ok := character.(interface{ GetHwanLevel() byte }); ok {
		return titled.GetHwanLevel()
	}
	return 0
}

func WriteEntitySpawnData(p *network.Packet, object ISRObject) {
	p.WriteUInt32(object.GetRefObjectID())

	typeInfo := object.GetTypeInfo()

	// Load RefData
	if typeInfo.IsCharacter() {
		// Bionic
		if typeInfo.IsPlayerCharacter() {
			// Player Character
			player, isPlayer := object.(IPlayer)

			if !isPlayer {
				logrus.Warnf("object %s is not a player. Type: %T", object.GetName(), object)
				return
			}

			p.WriteByte(byte(player.GetScale()))
			p.WriteByte(entityHwanLevel(player))
			p.WriteByte(byte(player.GetPVPCape()))
			p.WriteByte(1) // AutoInverstXP
			inventory := player.GetInventory()
			slots := equippedAppearanceSlots(inventory)
			p.WriteByte(playerInventorySize(player))
			p.WriteByte(byte(len(slots)))

			// The legacy appearance list does not include a slot field. Keep
			// its order explicitly aligned with ascending inventory slots.
			for _, slot := range slots {
				item := inventory.Items[slot]
				p.WriteUInt32(item.GetRefObjectID())

				if item.IsEquipment() {
					p.WriteByte(item.OptLevel)
				}
			}

			writeAvatarAppearance(p, player.GetAvatarInventory())

			mask, hasMask := CharacterMask{}, false
			if masked, ok := player.(interface {
				CharacterMask(time.Time) (CharacterMask, bool)
			}); ok {
				mask, hasMask = masked.CharacterMask(time.Now())
			}
			p.WriteBool(hasMask)
			if hasMask {
				p.WriteUInt32(mask.RefCharID)
				p.WriteByte(mask.Scale)
				count := min(len(mask.Skills), 255)
				p.WriteByte(byte(count))
				for _, skillID := range mask.Skills[:count] {
					p.WriteUInt32(skillID)
				}
			}

		} else if typeInfo.IsSiegeStruct() {
			// NPC_FORTRESS_STRUCT
		}
		p.WriteUInt32(object.GetUniqueID())
		WritePosition(p, object.GetNavmeshPosition())

		// Introduce every entity with a complete stationary snapshot. If it is
		// moving, SpawnManager sends the regular B021 update after the group-spawn
		// batch; embedding a destination here makes the stock client terminate in
		// some visibility transitions.
		p.WriteByte(0)
		p.WriteByte(1)
		p.WriteByte(1)
		p.WriteUInt16(uint16(object.GetNavmeshPosition().Heading))
		character := object.(ICharacter)

		p.WriteByte(byte(character.GetLifeState()))
		p.WriteByte(0) // Unknown
		p.WriteByte(byte(character.GetMotionState()))
		p.WriteByte(byte(character.GetBodyState()))
		p.WriteFloat32(character.GetWalkSpeed())
		p.WriteFloat32(character.GetRunSpeed())
		p.WriteFloat32(character.GetHwanSpeed())

		p.WriteByte(0) // TODO BuffCount
		//	for (int i = 0; i < State.BuffCount; i++)
		//	{
		//	4   uint    Buff.RefSkillID
		//	4   uint    Buff.Duration
		//	if(skill.Params.Contains(1701213281))
		//	{
		//	//1701213281 -> atfe -> "auto transfer effect" like Recovery Division
		//	1   bool    IsCreator
		//	}
		//	}

		if typeInfo.IsPlayerCharacter() {
			player := character.(IPlayer)
			p.WriteString(player.GetName())
			writePlayerJobState(p, player.GetJob())
			p.WriteByte(byte(player.GetPKState()))
			WritePlayerTransportState(p, player)
			p.WriteByte(0) // TODO ScrollMode / 0 = None / 1 = Return Scroll / 2 = Bandit Return Scroll
			p.WriteByte(0) // TODO Interact Mode / 0 = None / 2 = P2P / 4 = P2N_Talk / 6 = OPNMKT_DEAL
			p.WriteByte(0) // unknown

			WriteGuild(p, player)

			p.WriteByte(0)    // Equipment Cooldown, probably when equipping pvp capes or job eq
			p.WriteByte(0xFF) // PK Flag
		} else if typeInfo.IsNPC() {
			if typeInfo.IsNPCNpc() {
				// Normal service NPCs must advertise talkability or the stock
				// client never emits 0x7046. Keep the inline option list empty;
				// the selected-NPC response supplies the actual service actions.
				p.WriteByte(2)
				p.WriteByte(0)
			} else {
				p.WriteByte(0)
			}

			if typeInfo.IsNPCMob() {
				rarity := byte(0)
				if npc, ok := object.(*NPC); ok {
					rarity = npc.Rarity()
				}
				p.WriteByte(rarity)
			}
			if typeInfo.IsCOS() {
				cos, ok := object.(*COS)
				if !ok {
					logrus.Warnf("COS ref=%d uid=%d has model type %T", object.GetRefObjectID(), object.GetUniqueID(), object)
					return
				}
				WriteCOSSpawnTail(p, cos.Snapshot())
			}
			//	//NPC
			//	1   byte    TalkFlag
			//	if(TalkFlag == 2)
			//	{
			//		1   byte    TalkOptionCount
			//		*   byte[]  TalkOptions
			//	}
			//
			//	if(obj.TypeID3 == 1)
			//	{
			//		//NPC_MOB
			//		1   byte    Rarity
			//		if(obj.TypeID4 == 2 || obj.TypeID4 == 3)
			//		{
			//			// NPC_MOB_THIEF, NPC_MOB_HUNTER
			//			1   byte    Appearance  //Randomized by server.
			//		}
			//	}
			//	else if(obj.TypeID3 == 3)
			//	{
			//		//NPC_COS
			//		if(obj.TypeID4 == 3 || obj.TypeID4 == 4)
			//		{
			//			//NPC_COS_P (Growth)
			//			//NPC_COS_P (Ability)
			//			2   ushort  Name.Length
			//			2   string  Name
			//		}
			//
			//		if(obj.TypeID4 == 5)
			//		{
			//			//NPC_COS_GUILD
			//			2   ushort  GuildName.Length
			//			2   string  GuildName
			//		}
			//		else
			//		{
			//			2   ushort  Owner.Name.Length
			//			2   string  Owner.Name
			//		}
			//
			//		if(obj.TypeID4 == 2 ||      //NPC_COS_T
			//			obj.TypeID4 == 3 ||      //NPC_COS_P (Growth)
			//			obj.TypeID4 == 4 ||      //NPC_COS_P (Ability)
			//			obj.TypeID4 == 5)        //NPC_COS_GUILD
			//		{
			//			1    byte    JobType
			//			if(obj.TypeID4 != 4) //NO NPC_COS_P (Ability)
			//			{
			//				1    byte    MurderFlag    //0 = White, 1 = Purple, 2 = Red
			//			}
			//
			//			if(obj.TypeID4 == 5)
			//			{
			//				//NPC_COS_GUILD
			//				4    uint    Owner.RefObjID
			//			}
			//		}
			//
			//		4    uint    Owner.UniqueID
			//	}
			//	else if (obj.TypeID3 == 4)
			//	{
			//		//NPC_FORTRESS_COS
			//		4   uint    Guild.ID
			//		2   ushort  Guild.Name.Length
			//		*   string  Guild.Name
			//	}
			//}
		}

	} else if typeInfo.IsItem() {
		// Item
		item := object.(IItem)
		if item.GetTypeInfo().IsEquipment() {
			// Persisted inventory drops are materialized as *Item even when their
			// reference type is equipment. Do not require the world representation
			// to be the *Equipment wrapper just to serialize its option level.
			switch equipment := item.(type) {
			case IEquipment:
				p.WriteByte(equipment.GetOptLevel())
			case *Item:
				p.WriteByte(equipment.OptLevel)
			default:
				p.WriteByte(0)
			}
		} else if typeInfo.IsExpendable() {
			if typeInfo.IsGold() {
				// Gold candidates carry the amount in ITEM.DATA (the same
				// item-data +0x38 field consumed by the pickup credit path).
				if concrete, ok := object.(*Item); ok && concrete != nil {
					p.WriteUInt32(concrete.Data)
				} else {
					p.WriteUInt32(0)
				}
			} else if typeInfo.IsTradeItem() || typeInfo.IsQuestItem() {
				creator := ""
				if concrete, ok := object.(*Item); ok && concrete != nil {
					creator = concrete.CreatorName
				}
				p.WriteString(creator)
			}
		}

		p.WriteUInt32(object.GetUniqueID())
		WritePosition(p, object.GetNavmeshPosition())

		ownerJID := item.GetOwner()
		// The mode-2/no-context item serializer writes an explicit marker
		// presence byte before the optional owner UID.
		p.WriteBool(ownerJID > 0)
		if ownerJID > 0 {
			p.WriteUInt32(ownerJID)
		}
		p.WriteByte(item.GetRarity())
	} else if typeInfo.IsStructure() {
		p.WriteUInt32(object.GetUniqueID())
		WritePosition(p, object.GetNavmeshPosition())

		p.WriteByte(0) // CGObjStruct +0x15C
		p.WriteByte(0) // CGObjStruct +0x15D
		if portal, ok := object.(*DimensionHolePortal); ok && IsDimensionHolePortalObject(portal) {
			// SR_GameServer 0x00601EB0 passes item Param7 (1) into
			// CGObjStruct +0x15E and CRefObjCommon +0x08 (the source item
			// reference ID loaded at 0x006A3090) into the final dword. Its
			// 0x0052F000 serializer and the stock client's 0x0056DFA0 decoder
			// agree on the remaining field order.
			p.WriteByte(1)
			p.WriteByte(6)
			p.WriteString(portal.OwnerName)
			p.WriteUInt32(portal.Definition.ItemRefID)
		} else {
			p.WriteByte(0) // CGObjStruct +0x15E
			p.WriteByte(1) // CGObjStruct's normal runtime state

			if IsTeleportGateObject(object) {
				// These are CGObjStruct runtime-state fields, not RefTeleport IDs.
				// The client resolves the teleport-data record from RefObjectID. Treating
				// TeleportID as the first runtime field makes it an invalid object/state
				// reference and crashes while the group-spawn record is consumed.
				p.WriteUInt32(0)
				p.WriteUInt32(0)
			}
		}

		// if unkByte1 == 1 {
		//     p.WriteUint32(0) // TODO unkUint2
		// 	   p.WriteByte(0)	// TODO unkByte4
		// }
	} else if object.GetRefObjectID() == 0xFFFFFFFF {
		p.WriteUInt16(0) // TODO unkUint160
		p.WriteUInt32(0) // TODO RefSkillID
		p.WriteUInt32(object.GetUniqueID())
		WritePosition(p, object.GetNavmeshPosition())
	}

	if p.MessageID == opcode.EntitySingleSpawn {
		if typeInfo.IsCharacter() || typeInfo.IsStructure() {
			p.WriteByte(0) // TODO unkByte5
		} else if typeInfo.IsItem() {
			// SR_GameServer 0x00533F70 appends one update-state byte after the
			// item's polymorphic mode-2 record. Only states 5 and 6 append the
			// following u32 association. The ordinary ground-entry state is 0.
			// Writing an unconditional u32 leaves four bytes unread by client
			// CIItem update parser 0x009E1A30 and triggers its packet assertion.
			p.WriteByte(0)
		}
	}
}

// WriteCOSSpawnTail is the vSRO 1.188 NPC_COS branch consumed after the
// ordinary bionic/NPC state. The layout is cross-checked against RSBot's
// SpawnedCos reader; unsupported guild metadata remains explicit and bounded.
func WriteCOSSpawnTail(p *network.Packet, cos COSSnapshot) {
	// Ride transports (TypeID4=1) end after the ordinary NPC state. Their
	// owner association is delivered owner-only by 0x30C8, not in 0x3019.
	if cos.Kind == COSRideTransport {
		return
	}
	switch cos.Kind {
	case COSGrowthPet, COSAbilityPet, COSFellowPet:
		p.WriteString(cos.Name)
	case COSGuild:
		p.WriteString(cos.GuildName)
	}
	switch cos.Kind {
	case COSJobTransport, COSGrowthPet, COSAbilityPet, COSGuild, COSCaptured, COSFellowPet:
		p.WriteString(cos.OwnerName)
	}
	switch cos.Kind {
	case COSJobTransport, COSGrowthPet, COSAbilityPet, COSGuild, COSFellowPet:
		p.WriteByte(0) // owner job type
		if cos.Kind != COSAbilityPet {
			p.WriteByte(0) // owner PvP/murder state
		}
		if cos.Kind == COSGuild {
			p.WriteUInt32(0) // owner reference/guild field
		}
	}
	p.WriteUInt32(cos.OwnerUniqueID)
	if cos.Kind == COSFellowPet {
		p.WriteByte(0) // v188 fellow trailing discriminator
	}
}

func WriteEntitySpawn(p *network.Packet, player *Player) {
	// TODO not just players supported
	p.WriteUInt32(uint32(player.GetRefObjectID()))

	p.WriteByte(byte(player.Scale))
	p.WriteByte(player.GetHwanLevel())
	p.WriteByte(byte(player.PVPCape))
	p.WriteByte(1) // AutoInverstXP
	inventory := player.GetInventory()
	slots := equippedAppearanceSlots(inventory)
	p.WriteByte(player.GetInventorySize())
	p.WriteByte(byte(len(slots)))

	// The legacy appearance list does not include a slot field. Keep its
	// order explicitly aligned with ascending inventory slots.
	for _, slot := range slots {
		item := inventory.Items[slot]
		p.WriteUInt32(item.GetRefObjectID())

		if item.IsEquipment() {
			p.WriteByte(item.OptLevel)
		}
	}

	writeAvatarAppearance(p, player.GetAvatarInventory())

	p.WriteByte(0) // HasMask
	p.WriteUInt32(player.UniqueID)
	WritePosition(p, player.GetNavmeshPosition())

	// TODO movement still does not save a target location
	p.WriteByte(0)                                             // Movement.HasDestination
	p.WriteByte(1)                                             // Movement.Type
	p.WriteByte(1)                                             // Movement Source | 0 - Spinning | 1 - Skywalking
	p.WriteUInt16(uint16(player.GetNavmeshPosition().Heading)) // Angle

	p.WriteByte(byte(player.LifeState))
	p.WriteByte(0) // unknown
	p.WriteByte(byte(player.MotionState))
	p.WriteByte(byte(player.BodyState))
	p.WriteFloat32(player.GetWalkSpeed()) // WalkSpeed
	p.WriteFloat32(player.GetRunSpeed())
	p.WriteFloat32(player.GetHwanSpeed()) // HwanSpeed TODO: is this correct?
	p.WriteByte(0)                        // TODO BuffCount

	p.WriteString(player.GetName())
	writePlayerJobState(p, player.GetJob())
	p.WriteByte(byte(player.GetPKState()))
	WritePlayerTransportState(p, player)
	p.WriteByte(0) // TODO ScrollMode / 0 = None / 1 = Return Scroll / 2 = Bandit Return Scroll
	p.WriteByte(0) // TODO Interact Mode / 0 = None / 2 = P2P / 4 = P2N_Talk / 6 = OPNMKT_DEAL
	p.WriteByte(0) // unknown

	WriteGuild(p, player)

	p.WriteByte(0)    // Equipment Cooldown, probably when equipping pvp capes or job eq
	p.WriteByte(0xFF) // PK Flag

	// TODO:
	//  - Check if inventory contains job equipment
	//  - Check interact mode os P2N_Talk
	//  - If player has transport(camel, horse, etc.) send transport's unique id

	if p.MessageID == opcode.EntitySingleSpawn {
		p.WriteByte(0) // TODO find out what this is
	}
}

func writePlayerJobState(packet *network.Packet, job PlayerJob) {
	if job.Active() {
		packet.WriteByte(job.Type)
		packet.WriteByte(job.Level)
		return
	}
	packet.WriteByte(PlayerJobNone)
	// The 0x3015/0x3019 player-spawn reader always consumes a level byte,
	// including the no-job discriminator. This differs from the B045 player
	// selection body, whose no-job branch omits the level.
	packet.WriteByte(0)
}

func equippedAppearanceSlots(inventory Inventory) []byte {
	all := SortedInventorySlots(inventory.Items)
	slots := make([]byte, 0, len(all))
	for _, slot := range all {
		if slot <= SlotRightRing {
			slots = append(slots, slot)
		}
	}
	return slots
}

func writeAvatarAppearance(p *network.Packet, inventory Inventory) {
	slots := SortedInventorySlots(inventory.Items)
	p.WriteByte(AvatarInventorySize)
	p.WriteByte(byte(len(slots)))
	for _, slot := range slots {
		item := inventory.Items[slot]
		p.WriteUInt32(item.GetRefObjectID())
		if item.IsEquipment() {
			p.WriteByte(item.OptLevel)
		}
	}
}

func playerInventorySize(player IPlayer) byte {
	if sized, ok := player.(interface{ GetInventorySize() byte }); ok {
		return sized.GetInventorySize()
	}
	return BaseInventorySize
}

// WritePlayerTransportState writes the stock player-spawn transport segment:
// mounted flag, combat flag, and the transport unique ID only when mounted.
func WritePlayerTransportState(p *network.Packet, player IPlayer) {
	snapshot := player.GetCOSStateSnapshot()
	mounted := snapshot.Mounted && snapshot.TransportUniqueID != 0
	p.WriteBool(mounted)
	p.WriteByte(0) // TODO in combat?
	if mounted {
		p.WriteUInt32(snapshot.TransportUniqueID)
	}
}

func WritePosition(p *network.Packet, position navmeshv2.RtNavmeshPosition) {
	p.WriteUInt16(uint16(position.Region.ID))
	p.WriteFloat32(position.Offset.X)
	p.WriteFloat32(position.Offset.Y)
	p.WriteFloat32(position.Offset.Z)
	p.WriteUInt16(uint16(position.Heading))
}

func WriteGuild(p *network.Packet, player IPlayer) {
	// todo
	p.WriteUInt16(0) // GuildNameLength

	p.WriteUInt32(0) // Guild ID
	p.WriteUInt16(0) // Guild Member Nickname length
	p.WriteUInt32(0) // Guild last crest rev
	p.WriteUInt32(0) // Union ID
	p.WriteUInt32(0) // Union last crest rev
	p.WriteByte(0)   // Guild IsFriendly
	p.WriteByte(0)   // GuildMember.SiegeAuthority
}
