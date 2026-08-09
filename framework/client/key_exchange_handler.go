package client

import (
	"errors"
	"fmt"

	network2 "github.com/Harvi8/AegisCore/framework/network"
	security2 "github.com/Harvi8/AegisCore/framework/security"
	blowfish2 "github.com/Harvi8/AegisCore/framework/security/blowfish"
	utils2 "github.com/Harvi8/AegisCore/framework/utils"
	log "github.com/sirupsen/logrus"
)

var (
	ErrUnexpectedKeyExchangePhase = errors.New("unexpected key exchange phase")
	ErrInvalidServerSignature     = errors.New("invalid server key-exchange signature")
	ErrInvalidKeyExchangeValues   = errors.New("invalid key exchange parameters")
	ErrMalformedKeyExchangePacket = errors.New("malformed key exchange packet")
)

type KeyExchangeHandler struct {
	client *Client
}

// Handle is deliberately fail-closed. A malformed, duplicate, out-of-order,
// or cryptographically invalid 0x5000 packet closes its generation instead of
// leaving a half-initialized client connected.
func (h *KeyExchangeHandler) Handle(packet network2.Packet) {
	if h == nil || h.client == nil {
		return
	}
	if err := h.handle(packet); err != nil {
		log.Errorf("rejecting key exchange packet: %v", err)
		h.client.failActiveGeneration(err)
	}
}

func (h *KeyExchangeHandler) handle(packet network2.Packet) error {
	h.client.securityMu.RLock()
	started := h.client.Ctx.HandshakeStarted
	localSetupCompleted := h.client.Ctx.LocalSetupCompleted
	handshakeCompleted := h.client.Ctx.HandshakeCompleted
	h.client.securityMu.RUnlock()
	switch {
	case !started:
		return h.handleInitial(packet)
	case started && localSetupCompleted && !handshakeCompleted:
		return h.handleChallenge(packet)
	default:
		return fmt.Errorf("%w: started=%t local-setup=%t complete=%t", ErrUnexpectedKeyExchangePhase, started, localSetupCompleted, handshakeCompleted)
	}
}

func (h *KeyExchangeHandler) handleInitial(packet network2.Packet) error {
	optionsByte, err := readKeyExchangeByte(&packet, "encoding options")
	if err != nil {
		return err
	}
	const initialOptionMask = network2.EncodingDisabled | network2.EncodingEncryption | network2.EncodingEDC | network2.EncodingKeyExchange
	if optionsByte&^initialOptionMask != 0 {
		return fmt.Errorf("%w: invalid initial encoding options 0x%02X", ErrMalformedKeyExchangePacket, optionsByte)
	}
	options := network2.NewEncodingOptions(optionsByte)

	var (
		initialKey uint64
		cipher     *blowfish2.Cipher
		sequence   network2.MessageSequence
		crc        network2.MessageCRC
	)
	if options.Encryption {
		initialKey, err = readKeyExchangeUInt64(&packet, "initial blowfish key")
		if err != nil {
			return err
		}
		cipher, err = blowfish2.NewCipher(utils2.Uint64ToByteArray(initialKey))
		if err != nil {
			return fmt.Errorf("create initial blowfish cipher: %w", err)
		}
	}
	if options.EDC {
		sequenceSeed, readErr := readKeyExchangeUInt32(&packet, "sequence seed")
		if readErr != nil {
			return readErr
		}
		crcSeed, readErr := readKeyExchangeUInt32(&packet, "CRC seed")
		if readErr != nil {
			return readErr
		}
		sequence = network2.NewMessageSequence(sequenceSeed)
		crc = network2.NewMessageCRC(crcSeed)
	}

	h.client.securityMu.Lock()
	h.client.EncodingOptions = options
	h.client.Ctx.HandshakeStarted = true
	h.client.Ctx.HandshakeBlowfishKey = initialKey
	h.client.Cipher = cipher
	if options.EDC {
		h.client.Sequence = sequence
		h.client.Crc = crc
	}
	h.client.securityMu.Unlock()
	if options.KeyExchange {
		return h.setupHandshake(packet)
	}
	if err := requireNoKeyExchangeTrailingBytes(&packet, "initial handshake"); err != nil {
		return err
	}

	// No challenge follows when the peer did not negotiate key exchange. The
	// initial cipher, if any, is now the active outbound cipher.
	h.client.securityMu.Lock()
	h.client.Ctx.HandshakeCompleted = true
	h.client.securityMu.Unlock()
	if err := h.sendModuleIdentifcation(); err != nil {
		return err
	}
	return nil
}

