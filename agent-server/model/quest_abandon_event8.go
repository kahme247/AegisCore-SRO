package model

// nativeQuestAbandonCleanup is the concrete SR_GameServer event-8 behavior
// recovered from the quest vtable slot at +0x74. Item references are resolved
// from the pinned vSRO 1.188 Media catalog.
type nativeQuestAbandonCleanupDefinition struct {
	ItemRefIDs      []uint32
	RelatedQuestIDs []int
}

var nativeQuestAbandonCleanup = map[int]nativeQuestAbandonCleanupDefinition{
	141: {ItemRefIDs: []uint32{5941}},
	227: {ItemRefIDs: []uint32{10494}},
	276: {ItemRefIDs: []uint32{10548, 10549, 10550}},
	286: {ItemRefIDs: []uint32{10562, 10563}},
	287: {ItemRefIDs: []uint32{10564, 10567, 10565}},
	308: {RelatedQuestIDs: []int{307}},
	310: {RelatedQuestIDs: []int{309}},
	311: {ItemRefIDs: []uint32{10585, 10586}},
	349: {ItemRefIDs: []uint32{23354, 23355}},
	352: {ItemRefIDs: []uint32{23360, 23361}},
	354: {ItemRefIDs: []uint32{23364}},
	358: {ItemRefIDs: []uint32{23369, 23370}},
	363: {ItemRefIDs: []uint32{23354, 23355}},
	366: {ItemRefIDs: []uint32{23363, 23361}},
	368: {ItemRefIDs: []uint32{23964}},
	372: {ItemRefIDs: []uint32{23371, 23370}},
	92:  {RelatedQuestIDs: []int{91}},
	96:  {ItemRefIDs: []uint32{23974, 23975}, RelatedQuestIDs: []int{92}},
}

func nativeQuestAbandonDefinition(questID int) nativeQuestAbandonCleanupDefinition {
	return nativeQuestAbandonCleanup[questID]
}
