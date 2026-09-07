package model

import "testing"

func TestDatabaseChatFilterReplacesEveryCaseInsensitiveOccurrence(t *testing.T) {
	repository := &SQLChatFilterRepository{terms: []chatFilterTerm{{match: "bad", replacement: "***"}}}
	filtered, changed := repository.Filter("BAD wolf and bad actor")
	if !changed || filtered != "*** wolf and *** actor" {
		t.Fatalf("Filter() = %q, %t", filtered, changed)
	}
	unchanged, changed := repository.Filter("ordinary chat")
	if changed || unchanged != "ordinary chat" {
		t.Fatalf("unchanged Filter() = %q, %t", unchanged, changed)
	}
}
