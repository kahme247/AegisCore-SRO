package main

import "testing"

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
