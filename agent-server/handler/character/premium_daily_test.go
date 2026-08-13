package character

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestReadPremiumDailyUseRequestLayouts(t *testing.T) {
	base := network.EmptyPacket()
	base.WriteUInt32(9278)
	base.WriteUInt32(9)
	request, err := readPremiumDailyUseRequest(base)
	if err != nil || request.EntryKey != 9278 || request.EntryValue != 9 || request.Mode != 0 {
		t.Fatalf("base request = %+v, err=%v", request, err)
	}

	mode := network.EmptyPacket()
	mode.WriteUInt32(9278)
	mode.WriteUInt32(9)
	mode.WriteByte(3)
	request, err = readPremiumDailyUseRequest(mode)
	if err != nil || request.Mode != 3 {
		t.Fatalf("mode request = %+v, err=%v", request, err)
	}

	extra := network.EmptyPacket()
	extra.WriteUInt32(9278)
	extra.WriteUInt32(9)
	extra.WriteByte(7)
	extra.WriteUInt32(42)
	request, err = readPremiumDailyUseRequest(extra)
	if err != nil || request.Mode != 7 || request.ExtraValue != 42 {
		t.Fatalf("mode 7 request = %+v, err=%v", request, err)
	}
}
