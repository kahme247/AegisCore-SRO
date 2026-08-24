package vsro188

import "github.com/Harvi8/AegisCore/framework/network"

// Decoder and Encoder are the typed packet-codec boundaries used once a
// vSRO 1.188 layout is stable enough to leave an application handler.
type Decoder[T any] interface {
	Decode(network.Packet) (T, error)
}

type Encoder[T any] interface {
	Encode(T) (network.Packet, error)
}
