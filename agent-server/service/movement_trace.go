package service

import (
	"os"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/sirupsen/logrus"
)

type movementTraceMode uint8

const (
	movementTraceOff movementTraceMode = iota
	movementTraceAll
	movementTraceMobs
	movementTracePlayers
)

func traceMovementf(format string, args ...interface{}) {
	mode := configuredMovementTraceMode()
	if mode == movementTraceAll || mode == movementTracePlayers {
		logrus.Infof("MOVEMENT_TRACE "+format, args...)
	}
}

func traceMovementFor(character model.ICharacter, format string, args ...interface{}) {
	if !movementTraceAllows(character) {
		return
	}
	logrus.Infof("MOVEMENT_TRACE "+format, args...)
}

// MovementTraceEnabled lets bounded companion diagnostics, such as player
// visibility transitions, follow the same on/off switch without duplicating
// the mode parser. Those events remain useful in every enabled mode because
// they are emitted only when a player's known list actually changes.
func MovementTraceEnabled() bool {
	return configuredMovementTraceMode() != movementTraceOff
}

func movementTraceAllows(character model.ICharacter) bool {
	mode := configuredMovementTraceMode()
	if mode == movementTraceOff || character == nil {
		return false
	}
	if mode == movementTraceAll {
		return true
	}
	if _, ok := character.(*model.Player); ok {
		return mode == movementTracePlayers
	}
	return mode == movementTraceMobs && character.GetTypeInfo().IsNPCMob()
}

func configuredMovementTraceMode() movementTraceMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SRO_MOVEMENT_TRACE"))) {
	case "1", "all", "everything":
		return movementTraceAll
	case "mob", "mobs":
		return movementTraceMobs
	case "player", "players", "char", "chars", "character", "characters":
		return movementTracePlayers
	default:
		return movementTraceOff
	}
}
