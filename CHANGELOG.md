# Changelog

## v0.1.1

### Added
- **WhatsApp messaging support**: Send and receive WhatsApp messages using a built-in WhatsApp Web client. Pair via QR code in the web UI, no external services or per-message fees required.
- New `whatsapp` action type for alert chains — send WhatsApp notifications to topic subscribers
- WhatsApp subscriber management (separate from SMS subscribers) with self-service commands and web UI admin
- WhatsApp subscriber commands: same command set as SMS (`SUB`, `UNSUB`, `LIST`, `TOPICS`, `STOP`, `HELP`)
- WhatsApp provider configuration page with connection status, pairing, test messaging, and rate limiting
- WhatsApp incoming messages shown in the Event Log with sender name (push name) and phone number
- `FromName` field in audit log entries for sender display names
- WhatsApp setup documentation with important warnings about automated messaging risks

### Changed
- Refactored SMS command system to be channel-agnostic (`SubscriptionManager` interface) — shared between SMS and WhatsApp
- Renamed `SMSSender` interface to `MessageSender` for clarity
- Audit log history table now distinguishes SMS and WhatsApp incoming events

### Fixed
- Phone number sanitization for outbound WhatsApp messages (strips formatting characters)
- Correct handling of WhatsApp LID (Linked Identity) addressing — resolves real phone numbers from LID-addressed messages and routes replies correctly

## v0.1.0

Initial release: alert chains, SMS (SMS-Gate local/cloud), email, webhooks, PLC tag monitoring, ping sources, TLS auto-renewal, audit logging, web UI.
