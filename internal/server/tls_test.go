package server

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSelfSignedTLS(t *testing.T) {
	t.Run("generates and reuses cert/key", func(t *testing.T) {
		root := t.TempDir()
		certPath, keyPath, err := EnsureSelfSignedTLS(root, ":8443")
		if err != nil {
			t.Fatalf("EnsureSelfSignedTLS: %v", err)
		}

		if _, err := os.Stat(certPath); err != nil {
			t.Fatalf("cert file missing: %v", err)
		}
		if _, err := os.Stat(keyPath); err != nil {
			t.Fatalf("key file missing: %v", err)
		}

		raw, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatalf("read cert: %v", err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			t.Fatalf("failed to decode cert pem")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse cert: %v", err)
		}

		if !containsString(cert.DNSNames, "localhost") {
			t.Fatalf("expected localhost dns name in cert")
		}

		certPath2, keyPath2, err := EnsureSelfSignedTLS(root, ":8443")
		if err != nil {
			t.Fatalf("EnsureSelfSignedTLS second call: %v", err)
		}
		if certPath2 != certPath || keyPath2 != keyPath {
			t.Fatalf("expected same cert/key paths on reuse")
		}
	})

	t.Run("fails on partial files", func(t *testing.T) {
		root := t.TempDir()
		tlsDir := filepath.Join(root, ".deck", "tls")
		if err := os.MkdirAll(tlsDir, 0o755); err != nil {
			t.Fatalf("mkdir tls dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tlsDir, "server.crt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write partial cert: %v", err)
		}

		_, _, err := EnsureSelfSignedTLS(root, ":8443")
		if err == nil {
			t.Fatalf("expected partial file error")
		}
		if !strings.Contains(err.Error(), "E_SERVER_TLS_PARTIAL_FILES") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTLSSelfSigned(t *testing.T) {
	root := t.TempDir()
	certPath, keyPath, err := EnsureSelfSignedTLS(root, ":8443")
	if err != nil {
		t.Fatalf("EnsureSelfSignedTLS: %v", err)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file missing: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file missing: %v", err)
	}
}

func containsString(items []string, target string) bool {
	for _, it := range items {
		if it == target {
			return true
		}
	}
	return false
}
