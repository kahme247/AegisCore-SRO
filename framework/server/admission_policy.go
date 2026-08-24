package server

import "fmt"

// PacketAdmissionPolicy decides whether a non-core packet may reach an
// application handler. Core handshake and module-identification packets are
// always handled first by the protocol state machine.
type PacketAdmissionPolicy func(*Session, uint16) error

// AvailabilityAdmissionPolicy rejects every application packet while its
// process-level dependency is not ready. Handshake traffic remains handled by
// the core protocol state machine, but no authentication or gameplay handler
// can run during loading, degradation, drain, or after stop.
func AvailabilityAdmissionPolicy(isReady func() bool, next PacketAdmissionPolicy) PacketAdmissionPolicy {
	return func(session *Session, opcode uint16) error {
		if isReady == nil || !isReady() {
			return fmt.Errorf("application opcode 0x%04X rejected while service is unavailable", opcode)
		}
		if next == nil {
			return fmt.Errorf("application opcode 0x%04X rejected without an admission policy", opcode)
		}
		return next(session, opcode)
	}
}

// AllowListedAdmissionPolicy requires a completed module identification and
// then permits only the supplied application opcodes. It is appropriate for
// Gateway's documented pre-login surface, where no player session becomes
// authenticated on this listener.
func AllowListedAdmissionPolicy(opcodes ...uint16) PacketAdmissionPolicy {
	allowed := make(map[uint16]struct{}, len(opcodes))
	for _, opcode := range opcodes {
		allowed[opcode] = struct{}{}
	}
	return func(session *Session, opcode uint16) error {
		if !session.ModuleIdentified() {
			return fmt.Errorf("application opcode 0x%04X arrived before module identification", opcode)
		}
		if _, exists := allowed[opcode]; !exists {
			return fmt.Errorf("application opcode 0x%04X is not admitted on this listener", opcode)
		}
		return nil
	}
}

// AgentAdmissionPolicy permits exactly one authentication opcode before an
// Agent session is marked authenticated. Once the one-time ticket handler
// succeeds, normal Agent traffic is admitted. Backend transport must use a
// distinct listener rather than adding a plaintext backend opcode here.
func AgentAdmissionPolicy(authenticationOpcode uint16) PacketAdmissionPolicy {
	return func(session *Session, opcode uint16) error {
		if !session.ModuleIdentified() {
			return fmt.Errorf("application opcode 0x%04X arrived before module identification", opcode)
		}
		if session.IsAuthenticated() {
			return nil
		}
		if opcode == authenticationOpcode {
			return nil
		}
		return fmt.Errorf("unauthenticated session sent opcode 0x%04X", opcode)
	}
}

// AgentLifecycleAdmissionPolicy layers the character lifecycle on top of the
// one-time Agent authentication gate. Before authentication it preserves the
// exact AgentAdmissionPolicy behaviour. Afterwards it exposes only the lobby
// surface until a character selection begins, then only the documented
// GameReady confirmation while bootstrap is pending. Once the session reaches
// InWorld, normal Agent application traffic resumes.
//
// The caller supplies the profile-specific opcode sets so this framework
// package never guesses vSRO wire values.
func AgentLifecycleAdmissionPolicy(authenticationOpcode uint16, lobbyOpcodes, loadingOpcodes []uint16) PacketAdmissionPolicy {
	base := AgentAdmissionPolicy(authenticationOpcode)
	lobbyAllowed := opcodeSet(lobbyOpcodes)
	loadingAllowed := opcodeSet(loadingOpcodes)

	return func(session *Session, opcode uint16) error {
		if err := base(session, opcode); err != nil {
			return err
		}
		if !session.IsAuthenticated() {
			return nil
		}

		switch session.LifecyclePhase() {
		case SessionAuthenticated, SessionLobby:
			if _, allowed := lobbyAllowed[opcode]; allowed {
				return nil
			}
			return fmt.Errorf("authenticated lobby session rejected opcode 0x%04X", opcode)
		case SessionCharacterLoading:
			if _, allowed := loadingAllowed[opcode]; allowed {
				return nil
			}
			return fmt.Errorf("character-loading session rejected opcode 0x%04X", opcode)
		case SessionInWorld:
			return nil
		default:
			return fmt.Errorf("authenticated session has invalid lifecycle phase %s for opcode 0x%04X", session.LifecyclePhase(), opcode)
		}
	}
}

func opcodeSet(opcodes []uint16) map[uint16]struct{} {
	allowed := make(map[uint16]struct{}, len(opcodes))
	for _, opcode := range opcodes {
		allowed[opcode] = struct{}{}
	}
	return allowed
}

func isCoreOpcode(opcode uint16) bool {
	switch opcode {
	case 0x5000, 0x9000, 0x2001, 0x2002:
		return true
	default:
		return false
	}
}
