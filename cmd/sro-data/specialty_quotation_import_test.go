package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSpecialtyQuotationRowsPreservesStockRealValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "itemquotation.psv")
	source := "1|1|2010|2151|1.88|1.58|2.1800001|50000|250|42499\n" +
		"2|1|2010|2152|3.02|2.6199999|3.3199999|50000|200|44000\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := loadSpecialtyQuotationRows(path)
	if err != nil {
		t.Fatalf("loadSpecialtyQuotationRows: %v", err)
	}
	if len(rows) != 2 || rows[0].AssocNPC != 2010 || rows[0].RefItemID != 2151 || rows[0].BaseStockAmount != 50000 || rows[0].CurStockAmount != 42499 {
		t.Fatalf("rows = %+v", rows)
	}
	// ParseFloat(..., 32) retains the original SQL Server REAL value rather
	// than pretending the displayed decimal was a higher-precision source.
	if rows[0].QuotUB != float64(float32(2.1800001)) {
		t.Fatalf("upper quotation = %.12f, want float32-preserved %.12f", rows[0].QuotUB, float64(float32(2.1800001)))
	}
}

func TestLoadSpecialtyQuotationRowsRejectsConflictingOrInvalidCatalog(t *testing.T) {
	for name, source := range map[string]string{
		"duplicate NPC item": "1|1|2010|2151|1.88|1.58|2.18|50000|250|42499\n2|1|2010|2151|1.88|1.58|2.18|50000|250|42499\n",
		"invalid bounds":     "1|1|2010|2151|1.00|1.58|2.18|50000|250|42499\n",
		"invalid stock":      "1|1|2010|2151|1.88|1.58|2.18|0|250|42499\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "itemquotation.psv")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSpecialtyQuotationRows(path); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(strings.Split(name, " ")[1])) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
