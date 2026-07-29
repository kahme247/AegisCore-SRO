package model

import "testing"

func TestPlayerGetKnownObjectListReturnsNilWithoutAttachedList(t *testing.T) {
	player := &Player{}
	if known := player.GetKnownObjectList(); known != nil {
		t.Fatalf("GetKnownObjectList() = %#v, want nil before a list is attached", known)
	}
}
