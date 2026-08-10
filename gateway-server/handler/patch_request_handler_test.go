package handler

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/gateway-server/config"
)

func gatewayTestProtocolProfile() config.ProtocolProfile {
	return config.ProtocolProfile{
		Name:       config.VSRO188Profile,
		ContentID:  122,
		ModuleName: "SR_Client",
		Version:    188,
	}
}

func makePatchRequest(t *testing.T, contentID byte, module string, version uint32, trailing []byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.PatchRequest
	if err := packet.WriteByte(contentID); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteString(module); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteUInt32(version); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteBytes(trailing); err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestParsePatchRequestAndValidatePinnedProfile(t *testing.T) {
	profile := gatewayTestProtocolProfile()
	packet := makePatchRequest(t, profile.ContentID, profile.ModuleName, profile.Version, nil)
	request, err := parsePatchRequest(packet)
	if err != nil {
		t.Fatalf("parsePatchRequest() error = %v", err)
	}
	if err := request.Validate(profile); err != nil {
		t.Fatalf("PatchRequest.Validate() error = %v", err)
	}
}

func TestPatchRequestRejectsProfileMismatchAndTrailingData(t *testing.T) {
	profile := gatewayTestProtocolProfile()
	tests := []struct {
		name     string
		packet   network.Packet
		validate bool
	}{
		{name: "content ID", packet: makePatchRequest(t, profile.ContentID+1, profile.ModuleName, profile.Version, nil), validate: true},
		{name: "module", packet: makePatchRequest(t, profile.ContentID, "OtherModule", profile.Version, nil), validate: true},
		{name: "version", packet: makePatchRequest(t, profile.ContentID, profile.ModuleName, profile.Version+1, nil), validate: true},
		{name: "trailing", packet: makePatchRequest(t, profile.ContentID, profile.ModuleName, profile.Version, []byte{1})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := parsePatchRequest(test.packet)
			if test.validate {
				if err != nil {
					t.Fatalf("parsePatchRequest() error = %v", err)
				}
				if err := request.Validate(profile); err == nil {
					t.Fatal("PatchRequest.Validate() error = nil, want mismatch rejection")
				}
				return
			}
			if err == nil {
				t.Fatal("parsePatchRequest() error = nil, want trailing data rejection")
			}
		})
	}
}

func TestPatchSuccessResponseIsOneByteMassivePayload(t *testing.T) {
	response, err := buildPatchSuccessResponse()
	if err != nil {
		t.Fatalf("buildPatchSuccessResponse() error = %v", err)
	}
	if response.MessageID != opcode.PatchResponse {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", response.MessageID, opcode.PatchResponse)
	}
	if got, want := response.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("response payload = % X, want % X", got, want)
	}

	frames, err := network.NewDefaultMassiveCodec().EncodeMassive(response)
	if err != nil {
		t.Fatalf("EncodeMassive() error = %v", err)
	}
	if got, want := len(frames), 2; got != want {
		t.Fatalf("massive frame count = %d, want %d", got, want)
	}
	if frames[0].MessageID != network.MassivePacketID || frames[1].MessageID != network.MassivePacketID {
		t.Fatalf("response frames are not massive wrappers: 0x%04X, 0x%04X", frames[0].MessageID, frames[1].MessageID)
	}

	assembler := network.NewMassiveAssembler(network.NewDefaultMassiveCodec())
	var decoded network.Packet
	for _, frame := range frames {
		packet, done, err := assembler.ProcessFrame(frame)
		if err != nil {
			t.Fatalf("ProcessFrame() error = %v", err)
		}
		if done {
			decoded = packet
		}
	}
	if decoded.MessageID != opcode.PatchResponse {
		t.Fatalf("reassembled opcode = 0x%04X, want 0x%04X", decoded.MessageID, opcode.PatchResponse)
	}
	if got, want := decoded.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("reassembled payload = % X, want % X", got, want)
	}
}

func TestBuildPatchResponseFollowsBinaryConfirmedVersionBranches(t *testing.T) {
	profile := gatewayTestProtocolProfile()
	tests := []struct {
		name    string
		request PatchRequest
		payload []byte
	}{
		{name: "current", request: PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: profile.Version}, payload: []byte{1}},
		{name: "unknown content", request: PatchRequest{ContentID: profile.ContentID + 1, ModuleName: profile.ModuleName, Version: profile.Version}, payload: []byte{2, 1}},
		{name: "unknown module", request: PatchRequest{ContentID: profile.ContentID, ModuleName: "OtherModule", Version: profile.Version}, payload: []byte{2, 1}},
		{name: "higher than known", request: PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: profile.Version + 1}, payload: []byte{2, 1}},
		{name: "older without patch server", request: PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: profile.Version - 1}, payload: []byte{2, 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, _, err := buildPatchResponse(test.request, profile)
			if err != nil {
				t.Fatalf("buildPatchResponse() error = %v", err)
			}
			if response.MessageID != opcode.PatchResponse {
				t.Fatalf("opcode = 0x%04X, want 0x%04X", response.MessageID, opcode.PatchResponse)
			}
			if got := response.Payload(); !bytes.Equal(got, test.payload) {
				t.Fatalf("payload = % X, want % X", got, test.payload)
			}
		})
	}
}

func TestBuildPatchResponseAcceptsEachConfiguredProfile(t *testing.T) {
	profiles := []config.ProtocolProfile{
		{Name: config.VSRO188Profile, ContentID: 22, ModuleName: "SR_Client", Version: 188},
		{Name: config.VSRO274Profile, ContentID: 22, ModuleName: "SR_Client", Version: 274},
	}
	for _, profile := range profiles {
		response, outcome, err := buildPatchResponseForProfiles(PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: profile.Version}, profiles)
		if err != nil {
			t.Fatalf("buildPatchResponseForProfiles(%d): %v", profile.Version, err)
		}
		if outcome != "result=1" || !bytes.Equal(response.Payload(), []byte{1}) {
			t.Fatalf("profile %d response = %q % X, want success", profile.Version, outcome, response.Payload())
		}
	}
}
