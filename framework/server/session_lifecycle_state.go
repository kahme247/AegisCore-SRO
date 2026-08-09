package server

// SessionLifecyclePhase is the application-facing portion of a connection's
// lifetime. HandshakePhase remains responsible for the wire security
// exchange; this state starts governing packets only after admission has
// established an authenticated user identity.
//
// The legal application path is:
//
//	Connected -> Handshaking -> Authenticated -> Lobby -> CharacterLoading -> InWorld -> Closing
//
// A phase never moves backwards except when a character bootstrap fails: in
// that case AbortCharacterLoading returns the authenticated session to Lobby
// without retaining a selected character or world player.
type SessionLifecyclePhase uint32

const (
	SessionConnected SessionLifecyclePhase = iota
	SessionHandshaking
	SessionAuthenticated
	SessionLobby
	SessionCharacterLoading
	SessionInWorld
	SessionClosing
)

func (p SessionLifecyclePhase) String() string {
	switch p {
	case SessionConnected:
		return "Connected"
	case SessionHandshaking:
		return "Handshaking"
	case SessionAuthenticated:
		return "Authenticated"
	case SessionLobby:
		return "Lobby"
	case SessionCharacterLoading:
		return "CharacterLoading"
	case SessionInWorld:
		return "InWorld"
	case SessionClosing:
		return "Closing"
	default:
		return "Unknown"
	}
}

// LifecyclePhase returns the current application phase without exposing the
// mutable UserContext fields. It is safe for admission policies and terminal
// cleanup hooks to inspect concurrently.
func (s *Session) LifecyclePhase() SessionLifecyclePhase {
	if s == nil {
		return SessionClosing
	}
	return SessionLifecyclePhase(s.lifecyclePhase.Load())
}

func (s *Session) markHandshaking() {
	if s == nil {
		return
	}
	s.lifecyclePhase.CompareAndSwap(uint32(SessionConnected), uint32(SessionHandshaking))
}

func (s *Session) markClosing() {
	if s == nil {
		return
	}
	s.lifecyclePhase.Store(uint32(SessionClosing))
}

// markAuthenticated advances a successfully admitted session into the
// authenticated state. It intentionally does not enter Lobby: the first
// lobby request performs that transition, keeping all character-selection
// paths explicit and fail-closed.
func (s *Session) markAuthenticated() bool {
	if s == nil {
		return false
	}
	for {
		switch phase := s.LifecyclePhase(); phase {
		case SessionConnected, SessionHandshaking:
			if s.lifecyclePhase.CompareAndSwap(uint32(phase), uint32(SessionAuthenticated)) {
				return true
			}
		case SessionAuthenticated:
			return true
		default:
			return false
		}
	}
}

// EnterLobby admits the character list and selection surface for an
// authenticated session. Once character loading has begun, lobby mutations
// are no longer admitted until that load is explicitly aborted.
func (s *Session) EnterLobby() bool {
	if s == nil || !s.IsAuthenticated() {
		return false
	}
	for {
		switch phase := s.LifecyclePhase(); phase {
		case SessionAuthenticated:
			if s.lifecyclePhase.CompareAndSwap(uint32(SessionAuthenticated), uint32(SessionLobby)) {
				return true
			}
		case SessionLobby:
			return true
		default:
			return false
		}
	}
}

// BeginCharacterLoading reserves a single character-selection attempt for
// this session. Duplicate joins and out-of-order GameReady packets cannot
// pass this transition.
func (s *Session) BeginCharacterLoading() bool {
	if s == nil || !s.IsAuthenticated() {
		return false
	}
	return s.lifecyclePhase.CompareAndSwap(uint32(SessionLobby), uint32(SessionCharacterLoading))
}

// BeginTeleportLoading reuses the stock character-data loading exchange after
// an in-server teleport. It deliberately accepts only an active world session.
func (s *Session) BeginTeleportLoading() bool {
	if s == nil || !s.IsAuthenticated() {
		return false
	}
	return s.lifecyclePhase.CompareAndSwap(uint32(SessionInWorld), uint32(SessionCharacterLoading))
}

// AbortTeleportLoading restores an active session if a teleport fails before
// its loading handshake has been sent.
func (s *Session) AbortTeleportLoading() bool {
	if s == nil {
		return false
	}
	return s.lifecyclePhase.CompareAndSwap(uint32(SessionCharacterLoading), uint32(SessionInWorld))
}

// AbortCharacterLoading returns a failed bootstrap to the lobby. Callers must
// first remove any partially admitted player and clear the selected character
// fields in UserContext.
func (s *Session) AbortCharacterLoading() bool {
	if s == nil {
		return false
	}
	return s.lifecyclePhase.CompareAndSwap(uint32(SessionCharacterLoading), uint32(SessionLobby))
}

// CompleteCharacterLoading is the one-way confirmation made by a validated
// client GameReady packet. It succeeds exactly once.
func (s *Session) CompleteCharacterLoading() bool {
	if s == nil {
		return false
	}
	return s.lifecyclePhase.CompareAndSwap(uint32(SessionCharacterLoading), uint32(SessionInWorld))
}

// IsInWorld lets application handlers avoid treating a staged character as a
// live world actor.
func (s *Session) IsInWorld() bool {
	return s != nil && s.LifecyclePhase() == SessionInWorld
}
