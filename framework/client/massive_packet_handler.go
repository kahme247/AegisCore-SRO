package client

import (
	network2 "github.com/ferdoran/go-sro/framework/network"
	log "github.com/sirupsen/logrus"
)

// MassivePacketHandler remains available for callers that registered it
// directly. Client now intercepts 0x600D before its generic handler map so a
// completed logical packet follows the normal target-opcode dispatch path.
type MassivePacketHandler struct {
	client    *Client
	assembler *network2.MassiveAssembler
}

func NewMassivePacketHandler(client *Client) *MassivePacketHandler {
	return &MassivePacketHandler{
		client:    client,
		assembler: network2.NewMassiveAssembler(network2.NewDefaultMassiveCodec()),
	}
}

func (h *MassivePacketHandler) Handle(packet network2.Packet) {
	complete, done, err := h.assembler.Accept(packet)
	if err != nil {
		log.Errorf("rejecting malformed massive packet: %v", err)
		if h.client != nil {
			h.client.failActiveGeneration(err)
		}
		return
	}
	if done && h.client != nil {
		h.client.dispatchIncomingPacket(complete)
	}
}
