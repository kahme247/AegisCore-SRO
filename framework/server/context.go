package server

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"github.com/ferdoran/go-sro/framework/security"
)

type SecurityContext struct {
	security.BaseSecurityContext
	InitialBlowfishKey   uint64
	HandshakeBlowfishKey uint64
	StartedHandshake     bool
	CompletedLocalSetup  bool
	CompletedRemoteSetup bool
	FinishedHandshake    bool
}

func NewContext() SecurityContext {
	// Handshake values protect a live connection.  math/rand makes these
	// values predictable from process start time, so fail closed if the OS CSPRNG
	// is unavailable rather than constructing an insecure session.
	prime := secureHandshakeValue(3)
	generator := secureHandshakeValue(2)
	private := secureHandshakeValue(1)
	key := secureNonZeroRandomBytes(8)
	handshakeBlowfishKey := secureNonZeroRandomBytes(8)

	ctx := SecurityContext{}
	ctx.Prime = prime
	ctx.Generator = generator
	ctx.Private = private
	ctx.InitialBlowfishKey = binary.LittleEndian.Uint64(key)
	ctx.HandshakeBlowfishKey = binary.LittleEndian.Uint64(handshakeBlowfishKey)
	ctx.LocalPublic = security.G_pow_X_mod_P(ctx.Generator, ctx.Private, ctx.Prime)
	return ctx
}

func secureHandshakeValue(minimum uint32) uint32 {
	for {
		value := binary.LittleEndian.Uint32(secureRandomBytes(4)) & 0x7FFFFFFF
		if value >= minimum {
			return value
		}
	}
}

func secureRandomBytes(length int) []byte {
	bytes := make([]byte, length)
	if _, err := cryptorand.Read(bytes); err != nil {
		panic(fmt.Sprintf("secure random source unavailable: %v", err))
	}
	return bytes
}

func secureNonZeroRandomBytes(length int) []byte {
	for {
		bytes := secureRandomBytes(length)
		for _, value := range bytes {
			if value != 0 {
				return bytes
			}
		}
	}
}
