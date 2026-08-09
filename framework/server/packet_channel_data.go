package server

import "github.com/Harvi8/AegisCore/framework/network"

type PacketChannelData struct {
	*Session
	network.Packet
}
