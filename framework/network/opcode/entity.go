package opcode

const (
	EntityGroupSpawnBegin uint16 = 0x3017
	EntityGroupSpawnData  uint16 = 0x3019
	EntityGroupSpawnEnd   uint16 = 0x3018
	EntitySingleSpawn     uint16 = 0x3015
	EntitySingleDespawn   uint16 = 0x3016

	EntityUpdatePosition         uint16 = 0x3028
	EntityAnimationPickup        uint16 = 0x3036
	EntityEquipItem              uint16 = 0x3038
	EntityUnequipItem            uint16 = 0x3039
	EntityUpdateStats            uint16 = 0x303D
	EntityRemoveOwnership        uint16 = 0x304D
	EntityUpdatePoints           uint16 = 0x304E
	EntityAnimationPromote       uint16 = 0x3054
	EntityUpdateExperience       uint16 = 0x3056
	EntityUpdateStatus           uint16 = 0x3057
	EntityDamageEffect           uint16 = 0x3058
	EntityItemUsed               uint16 = 0x305C
	EntityEmotion                uint16 = 0x3091
	EntityUpdateMovementState    uint16 = 0x30BF
	EntityUpdateMovementSpeed    uint16 = 0x30D0
	EntityUpdateHwanLevel        uint16 = 0x30DF
	EntityUpdateAttackSpeed      uint16 = 0x3200
	EntityMask                   uint16 = 0x3207
	CharacterResurrectRequest    uint16 = 0x3053
	EntityActionRequest          uint16 = 0x7074
	EntityActionResponse         uint16 = 0xB074
	EntitySkillCastBegin         uint16 = 0xB070
	EntitySkillCastEnd           uint16 = 0xB071
	EntityBuffRemove             uint16 = 0xB072
	EntityBuffAdd                uint16 = 0xB0BD
	EntityMovementRequest        uint16 = 0x7021
	EntityMovementForwardRequest uint16 = 0x7023
	EntityMovementTurnRequest    uint16 = 0x7024
	EntityMovementResponse       uint16 = 0xB021
	MovementPositionUpdate       uint16 = 0xB023

	EntitySelectRequest  uint16 = 0x7045
	EntitySelectResponse uint16 = 0xB045
	// EntityTalkRequest is sent by the stock client after it has selected a
	// normal NPC and the player chooses to interact with it.
	EntityTalkRequest                  uint16 = 0x7046
	EntityTalkResponse                 uint16 = 0xB046
	EntityDeselectRequest              uint16 = 0x704B
	EntityDeselectResponse             uint16 = 0xB04B
	CharacterIncreaseStrengthRequest   uint16 = 0x7050
	CharacterIncreaseStrengthResponse  uint16 = 0xB050
	CharacterIncreaseIntellectRequest  uint16 = 0x7051
	CharacterIncreaseIntellectResponse uint16 = 0xB051
	TeleportUseRequest                 uint16 = 0x705A
	TeleportUseResponse                uint16 = 0xB05A
	TeleportReadyRequest               uint16 = 0x34B5
	TeleportReadyResponse              uint16 = 0x34B6
)
