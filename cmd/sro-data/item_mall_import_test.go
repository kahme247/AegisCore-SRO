package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadItemMallPlacements(t *testing.T) {
	directory := itemMallFixture(t, map[string]string{})
	rows, err := loadItemMallPlacements(directory)
	if err != nil {
		t.Fatalf("loadItemMallPlacements() error = %v", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("placements = %d, want %d", got, want)
	}
	got := rows[0]
	if got.shopGroupID != 1051 || got.groupIndex != 0 || got.tabIndex != 0 || got.slotIndex != 7 {
		t.Fatalf("position = %d/%d/%d/%d, want 1051/0/0/7", got.shopGroupID, got.groupIndex, got.tabIndex, got.slotIndex)
	}
	if got.packageID != 40000 || got.itemCode != "ITEM_MALL_TEST" || got.currency != 0 || got.price != 125 || got.optLevel != 3 || got.variance != 4 || got.data != 5 {
		t.Fatalf("placement = %+v", got)
	}
}

func TestLoadItemMallPlacementsRejectsAmbiguousMappedPrices(t *testing.T) {
	directory := itemMallFixture(t, map[string]string{
		"refpricepolicyofitem.txt": "1\t15\tPACKAGE_ITEM_MALL_TEST\t2\t0\t125\t0\txxx\t0\txxx\t0\txxx\t0\txxx\n1\t15\tPACKAGE_ITEM_MALL_TEST\t4\t0\t100\t0\txxx\t0\txxx\t0\txxx\t0\txxx\n",
	})
	_, err := loadItemMallPlacements(directory)
	if err == nil || !strings.Contains(err.Error(), "positive mapped prices") {
		t.Fatalf("loadItemMallPlacements() error = %v, want ambiguous price error", err)
	}
}

func TestLoadItemMallPlacementsRejectsUnresolvedPackage(t *testing.T) {
	directory := itemMallFixture(t, map[string]string{
		"refpackageitem.txt": "1\t15\t40001\tPACKAGE_ITEM_OTHER\n",
	})
	_, err := loadItemMallPlacements(directory)
	if err == nil || !strings.Contains(err.Error(), "unresolved package") {
		t.Fatalf("loadItemMallPlacements() error = %v, want unresolved package error", err)
	}
}

func TestLoadItemMallPlacementsRejectsDuplicatePosition(t *testing.T) {
	directory := itemMallFixture(t, map[string]string{
		"refshopgoods.txt": "1\t15\tMALL_TEST_TAB\tPACKAGE_ITEM_MALL_TEST\t7\n1\t15\tMALL_TEST_TAB\tPACKAGE_ITEM_MALL_TEST\t7\n",
	})
	_, err := loadItemMallPlacements(directory)
	if err == nil || !strings.Contains(err.Error(), "duplicate target position") {
		t.Fatalf("loadItemMallPlacements() error = %v, want duplicate position error", err)
	}
}

func TestLoadItemMallPlacementsAllowsPackageInMultipleTabs(t *testing.T) {
	directory := itemMallFixture(t, map[string]string{
		"refshoptab.txt":   "1\t15\t1\tMALL_TEST_TAB\tMALL_TEST_GROUP1\n1\t15\t2\tMALL_TEST_TAB_2\tMALL_TEST_GROUP1\n",
		"refshopgoods.txt": "1\t15\tMALL_TEST_TAB\tPACKAGE_ITEM_MALL_TEST\t7\n1\t15\tMALL_TEST_TAB_2\tPACKAGE_ITEM_MALL_TEST\t7\n",
	})
	rows, err := loadItemMallPlacements(directory)
	if err != nil {
		t.Fatalf("loadItemMallPlacements() error = %v", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("placements = %d, want %d", got, want)
	}
}

func itemMallFixture(t *testing.T, replacements map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	files := map[string]string{
		"refshopgroup.txt":          "1\t15\t1051\tGROUP_MALL\txxx\n",
		"refmappingshopgroup.txt":   "1\t15\tGROUP_MALL\tMALL_TEST\n",
		"refmappingshopwithtab.txt": "1\t15\tMALL_TEST\tMALL_TEST_GROUP1\n",
		"refshoptab.txt":            "1\t15\t1\tMALL_TEST_TAB\tMALL_TEST_GROUP1\n",
		"refshopgoods.txt":          "1\t15\tMALL_TEST_TAB\tPACKAGE_ITEM_MALL_TEST\t7\n",
		"refpackageitem.txt":        "1\t15\t40000\tPACKAGE_ITEM_MALL_TEST\n",
		"refscrapofpackageitem.txt": "1\t15\tPACKAGE_ITEM_MALL_TEST\tITEM_MALL_TEST\t3\t4\t5\n",
		"refpricepolicyofitem.txt":  "1\t15\tPACKAGE_ITEM_MALL_TEST\t2\t0\t125\t0\txxx\t0\txxx\t0\txxx\t0\txxx\n",
	}
	for name, contents := range replacements {
		files[name] = contents
	}
	for name, contents := range files {
		encoded := make([]byte, 2+len([]rune(contents))*2)
		encoded[0], encoded[1] = 0xff, 0xfe
		for index, value := range []rune(contents) {
			binary.LittleEndian.PutUint16(encoded[2+index*2:], uint16(value))
		}
		if err := os.WriteFile(filepath.Join(directory, name), encoded, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return directory
}
