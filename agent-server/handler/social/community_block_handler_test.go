package social

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestCommunityBlockResultCodesMatchGameServerBranches(t *testing.T) {
	tests := []struct {
		action byte
		err    error
		want   byte
	}{
		{vsro188.CommunityBlockAdd, nil, 0},
		{vsro188.CommunityBlockAdd, model.ErrSocialNotFound, 1},
		{vsro188.CommunityBlockAdd, model.ErrSocialConflict, 2},
		{vsro188.CommunityBlockAdd, model.ErrSocialCapacity, 3},
		{vsro188.CommunityBlockAdd, errors.New("database"), 4},
		{vsro188.CommunityBlockRemove, model.ErrSocialNotFound, 2},
	}
	for _, test := range tests {
		if got := communityBlockResultCode(test.action, test.err); got != test.want {
			t.Fatalf("action=%d err=%v result=%d, want %d", test.action, test.err, got, test.want)
		}
	}
}
