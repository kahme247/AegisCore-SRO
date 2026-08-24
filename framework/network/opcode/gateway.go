package opcode

const (
	PatchRequest                 uint16 = 0x6100
	PatchResponse                uint16 = 0xA100
	ShardlistRequest             uint16 = 0x6101
	ShardlistResponse            uint16 = 0xA101
	LoginRequest                 uint16 = 0x6102
	LoginResponse                uint16 = 0xA102
	AuthRequest                  uint16 = 0x6103
	AuthResponse                 uint16 = 0xA103
	NoticeRequest                uint16 = 0x6104
	NoticeResponse               uint16 = 0xA104
	ShardlistPing                uint16 = 0x6106
	ShardlistPong                uint16 = 0xA106
	JoinLobbyRequest             uint16 = 0x7001
	LobbySelectCharacterRequest  uint16 = 0x7001
	LobbyActionRequest           uint16 = 0x7007
	LobbyRenameRequest           uint16 = 0x7450
	LobbySelectCharacterResponse uint16 = 0xB001
	LobbyActionResponse          uint16 = 0xB007
	LobbyRenameResponse          uint16 = 0xB450
)
