package handler

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	patchcatalog "github.com/Harvi8/AegisCore/patching/catalog"
)

func gatewayTestProtocolProfile() config.ProtocolProfile {
	return config.ProtocolProfile{
		Name:       config.VSRO188Profile,
		ContentID:  122,
		ModuleName: "SR_Client",
		Version:    188,
	}
}

func TestBuildPatchAvailableResponseMatchesLauncherLayout(t *testing.T) {
	plan := patchcatalog.PatchPlan{
		TargetVersion: 189,
		Server:        patchcatalog.DownloadServer{Host: "patch.example.test", Port: 15880},
		Files: []patchcatalog.File{{
			ID: 17, Filename: "itemdata_45000.txt", Path: `media\server_dep\silkroad\textdata`,
			PreparedSize: 1234, ImportToPK2: true,
		}},
	}
	response, err := buildPatchAvailableResponse(plan)
	if err != nil {
		t.Fatalf("buildPatchAvailableResponse() error = %v", err)
	}
	result, _ := response.ReadByte()
	reason, _ := response.ReadByte()
	host, _ := response.ReadString()
	port, _ := response.ReadUInt16()
	version, _ := response.ReadUInt32()
	hasFile, _ := response.ReadByte()
	fileID, _ := response.ReadUInt32()
	filename, _ := response.ReadString()
	path, _ := response.ReadString()
	size, _ := response.ReadUInt32()
	packing, _ := response.ReadByte()
	terminator, _ := response.ReadByte()
	if response.Remaining() != 0 || result != 2 || reason != 2 || host != plan.Server.Host || port != 15880 || version != 189 ||
		hasFile != 1 || fileID != 17 || filename != plan.Files[0].Filename || path != plan.Files[0].Path || size != 1234 || packing != 1 || terminator != 0 {
		t.Fatalf("unexpected patch response fields: result=%d reason=%d host=%q port=%d version=%d file=%d/%d %q %q size=%d packing=%d end=%d remaining=%d",
			result, reason, host, port, version, hasFile, fileID, filename, path, size, packing, terminator, response.Remaining())
	}
}

func TestBuildPatchAvailableResponsePreservesMultiReleaseFileOrder(t *testing.T) {
	plan := patchcatalog.PatchPlan{
		TargetVersion: 190,
		Server:        patchcatalog.DownloadServer{Host: "patch.example.test", Port: 15880},
		Files: []patchcatalog.File{
			{ID: 18901, Filename: "from-189.txt", Path: "media", PreparedSize: 11, ImportToPK2: true},
			{ID: 19001, Filename: "from-190.txt", Path: "media", PreparedSize: 22, ImportToPK2: true},
		},
	}
	response, err := buildPatchAvailableResponse(plan)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = response.ReadByte()
	_, _ = response.ReadByte()
	_, _ = response.ReadString()
	_, _ = response.ReadUInt16()
	version, _ := response.ReadUInt32()
	if version != 190 {
		t.Fatalf("target version = %d", version)
	}
	for index, want := range plan.Files {
		hasFile, _ := response.ReadByte()
		id, _ := response.ReadUInt32()
		name, _ := response.ReadString()
		path, _ := response.ReadString()
		size, _ := response.ReadUInt32()
		packing, _ := response.ReadByte()
		if hasFile != 1 || id != want.ID || name != want.Filename || path != want.Path || uint64(size) != want.PreparedSize || packing != 1 {
			t.Fatalf("file %d decoded as marker=%d id=%d name=%q path=%q size=%d packing=%d", index, hasFile, id, name, path, size, packing)
		}
	}
	terminator, _ := response.ReadByte()
	if terminator != 0 || response.Remaining() != 0 {
		t.Fatalf("terminator=%d remaining=%d", terminator, response.Remaining())
	}
}

func TestCatalogPatchResponseUsesPublishedPlanAndFailureReasons(t *testing.T) {
	profile := gatewayTestProtocolProfile()
	handler := NewPatchRequestHandler(func() (config.ProtocolProfile, error) { return profile, nil })
	handler.Channel = patchcatalog.StatusPublished
	handler.Catalog = patchcatalog.ReaderFunc(func(_ context.Context, _ uint8, _ string, requested uint32, channel string) (patchcatalog.PatchPlan, error) {
		if channel != patchcatalog.StatusPublished {
			t.Fatalf("channel = %q", channel)
		}
		switch requested {
		case 188:
			return patchcatalog.PatchPlan{TargetVersion: 189, Server: patchcatalog.DownloadServer{Host: "127.0.0.1", Port: 15880}, Files: []patchcatalog.File{{ID: 1, Filename: "x", PreparedSize: 9}}}, nil
		case 187:
			return patchcatalog.PatchPlan{}, patchcatalog.ErrClientTooOld
		default:
			return patchcatalog.PatchPlan{}, patchcatalog.ErrClientNewer
		}
	})
	response, _, selected, err := handler.buildResponse(context.Background(), PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: 188}, []config.ProtocolProfile{profile})
	if err != nil || selected == nil || !bytes.Equal(response.Payload()[:2], []byte{2, 2}) {
		t.Fatalf("patch plan response = % X selected=%v err=%v", response.Payload(), selected, err)
	}
	response, _, _, err = handler.buildResponse(context.Background(), PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: 187}, []config.ProtocolProfile{profile})
	if err != nil || !bytes.Equal(response.Payload(), []byte{2, 5}) {
		t.Fatalf("too-old response = % X err=%v", response.Payload(), err)
	}
	handler.Catalog = patchcatalog.ReaderFunc(func(context.Context, uint8, string, uint32, string) (patchcatalog.PatchPlan, error) {
		return patchcatalog.PatchPlan{}, patchcatalog.ErrNoDownloadHost
	})
	response, _, _, err = handler.buildResponse(context.Background(), PatchRequest{ContentID: profile.ContentID, ModuleName: profile.ModuleName, Version: 188}, []config.ProtocolProfile{profile})
	if err != nil || !bytes.Equal(response.Payload(), []byte{2, 4}) {
		t.Fatalf("unavailable response = % X err=%v", response.Payload(), err)
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
