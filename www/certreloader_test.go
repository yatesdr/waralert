package www

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// genTestCert creates a self-signed cert+key pair that expires at the given time.
func genTestCert(t *testing.T, dir string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     notAfter,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatal(err)
	}

	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestCertReloader_HotReload(t *testing.T) {
	dir := t.TempDir()

	// Step 1: Generate a cert that "expires tomorrow" (simulating near-expiry).
	expiry1 := time.Now().Add(24 * time.Hour)
	certFile, keyFile := genTestCert(t, dir, expiry1)

	reloader, err := NewCertReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial NotAfter.
	if got := reloader.NotAfter(); !got.Equal(expiry1.Truncate(time.Second)) && got.Sub(expiry1).Abs() > time.Second {
		t.Fatalf("initial NotAfter: got %v, want ~%v", got, expiry1)
	}

	// Step 2: Start a real TLS server using the reloader.
	tlsCfg := &tls.Config{GetCertificate: reloader.GetCertificate}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})}
	go srv.Serve(ln)
	defer srv.Close()

	clientCfg := &tls.Config{InsecureSkipVerify: true}

	// Step 3: Connect and read the cert expiry from the TLS handshake.
	peerExpiry1 := getPeerCertExpiry(t, ln.Addr().String(), clientCfg)
	if peerExpiry1.Sub(expiry1).Abs() > time.Second {
		t.Fatalf("handshake 1: peer cert expires %v, want ~%v", peerExpiry1, expiry1)
	}
	t.Logf("handshake 1: cert expires %s (expected)", peerExpiry1.Format(time.RFC3339))

	// Step 4: Write a NEW cert to the same files (expires in 365 days).
	expiry2 := time.Now().Add(365 * 24 * time.Hour)
	genTestCert(t, dir, expiry2)

	// Step 5: Hot-reload.
	if err := reloader.Reload(); err != nil {
		t.Fatal(err)
	}

	// Verify NotAfter updated.
	if got := reloader.NotAfter(); got.Sub(expiry2).Abs() > time.Second {
		t.Fatalf("after reload NotAfter: got %v, want ~%v", got, expiry2)
	}

	// Step 6: Connect again — the server should serve the NEW cert.
	peerExpiry2 := getPeerCertExpiry(t, ln.Addr().String(), clientCfg)
	if peerExpiry2.Sub(expiry2).Abs() > time.Second {
		t.Fatalf("handshake 2: peer cert expires %v, want ~%v", peerExpiry2, expiry2)
	}
	t.Logf("handshake 2: cert expires %s (new cert served!)", peerExpiry2.Format(time.RFC3339))

	// Sanity: the two certs must be different.
	if peerExpiry1.Equal(peerExpiry2) {
		t.Fatal("both handshakes returned the same cert — hot-reload did NOT work")
	}
	t.Log("hot-reload confirmed: server served new cert without restart")
}

// getPeerCertExpiry does a TLS handshake and returns the peer cert's NotAfter.
func getPeerCertExpiry(t *testing.T, addr string, cfg *tls.Config) time.Time {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("TLS dial %s: %v", addr, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("no peer certificates")
	}
	return certs[0].NotAfter
}
