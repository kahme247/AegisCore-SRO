package service

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestPartyMatchingPaginationIsBoundedAndStable(t *testing.T) {
	service := &PartyService{FormedParties: make(map[uint32]*model.Party)}
	for id := uint32(25); id > 4; id-- {
		service.FormedParties[id] = &model.Party{Number: id}
	}
	if pages := service.GetFormedPartiesPageCount(); pages != 2 {
		t.Fatalf("page count = %d, want 2", pages)
	}
	first := service.GetFormedParties(0, 0)
	second := service.GetFormedParties(0, 1)
	if len(first) != PartiesPerPage || len(second) != 1 || first[0].Number != 5 || first[19].Number != 24 || second[0].Number != 25 {
		t.Fatalf("pages = first:%v second:%v", first, second)
	}
	if out := service.GetFormedParties(0, 2); len(out) != 0 {
		t.Fatalf("out-of-range page = %v", out)
	}
}
