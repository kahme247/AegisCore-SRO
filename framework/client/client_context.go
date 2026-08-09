package client

import (
	security2 "github.com/Harvi8/AegisCore/framework/security"
)

type SecurityContext struct {
	security2.BaseSecurityContext
	HandshakeStarted     bool
	HandshakeCompleted   bool
	HandshakeBlowfishKey uint64
	HandshakeKey         uint64
	CommonSecret         uint32
	LocalSetupCompleted  bool
}
