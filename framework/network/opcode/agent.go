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

	CharacterDataBegin         uint16 = 0x34A5
	CharacterDataBody          uint16 = 0x3013
	CharacterDataEnd           uint16 = 0x34A6
	AlchemyAvatarRequest       uint16 = 0x34A7
	AlchemyAvatarResponse      uint16 = 0x34A8
	AlchemyAvatarGrantRequest  uint16 = 0x34A9
	AlchemyAvatarGrantResponse uint16 = 0x34AA
	GMForceCircleUpdate        uint16 = 0x34BB

	ItemOperationRequest         uint16 = 0x7034
	ItemOperationResponse        uint16 = 0xB034
	NPCBuyBackRequest            uint16 = 0x7168
	NPCBuyBackResponse           uint16 = 0xB168
	ItemRepairRequest            uint16 = 0x703E
	ItemRepairResponse           uint16 = 0xB03E
	ItemDurabilityUpdate         uint16 = 0x3052
	InventorySizeUpdate          uint16 = 0x3092
	InventoryItemUseRequest      uint16 = 0x704C
	InventoryItemUseResponse     uint16 = 0xB04C
	AlchemyElixirRequest         uint16 = 0x7150
	AlchemyElixirResponse        uint16 = 0xB150
	AlchemyStoneRequest          uint16 = 0x7151
	AlchemyStoneResponse         uint16 = 0xB151
	AlchemyMaterialRequest       uint16 = 0x7155
	AlchemyMaterialResponse      uint16 = 0xB155
	AlchemyDismantleRequest      uint16 = 0x7157
	AlchemyDismantleResponse     uint16 = 0xB157
	AlchemySocketRequest         uint16 = 0x716A
	AlchemySocketResponse        uint16 = 0xB16A
	InventoryItemUpdate          uint16 = 0x3040
	InventoryEquipCountdownStart uint16 = 0x3041
	InventoryEquipCountdownStop  uint16 = 0x3042
	ItemMallBalanceUpdate        uint16 = 0x3153
	ItemMallCashNotification     uint16 = 0x3154
	PremiumDailyAdd              uint16 = 0x3159
	PremiumDailyRemove           uint16 = 0x315B
	PremiumDailyUseRequest       uint16 = 0x715F
	PremiumDailyUseResponse      uint16 = 0xB15F
	ItemPerkAdd                  uint16 = 0x325F
	ItemPerkRemove               uint16 = 0x3261

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
	AcademyBuffUpdate               uint16 = 0x3C82

	OpenMarketDetailRequest      uint16 = 0x7506
	OpenMarketCloseRequest       uint16 = 0x7507
	OpenMarketRegisterRequest    uint16 = 0x7508
	OpenMarketUnregisterRequest  uint16 = 0x7509
	OpenMarketBuyRequest         uint16 = 0x750A
	OpenMarketSettleRequest      uint16 = 0x750B
	OpenMarketSearchRequest      uint16 = 0x750C
	OpenMarketListRequest        uint16 = 0x750E
	OpenMarketDetailResponse     uint16 = 0xB506
	OpenMarketCloseResponse      uint16 = 0xB507
	OpenMarketRegisterResponse   uint16 = 0xB508
	OpenMarketUnregisterResponse uint16 = 0xB509
	OpenMarketBuyResponse        uint16 = 0xB50A
	OpenMarketSettleResponse     uint16 = 0xB50B
	OpenMarketSearchResponse     uint16 = 0xB50C
	OpenMarketListResponse       uint16 = 0xB50E
	OpenMarketUpdate             uint16 = 0x350D

	JobUpdatePrice          uint16 = 0x30E0
	JobUpdateExperience     uint16 = 0x30E6
	JobCOSDistance          uint16 = 0x30E7
	JobUpdateTradeScale     uint16 = 0x30E8
	JobUpdateSafeTrade      uint16 = 0x34D5
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
	SkillWithdrawRequest      uint16 = 0x7202
	SkillWithdrawResponse     uint16 = 0xB202
	MasteryWithdrawRequest    uint16 = 0x7203
	MasteryWithdrawResponse   uint16 = 0xB203

	QuestTalk                    uint16 = 0x30D4
	QuestUpdate                  uint16 = 0x30D5
	QuestMarkAdd                 uint16 = 0x30D6
	QuestMarkRemove              uint16 = 0x30D7
	QuestGatherResult            uint16 = 0x30DA
	QuestCatchResult             uint16 = 0x30DC
	QuestNotification            uint16 = 0x30EC
	QuestRewardPrompt            uint16 = 0x3514
	GlobalEventUpdate            uint16 = 0x3CA2
	QuestInteractionRequest      uint16 = 0x70D8
	QuestInteractionResponse     uint16 = 0xB0D8
	QuestAbandonRequest          uint16 = 0x70D9
	QuestAbandonResponse         uint16 = 0xB0D9
	QuestLocalActionRequest      uint16 = 0x70DB
	QuestLocalActionResponse     uint16 = 0xB0DB
	QuestRewardSelectionRequest  uint16 = 0x7515
	QuestRewardSelectionResponse uint16 = 0xB515

	// QuestScript is retained as a source-compatibility alias. Binary analysis
	// identifies 0x3CA2 as an adjacent global-event update, not a quest packet.
	QuestScript = GlobalEventUpdate

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

	// Cross-binary vSRO 1.188 Agent opcode candidates. Each value is present
	// in both SR_GameServer.exe and sro_client.exe and is named by the local
	// protocol corpus; direction-specific behavior still belongs in the registry.
	GameNotify                        uint16 = 0x300C
	EnvironmentCelestialUpdate        uint16 = 0x3027
	COSUpdateState                    uint16 = 0x30CA
	PKUpdatePenalty                   uint16 = 0x30CD
	PKUpdateDaily                     uint16 = 0x30CE
	PKUpdateLevel                     uint16 = 0x30D3
	EntityUpdateGuildHostility        uint16 = 0x30EF
	EntityGuildUpdateSiegeAuthority   uint16 = 0x3103
	GuildWarInfo                      uint16 = 0x3109
	GachaAnnounce                     uint16 = 0x3120
	PremiumDeco                       uint16 = 0x315C
	PremiumDecoReset                  uint16 = 0x315D
	PremiumDailyReset                 uint16 = 0x3161
	InventoryUpdateAmmo               uint16 = 0x3201
	SkillWithdrawWindow               uint16 = 0x3204
	EntityUpdateGuildNickname         uint16 = 0x3256
	EntityUpdateCrest                 uint16 = 0x3257
	CommunityFriendInfo               uint16 = 0x3305
	OperatorPunishment                uint16 = 0x3405
	FlagWarUpdate                     uint16 = 0x34B1
	GameServerTime                    uint16 = 0x34BE
	PremiumPause                      uint16 = 0x34BF
	BattleArenaOperation              uint16 = 0x34D2
	TapIcon                           uint16 = 0x34E1
	FGWUpdate                         uint16 = 0x351E
	ConsignmentBuffAdd                uint16 = 0x3530
	ConsignmentBuffUpdate             uint16 = 0x3531
	ConsignmentBuffRemove             uint16 = 0x3532
	SiegeUpdate                       uint16 = 0x385F
	GuildElectionUpdate               uint16 = 0x3908
	CommunityUpdate                   uint16 = 0x3B07
	CASClientRequest                  uint16 = 0x6314
	OperatorRequest                   uint16 = 0x7010
	RecallChallenge                   uint16 = 0x7030
	CharacterUpdateMoveStateRequest   uint16 = 0x704F
	SiegeActionRequest                uint16 = 0x705E
	GuildUpdateSiegeAuthorityRequest  uint16 = 0x70FF
	GuildWarRequest                   uint16 = 0x7110
	GuildWarEndRequest                uint16 = 0x7112
	GachaPlayRequest                  uint16 = 0x7118
	CommunityFriendRequest            uint16 = 0x7302
	CommunityMemoOpenRequest          uint16 = 0x7308
	CommunityMemoSendRequest          uint16 = 0x7309
	CommunityMemoDeleteRequest        uint16 = 0x730A
	FGWRecallRequest                  uint16 = 0x751A
	OperatorResponse                  uint16 = 0xB010
	RecallResponse                    uint16 = 0xB030
	RecallSolveResponse               uint16 = 0xB031
	InventoryStorageOpenResponse      uint16 = 0xB03C
	SiegeReturnResponse               uint16 = 0xB05D
	SiegeActionResponse               uint16 = 0xB05E
	BuffLink                          uint16 = 0xB0BE
	GuildUpdateSiegeAuthorityResponse uint16 = 0xB0FF
	GuildElectionResponse             uint16 = 0xB105
	GuildElectionParticipateResponse  uint16 = 0xB106
	GuildElectionVoteResponse         uint16 = 0xB107
	GuildWarStartResponse             uint16 = 0xB110
	GuildWarEndResponse               uint16 = 0xB112
	GuildWarCompensationClaimResponse uint16 = 0xB113
	GuildWarRewardResponse            uint16 = 0xB114
	GachaPlayResponse                 uint16 = 0xB118
	GachaExchangeResponse             uint16 = 0xB119
	SilkHistoryResponse               uint16 = 0xB11A
	PremiumDecoUseResponse            uint16 = 0xB160
	GuildStorageListResponse          uint16 = 0xB252
	GuildMercenaryAttrResponse        uint16 = 0xB259
	GuildMercenaryTerminateResponse   uint16 = 0xB25A
	CommunityFriendAddResponse        uint16 = 0xB302
	CommunityFriendDeleteResponse     uint16 = 0xB304
	CommunityMemoOpenResponse         uint16 = 0xB308
	CommunityMemoSendResponse         uint16 = 0xB309
	CommunityMemoDeleteResponse       uint16 = 0xB30A
	CommunityMemoListResponse         uint16 = 0xB30B
	CommunityMemoGroupSendResponse    uint16 = 0xB30C
	CommunityBlockUpdateResponse      uint16 = 0xB30D
	EntityUpdateAutoInvest            uint16 = 0xB402
	JobExportDetailResponse           uint16 = 0xB4D4
	TapInfoResponse                   uint16 = 0xB4DF
	TapUpdateResponse                 uint16 = 0xB4E0
	GuildGPHistoryResponse            uint16 = 0xB501
	FRPVPUpdateResponse               uint16 = 0xB516
	FGWRecallListResponse             uint16 = 0xB519
	FGWRecallMemberResponse           uint16 = 0xB51A
	FGWRecallMemberAcceptedResponse   uint16 = 0xB51C
	FGWExitResponse                   uint16 = 0xB51D

	// Second-pass client builder and inbound-handler registrations.
	CommunityFriendResponse          uint16 = 0x3303
	AcademyMatchingResponse          uint16 = 0x347F
	RecallSolveRequest               uint16 = 0x7031
	SiegeReturnRequest               uint16 = 0x705D
	GuildDonateLegacyRequest         uint16 = 0x70F6
	GuildElectionRequest             uint16 = 0x7105
	GuildElectionParticipateRequest  uint16 = 0x7106
	GuildElectionVoteRequest         uint16 = 0x7107
	GuildWarCompensationClaimRequest uint16 = 0x7113
	GuildWarRewardRequest            uint16 = 0x7114
	GachaExchangeRequest             uint16 = 0x7119
	SilkHistoryRequest               uint16 = 0x711A
	PremiumDecoUseRequest            uint16 = 0x7160
	GuildMercenaryAttrRequest        uint16 = 0x7259
	GuildMercenaryTerminateRequest   uint16 = 0x725A
	CommunityFriendDeleteRequest     uint16 = 0x7304
	CommunityMemoListRequest         uint16 = 0x730B
	CommunityMemoGroupSendRequest    uint16 = 0x730C
	CommunityBlockUpdateRequest      uint16 = 0x730D
	CharacterUpdateAutoInvestRequest uint16 = 0x7402
	FlagWarRegisterRequest           uint16 = 0x74B2
	BattleArenaRequest               uint16 = 0x74D3
	JobExportDetailRequest           uint16 = 0x74D4
	TapInfoRequest                   uint16 = 0x74DF
	TapUpdateRequest                 uint16 = 0x74E0
	GuildGPHistoryRequest            uint16 = 0x7501
	FRPVPUpdateRequest               uint16 = 0x7516
	FGWRecallListRequest             uint16 = 0x7519
	FGWRecallResponse                uint16 = 0x751C
	FGWExitRequest                   uint16 = 0x751D
	CharacterUpdateMoveStateResponse uint16 = 0xB04F
	GuildDonateLegacyResponse        uint16 = 0xB0F6

	// CAS handshake IDs are present in the local vSRO 1.188 protocol corpus.
	// They are separate because this GameServer/client pass did not establish
	// payload semantics for these three messages.
	CASServerRequest  uint16 = 0x6315
	CASServerResponse uint16 = 0x6316
	CASClientResponse uint16 = 0xA314

	// Third-pass entries recovered from the client's concrete inbound handler
	// registry at 0x0086FBB0. Unknown names deliberately preserve uncertainty.
	ClientReset              uint16 = 0x3055
	LegacyRemovedUpdate3076  uint16 = 0x3076
	COSUnknownUpdate1        uint16 = 0x30C2
	COSUnknownUpdate2        uint16 = 0x30C3
	NoActionUpdate30CF       uint16 = 0x30CF
	CharacterStateUpdate     uint16 = 0x3011
	MonsterCaptureSystem     uint16 = 0x330F
	LegacyRemovedUpdate30A6  uint16 = 0x30A6
	ActionUnknownUpdate      uint16 = 0x30D1
	EntityTicket             uint16 = 0x3206
	NoActionUpdate30EB       uint16 = 0x30EB
	GuildUnknownResponse1    uint16 = 0xB0F8
	AlchemyCanceled          uint16 = 0x3156
	InternalControlMessage   uint16 = 0x0FFC
	InternalDebugMessageDump uint16 = 0x2000
	LegacyRemovedControl     uint16 = 0x0FFD
	UnknownUpdate3078        uint16 = 0x3078

	// Registered responses and their request counterparts from the v1.188
	// Agent packet catalog. Payload semantics remain unresolved.
	InventoryUnknownRequest  uint16 = 0x703F
	InventoryUnknownResponse uint16 = 0xB03F
	COSUnknownRequest        uint16 = 0x70C7
	COSUnknownResponse       uint16 = 0xB0C7
	LegacyUnknownC0Request   uint16 = 0x70C0
	LegacyUnknownC0Response  uint16 = 0xB0C0
	GuildUnknownRequest2     uint16 = 0x7113
	GuildUnknownResponse2    uint16 = 0xB113
	ChristmasEventRequest    uint16 = 0x70ED
	ChristmasEventResponse   uint16 = 0xB0ED

	// Fourth-pass stock-client registrations. These close the remaining gaps
	// across CNetProcessIn, CNetProcessSecond, and CNetProcessThird.
	UnknownUpdate3077              uint16 = 0x3077
	COSUnknownUpdate0              uint16 = 0x30C1
	ActionUnknownUpdate2           uint16 = 0x30D2
	VietnamMRSystemUpdate          uint16 = 0x3122
	PremiumUnknownUpdate315A       uint16 = 0x315A
	LegacyRemovedUpdate315E        uint16 = 0x315E
	UnknownUpdate3209              uint16 = 0x3209
	UnknownUpdate320A              uint16 = 0x320A
	UnknownUpdate320B              uint16 = 0x320B
	UnknownUpdate320C              uint16 = 0x320C
	UnknownUpdate325C              uint16 = 0x325C
	UnknownUpdate325D              uint16 = 0x325D
	UnknownUpdate325E              uint16 = 0x325E
	UnknownUpdate34BA              uint16 = 0x34BA
	ServerEventUpdate              uint16 = 0x34F2
	AcademyUnknownUpdate3C86       uint16 = 0x3C86
	AcademyUnknownUpdate3C87       uint16 = 0x3C87
	EntitySkillApplicationResponse uint16 = 0xB0BC
	// EntitySkillUnknownResponse is retained for source compatibility.
	EntitySkillUnknownResponse      uint16 = EntitySkillApplicationResponse
	SilkUnknownResponse             uint16 = 0xB121
	UnhandledResponseB461           uint16 = 0xB461
	UnhandledResponseB462           uint16 = 0xB462
	UnhandledResponseB463           uint16 = 0xB463
	AcademyInviteResponse           uint16 = 0xB472
	AcademyMemberActionResponse     uint16 = 0xB476
	AcademyUnknownOperationResponse uint16 = 0xB483

	// Fourth-pass outbound packet builders at sub_841780.
	GameReadyAlternateRequest      uint16 = 0x3014
	COSUnknownRequest30C4          uint16 = 0x30C4
	SilkUnknownRequest             uint16 = 0x7121
	UnhandledRequest7461           uint16 = 0x7461
	UnhandledRequest7462           uint16 = 0x7462
	UnhandledRequest7463           uint16 = 0x7463
	AcademyInviteRequest           uint16 = 0x7472
	AcademyMemberActionRequest     uint16 = 0x7476
	AcademyUnknownOperationRequest uint16 = 0x7483

	// The stock server registers low slot 0x0B6 in the stall family. The full
	// 0x70B6 ID is corroborated only by the secondary protocol reference, and
	// its stock handler intentionally consumes the packet without a response.
	LegacyStallNoActionRequest uint16 = 0x70B6

	// Pre-dispatch controls handled by sub_842FF0. These are distinct from the
	// transport security handshake carried by 0x5000/0x9000.
	PreDispatchTimerControl0FFB      uint16 = 0x0FFB
	PreDispatchControl2110           uint16 = 0x2110
	PreDispatchChallengeResponse2113 uint16 = 0x2113

	// Direction/phase aliases for shared numeric IDs.
	InventoryStorageOpenRequest = StorageOpenRequest
	GuildStorageListRequest     = GuildStorageInfoRequest
	GuildWarStartRequest        = GuildWarRequest
	CommunityFriendAddRequest   = CommunityFriendRequest
	FGWRecallMemberRequest      = FGWRecallRequest
	PremiumPauseRequest         = PremiumPause
)
