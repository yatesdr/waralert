package www

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"waralert/audit"
	"waralert/config"
)

const smsGateCABaseURL = "https://ca.sms-gate.app/api/v1"

// requestSMSGateCert generates an ECDSA P-256 key and CSR for the given IP,
// submits it to the SMS-gate CA, polls until the certificate is issued (or
// the request is denied / times out), and saves the cert and key to disk.
func requestSMSGateCert(ip, certFile, keyFile string) error {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}

	// Generate ECDSA P-256 private key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// Create CSR with IP SAN.
	csrTemplate := &x509.CertificateRequest{
		Subject:     pkix.Name{CommonName: ip},
		IPAddresses: []net.IP{parsedIP},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Submit CSR to CA.
	reqBody, _ := json.Marshal(map[string]string{
		"content": string(csrPEM),
		"type":    "webhook",
	})
	resp, err := http.Post(smsGateCABaseURL+"/csr", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("submit CSR: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("CA rejected CSR (HTTP %d): %s", resp.StatusCode, body)
	}

	var csrResp struct {
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(body, &csrResp); err != nil {
		return fmt.Errorf("parse CSR response: %w", err)
	}
	if csrResp.RequestID == "" {
		return fmt.Errorf("CA returned empty request ID")
	}

	// Poll for approval (up to 2 minutes, every 5 seconds).
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		pollResp, err := http.Get(smsGateCABaseURL + "/csr/" + csrResp.RequestID)
		if err != nil {
			continue
		}
		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		var status struct {
			Status      string `json:"status"`
			Certificate string `json:"certificate"`
		}
		if err := json.Unmarshal(pollBody, &status); err != nil {
			continue
		}

		switch status.Status {
		case "approved":
			if status.Certificate == "" {
				return fmt.Errorf("CA approved but returned no certificate")
			}
			// Encode private key to PEM.
			keyDER, err := x509.MarshalECPrivateKey(key)
			if err != nil {
				return fmt.Errorf("marshal key: %w", err)
			}
			keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

			// Write key first, then cert (atomic-ish: if cert write fails, old cert still works with old key).
			if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
				return fmt.Errorf("write key file: %w", err)
			}
			if err := os.WriteFile(certFile, []byte(status.Certificate), 0644); err != nil {
				return fmt.Errorf("write cert file: %w", err)
			}
			return nil
		case "denied":
			return fmt.Errorf("CA denied the certificate request")
		}
		// "pending" — keep polling.
	}

	return fmt.Errorf("timed out waiting for CA approval (2 minutes)")
}

// StartAutoRenew launches a background goroutine that checks the TLS certificate
// expiry every 12 hours and renews it automatically when it is within 30 days of
// expiring. It returns a stop function that shuts down the goroutine.
func StartAutoRenew(reloader *CertReloader, cfg *config.Config, certFile, keyFile string, auditLog *audit.Logger) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		logCertStatus(reloader, auditLog)

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				renewIfNeeded(reloader, cfg, certFile, keyFile, auditLog)
			}
		}
	}()
	return func() { close(done) }
}

func logCertStatus(reloader *CertReloader, auditLog *audit.Logger) {
	expiry := reloader.NotAfter()
	remaining := time.Until(expiry)
	days := int(remaining.Hours() / 24)

	if remaining <= 0 {
		msg := fmt.Sprintf("TLS certificate expired %s ago", -remaining)
		log.Print(msg)
		auditLog.Log(audit.Entry{
			EventType:  "admin",
			ActionType: "cert_status",
			Message:    msg,
			Success:    false,
		})
	} else {
		msg := fmt.Sprintf("TLS certificate expires in %d days (%s)", days, expiry.Format("2006-01-02"))
		log.Print(msg)
		auditLog.Log(audit.Entry{
			EventType:  "admin",
			ActionType: "cert_status",
			Message:    msg,
			Success:    remaining > 30*24*time.Hour,
		})
	}
}

func renewIfNeeded(reloader *CertReloader, cfg *config.Config, certFile, keyFile string, auditLog *audit.Logger) {
	expiry := reloader.NotAfter()
	remaining := time.Until(expiry)
	days := int(remaining.Hours() / 24)

	if remaining > 30*24*time.Hour {
		return
	}

	extURL := cfg.Web.ExternalURL
	if extURL == "" {
		return
	}
	parsed, err := url.Parse(extURL)
	if err != nil {
		return
	}
	ip := parsed.Hostname()
	if net.ParseIP(ip) == nil {
		return
	}

	msg := fmt.Sprintf("TLS certificate expires in %d days, requesting renewal...", days)
	log.Print(msg)
	auditLog.Log(audit.Entry{
		EventType:  "admin",
		ActionType: "cert_renewal",
		Message:    msg,
		Success:    true,
	})

	if err := requestSMSGateCert(ip, certFile, keyFile); err != nil {
		log.Printf("auto-renewal failed: %v", err)
		auditLog.Log(audit.Entry{
			EventType:  "admin",
			ActionType: "cert_renewal",
			Message:    "Auto-renewal failed",
			Success:    false,
			Error:      err.Error(),
		})
		return
	}
	if err := reloader.Reload(); err != nil {
		log.Printf("auto-renewal reload failed: %v", err)
		auditLog.Log(audit.Entry{
			EventType:  "admin",
			ActionType: "cert_renewal",
			Message:    "Certificate saved but reload failed",
			Success:    false,
			Error:      err.Error(),
		})
		return
	}
	newExpiry := reloader.NotAfter()
	msg = fmt.Sprintf("TLS certificate renewed, new expiry: %s", newExpiry.Format("2006-01-02"))
	log.Print(msg)
	auditLog.Log(audit.Entry{
		EventType:  "admin",
		ActionType: "cert_renewal",
		Message:    msg,
		Success:    true,
	})
}
