package opcode

const (
	GameRegisterServerRequest uint16 = 0xE201

	GameRegisterServerResponse uint16 = 0xF201

	// GameReadyRequest is the client Agent GameReady opcode listed by the
	// repository's vSRO opcode corpus. Its payload has no capture-backed
	// extension fields, so handlers must reject trailing bytes rather than
	// inventing a layout.
	GameReadyRequest uint16 = 0x3012

	GuideRequest  uint16 = 0x70EA
	GuideResponse uint16 = 0xB0EA

	CharacterConfigUpdateRequest uint16 = 0x7158

	PlayerInvitationRequest  uint16 = 0x3080
	PlayerInvitationResponse uint16 = 0x3080
)
