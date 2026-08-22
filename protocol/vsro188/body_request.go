package vsro188

import (
	"errors"
	"fmt"
)

// ClientBodyRequest is the narrow, client-originated request namespace. It
// must never be cast directly to BodyMode because their numeric values have
// different authority and semantics.
type ClientBodyRequest uint8

const (
	ClientBodyRequestNormal  ClientBodyRequest = 0
	ClientBodyRequestBerserk ClientBodyRequest = 1
)

// ErrClientBodyRequestRejected reports a client-selected body mode that is
// neither an allowed normal request nor the allowed Berserk request. In
// particular, it prevents a client from selecting privileged Hwan,
// invincible, invisible, or future modes by number.
var ErrClientBodyRequestRejected = errors.New("client body request is not allowed")

// TranslateClientBodyRequest validates a client-originated body request and
// returns the corresponding authoritative state. Berserk's client request
// value and the vSRO 1.188 entity BodyBerserker value are both 1. The types
// remain separate so privileged body modes can never be selected by casting a
// client byte directly into the authoritative namespace.
//
// This is a pure domain translation. It deliberately does not parse or emit
// packet bytes; the exact wire codec remains capture-blocked.
func TranslateClientBodyRequest(request ClientBodyRequest) (BodyMode, error) {
	switch request {
	case ClientBodyRequestNormal:
		return BodyNormal, nil
	case ClientBodyRequestBerserk:
		return BodyBerserker, nil
	default:
		return BodyNormal, fmt.Errorf("%w: %d", ErrClientBodyRequestRejected, request)
	}
}
