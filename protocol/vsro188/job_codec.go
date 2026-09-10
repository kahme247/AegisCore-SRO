package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	JobTrader byte = 1
	JobThief  byte = 2
	JobHunter byte = 3
	JobNone   byte = 4
)

type JobJoinRequest struct {
	NPCUID  uint32
	JobType byte
}

type JobLeaveRequest struct{ EnrollmentFee uint32 }
type JobAliasRequest struct {
	EnrollmentFee uint32
	Action        byte
	Alias         string
}
type JobRankingRequest struct {
	EnrollmentFee uint32
	JobType       byte
	Kind          byte
}
type JobOutcomeRequest struct {
	EnrollmentFee uint32
	Action        byte
}
type JobPreviousInfoRequest struct{ EnrollmentFee uint32 }

type JobState struct {
	Type       byte
	Level      byte
	Experience uint32
}

type JobRankingEntry struct {
	Rank       byte
	Alias      string
	Level      byte
	Experience uint32
	// CompatibilityByte is present only in kind-0 rows. The pinned v188
	// client consumes and discards it without storing or branching on it.
	CompatibilityByte byte
}

type JobPreviousInfo struct {
	Current              JobState
	PreviousType         byte
	PreviousExperience   uint32
	PreviousContribution uint32
}

type JobJoinRequestCodec struct{}
type JobLeaveRequestCodec struct{}
type JobAliasRequestCodec struct{}
type JobRankingRequestCodec struct{}
type JobOutcomeRequestCodec struct{}
type JobPreviousInfoRequestCodec struct{}
type JobUpdateCodec struct{}
type JobJoinResponseCodec struct{}
type JobAliasResponseCodec struct{}
type JobRankingResponseCodec struct{}
type JobOutcomeResponseCodec struct{}
type JobPreviousInfoResponseCodec struct{}

func (JobJoinRequestCodec) Decode(packet network.Packet) (JobJoinRequest, error) {
	npcUID, jobType, err := decodeJobFeeAndByte(packet, "job join")
	if err != nil {
		return JobJoinRequest{}, err
	}
	if !activeJobType(jobType) {
		return JobJoinRequest{}, fmt.Errorf("job join type %d is invalid", jobType)
	}
	return JobJoinRequest{NPCUID: npcUID, JobType: jobType}, nil
}

func (JobLeaveRequestCodec) Decode(packet network.Packet) (JobLeaveRequest, error) {
	fee, err := decodeJobFee(packet, "job leave")
	return JobLeaveRequest{EnrollmentFee: fee}, err
}

func (JobAliasRequestCodec) Decode(packet network.Packet) (JobAliasRequest, error) {
	if packet.Remaining() < 7 {
		return JobAliasRequest{}, fmt.Errorf("job alias payload length %d is too short", packet.Remaining())
	}
	fee, err := packet.ReadUInt32()
	if err != nil {
		return JobAliasRequest{}, err
	}
	action, err := packet.ReadByte()
	if err != nil || action > 1 {
		return JobAliasRequest{}, fmt.Errorf("job alias action is invalid")
	}
	alias, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return JobAliasRequest{}, fmt.Errorf("job alias payload is malformed")
	}
	return JobAliasRequest{EnrollmentFee: fee, Action: action, Alias: alias}, nil
}

func (JobRankingRequestCodec) Decode(packet network.Packet) (JobRankingRequest, error) {
	if packet.Remaining() != 6 {
		return JobRankingRequest{}, fmt.Errorf("job ranking payload length %d, want 6", packet.Remaining())
	}
	fee, _ := packet.ReadUInt32()
	jobType, _ := packet.ReadByte()
	kind, _ := packet.ReadByte()
	if !activeJobType(jobType) || kind > 1 {
		return JobRankingRequest{}, fmt.Errorf("job ranking discriminator is invalid")
	}
	return JobRankingRequest{EnrollmentFee: fee, JobType: jobType, Kind: kind}, nil
}

func (JobOutcomeRequestCodec) Decode(packet network.Packet) (JobOutcomeRequest, error) {
	fee, action, err := decodeJobFeeAndByte(packet, "job outcome")
	return JobOutcomeRequest{EnrollmentFee: fee, Action: action}, err
}

func (JobPreviousInfoRequestCodec) Decode(packet network.Packet) (JobPreviousInfoRequest, error) {
	fee, err := decodeJobFee(packet, "previous job info")
	return JobPreviousInfoRequest{EnrollmentFee: fee}, err
}

