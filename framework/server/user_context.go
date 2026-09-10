package server

type UserContext struct {
	UserID       uint32
	GameServerID byte
	ShardID      uint16
	// CharacterID is the immutable shard-database identity of the character
	// staged on this session. Character names are mutable display data and must
	// not be used as the sole authority for lifecycle ownership or teardown.
	CharacterID         int
	CharName            string
	UniqueID            uint32
	RefObjId            uint32
	SelectedNPCUniqueID uint32
	SelectedNPCRefObjID uint32
	Username            string
	// ProtocolProfile is selected by Gateway patch negotiation and scopes
	// profile-specific behavior for the lifetime of that connection.
	ProtocolProfile string
}
