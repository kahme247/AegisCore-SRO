package client

import "github.com/Harvi8/AegisCore/framework/network"

type PacketHandler interface {
	Handle(packet network.Packet)
}
