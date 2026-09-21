package handler

import (
	"errors"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// sendFailureWithCause preserves the original cause ahead of a failed completion send.
func sendFailureWithCause(sender PacketSender, cause error) error {
	if sendErr := sendCompletion(sender, false); sendErr != nil {
		return errors.Join(cause, sendErr)
	}
	return nil
}

func sendCompletion(sender PacketSender, success bool) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.DownloadFileCompleted
	result := byte(0)
	if success {
		result = 1
	}
	if err := packet.WriteByte(result); err != nil {
		return err
	}
	return sender.Send(packet)
}
