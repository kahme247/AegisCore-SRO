package vsro188

// AccountID identifies an account in account-domain operations.
type AccountID uint32

// CharacterDBID identifies a persisted character. It is called a JID in much
// of the vSRO protocol/domain vocabulary and is intentionally distinct from a
// runtime EntityUID.
type CharacterDBID uint32

// JID is the vSRO name for a persisted character identifier.
//
// It is an alias rather than another identity namespace: a JID and a
// CharacterDBID name the same persisted-character value. Both remain distinct
// named types from AccountID and EntityUID.
type JID = CharacterDBID

// EntityUID identifies a live world entity. Zero is reserved and must never
// be allocated to a player or another live entity.
type EntityUID uint32

// ShardID identifies the shard that owns a character or admission.
type ShardID uint16

// RegionID identifies a vSRO region. It is unsigned so callers can safely
// inspect protocol-defined flag bits such as the dungeon high bit.
type RegionID uint16
