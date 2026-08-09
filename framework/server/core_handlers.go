package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security"
	"github.com/Harvi8/AegisCore/framework/security/blowfish"
	utils2 "github.com/Harvi8/AegisCore/framework/utils"
	log "github.com/sirupsen/logrus"
)

func registerCoreHandlers(registry *HandlerRegistry) {
	registry.Register(0x5000, handleKeyExchangePacket)
	registry.Register(0x9000, handleKeyExchangeCompletedPacket)
	registry.Register(0x2001, handleModuleIdentificationPacket)
	registry.Register(0x2002, handleKeepAlivePacket)
}

func handleKeyExchangePacket(_ context.Context, session *Session, packet network.Packet) error {
	if packet.PayloadLen() != 12 {
		return fmt.Errorf("invalid key exchange payload length %d", packet.PayloadLen())
	}
	session.securityMu.Lock()
	if session.HandshakePhase() != HandshakeChallengeSent || !session.EncodingOptions.KeyExchange {
		session.securityMu.Unlock()
		return errors.New("received key exchange response outside the challenge phase")
	}

	remotePublic, err := packet.ReadUInt32()
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("read remote public key: %w", err)
	}
	// The vSRO handshake is not a modern authenticated key exchange, but it
	// must still reject the trivial public values that collapse the shared
	// secret to a value visible to an on-path attacker.  Keep this deliberately
	// conservative: profile compatibility is preserved for all non-trivial
	// values and we do not impose undocumented subgroup checks.
	if session.Context.Generator <= 1 || session.Context.Prime < 3 ||
		session.Context.LocalPublic <= 1 || session.Context.LocalPublic >= session.Context.Prime-1 ||
		remotePublic <= 1 || remotePublic >= session.Context.Prime-1 {
		session.securityMu.Unlock()
		return errors.New("invalid key exchange public values")
	}
	remoteSignature, err := packet.ReadUInt64()
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("read remote challenge: %w", err)
	}
	session.Context.RemotePublic = remotePublic
	session.Context.RemoteSignature = remoteSignature
	session.Context.CommonSecret = security.G_pow_X_mod_P(remotePublic, session.Context.Private, session.Context.Prime)

	key := security.CalculateKey(session.Context.CommonSecret, session.Context.LocalPublic, remotePublic)
	cipher, err := blowfish.NewCipher(key)
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("derive handshake cipher: %w", err)
	}
	session.Cipher = cipher

	challenge := make([]byte, 8)
	binary.LittleEndian.PutUint64(challenge, session.Context.RemoteSignature)
	cipher.DecryptRev(challenge, challenge)
	session.Context.RemoteSignature = utils2.ByteArrayToUint64(challenge)

	expectedSignature := utils2.ByteArrayToUint64(
		security.CalculateChallenge(session.Context.CommonSecret, remotePublic, session.Context.LocalPublic))
	if expectedSignature != session.Context.RemoteSignature {
		session.securityMu.Unlock()
		return fmt.Errorf("invalid client signature: got %d, want %d", session.Context.RemoteSignature, expectedSignature)
	}

	localChallenge := security.CalculateChallenge(session.Context.CommonSecret, session.Context.LocalPublic, remotePublic)
	cipher.EncryptRev(localChallenge, localChallenge)
	session.Context.LocalSignature = utils2.ByteArrayToUint64(localChallenge)

	finalKey := security.KeyTransformValue(utils2.Uint64ToByteArray(session.Context.HandshakeBlowfishKey), session.Context.CommonSecret, 3)
	finalCipher, err := blowfish.NewCipher(finalKey)
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("derive final handshake cipher: %w", err)
	}
	session.Cipher = finalCipher
	session.Context.HandshakeBlowfishKey = utils2.ByteArrayToUint64(finalKey)
	session.Context.CompletedLocalSetup = true
	session.Context.CompletedRemoteSetup = true
	session.setHandshakePhaseLocked(HandshakeAwaitingCompletion)

	response := network.EmptyPacket()
	response.MessageID = 0x5000
	if err := response.WriteByte(network.EncodingKeyChallenge); err != nil {
		session.securityMu.Unlock()
		return err
	}
	if err := response.WriteUInt64(session.Context.LocalSignature); err != nil {
		session.securityMu.Unlock()
		return err
	}
	session.securityMu.Unlock()
	return session.Send(response)
}

func handleKeyExchangeCompletedPacket(_ context.Context, session *Session, packet network.Packet) error {
	if packet.PayloadLen() != 0 {
		return fmt.Errorf("invalid key exchange completion payload length %d", packet.PayloadLen())
	}
	session.securityMu.Lock()
	defer session.securityMu.Unlock()
	if session.HandshakePhase() != HandshakeAwaitingCompletion || !session.Context.CompletedLocalSetup || !session.Context.CompletedRemoteSetup {
		return errors.New("key exchange completion arrived before setup completed")
	}
	log.Debugln("Finished Handshake")
	session.Context.FinishedHandshake = true
	session.setHandshakePhaseLocked(HandshakeEstablished)
	return nil
}

func handleModuleIdentificationPacket(_ context.Context, session *Session, packet network.Packet) error {
	session.securityMu.Lock()
	if session.HandshakePhase() != HandshakeEstablished {
		session.securityMu.Unlock()
		return errors.New("module identification arrived before handshake completion")
	}
	if session.moduleIdentified.Load() {
		session.securityMu.Unlock()
		return errors.New("duplicate module identification")
	}
	moduleName, err := packet.ReadString()
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("read module name: %w", err)
	}
	isLocalModule, err := packet.ReadByte()
	if err != nil {
		session.securityMu.Unlock()
		return fmt.Errorf("read module locality: %w", err)
	}
	if isLocalModule > 1 || packet.Remaining() != 0 {
		session.securityMu.Unlock()
		return errors.New("invalid module identification payload")
	}
	log.Debugf("Module identified %v. Local: %v", moduleName, isLocalModule)

	response := network.EmptyPacket()
	response.MessageID = 0x2001
	if err := response.WriteString(session.ServerModuleID); err != nil {
		session.securityMu.Unlock()
		return err
	}
	if err := response.WriteByte(0); err != nil {
		session.securityMu.Unlock()
		return err
	}
	session.moduleIdentified.Store(true)
	session.securityMu.Unlock()
	return session.Send(response)
}

func handleKeepAlivePacket(_ context.Context, session *Session, packet network.Packet) error {
	if packet.PayloadLen() != 0 {
		return fmt.Errorf("invalid keep-alive payload length %d", packet.PayloadLen())
	}
	if session.HandshakePhase() != HandshakeEstablished || !session.ModuleIdentified() {
		return errors.New("keep-alive arrived before module identification")
	}
	return nil
}
