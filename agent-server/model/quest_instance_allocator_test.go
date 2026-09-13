package model

import "testing"

func TestFirstAvailableQuestInstanceLayerUsesLowestUnreservedLayer(t *testing.T) {
	layer, ok := firstAvailableQuestInstanceLayer(4, map[uint16]struct{}{1: {}, 3: {}})
	if !ok || layer != 2 {
		t.Fatalf("layer=%d ok=%t", layer, ok)
	}
	if _, ok = firstAvailableQuestInstanceLayer(2, map[uint16]struct{}{1: {}, 2: {}}); ok {
		t.Fatal("allocator exceeded authored WorldMaxCount")
	}
}
