package model

import (
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"math"
	"time"
)

const MaxMovementHeading = float32(0xFFFF)

type MovementData struct {
	Generation     uint64
	StartTime      time.Time
	UpdateTime     time.Time
	TargetPosition navmeshv2.RtNavmeshPosition
	HasDestination bool
	DirectionAngle float32
}

func HeadingToRadians(heading float32) float32 {
	normalized := float32(math.Mod(float64(heading), float64(MaxMovementHeading+1)))
	if normalized < 0 {
		normalized += MaxMovementHeading + 1
	}
	return normalized / (MaxMovementHeading + 1) * 2 * math.Pi
}

func RadiansToHeading(radians float32) float32 {
	normalized := math.Mod(float64(radians), 2*math.Pi)
	if normalized < 0 {
		normalized += 2 * math.Pi
	}
	wireHeading := math.Round(normalized / (2 * math.Pi) * float64(MaxMovementHeading+1))
	return float32(math.Mod(wireHeading, float64(MaxMovementHeading+1)))
}
