package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVSROItemCatalogUsesAuthoritativeCommonLink(t *testing.T) {
	if !contains(vsroItemTables[0].columns, "Link") {
		t.Fatal("ref_obj_common import omits Link")
	}
	if !strings.Contains(selectItemMediaCatalog, "item.ID=common.LINK") || strings.Contains(selectItemMediaCatalog, "item.ID=common.ID") {
		t.Fatalf("item Media join is not Link-based: %s", selectItemMediaCatalog)
	}

	data := validVSROItemLinkFixture()
	if err := validateVSROItemLinks(data); err != nil {
		t.Fatalf("valid divergent identities rejected: %v", err)
	}
	if got := data[0][2]["ID"]; got != json.Number("19556") {
		t.Fatalf("structure common fixture ID = %v, want 19556", got)
	}
	if got := data[0][2]["Link"]; got != json.Number("7") {
		t.Fatalf("structure common fixture Link = %v, want 7", got)
	}
	if got := data[2][0]["ID"]; got != json.Number("7") {
		t.Fatalf("structure detail fixture ID = %v, want private Link identity 7", got)
	}
	if got := data[2][0]["Dummy_Data"]; got != json.Number("0") {
		t.Fatalf("structure detail fixture Dummy_Data = %v, want source value 0", got)
	}
	if len(vsroItemTables[2].columns) != 1 || vsroItemTables[2].columns[0] != "ID" {
		t.Fatalf("structure import typed columns = %v, want only ID; task bytes must remain source RAW_DATA", vsroItemTables[2].columns)
	}
}

func TestVSROItemCatalogRejectsBrokenOrAmbiguousLinks(t *testing.T) {
	tests := []struct {
		name string
		edit func([][]map[string]any)
		want string
	}{
		{
			name: "missing detail",
			edit: func(data [][]map[string]any) { data[0][0]["Link"] = json.Number("9999") },
			want: "links to missing",
		},
		{
			name: "duplicate link",
			edit: func(data [][]map[string]any) { data[0][1]["Link"] = json.Number("1907") },
			want: "both link",
		},
		{
			name: "orphan detail",
			edit: func(data [][]map[string]any) {
				data[0] = data[0][:1]
			},
			want: "has no TypeID1=3",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := validVSROItemLinkFixture()
			test.edit(data)
			err := validateVSROItemLinks(data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVSROStructureCatalogRejectsBrokenOrAmbiguousLinks(t *testing.T) {
	tests := []struct {
		name string
		edit func([][]map[string]any)
		want string
	}{
		{
			name: "missing detail",
			edit: func(data [][]map[string]any) { data[0][2]["Link"] = json.Number("9999") },
			want: "links to missing _RefObjStruct ID 9999",
		},
		{
			name: "missing structure row",
			edit: func(data [][]map[string]any) { data[2] = nil },
			want: "links to missing _RefObjStruct ID 7",
		},
		{
			name: "duplicate link",
			edit: func(data [][]map[string]any) {
				data[0] = append(data[0], map[string]any{"ID": json.Number("19557"), "TypeID1": json.Number("4"), "Link": json.Number("7")})
			},
			want: "both link to _RefObjStruct ID 7",
		},
		{
			name: "orphan detail",
			edit: func(data [][]map[string]any) { data[0] = data[0][:2] },
			want: "_RefObjStruct ID 7 has no TypeID1=4",
		},
		{
			name: "public ID is not detail ID",
			edit: func(data [][]map[string]any) { data[2][0]["ID"] = json.Number("19556") },
			want: "links to missing _RefObjStruct ID 7",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := validVSROItemLinkFixture()
			test.edit(data)
			err := validateVSROItemLinks(data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func validVSROItemLinkFixture() [][]map[string]any {
	data := make([][]map[string]any, len(vsroItemTables))
	data[0] = []map[string]any{
		{"ID": json.Number("2128"), "TypeID1": json.Number("3"), "Link": json.Number("1907")},
		{"ID": json.Number("2129"), "TypeID1": json.Number("3"), "Link": json.Number("1908")},
		{"ID": json.Number("19556"), "TypeID1": json.Number("4"), "Link": json.Number("7")},
	}
	data[1] = []map[string]any{
		{"ID": json.Number("1907")},
		{"ID": json.Number("1908")},
	}
	data[2] = []map[string]any{
		{"ID": json.Number("7"), "Dummy_Data": json.Number("0")},
	}
	return data
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
