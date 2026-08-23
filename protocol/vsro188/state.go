package vsro188

// LifeState describes whether an entity is being introduced to the world,
// active, dead, or no longer present.
type LifeState uint8

const (
	LifeSpawning LifeState = 0
	LifeAlive    LifeState = 1
	LifeDead     LifeState = 2
	LifeGone     LifeState = 3
)

// BodyMode is the authoritative body-state namespace. It is deliberately
// separate from ClientBodyRequest: a client request must be translated before
// it changes this state.
type BodyMode uint8

const (
	BodyNormal        BodyMode = 0
	BodyBerserker     BodyMode = 1
	BodyHwan                   = BodyBerserker
	BodyUntouchable   BodyMode = 2
	BodyGMInvincible  BodyMode = 3
	BodyGMUntouchable BodyMode = 4
	BodyGMInvisible   BodyMode = 5
	BodyStealth       BodyMode = 6
	BodyInvisible     BodyMode = 7
)

// MotionState covers the documented locomotion states. Action-state values
// are intentionally not guessed here: their exact values and wire encoding
// remain capture-blocked.
type MotionState uint8

const (
	MotionStand MotionState = 0
	MotionWalk  MotionState = 1
	MotionRun   MotionState = 2
	MotionSit   MotionState = 3
)

// BattleState is independent from PvP state. It describes the entity's
// battle/peace mode, not reputation, cape, or other PvP semantics.
type BattleState uint8

const (
	BattlePeace  BattleState = 0
	BattleBattle BattleState = 1
)

// PvPState is intentionally a distinct type from BattleState. Its value set
// and codecs will be added only from capture-backed protocol evidence.
type PvPState uint8
