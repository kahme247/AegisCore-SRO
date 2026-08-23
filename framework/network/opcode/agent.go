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
	AgentPingRequest          uint16 = 0x7F00
	AgentPingResponse         uint16 = 0xBF00

	CharacterDataBegin uint16 = 0x34A5
	CharacterDataBody  uint16 = 0x3013
	CharacterDataEnd   uint16 = 0x34A6

	ItemOperationRequest     uint16 = 0x7034
	ItemOperationResponse    uint16 = 0xB034
	NPCBuyBackRequest        uint16 = 0x7168
	NPCBuyBackResponse       uint16 = 0xB168
	ItemRepairRequest        uint16 = 0x703E
	ItemRepairResponse       uint16 = 0xB03E
	ItemDurabilityUpdate     uint16 = 0x3052
	InventoryItemUseRequest  uint16 = 0x704C
	InventoryItemUseResponse uint16 = 0xB04C
	AlchemyElixirRequest     uint16 = 0x7150
	AlchemyElixirResponse    uint16 = 0xB150
	AlchemyStoneRequest      uint16 = 0x7151
	AlchemyStoneResponse     uint16 = 0xB151
	AlchemySocketRequest     uint16 = 0x716A
	AlchemySocketResponse    uint16 = 0xB16A
	InventoryItemUpdate      uint16 = 0x3040
	ItemMallBalanceUpdate    uint16 = 0x3153
	PremiumDailyAdd          uint16 = 0x3159
	PremiumDailyRemove       uint16 = 0x315B
	PremiumDailyUseRequest   uint16 = 0x715F
	PremiumDailyUseResponse  uint16 = 0xB15F
	ItemPerkAdd              uint16 = 0x325F
	ItemPerkRemove           uint16 = 0x3261

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

	GuildEntityUpdate        uint16 = 0x30FF
	GuildEntityRemove        uint16 = 0x3100
	GuildInfoData            uint16 = 0x3101
	GuildUnionInfo           uint16 = 0x3102
	GuildInfoBegin           uint16 = 0x34B3
	GuildInfoEnd             uint16 = 0x34B4
	GuildUpdate              uint16 = 0x38F5
	GuildCreateRequest       uint16 = 0x70F0
	GuildDisbandRequest      uint16 = 0x70F1
	GuildLeaveRequest        uint16 = 0x70F2
	GuildInviteRequest       uint16 = 0x70F3
	GuildKickRequest         uint16 = 0x70F4
	GuildNoticeRequest       uint16 = 0x70F9
	GuildPromoteRequest      uint16 = 0x70FA
	GuildUnionInviteRequest  uint16 = 0x70FB
	GuildUnionLeaveRequest   uint16 = 0x70FC
	GuildUnionKickRequest    uint16 = 0x70FD
	GuildTransferRequest     uint16 = 0x7103
	GuildPermissionRequest   uint16 = 0x7104
	GuildNicknameRequest     uint16 = 0x7256
	GuildDonateRequest       uint16 = 0x7258
	GuildCreateResponse      uint16 = 0xB0F0
	GuildDisbandResponse     uint16 = 0xB0F1
	GuildLeaveResponse       uint16 = 0xB0F2
	GuildInviteResponse      uint16 = 0xB0F3
	GuildKickResponse        uint16 = 0xB0F4
	GuildNoticeResponse      uint16 = 0xB0F9
	GuildPromoteResponse     uint16 = 0xB0FA
	GuildUnionInviteResponse uint16 = 0xB0FB
	GuildUnionLeaveResponse  uint16 = 0xB0FC
	GuildUnionKickResponse   uint16 = 0xB0FD
	GuildTransferResponse    uint16 = 0xB103
	GuildPermissionResponse  uint16 = 0xB104
	GuildNicknameResponse    uint16 = 0xB256
	GuildDonateResponse      uint16 = 0xB258

	AcademyCreateRequest            uint16 = 0x7470
	AcademyDisbandRequest           uint16 = 0x7471
	AcademyKickRequest              uint16 = 0x7473
	AcademyLeaveRequest             uint16 = 0x7474
	AcademyGradeRequest             uint16 = 0x7475
	AcademyCommentRequest           uint16 = 0x7477
	AcademyHonorRankRequest         uint16 = 0x7478
	AcademyMatchingRegisterRequest  uint16 = 0x747A
	AcademyMatchingChangeRequest    uint16 = 0x747B
	AcademyMatchingDeleteRequest    uint16 = 0x747C
	AcademyMatchingListRequest      uint16 = 0x747D
	AcademyMatchingJoinRequest      uint16 = 0x747E
	AcademyCreateResponse           uint16 = 0xB470
	AcademyDisbandResponse          uint16 = 0xB471
	AcademyKickResponse             uint16 = 0xB473
	AcademyLeaveResponse            uint16 = 0xB474
	AcademyGradeResponse            uint16 = 0xB475
	AcademyCommentResponse          uint16 = 0xB477
	AcademyHonorRankResponse        uint16 = 0xB478
	AcademyMatchingRegisterResponse uint16 = 0xB47A
	AcademyMatchingChangeResponse   uint16 = 0xB47B
	AcademyMatchingDeleteResponse   uint16 = 0xB47C
	AcademyMatchingListResponse     uint16 = 0xB47D
	AcademyMatchingJoinResponse     uint16 = 0xB47E
	AcademyUpdate                   uint16 = 0x3C80
	AcademyInfo                     uint16 = 0x3C81

	JobUpdateExperience     uint16 = 0x30E6
	JobJoinRequest          uint16 = 0x70E1
	JobLeaveRequest         uint16 = 0x70E2
	JobAliasRequest         uint16 = 0x70E3
	JobRankingRequest       uint16 = 0x70E4
	JobOutcomeRequest       uint16 = 0x70E5
	JobPreviousInfoRequest  uint16 = 0x70E6
	JobJoinResponse         uint16 = 0xB0E1
	JobLeaveResponse        uint16 = 0xB0E2
	JobAliasResponse        uint16 = 0xB0E3
	JobRankingResponse      uint16 = 0xB0E4
	JobOutcomeResponse      uint16 = 0xB0E5
	JobPreviousInfoResponse uint16 = 0xB0E6

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
	COSCommandRequest           uint16 = 0x70C5
	COSCommandResponse          uint16 = 0xB0C5
	COSTerminationRequest       uint16 = 0x70C6
	COSTerminationResponse      uint16 = 0xB0C6
	COSUpdateMountStateRequest  uint16 = 0x70CB
	COSUpdateMountStateResponse uint16 = 0xB0CB
	COSUnsummonRequest          uint16 = 0x7116
	COSUnsummonResponse         uint16 = 0xB116
	COSNameRequest              uint16 = 0x7117
	COSNameResponse             uint16 = 0xB117
	COSSettingsRequest          uint16 = 0x7420
	COSSettingsResponse         uint16 = 0xB420
)