func (JobUpdateCodec) Encode(state JobState) (network.Packet, error) {
	if err := validateJobState(state); err != nil {
		return network.Packet{}, err
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.JobUpdateExperience
	packet.WriteByte(state.Type)
	packet.WriteByte(state.Level)
	packet.WriteUInt32(state.Experience)
	return packet, packet.WriteError()
}

func (JobJoinResponseCodec) AppendSuccess(packet *network.Packet, state JobState) error {
	if err := validateJobState(state); err != nil {
		return err
	}
	packet.WriteByte(state.Type)
	packet.WriteByte(state.Level)
	packet.WriteUInt32(state.Experience)
	return packet.WriteError()
}

func (JobAliasResponseCodec) AppendSuccess(packet *network.Packet, action byte, alias string) error {
	if action > 1 {
		return fmt.Errorf("job alias action %d is invalid", action)
	}
	packet.WriteByte(action)
	packet.WriteString(alias)
	return packet.WriteError()
}

func (JobAliasResponseCodec) AppendFailure(packet *network.Packet, action byte, alias string) error {
	if action > 1 {
		return fmt.Errorf("job alias action %d is invalid", action)
	}
	packet.WriteByte(action)
	packet.WriteString(alias)
	return packet.WriteError()
}

func (JobRankingResponseCodec) AppendSuccess(packet *network.Packet, request JobRankingRequest, entries []JobRankingEntry) error {
	if len(entries) > math.MaxUint8 {
		return fmt.Errorf("job ranking has %d entries", len(entries))
	}
	packet.WriteByte(request.JobType)
	packet.WriteByte(request.Kind)
	packet.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		packet.WriteByte(entry.Rank)
		packet.WriteString(entry.Alias)
		packet.WriteByte(entry.Level)
		packet.WriteUInt32(entry.Experience)
		if request.Kind == 0 {
			packet.WriteByte(entry.CompatibilityByte)
		}
	}
	return packet.WriteError()
}

func (JobOutcomeResponseCodec) AppendSuccess(packet *network.Packet, action byte, amount uint64) error {
	if amount > math.MaxUint32 {
		return fmt.Errorf("job outcome amount %d exceeds uint32", amount)
	}
	packet.WriteByte(action)
	packet.WriteUInt32(uint32(amount))
	return packet.WriteError()
}

func (JobOutcomeResponseCodec) AppendFailure(packet *network.Packet, action byte) error {
	packet.WriteByte(action)
	return packet.WriteError()
}

func (JobPreviousInfoResponseCodec) AppendSuccess(packet *network.Packet, info JobPreviousInfo) error {
	if err := validateCurrentOrInactiveJobState(info.Current); err != nil {
		return err
	}
	packet.WriteByte(info.Current.Type)
	packet.WriteUInt32(info.Current.Experience)
	packet.WriteByte(info.Current.Level)
	packet.WriteUInt32(info.PreviousExperience)
	packet.WriteByte(info.PreviousType)
	packet.WriteUInt32(info.PreviousContribution)
	return packet.WriteError()
}

func validateCurrentOrInactiveJobState(state JobState) error {
	if state.Type == JobNone && state.Level == 0 && state.Experience == 0 {
		return nil
	}
	return validateJobState(state)
}

func decodeJobFee(packet network.Packet, name string) (uint32, error) {
	if packet.Remaining() != 4 {
		return 0, fmt.Errorf("%s payload length %d, want 4", name, packet.Remaining())
	}
	fee, err := packet.ReadUInt32()
	if err != nil {
		return 0, fmt.Errorf("read %s fee: %w", name, err)
	}
	return fee, nil
}

func decodeJobFeeAndByte(packet network.Packet, name string) (uint32, byte, error) {
	if packet.Remaining() != 5 {
		return 0, 0, fmt.Errorf("%s payload length %d, want 5", name, packet.Remaining())
	}
	fee, _ := packet.ReadUInt32()
	value, _ := packet.ReadByte()
	return fee, value, nil
}

func activeJobType(jobType byte) bool { return jobType >= JobTrader && jobType <= JobHunter }

func validateJobState(state JobState) error {
	if !activeJobType(state.Type) {
		return fmt.Errorf("active job type %d is invalid", state.Type)
	}
	if state.Level == 0 {
		return fmt.Errorf("job level is zero")
	}
	return nil
}