func (h *KeyExchangeHandler) setupHandshake(packet network2.Packet) error {
	handshakeKey, err := readKeyExchangeUInt64(&packet, "handshake key")
	if err != nil {
		return err
	}
	generator, err := readKeyExchangeUInt32(&packet, "generator")
	if err != nil {
		return err
	}
	prime, err := readKeyExchangeUInt32(&packet, "prime")
	if err != nil {
		return err
	}
	remotePublic, err := readKeyExchangeUInt32(&packet, "remote public key")
	if err != nil {
		return err
	}
	if err := requireNoKeyExchangeTrailingBytes(&packet, "initial key exchange"); err != nil {
		return err
	}
	h.client.securityMu.RLock()
	private := h.client.Ctx.Private
	h.client.securityMu.RUnlock()
	// Reject only the universally weak DH values here.  In particular, 1 (and
	// prime-1) lets an on-path attacker force a predictable shared secret.  Do
	// not add stricter subgroup assumptions without a capture-verified profile.
	if private == 0 || generator <= 1 || prime < 3 || remotePublic <= 1 || remotePublic >= prime-1 {
		// Do not include the local exponent in an error: Handle logs this error
		// for a malformed peer packet, and the exponent remains handshake-secret
		// material even when the handshake is rejected.
		return fmt.Errorf("%w: invalid generator, prime, remote public, or local private value", ErrInvalidKeyExchangeValues)
	}

	localPublic := security2.G_pow_X_mod_P(generator, private, prime)
	commonSecret := security2.G_pow_X_mod_P(remotePublic, private, prime)
	if localPublic <= 1 || localPublic >= prime-1 || commonSecret == 0 {
		return fmt.Errorf("%w: derived weak public value or zero common secret", ErrInvalidKeyExchangeValues)
	}
	key := security2.CalculateKey(commonSecret, remotePublic, localPublic)
	cipher, err := blowfish2.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create handshake cipher: %w", err)
	}
	localSignatureBytes := security2.CalculateChallenge(commonSecret, localPublic, remotePublic)
	cipher.EncryptRev(localSignatureBytes, localSignatureBytes)
	localSignature := utils2.ByteArrayToUint64(localSignatureBytes)

	response := network2.EmptyClientPacket()
	response.MessageID = 0x5000
	if err := response.WriteUInt32(localPublic); err != nil {
		return fmt.Errorf("write local public key: %w", err)
	}
	if err := response.WriteUInt64(localSignature); err != nil {
		return fmt.Errorf("write local signature: %w", err)
	}

	h.client.securityMu.Lock()
	h.client.Ctx.HandshakeKey = handshakeKey
	h.client.Ctx.Generator = generator
	h.client.Ctx.Prime = prime
	h.client.Ctx.RemotePublic = remotePublic
	h.client.Ctx.LocalPublic = localPublic
	h.client.Ctx.CommonSecret = commonSecret
	h.client.Ctx.LocalSignature = localSignature
	h.client.Cipher = cipher
	h.client.Ctx.LocalSetupCompleted = true
	h.client.securityMu.Unlock()
	if err := h.client.sendPacket(response); err != nil {
		return fmt.Errorf("send local key exchange response: %w", err)
	}
	return nil
}

func (h *KeyExchangeHandler) handleChallenge(packet network2.Packet) error {
	encodingOption, err := readKeyExchangeByte(&packet, "key challenge options")
	if err != nil {
		return err
	}
	if encodingOption != network2.EncodingKeyChallenge {
		return fmt.Errorf("%w: invalid challenge options 0x%02X", ErrUnexpectedKeyExchangePhase, encodingOption)
	}
	if err := h.validateServerSignature(&packet); err != nil {
		return err
	}
	if err := requireNoKeyExchangeTrailingBytes(&packet, "key challenge"); err != nil {
		return err
	}
	if err := h.deriveFinalKey(); err != nil {
		return err
	}

	ack := network2.EmptyClientPacket()
	ack.MessageID = 0x9000
	if err := h.client.sendPacket(ack); err != nil {
		return fmt.Errorf("send key challenge acknowledgement: %w", err)
	}
	h.client.securityMu.Lock()
	h.client.Ctx.HandshakeCompleted = true
	h.client.securityMu.Unlock()
	if err := h.sendModuleIdentifcation(); err != nil {
		return err
	}
	return nil
}

