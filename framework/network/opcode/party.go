package opcode

const (
	PartyCreateRequest          uint16 = 0x7060
	PartyLeaveRequest           uint16 = 0x7061
	PartyInviteRequest          uint16 = 0x7062
	PartyKickRequest            uint16 = 0x7063
	PartyInvitation             uint16 = 0x3080
	PartyCreate                 uint16 = 0x3865
	PartyUpdateResponse         uint16 = 0x3864
	PartyCreateResponse         uint16 = 0xB060
	PartyInviteResponse         uint16 = 0xB062
	PartyMatchingFormRequest    uint16 = 0x7069
	PartyMatchingFormResponse   uint16 = 0xB069
	PartyMatchingUpdateRequest  uint16 = 0x706A
	PartyMatchingUpdateResponse uint16 = 0xB06A
	PartyMatchingDeleteRequest  uint16 = 0x706B
	PartyMatchingDeleteResponse uint16 = 0xB06B
	PartyMatchingListRequest    uint16 = 0x706C
	PartyMatchingListResponse   uint16 = 0xB06C
	PartyMatchingJoinRequest    uint16 = 0x706D
	PartyMatchingJoinResponse   uint16 = 0xB06D
	// 0x306E is the client approval answer for a server 0x706D prompt.
	PartyMatchingPlayerJoinRequest uint16 = 0x306E
	// TestIn's dispatcher also assigns the matching-completion slot at 0x706E;
	// some client families use it instead of the 0x306E directional alias.
	PartyMatchingCompletionRequest uint16 = 0x706E
	// vSRO reuses the listing-join numeric family in the opposite direction.
	PartyMatchingPlayerJoinNotification uint16 = 0x706D
	// Deprecated direction-neutral alias retained for older callers.
	PartyMatchingPlayerJoinResponse uint16 = PartyMatchingPlayerJoinNotification
	PartyMemberCountResponse        uint16 = 0xB067
	// Server notification that a dropped item was assigned to a party member.
	PartyDistribution                uint16 = 0x3068
	PartyCreatedFromMatchingResponse uint16 = 0x3065
)
