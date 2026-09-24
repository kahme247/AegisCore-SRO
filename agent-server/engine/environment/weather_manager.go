package environment

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const WeatherChangeInterval = 15 * time.Minute

type weatherBroadcaster interface {
	Broadcast(network.Packet)
}

type WeatherManager struct {
	mu          sync.RWMutex
	weatherType model.WeatherType
	intensity   byte
	broadcaster weatherBroadcaster
	random      *rand.Rand
	running     bool
}

var currentWeather = NewWeatherManager(nil, time.Now().UnixNano())

func NewWeatherManager(broadcaster weatherBroadcaster, seed int64) *WeatherManager {
	return &WeatherManager{
		weatherType: model.Clear,
		broadcaster: broadcaster,
		random:      rand.New(rand.NewSource(seed)),
	}
}

func GetCelestialPositionForPlayer(player *model.Player) CelestialPosition {
	return CelestialPosition{CharUniqueID: player.UniqueID}
}

func GetCurrentWeather() (model.WeatherType, byte) { return currentWeather.Current() }

func SetCurrentWeather(weatherType model.WeatherType, intensity byte) error {
	return currentWeather.Set(weatherType, intensity)
}

func WeatherForRegion(regionID int16, weatherType model.WeatherType, intensity byte) (model.WeatherType, byte) {
	if regionID >= -32766 && regionID <= -32761 {
		return model.Clear, 0
	}
	return weatherType, intensity
}

func StartWeatherSystem(ctx context.Context, broadcaster weatherBroadcaster) {
	if ctx == nil {
		return
	}
	currentWeather.mu.Lock()
	if currentWeather.running {
		if broadcaster != nil {
			currentWeather.broadcaster = broadcaster
		}
		currentWeather.mu.Unlock()
		return
	}
	currentWeather.broadcaster = broadcaster
	currentWeather.running = true
	currentWeather.mu.Unlock()
	go func() {
		currentWeather.Run(ctx, WeatherChangeInterval)
		currentWeather.mu.Lock()
		currentWeather.running = false
		currentWeather.mu.Unlock()
	}()
}

func (m *WeatherManager) Current() (model.WeatherType, byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.weatherType, m.intensity
}

func (m *WeatherManager) Set(weatherType model.WeatherType, intensity byte) error {
	if weatherType < model.Clear || weatherType > model.Snow || intensity > 100 {
		return fmt.Errorf("invalid weather type or intensity")
	}
	if weatherType == model.Clear {
		intensity = 0
	}
	m.mu.Lock()
	m.weatherType, m.intensity = weatherType, intensity
	broadcaster := m.broadcaster
	m.mu.Unlock()
	if broadcaster != nil {
		broadcaster.Broadcast(BuildWeatherUpdate(weatherType, intensity))
	}
	return nil
}

func (m *WeatherManager) Run(ctx context.Context, interval time.Duration) {
	if ctx == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.advance()
		}
	}
}

func (m *WeatherManager) advance() {
	m.mu.Lock()
	weatherType := model.WeatherType(m.random.Intn(3) + 1)
	intensity := byte(0)
	if weatherType != model.Clear {
		intensity = byte(m.random.Intn(100) + 1)
	}
	m.mu.Unlock()
	_ = m.Set(weatherType, intensity)
}

func BuildWeatherUpdate(weatherType model.WeatherType, intensity byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.WeatherUpdate
	packet.WriteByte(byte(weatherType))
	packet.WriteByte(intensity)
	return packet
}
