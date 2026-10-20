package client

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatePrometheusClientUsesHostnameForServerName(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	originalK8sCAFilePath := K8sPodCAFilePath
	K8sPodCAFilePath = caFile
	t.Cleanup(func() {
		K8sPodCAFilePath = originalK8sCAFilePath
	})

	_, transport, err := CreatePrometheusClient(server.URL, "token")
	if err != nil {
		t.Fatalf("CreatePrometheusClient() error = %v", err)
	}
	if strings.Contains(transport.TLSClientConfig.ServerName, ":") {
		t.Errorf("TLS ServerName = %q, want hostname without port", transport.TLSClientConfig.ServerName)
	}
}
