package main

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestNewAgentServerWithDependenciesRequiresPublicServer(t *testing.T) {
	if _, err := NewAgentServerWithDependencies(AgentDependencies{}); err == nil {
		t.Fatal("NewAgentServerWithDependencies() error = nil, want missing-server rejection")
	}
}

func TestNewAgentServerWithDependenciesOwnsPacketLifetime(t *testing.T) {
	engine := server.NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	agent, err := NewAgentServerWithDependencies(AgentDependencies{Server: &engine})
	if err != nil {
		t.Fatalf("NewAgentServerWithDependencies() error = %v", err)
	}
	if agent.Server != &engine || agent.packetContext == nil || agent.cancelPacketHandling == nil {
		t.Fatal("composed Agent did not retain server and packet lifetime")
	}
	agent.cancelPacketHandling()
	if agent.packetContext.Err() == nil {
		t.Fatal("composed Agent packet context was not cancelled")
	}
}
