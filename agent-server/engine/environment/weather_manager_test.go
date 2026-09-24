package environment

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type weatherBroadcastFake struct{ packets []network.Packet }

func (f *weatherBroadcastFake) Broadcast(packet network.Packet) {
	f.packets = append(f.packets, packet)
}

func TestWeatherManagerValidatesStoresAndBroadcasts(t *testing.T) {
	broadcast := &weatherBroadcastFake{}
	manager := NewWeatherManager(broadcast, 1)
	if err := manager.Set(model.Rain, 75); err != nil {
		t.Fatal(err)
	}
	weatherType, intensity := manager.Current()
	if weatherType != model.Rain || intensity != 75 {
		t.Fatalf("Current() = (%d, %d)", weatherType, intensity)
	}
	if len(broadcast.packets) != 1 || broadcast.packets[0].MessageID != opcode.WeatherUpdate || string(broadcast.packets[0].Payload()) != string([]byte{2, 75}) {
		t.Fatalf("unexpected weather broadcast")
	}
	if err := manager.Set(model.Snow, 101); err == nil {
		t.Fatal("invalid intensity unexpectedly accepted")
	}
}

func TestWeatherForRegionKeepsQinShiTombClear(t *testing.T) {
	for regionID := int16(-32766); regionID <= -32761; regionID++ {
		weatherType, intensity := WeatherForRegion(regionID, model.Rain, 75)
		if weatherType != model.Clear || intensity != 0 {
			t.Fatalf("WeatherForRegion(%d) = (%d, %d)", regionID, weatherType, intensity)
		}
	}
	weatherType, intensity := WeatherForRegion(25000, model.Snow, 40)
	if weatherType != model.Snow || intensity != 40 {
		t.Fatalf("outside Tomb = (%d, %d)", weatherType, intensity)
	}
}

func TestWeatherManagerRunStopsWithContext(t *testing.T) {
	broadcast := &signalingWeatherBroadcastFake{sent: make(chan struct{}, 1)}
	manager := NewWeatherManager(broadcast, 2)
	ctx, cancel := context.WithCancel(context.Background())
	go manager.Run(ctx, time.Millisecond)
	select {
	case <-broadcast.sent:
		cancel()
	case <-time.After(100 * time.Millisecond):
		cancel()
		t.Fatal("weather runtime did not transition")
	}
}

type signalingWeatherBroadcastFake struct{ sent chan struct{} }

func (f *signalingWeatherBroadcastFake) Broadcast(network.Packet) {
	select {
	case f.sent <- struct{}{}:
	default:
	}
}
