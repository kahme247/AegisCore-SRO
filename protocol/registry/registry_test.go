package registry

import "testing"

func TestRegistryUsesCompletePacketIdentity(t *testing.T) {
	first := testEntry(Key{Service: ServiceAgent, Direction: ClientToServer, Phase: AgentLobby, Opcode: 0x7F00}, "AgentPingRequest")
	second := testEntry(Key{Service: ServiceAgent, Direction: ClientToServer, Phase: AgentCharacterLoading, Opcode: 0x7F00}, "AgentPingRequest")
	registry, err := New([]Entry{first, second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := registry.Opcodes(ServiceAgent, ClientToServer, AgentLobby); len(got) != 1 || got[0] != 0x7F00 {
		t.Fatalf("lobby opcodes = %v, want [0x7F00]", got)
	}
	if _, exists := registry.Lookup(second.Key); !exists {
		t.Fatal("loading-phase identity was not retained")
	}
}

func TestRegistryRejectsDuplicateIdentity(t *testing.T) {
	entry := testEntry(Key{Service: ServiceGateway, Direction: ClientToServer, Phase: GatewayPreLogin, Opcode: 0x6100}, "PatchRequest")
	if _, err := New([]Entry{entry, entry}); err == nil {
		t.Fatal("New() accepted a duplicate complete identity")
	}
}

func TestRegistryRequiresReciprocalExistingPair(t *testing.T) {
	requestKey := Key{Service: ServiceGateway, Direction: ClientToServer, Phase: GatewayPreLogin, Opcode: 0x6100}
	responseKey := Key{Service: ServiceGateway, Direction: ServerToClient, Phase: GatewayPreLogin, Opcode: 0xA100}
	request := testEntry(requestKey, "PatchRequest")
	request.Pair = &responseKey
	if _, err := New([]Entry{request}); err == nil {
		t.Fatal("New() accepted a missing pair")
	}

	response := testEntry(responseKey, "PatchResponse")
	response.Pair = &requestKey
	if _, err := New([]Entry{request, response}); err != nil {
		t.Fatalf("New() rejected reciprocal pair: %v", err)
	}
}

func TestRegistryReturnsDefensiveEvidenceCopies(t *testing.T) {
	entry := testEntry(Key{Service: ServiceAgent, Direction: ClientToServer, Phase: AgentAuthentication, Opcode: 0x6103}, "AuthRequest")
	registry, err := New([]Entry{entry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, _ := registry.Lookup(entry.Key)
	got.Evidence[0].Source = "mutated"
	again, _ := registry.Lookup(entry.Key)
	if again.Evidence[0].Source == "mutated" {
		t.Fatal("Lookup exposed mutable registry evidence")
	}
}

func TestRegistryReturnsDefensivePairCopies(t *testing.T) {
	requestKey := Key{Service: ServiceGateway, Direction: ClientToServer, Phase: GatewayPreLogin, Opcode: 0x6100}
	responseKey := Key{Service: ServiceGateway, Direction: ServerToClient, Phase: GatewayPreLogin, Opcode: 0xA100}
	request := testEntry(requestKey, "PatchRequest")
	request.Pair = &responseKey
	response := testEntry(responseKey, "PatchResponse")
	response.Pair = &requestKey
	registry, err := New([]Entry{request, response})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, _ := registry.Lookup(requestKey)
	got.Pair.Opcode = 0xFFFF
	again, _ := registry.Lookup(requestKey)
	if again.Pair.Opcode != responseKey.Opcode {
		t.Fatal("Lookup exposed mutable registry pair")
	}
}

func TestRegistryRejectsUnknownEnumValues(t *testing.T) {
	entry := testEntry(Key{Service: Service("other"), Direction: ClientToServer, Phase: AgentLobby, Opcode: 0x7001}, "Unknown")
	if _, err := New([]Entry{entry}); err == nil {
		t.Fatal("New() accepted an unknown service")
	}
}

func testEntry(key Key, name string) Entry {
	return Entry{
		Key:           key,
		CanonicalName: name,
		Encryption:    EncryptionRequired,
		Massive:       MassiveForbidden,
		RateLimit:     "test",
		Evidence: []Evidence{{
			Kind:       EvidenceImplementation,
			Source:     "test",
			Locator:    name,
			Confidence: "test-only",
		}},
		Status: StatusImplemented,
	}
}
