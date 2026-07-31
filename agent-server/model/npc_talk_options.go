package model

import "strings"

// The stock client interprets the five bytes advertised for a normal NPC as
// interaction masks. These layouts are shared by the spawn and selection
// packets; sending an empty or shorter layout leaves the dialog with no
// buttons and can make the client parse the following packet incorrectly.
var (
	defaultNPCTalkOptions   = [...]byte{0x00, 0x00, 0x01, 0x00, 0x00}
	shopNPCTalkOptions      = [...]byte{0x00, 0x01, 0x01, 0x00, 0x00}
	warehouseNPCTalkOptions = [...]byte{0x00, 0x20, 0x00, 0x00, 0x00}
	teleportNPCTalkOptions  = [...]byte{0x00, 0x02, 0x08, 0x00, 0x00}
)

// NPCTalkOptions returns a detached option mask for the original-game NPC
// families present in the shard reference data.
func NPCTalkOptions(codeName string) []byte {
	upper := strings.ToUpper(codeName)
	var options [5]byte
	switch {
	case isTeleportServiceCodeName(upper):
		options = teleportNPCTalkOptions
	case strings.Contains(upper, "WAREHOUSE"):
		options = warehouseNPCTalkOptions
	case isShopNPCCodeName(upper):
		options = shopNPCTalkOptions
	default:
		options = defaultNPCTalkOptions
	}
	return append([]byte(nil), options[:]...)
}

// NPCSelectionTalkData encodes the counted action list used after the selected
// NPC UID in B045. It is intentionally different from the five-byte spawn
// mask above: the client expects a health flag, action count, action bytes,
// terminator, and for a small original-game NPC set an additional info word.
func NPCSelectionTalkData(codeName string) []byte {
	upper := strings.ToUpper(codeName)
	var data []byte
	switch {
	case upper == "NPC_OPEN_MARKET_JUEL":
		// SR_GameServer.exe 0x004C7611–0x004C762F identifies this merchant
		// and advertises service action 0x23. The stock client dispatches that
		// action to CNIFStallNetwork.
		data = []byte{0, 1, 0x23, 0}
	case upper == "NPC_CH_GACHA_MACHINE":
		data = []byte{0, 1, 0x11, 0}
	case upper == "NPC_CH_GACHA_OPERATOR":
		data = []byte{0, 1, 0x12, 0}
	case strings.Contains(upper, "SMITH"):
		data = []byte{0, 4, 1, 2, 4, 0x20, 0}
	case strings.Contains(upper, "ARMOR"):
		data = []byte{0, 3, 1, 2, 4, 0}
	case strings.Contains(upper, "POTION"):
		data = []byte{0, 2, 1, 2, 0}
	case strings.Contains(upper, "ACCESSORY"):
		data = []byte{0, 2, 1, 2, 0}
	case strings.Contains(upper, "WAREHOUSE"):
		data = []byte{0, 3, 1, 2, 3, 0}
	case isTeleportServiceCodeName(upper):
		data = []byte{0, 2, 2, 8, 0}
	case strings.Contains(upper, "HORSE"):
		data = []byte{0, 3, 1, 2, 0x0B, 0}
	case strings.Contains(upper, "GUILD"):
		// 0x0D opens guild storage; 0x0F is the existing guild service.
		data = []byte{0, 4, 1, 2, 0x0D, 0x0F, 0}
	case strings.Contains(upper, "DOCTOR"):
		data = []byte{0, 3, 1, 2, 0x14, 0}
	case strings.Contains(upper, "DESIGNER"):
		data = []byte{0, 3, 1, 2, 0, 0}
	case isJobNPCCodeName(upper):
		// B045 advertises a job service group, not the final enrollment action.
		// sro_client.exe 0x00700440 maps groups 0x14/0x15/0x16 to job types
		// 1/2/3 and calls 0x006FDEB0, which builds the state-dependent menu and
		// creates action 0x1E for enrollment when the character has no job.
		// Store (1) is advertised separately from the job service group so the
		// association NPC can open its Media-defined equipment catalog.
		data = []byte{0, 2, 1, jobNPCServiceGroup(upper), 0}
	case isShopNPCCodeName(upper):
		data = []byte{0, 2, 1, 2, 0}
	default:
		data = []byte{0, 1, 2, 0}
	}

	if info, ok := npcSelectionInfoWord(upper); ok {
		data = append(data, byte(info), byte(info>>8))
	}
	return data
}

func jobNPCServiceGroup(codeName string) byte {
	switch {
	case strings.Contains(codeName, "THIEF"):
		return 0x15
	case strings.Contains(codeName, "HUNTER"):
		return 0x16
	default:
		return 0x14 // merchant/trader
	}
}

func isJobNPCCodeName(codeName string) bool {
	upper := strings.ToUpper(codeName)
	return strings.Contains(upper, "THIEF") || strings.Contains(upper, "HUNTER") ||
		strings.Contains(upper, "MERCHANT") || strings.Contains(upper, "TRADER")
}

func isTeleportServiceCodeName(codeName string) bool {
	return strings.Contains(codeName, "FERRY") || strings.Contains(codeName, "TELEPORT") ||
		(strings.HasPrefix(codeName, "STORE_") && strings.HasSuffix(codeName, "_GATE"))
}

// IsTeleportServiceCodeName exposes the catalog classification to handlers
// that need to admit ferry/teleport NPCs through the generic 0x7059/0x705A
// path without duplicating string heuristics.
func IsTeleportServiceCodeName(codeName string) bool {
	return isTeleportServiceCodeName(strings.ToUpper(strings.TrimSpace(codeName)))
}

func npcSelectionInfoWord(codeName string) (uint16, bool) {
	withInfo := map[string]uint16{
		"NPC_CH_SMITH":     0,
		"NPC_CH_ARMOR":     0,
		"NPC_CH_POTION":    0,
		"NPC_CH_ACCESSORY": 0x14,
		"NPC_CH_SPECIAL":   0,
		"NPC_CH_SPECIAL2":  0,
		"NPC_KT_SMITH":     0,
		"NPC_KT_ARMOR":     0,
		"NPC_KT_POTION":    0,
		"NPC_KT_ACCESSORY": 0x14,
		"NPC_KT_SPECIAL":   0,
		"STORE_CH_GATE":    0,
		"STORE_KT_GATE":    0,
		"NPC_CH_FERRY":     0,
		"NPC_CH_FERRY2":    0,
		"NPC_KT_FERRY":     0,
		"NPC_KT_FERRY2":    0,
		"NPC_KT_FERRY3":    0,
		"NPC_CH_GUILD":     0,
		"NPC_WC_GUILD":     0,
		"NPC_KT_GUILD":     0,
	}
	info, ok := withInfo[codeName]
	return info, ok
}

func isShopNPCCodeName(codeName string) bool {
	for _, marker := range [...]string{
		"ACCESSORY", "ARMOR", "DESIGNER", "DOCTOR", "EXCHANGER",
		"GENARAL", "GUILD", "HORSE", "HUNTER", "MERCHANT", "MINISTER",
		"POTION", "SELL", "SMITH", "SPECIAL", "THIEF", "_BUY",
	} {
		if strings.Contains(codeName, marker) {
			return true
		}
	}
	return false
}
