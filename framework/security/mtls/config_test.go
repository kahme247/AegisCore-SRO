package mtls

import "testing"

func TestTLSConfigurationsRequireCompleteMaterial(t *testing.T) {
	if _, err := LoadServerConfig(Files{}); err == nil {
		t.Fatal("LoadServerConfig accepted missing material")
	}
	if _, err := LoadClientConfig(Files{CertFile: "cert", KeyFile: "key", CAFile: "ca"}); err == nil {
		t.Fatal("LoadClientConfig accepted missing server name")
	}
}
