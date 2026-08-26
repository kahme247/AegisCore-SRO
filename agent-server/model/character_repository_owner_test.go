package model

import (
	"errors"
	"strings"
	"testing"
)

func TestOwnerQualifiedCharacterOperationsRejectInvalidInputBeforeDatabaseAccess(t *testing.T) {
	if _, err := GetCharacterForOwner("", 42, 1); !errors.Is(err, ErrCharacterNameRequired) {
		t.Fatalf("GetCharacterForOwner(empty name) error = %v, want ErrCharacterNameRequired", err)
	}
	if _, err := GetCharacterForOwner("Alice", 0, 1); !errors.Is(err, ErrCharacterOwnerRequired) {
		t.Fatalf("GetCharacterForOwner(empty owner) error = %v, want ErrCharacterOwnerRequired", err)
	}
	if _, err := GetCharacterForOwner("Alice", 42, 0); !errors.Is(err, ErrCharacterShardRequired) {
		t.Fatalf("GetCharacterForOwner(empty shard) error = %v, want ErrCharacterShardRequired", err)
	}
	if _, err := GetCharactersForOwner(42, 0); !errors.Is(err, ErrCharacterShardRequired) {
		t.Fatalf("GetCharactersForOwner(empty shard) error = %v, want ErrCharacterShardRequired", err)
	}
	if updated, err := SetCharacterDeletionForOwner("", 42, 1, true); updated || !errors.Is(err, ErrCharacterNameRequired) {
		t.Fatalf("SetCharacterDeletionForOwner(empty name) = (%t, %v), want (false, ErrCharacterNameRequired)", updated, err)
	}
	if updated, err := SetCharacterDeletionForOwner("Alice", 0, 1, true); updated || !errors.Is(err, ErrCharacterOwnerRequired) {
		t.Fatalf("SetCharacterDeletionForOwner(empty owner) = (%t, %v), want (false, ErrCharacterOwnerRequired)", updated, err)
	}
	if exists, err := CharacterNameExists(" \t "); err != nil || exists {
		t.Fatalf("CharacterNameExists(blank) = (%t, %v), want (false, nil)", exists, err)
	}
}

func TestOwnerQualifiedCharacterQueriesIncludeOwnerPredicate(t *testing.T) {
	for name, query := range map[string]string{
		"lookup":         SelectCharByNameAndOwner,
		"character list": SelectCharsByAccountAndShard,
		"delete/restore": update_is_deleting_for_owner,
	} {
		if name == "character list" {
			if !strings.Contains(query, "WHERE c.FK_USER=? AND c.FK_SHARD=?") {
				t.Fatalf("%s query is not owner-qualified: %s", name, query)
			}
			continue
		}
		if name == "delete/restore" {
			if !strings.Contains(query, "LOWER(TRIM(c.CHAR_NAME))=LOWER(TRIM(?)) AND c.FK_USER=? AND c.FK_SHARD=?") {
				t.Fatalf("%s query is not owner-qualified: %s", name, query)
			}
			continue
		}
		if !strings.Contains(query, "LOWER(TRIM(CHAR_NAME))=LOWER(TRIM(?)) AND FK_USER=? AND FK_SHARD=?") {
			t.Fatalf("%s query is not owner-qualified: %s", name, query)
		}
	}
	if strings.Contains(update_is_deleting_for_owner, "WHERE CHAR_NAME=?;") {
		t.Fatalf("delete/restore query retained a name-only predicate: %s", update_is_deleting_for_owner)
	}
}

func TestCharacterNameQueriesUseOneNormalizedIdentityRule(t *testing.T) {
	const normalizedName = "LOWER(TRIM(CHAR_NAME))=LOWER(TRIM(?))"
	for name, query := range map[string]string{
		"lookup":         SelectCharByNameAndOwner,
		"availability":   select_does_charname_exist,
		"delete/restore": strings.ReplaceAll(update_is_deleting_for_owner, "c.CHAR_NAME", "CHAR_NAME"),
	} {
		if !strings.Contains(query, normalizedName) {
			t.Fatalf("%s query does not use the normalized character identity: %s", name, query)
		}
	}
}

func TestCharacterDeletionQueriesEnforceOrganizationAndRetentionRules(t *testing.T) {
	for _, required := range []string{"GUILD_MEMBER", "MEMBER_CLASS=0", "ACADEMY_MEMBER", "DELETION_REQUESTED_AT=CURRENT_TIMESTAMP(6)"} {
		if !strings.Contains(update_is_deleting_for_owner, required) {
			t.Fatalf("temporary deletion query lacks %q guard: %s", required, update_is_deleting_for_owner)
		}
	}
	for _, required := range []string{"DELETING=TRUE", "DELETION_REQUESTED_AT>DATE_SUB(CURRENT_TIMESTAMP(6),INTERVAL 7 DAY)"} {
		if !strings.Contains(restore_deleting_for_owner, required) {
			t.Fatalf("restore query lacks %q guard: %s", required, restore_deleting_for_owner)
		}
	}
}