func (h *KeyExchangeHandler) validateServerSignature(packet *network2.Packet) error {
	receivedSignature, err := readKeyExchangeUInt64(packet, "server signature")
	if err != nil {
		return err
	}
	h.client.securityMu.RLock()
	cipher := h.client.Cipher
	commonSecret := h.client.Ctx.CommonSecret
	remotePublic := h.client.Ctx.RemotePublic
	localPublic := h.client.Ctx.LocalPublic
	h.client.securityMu.RUnlock()
	if cipher == nil {
		return fmt.Errorf("%w: missing handshake cipher", ErrUnexpectedKeyExchangePhase)
	}
	expectedSignature := security2.CalculateChallenge(commonSecret, remotePublic, localPublic)
	cipher.EncryptRev(expectedSignature, expectedSignature)
	if receivedSignature != utils2.ByteArrayToUint64(expectedSignature) {
		return ErrInvalidServerSignature
	}
	h.client.securityMu.Lock()
	h.client.Ctx.RemoteSignature = receivedSignature
	h.client.securityMu.Unlock()
	return nil
}

func (h *KeyExchangeHandler) deriveFinalKey() error {
	h.client.securityMu.RLock()
	handshakeKey := h.client.Ctx.HandshakeKey
	commonSecret := h.client.Ctx.CommonSecret
	h.client.securityMu.RUnlock()
	finalKey := security2.KeyTransformValue(utils2.Uint64ToByteArray(handshakeKey), commonSecret, 3)
	cipher, err := blowfish2.NewCipher(finalKey)
	if err != nil {
		return fmt.Errorf("create final blowfish cipher: %w", err)
	}
	h.client.securityMu.Lock()
	h.client.Cipher = cipher
	h.client.Ctx.HandshakeBlowfishKey = utils2.ByteArrayToUint64(finalKey)
	h.client.securityMu.Unlock()
	return nil
}

func (h *KeyExchangeHandler) sendModuleIdentifcation() error {
	packet := network2.EmptyClientPacket()
	packet.MessageID = 0x2001
	h.client.securityMu.RLock()
	packet.Encrypted = h.client.EncodingOptions.Encryption
	h.client.securityMu.RUnlock()
	if err := packet.WriteString(h.client.ModuleID); err != nil {
		return fmt.Errorf("write module id: %w", err)
	}
	if err := packet.WriteByte(0); err != nil {
		return fmt.Errorf("write module locale: %w", err)
	}
	if err := h.client.sendPacket(packet); err != nil {
		return fmt.Errorf("send module identification: %w", err)
	}
	return nil
}

func readKeyExchangeByte(packet *network2.Packet, field string) (byte, error) {
	if packet.Remaining() < 1 {
		return 0, fmt.Errorf("%w: missing %s", ErrMalformedKeyExchangePacket, field)
	}
	value, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", field, err)
	}
	return value, nil
}

func readKeyExchangeUInt32(packet *network2.Packet, field string) (uint32, error) {
	if packet.Remaining() < 4 {
		return 0, fmt.Errorf("%w: missing %s", ErrMalformedKeyExchangePacket, field)
	}
	value, err := packet.ReadUInt32()
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", field, err)
	}
	return value, nil
}

func readKeyExchangeUInt64(packet *network2.Packet, field string) (uint64, error) {
	if packet.Remaining() < 8 {
		return 0, fmt.Errorf("%w: missing %s", ErrMalformedKeyExchangePacket, field)
	}
	value, err := packet.ReadUInt64()
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", field, err)
	}
	return value, nil
}

func requireNoKeyExchangeTrailingBytes(packet *network2.Packet, phase string) error {
	if remaining := packet.Remaining(); remaining != 0 {
		return fmt.Errorf("%w: %s contains %d trailing bytes", ErrMalformedKeyExchangePacket, phase, remaining)
	}
	return nil
}
