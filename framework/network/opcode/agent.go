package opcode

const (
	AgentLoginTokenRequest          uint16 = 0xE100
	AgentGameserverForPlayerRequest uint16 = 0xE101
	AgentPlayerJoinRequest          uint16 = 0xE102

	AgentGameserverForPlayerResponse uint16 = 0xF101
	AgentPlayerJoinResponse          uint16 = 0xF102

	AgentLogoutRequest        uint16 = 0x7005
	AgentLogoutResponse       uint16 = 0xB005
	AgentLogoutSuccess        uint16 = 0x300A
	AgentLogoutCancelRequest  uint16 = 0x7006
	AgentLogoutCancelResponse uint16 = 0xB006

	CharacterDataBegin uint16 = 0x34A5
	CharacterDataBody  uint16 = 0x3013
	CharacterDataEnd   uint16 = 0x34A6

	ItemOperationRequest     uint16 = 0x7034
	ItemOperationResponse    uint16 = 0xB034
	ItemRepairRequest        uint16 = 0x703E
	ItemRepairResponse       uint16 = 0xB03E
	ItemDurabilityUpdate     uint16 = 0x3052
	InventoryItemUseRequest  uint16 = 0x704C
	InventoryItemUseResponse uint16 = 0xB04C
	AlchemyElixirRequest     uint16 = 0x7150
	AlchemyElixirResponse    uint16 = 0xB150
	InventoryItemUpdate      uint16 = 0x3040

	StorageOpenRequest uint16 = 0x703C
	StorageDataBegin   uint16 = 0x3047
	StorageDataEnd     uint16 = 0x3048
	StorageData        uint16 = 0x3049

	GuildStorageOpenRequest   uint16 = 0x7250
	GuildStorageCloseRequest  uint16 = 0x7251
	GuildStorageInfoRequest   uint16 = 0x7252
	GuildStorageOpenResponse  uint16 = 0xB250
	GuildStorageCloseResponse uint16 = 0xB251
	GuildStorageDataBegin     uint16 = 0x3253
	GuildStorageDataEnd       uint16 = 0x3254
	GuildStorageData          uint16 = 0x3255

	SkillLearnRequest         uint16 = 0x70A1
	SkillLearnResponse        uint16 = 0xB0A1
	SkillMasteryLearnRequest  uint16 = 0x70A2
	SkillMasteryLearnResponse uint16 = 0xB0A2

	QuestTalk            uint16 = 0x30D4
	QuestUpdate          uint16 = 0x30D5
	QuestScript          uint16 = 0x3CA2
	QuestAbandonRequest  uint16 = 0x70D9
	QuestAbandonResponse uint16 = 0xB0D9

	COSDataResponse             uint16 = 0x30C8
	COSUpdateResponse           uint16 = 0x30C9
	COSUpdateMountStateRequest  uint16 = 0x70CB
	COSUpdateMountStateResponse uint16 = 0xB0CB
)
