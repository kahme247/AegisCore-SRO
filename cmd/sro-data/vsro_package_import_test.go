package main

import "testing"

func TestVSROPackageColumnsMapToNormalizedVectorSchema(t *testing.T) {
	for source, want := range map[string]string{
		"RefPackageItemCodeName": "PACKAGE_CODE_NAME",
		"RefItemCodeName":        "ITEM_CODE_NAME",
		"RefTabCodeName":         "TAB_CODE_NAME",
		"RefNPCCodeName":         "NPC_CODE_NAME",
		"RefShopGroupCodeName":   "SHOP_GROUP_CODE_NAME",
		"RefShopCodeName":        "SHOP_CODE_NAME",
		"RefTabGroupCodeName":    "TAB_GROUP_CODE_NAME",
		"SlotIndex":              "SLOT_INDEX",
		"Index":                  "MEMBER_INDEX",
		"OptLevel":               "OPT_LEVEL",
		"MagParamNum":            "MAG_PARAM_NUM",
	} {
		if got := normalizeVSROColumn(source); got != want {
			t.Fatalf("normalizeVSROColumn(%q) = %q, want %q", source, got, want)
		}
	}
}
