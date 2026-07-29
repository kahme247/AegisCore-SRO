package model

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func TestLoadLevelTableParsesProgressionFields(t *testing.T) {
	table, err := LoadLevelTable(strings.NewReader("1\t118\t1\t0\t0\t24\t0\n2\t470\t1\t0\t0\t47\t0\n"))
	if err != nil {
		t.Fatalf("LoadLevelTable() error = %v", err)
	}
	if got, want := table.MaxLevel(), 2; got != want {
		t.Fatalf("MaxLevel() = %d, want %d", got, want)
	}
	row, ok := table.Level(2)
	if !ok {
		t.Fatal("Level(2) did not return a row")
	}
	if row.RequiredExperience != 470 {
		t.Fatalf("Level(2) = %+v, want EXP=470", row)
	}
	if row.RequiredSkillPoints != 1 {
		t.Fatalf("Level(2) = %+v, want mastery SP=1", row)
	}
}

func TestLoadLevelTableAcceptsPK2UTF16LE(t *testing.T) {
	var encoded bytes.Buffer
	writer := transform.NewWriter(&encoded, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder())
	if _, err := writer.Write([]byte("1\t118\t1\t0\t0\t24\r\n2\t470\t1\t0\t0\t25\r\n")); err != nil {
		t.Fatalf("encode test level data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close test encoder: %v", err)
	}
	table, err := LoadLevelTable(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("LoadLevelTable(UTF-16LE): %v", err)
	}
	if row, ok := table.Level(2); !ok || row.RequiredExperience != 470 {
		t.Fatalf("Level(2) = %+v, %t; want EXP=470", row, ok)
	}
}

func TestLoadLevelTableRejectsInvalidRows(t *testing.T) {
	for _, data := range []string{"", "1\tinvalid\t1\t0\t0\t24\n", "1\t118\t1\t0\t0\t24\n1\t470\t1\t0\t0\t47\n"} {
		_, err := LoadLevelTable(strings.NewReader(data))
		if err == nil {
			t.Fatalf("LoadLevelTable(%q) succeeded, want error", data)
		}
	}
	if _, err := LoadLevelTable(nil); !errors.Is(err, ErrLevelDataNotPresent) {
		t.Fatalf("LoadLevelTable(nil) error = %v, want ErrLevelDataNotPresent", err)
	}
}
