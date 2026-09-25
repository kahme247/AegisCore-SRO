package main

import (
	"bytes"
	"strings"
	"testing"
)

var mediaMapTestRows = []mediaTableMapping{
	{File: "gachaitemset.txt", Tables: "GACHA_REWARD_POOL", Authority: "candidate", DatabaseUpdate: "not-normalized", MediaSync: "not-supported", ReconcileMode: "none"},
	{File: "gachanpcmap.txt", Tables: "GACHA operator mapping", Authority: "candidate", DatabaseUpdate: "not-normalized", MediaSync: "not-supported", ReconcileMode: "none"},
	{File: "leveldata.txt", Tables: "REF_LEVEL_DATA", Authority: "database", DatabaseUpdate: "import-reference", MediaSync: "export-media", ReconcileMode: "snapshot"},
	{File: "levelgold.txt", Tables: "REF_LEVEL_DATA", Authority: "database", DatabaseUpdate: "import-reference", MediaSync: "export-media", ReconcileMode: "snapshot"},
	{File: "teleportdata.txt", Tables: "DUNGEON_TELEPORT_POINT; TELEPORT_POINT", Authority: "superseded", DatabaseUpdate: "migrations 000006 and 000134", MediaSync: "not-supported", ReconcileMode: "none"},
}

func TestMediaMapFindsFileAndTableInBothDirections(t *testing.T) {
	var output bytes.Buffer
	if err := writeMediaMap([]string{"--file", "teleportdata.txt"}, &output, mediaMapTestRows); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "teleportdata.txt\tDUNGEON_TELEPORT_POINT; TELEPORT_POINT\tsuperseded\tmigrations 000006 and 000134\tnot-supported\tnone") {
		t.Fatalf("file lookup output = %q", text)
	}

	output.Reset()
	if err := writeMediaMap([]string{"--table", "REF_LEVEL_DATA"}, &output, mediaMapTestRows); err != nil {
		t.Fatal(err)
	}
	text = output.String()
	if !strings.Contains(text, "leveldata.txt\tREF_LEVEL_DATA\tdatabase\timport-reference\texport-media\tsnapshot") ||
		!strings.Contains(text, "levelgold.txt\tREF_LEVEL_DATA\tdatabase\timport-reference\texport-media\tsnapshot") {
		t.Fatalf("table lookup output = %q", text)
	}
}

func TestMediaMapMarksUsefulUnimportedCatalogs(t *testing.T) {
	var output bytes.Buffer
	if err := writeMediaMap([]string{"--file", "gacha"}, &output, mediaMapTestRows); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"gachaitemset.txt\tGACHA_REWARD_POOL\tcandidate", "gachanpcmap.txt\tGACHA operator mapping\tcandidate"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("candidate lookup missing %q: %s", expected, text)
		}
	}
}

func TestMediaMapMakesUnknownFilesExplicit(t *testing.T) {
	var output bytes.Buffer
	if err := writeMediaMap([]string{"--file", "unknowncatalog.txt"}, &output, mediaMapTestRows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "unknowncatalog.txt\tnone\tunmapped\tnone\tnot-supported\tnone") {
		t.Fatalf("unknown lookup output = %q", output.String())
	}
}

func TestMediaMapRejectsPositionalArguments(t *testing.T) {
	if err := writeMediaMap([]string{"unexpected"}, nil, mediaMapTestRows); err != errMediaMapUsage {
		t.Fatalf("error = %v, want %v", err, errMediaMapUsage)
	}
}
