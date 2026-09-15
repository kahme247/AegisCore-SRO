package server

import (
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSessionTrySendDisconnectsAfterConsecutiveQueueSaturation(t *testing.T) {
	conn := newDrainingGateConn()
	session := NewSession(conn, network.EncodingOptions{}, nil, nil, "queue-safety-test")
	packet := network.NewServerPacket(0xB021)

	if err := session.TrySend(*packet); err != nil {
		t.Fatalf("initial TrySend() error = %v", err)
	}
	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("sender did not begin blocked write")
	}
	for index := 0; index < session.OutboundStats().Sender.QueueCapacity; index++ {
		if err := session.TrySend(*packet); err != nil {
			t.Fatalf("fill queue slot %d: %v", index, err)
		}
	}
	for rejection := uint32(1); rejection <= DefaultSendQueueRejectLimit; rejection++ {
		err := session.TrySend(*packet)
		if !errors.Is(err, ErrSenderQueueFull) {
			t.Fatalf("rejection %d error = %v, want %v", rejection, err, ErrSenderQueueFull)
		}
		if rejection < DefaultSendQueueRejectLimit && session.OutboundStats().SaturationDisconnects != 0 {
			t.Fatalf("session disconnected after only %d rejections", rejection)
		}
	}

	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("saturated session was not closed")
	}
	stats := session.OutboundStats()
	if stats.ConsecutiveQueueFull != DefaultSendQueueRejectLimit || stats.SaturationDisconnects != 1 || stats.Sender.Rejected != DefaultSendQueueRejectLimit {
		t.Fatalf("saturation stats = %+v", stats)
	}

	close(conn.releaseWrite)
	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("saturated session did not drain")
	}
}

func TestSessionTrySendSuccessResetsSaturationStreak(t *testing.T) {
	conn := newDrainingGateConn()
	session := NewSession(conn, network.EncodingOptions{}, nil, nil, "queue-reset-test")
	session.sendQueueFullStreak.Store(DefaultSendQueueRejectLimit - 1)
	packet := network.NewServerPacket(0xB021)
	if err := session.TrySend(*packet); err != nil {
		t.Fatalf("TrySend() error = %v", err)
	}
	if got := session.OutboundStats().ConsecutiveQueueFull; got != 0 {
		t.Fatalf("successful enqueue retained queue-full streak %d", got)
	}
	session.Close()
	close(conn.releaseWrite)
	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("session did not drain")
	}
}
