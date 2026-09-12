package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDropCatalogImportRequiresAndPreservesProfileTables(t *testing.T) {
	directory := t.TempDir()
	files := map[string]string{
		"_RefDropGold.csv":                    "MonLevel;DropProb;GoldMin;GoldMax\n1;100;1;2\n",
		"_RefDropItemGroup.csv":               "Service;Group;Type;Item;Ratio;Magic\n1;1;0;2;1;0\n",
		"_RefMonster_AssignedItemRndDrop.csv": "A;B;C;D;E;F;G;H;I;J\n1;2;3;4;0;1;1;1;0;0\n",
		"_RefMonster_AssignedItemDrop.csv":    strings.Repeat("H;", 25) + "H\n" + strings.Repeat("1;", 25) + "1\n",
		"_RefDropItemAssign.csv":              "Service;RefItemID;Prob_Relative;Prob_Absolute;AssignedGroup;DropCount\n1;62;100;100;1;20\n",
		"_RefDropOptLvlSel.csv":               "OptLevel;Prob;ReqOnlineTime\n0;0,883333;0\n",
	}
	for _, name := range []string{
		"_RefDropClassSel_Equip.csv", "_RefDropClassSel_RareEquip.csv",
		"_RefDropClassSel_Recover.csv", "_RefDropClassSel_Cure.csv",
		"_RefDropClassSel_Ammo.csv", "_RefDropClassSel_Scroll.csv",
		"_RefDropClassSel_Reinforce.csv", "_RefDropClassSel_Alchemy_Tablet.csv",
		"_RefDropClassSel_Alchemy_MagicStone.csv", "_RefDropClassSel_Alchemy_ATTRStone.csv",
	} {
		files[name] = "MonLevel;ProbGroup1\n1;0,25\n"
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := loadDropCatalogImport(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.classSelections) != 10 || len(data.itemAssign) != 1 || len(data.optLevels) != 1 {
		t.Fatalf("profile import = classes:%d items:%d options:%d", len(data.classSelections), len(data.itemAssign), len(data.optLevels))
	}
	if got, err := dropFloat(data.classSelections[0].rows[0][1]); err != nil || got != 0.25 {
		t.Fatalf("decimal-comma probability = %v, %v", got, err)
	}
}

func TestRetainedDropProfileCatalogParsesCompletely(t *testing.T) {
	data, err := loadDropCatalogImport(filepath.Join("..", "..", "framework", "docs", "db", "table_data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.gold) != 140 || len(data.groups) != 282 || len(data.random) != 127 || len(data.direct) != 991 {
		t.Fatalf("ordinary retained counts = gold:%d groups:%d random:%d direct:%d", len(data.gold), len(data.groups), len(data.random), len(data.direct))
	}
	if len(data.classSelections) != 10 || len(data.itemAssign) != 8966 || len(data.optLevels) != 13 {
		t.Fatalf("profile retained counts = classes:%d items:%d options:%d", len(data.classSelections), len(data.itemAssign), len(data.optLevels))
	}
	foundSentinel := false
	for _, row := range data.itemAssign {
		if group, parseErr := dropInt(row[4]); parseErr == nil && group == -1 {
			foundSentinel = true
			break
		}
	}
	if !foundSentinel {
		t.Fatal("retained item assignments lost the signed -1 group sentinel")
	}
	for _, source := range data.classSelections {
		if len(source.rows) != 180 {
			t.Fatalf("%s class-selection rows = %d, want 180", source.family, len(source.rows))
		}
	}
}

func TestDirectDropValuesPreservesExportColumnOrder(t *testing.T) {
	row := []string{"101", "202", "3", "4", "5", "6", "0.75"}
	for i := 1; i <= 9; i++ {
		row = append(row, "", "0")
	}
	row = append(row, "RENT_TEST")
	values, err := directDropValues(row)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 27 {
		t.Fatalf("value count = %d, want 27", len(values))
	}
	if values[0] != uint64(1) || values[1] != uint64(101) || values[2] != uint64(202) ||
		values[3] != uint64(3) || values[4] != uint64(4) || values[5] != uint64(5) ||
		values[6] != uint64(6) || values[7] != 0.75 || values[26] != "RENT_TEST" {
		t.Fatalf("values = %#v", values)
	}
}

func TestDirectDropValuesRejectsReversedAmountRange(t *testing.T) {
	row := []string{"101", "202", "0", "0", "8", "7", "1"}
	for i := 1; i <= 9; i++ {
		row = append(row, "", "0")
	}
	row = append(row, "xxx")
	if _, err := directDropValues(row); err == nil {
		t.Fatal("reversed amount range accepted")
	}
}
