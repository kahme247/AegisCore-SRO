package client

import (
	"fmt"

	network2 "github.com/Harvi8/AegisCore/framework/network"
	log "github.com/sirupsen/logrus"
)

type ModuleIdentificationHandler struct {
	client *Client
}

func (h *ModuleIdentificationHandler) Handle(packet network2.Packet) {
	if h == nil || h.client == nil {
		return
	}
	moduleName, err := readModuleIdentificationString(&packet)
	if err != nil {
		h.client.failActiveGeneration(fmt.Errorf("read module identification name: %w", err))
		return
	}
	if packet.Remaining() < 1 {
		h.client.failActiveGeneration(fmt.Errorf("read module identification locality: missing byte"))
		return
	}
	isLocalModule, err := packet.ReadByte()
	if err != nil {
		h.client.failActiveGeneration(fmt.Errorf("read module identification locality: %w", err))
		return
	}
	if remaining := packet.Remaining(); remaining != 0 {
		h.client.failActiveGeneration(fmt.Errorf("module identification contains %d trailing bytes", remaining))
		return
	}

	log.Debugf("Module identified %v. Local: %v\n", moduleName, isLocalModule)
	h.client.markConnectionEstablished()
}

func readModuleIdentificationString(packet *network2.Packet) (string, error) {
	if packet.Remaining() < 2 {
		return "", fmt.Errorf("missing length")
	}
	length, err := packet.ReadUInt16()
	if err != nil {
		return "", err
	}
	if packet.Remaining() < int(length) {
		return "", fmt.Errorf("declared length %d exceeds remaining payload", length)
	}
	value, err := packet.ReadBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(value), nil
}
