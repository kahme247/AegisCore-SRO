package model

import "github.com/ferdoran/go-sro/protocol/vsro188"

// LifeState, BodyState, and MotionState share the wire-format-neutral
// authoritative values defined by the pinned vSRO 1.88 profile. Keeping these
// aliases in the model layer lets existing game objects use the corrected
// values without making the model package responsible for a packet layout.
type LifeState = vsro188.LifeState

const (
	Spawning LifeState = vsro188.LifeSpawning
	Alive    LifeState = vsro188.LifeAlive
	Dead     LifeState = vsro188.LifeDead
	Gone     LifeState = vsro188.LifeGone
)

type BodyState = vsro188.BodyMode

const (
	Normal       BodyState = vsro188.BodyNormal
	Hwan         BodyState = vsro188.BodyHwan
	Untouchable  BodyState = vsro188.BodyUntouchable
	GmInvincible BodyState = vsro188.BodyGMInvincible
	GmInvisible  BodyState = vsro188.BodyGMInvisible
	Berserker    BodyState = vsro188.BodyBerserker
	Stealth      BodyState = vsro188.BodyStealth
	Invisible    BodyState = vsro188.BodyInvisible

	// Legacy names remain source-compatible aliases.
	NoStatus   = Normal
	Berserk    = Berserker
	Invincible = GmInvincible
)

type MotionState = vsro188.MotionState

const (
	Stand MotionState = vsro188.MotionStand
	Walk  MotionState = vsro188.MotionWalk
	Run   MotionState = vsro188.MotionRun
	Sit   MotionState = vsro188.MotionSit

	// Retain the existing names for callers while correcting their values.
	NoMotion = Stand
	Walking  = Walk
	Running  = Run
	Sitting  = Sit
)

// BattleState represents peace/battle mode. It is deliberately distinct from
// PKState, which describes PvP reputation/cape-related state elsewhere in the
// model. No battle-state packet field is emitted here because its wire layout
// is not yet capture-backed.
type BattleState = vsro188.BattleState

const (
	BattlePeace  BattleState = vsro188.BattlePeace
	BattleBattle BattleState = vsro188.BattleBattle
)
