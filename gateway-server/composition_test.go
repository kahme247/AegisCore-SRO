package main

import (
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
)

func TestNewGatewayServerWithDependenciesRequiresPublicServer(t *testing.T) {
	if _, err := NewGatewayServerWithDependencies(GatewayDependencies{}); err == nil {
		t.Fatal("NewGatewayServerWithDependencies() error = nil, want missing-server rejection")
	}
}

func TestNewGatewayServerWithDependenciesOwnsPacketLifetime(t *testing.T) {
	engine := server.NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	gateway, err := NewGatewayServerWithDependencies(GatewayDependencies{Server: &engine})
	if err != nil {
		t.Fatalf("NewGatewayServerWithDependencies() error = %v", err)
	}
	if gateway.Server != &engine || gateway.packetContext == nil || gateway.cancelPacketHandling == nil {
		t.Fatal("composed Gateway did not retain server and packet lifetime")
	}
	gateway.cancelPacketHandling()
	if gateway.packetContext.Err() == nil {
		t.Fatal("composed Gateway packet context was not cancelled")
	}
}
