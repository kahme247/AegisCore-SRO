package service

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

// worldBlockingWriteConn keeps a Session.Send active until the test permits
// it to complete. It lets the world test prove that Broadcast does not retain
// the membership mutex during a slow network write.
type worldBlockingWriteConn struct {
	writeStarted chan struct{}
	releaseWrite chan struct{}
	releaseOnce  sync.Once
	closeOnce    sync.Once
	closed       atomic.Bool
}

func newWorldBlockingWriteConn() *worldBlockingWriteConn {
	return &worldBlockingWriteConn{
		writeStarted: make(chan struct{}, 1),
		releaseWrite: make(chan struct{}),
	}
}

func (*worldBlockingWriteConn) Read([]byte) (int, error) { return 0, io.EOF }

func (c *worldBlockingWriteConn) Write(bytes []byte) (int, error) {
	select {
	case c.writeStarted <- struct{}{}:
	default:
	}
	<-c.releaseWrite
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return len(bytes), nil
}

func (c *worldBlockingWriteConn) release() {
	c.releaseOnce.Do(func() { close(c.releaseWrite) })
}

func (c *worldBlockingWriteConn) Close() error {
	c.closeOnce.Do(func() { c.closed.Store(true) })
	return nil
}

func (*worldBlockingWriteConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*worldBlockingWriteConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*worldBlockingWriteConn) SetDeadline(time.Time) error      { return nil }
func (*worldBlockingWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (*worldBlockingWriteConn) SetWriteDeadline(time.Time) error { return nil }

func TestWorldBroadcastDoesNotHoldMembershipLockDuringSend(t *testing.T) {
	conn := newWorldBlockingWriteConn()
	session := server.NewSession(conn, network.EncodingOptions{}, nil, nil, "test")
	t.Cleanup(func() {
		conn.release()
		session.Close()
		select {
		case <-session.Drained():
		case <-time.After(time.Second):
			t.Error("session sender did not drain")
		}
	})

	world := &WorldService{
		playersByUniqueId: map[uint32]*model.Player{
			1: {Session: session},
		},
		mutex: &sync.Mutex{},
	}
	broadcastDone := make(chan struct{})
	go func() {
		world.Broadcast(network.EmptyPacket())
		close(broadcastDone)
	}()

	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not reach the blocked socket write")
	}

	lockAcquired := make(chan struct{})
	go func() {
		world.mutex.Lock()
		world.mutex.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
		// The broadcast is still blocked in Send, so reaching here proves that
		// it released the world lock before doing network I/O.
	case <-time.After(time.Second):
		t.Fatal("Broadcast retained the world lock while Send was blocked")
	}

	conn.release()
	select {
	case <-broadcastDone:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not finish after releasing its socket write")
	}
}

func TestWorldNeighbourAndKnownObjectLookupsSynchronizeRegionStartup(t *testing.T) {
	regionID := int16(25000)
	terrain := navmeshv2.RtNavmeshTerrain{Region: navmeshv2.NewRegionFromInt16(regionID)}
	position := func(x float32) navmeshv2.RtNavmeshPosition {
		return navmeshv2.RtNavmeshPosition{
			Region: terrain.Region,
			Offset: math32.NewVector3(x, 0, 100),
		}
	}
	observer := &model.Player{SRObject: model.SRObject{UniqueID: 1, RtNavmeshPosition: position(100)}}
	other := &model.Player{SRObject: model.SRObject{UniqueID: 2, RtNavmeshPosition: position(150)}}
	world := &WorldService{
		regions: map[int16]*navmeshv2.RtNavmeshTerrain{
			regionID: &terrain,
		},
		visibleObjectsByRegion: map[int16][]model.ISRObject{
			regionID: {observer, other},
		},
		loader2: &navmeshv2.Loader{RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{
			regionID: &terrain,
		}},
		mutex: &sync.Mutex{},
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for range 1_000 {
			_ = world.GetNeighbourRegions(regionID)
			_ = world.GetVisibleObjectsForRegion(regionID)
			known := world.GetKnownObjectsAroundObject(observer)
			if known[other.GetUniqueID()] != other {
				panic("neighbouring visible object disappeared from known-object snapshot")
			}
		}
	}()

	// AddRegions is the startup writer for the same map that the neighbour and
	// known-object paths read. Running it concurrently makes this test fail
	// under -race if either read bypasses the world mutex.
	for range 1_000 {
		world.AddRegions("test", regionID)
	}
	<-readerDone
}
