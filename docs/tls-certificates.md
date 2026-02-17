# TLS Certificates

WarAlert always runs over HTTPS. It manages TLS certificates automatically, including generation, hot-reload, and auto-renewal.

## Self-Signed Certificate (Default)

On first startup, if no `cert.pem` and `key.pem` exist in the config directory, WarAlert generates a self-signed certificate:

- **Algorithm**: ECDSA P-256
- **Validity**: 10 years
- **SANs**: `localhost`, `127.0.0.1`, machine hostname, and all detected LAN IPs

This certificate works for browser access (after accepting the self-signed warning) but is **not trusted by SMS-Gate** for webhook delivery.

## CA-Signed Certificate (SMS-Gate)

For SMS-Gate webhook integration, you need a certificate signed by the SMS-Gate CA:

1. Set your **External URL** to your LAN IP (e.g. `https://192.168.1.50:8082`)
2. Click **Request Certificate** on the Providers page
3. Approve the request in the SMS-Gate app on your phone
4. The certificate is downloaded, saved, and loaded automatically

The CA-signed certificate is valid for **1 year**.

### What Happens During a Certificate Request

1. WarAlert generates a new ECDSA P-256 private key
2. A CSR (Certificate Signing Request) is created with your IP as the SAN
3. The CSR is submitted to `https://ca.sms-gate.app/api/v1/csr`
4. WarAlert polls every 5 seconds for up to 2 minutes, waiting for approval
5. Once approved, the certificate is downloaded and saved to `cert.pem` / `key.pem`
6. The running server loads the new certificate immediately — no restart needed

## Hot-Reload

WarAlert uses a `GetCertificate` callback in the TLS configuration. Every new TLS handshake fetches the current certificate from memory. When a new certificate is loaded (via the web UI or auto-renewal), subsequent connections immediately use the new certificate. Existing connections are not interrupted.

## Auto-Renewal

A background goroutine checks the certificate expiry every **12 hours**:

- If the certificate expires within **30 days** and the external URL is configured with an IP address, WarAlert automatically requests a new certificate from the SMS-Gate CA
- The new certificate is loaded immediately after issuance
- Success and failure are logged to both the application log and the **Event Log** page

### What Gets Logged

| Event | Event Log Entry |
|-------|----------------|
| Startup | `cert_status` — "TLS certificate expires in X days (YYYY-MM-DD)" |
| Manual request | `cert_request` — "TLS certificate issued for IP, expires YYYY-MM-DD" |
| Auto-renewal started | `cert_renewal` — "TLS certificate expires in X days, requesting renewal..." |
| Auto-renewal success | `cert_renewal` — "TLS certificate renewed, new expiry: YYYY-MM-DD" |
| Auto-renewal failure | `cert_renewal` — error details |

## File Locations

Certificates are stored in the same directory as the config file:

```
waralert.yaml     # config
cert.pem          # TLS certificate (PEM)
key.pem           # TLS private key (PEM, mode 0600)
```

## Troubleshooting

**Browser shows "Not Secure" warning:**
This is expected with the self-signed certificate. Accept the warning or request a CA-signed cert.

**SMS-Gate rejects webhook delivery:**
SMS-Gate requires a certificate signed by its CA. Click **Request Certificate** on the Providers page.

**Certificate expired:**
If auto-renewal is configured (external URL set to an IP), WarAlert will attempt renewal automatically. You can also manually click **Request Certificate** at any time. The new certificate takes effect immediately.

**Auto-renewal not working:**
- Ensure the external URL is set and uses an IP address (not a hostname)
- Check the Event Log for `cert_renewal` entries
- Verify the phone has internet access to reach `ca.sms-gate.app`
