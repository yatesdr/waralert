package www

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"
)

// CertReloader loads a TLS certificate from disk and allows hot-reloading
// without restarting the server. Pass its GetCertificate method to tls.Config.
type CertReloader struct {
	certFile string
	keyFile  string

	mu       sync.RWMutex
	cert     *tls.Certificate
	notAfter time.Time
}

// NewCertReloader loads the certificate pair from disk and returns a reloader.
func NewCertReloader(certFile, keyFile string) (*CertReloader, error) {
	cr := &CertReloader{certFile: certFile, keyFile: keyFile}
	if err := cr.Reload(); err != nil {
		return nil, err
	}
	return cr, nil
}

// Reload re-reads the certificate and key from disk.
func (cr *CertReloader) Reload() error {
	cert, err := tls.LoadX509KeyPair(cr.certFile, cr.keyFile)
	if err != nil {
		return fmt.Errorf("load cert: %w", err)
	}

	// Parse leaf to extract NotAfter.
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}
	cert.Leaf = leaf

	cr.mu.Lock()
	cr.cert = &cert
	cr.notAfter = leaf.NotAfter
	cr.mu.Unlock()
	return nil
}

// GetCertificate is intended for use as tls.Config.GetCertificate.
func (cr *CertReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.cert, nil
}

// NotAfter returns the expiry time of the currently loaded certificate.
func (cr *CertReloader) NotAfter() time.Time {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.notAfter
}
