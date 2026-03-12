package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func EnsureSelfSignedTLS(root, addr string) (string, string, error) {
	tlsDir := filepath.Join(root, ".deck", "tls")
	certPath := filepath.Join(tlsDir, "server.crt")
	keyPath := filepath.Join(tlsDir, "server.key")

	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)
	if certExists && keyExists {
		return certPath, keyPath, nil
	}
	if certExists != keyExists {
		return "", "", fmt.Errorf("E_SERVER_TLS_PARTIAL_FILES: cert/key must both exist or both be absent")
	}

	if err := os.MkdirAll(tlsDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create tls directory: %w", err)
	}

	hosts := tlsHosts(addr)
	if err := generateSelfSignedCertificate(certPath, keyPath, hosts); err != nil {
		return "", "", err
	}

	return certPath, keyPath, nil
}

func generateSelfSignedCertificate(certPath, keyPath string, hosts []string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate private key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"deck self-signed"},
			CommonName:   "deck-server",
		},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
			continue
		}
		tpl.DNSNames = append(tpl.DNSNames, h)
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	defer func() { _ = certOut.Close() }()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return fmt.Errorf("encode cert pem: %w", err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	defer func() { _ = keyOut.Close() }()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return fmt.Errorf("encode key pem: %w", err)
	}

	return nil
}

func tlsHosts(addr string) []string {
	host := ""
	if parsedHost, _, err := net.SplitHostPort(addr); err == nil {
		host = strings.TrimSpace(parsedHost)
	}

	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
	}

	add("localhost")
	add("127.0.0.1")

	if host != "" && host != "0.0.0.0" && host != "::" {
		add(host)
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}
