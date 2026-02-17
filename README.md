# WarAlert

Companion app to [WarLink](https://github.com/yatesdr/warlink) that converts process data into SMS and email alerts based on configurable conditions, timers, and schedules. Monitors PLC tags, ping targets, and WarLink sources through alert chains with gate logic, then delivers notifications via SMS, email, and webhooks.

<img width="1191" height="502" alt="image" src="https://github.com/user-attachments/assets/bc4baa42-a89e-4695-8ed0-c2dec35fbe00" />

<img width="896" height="597" alt="image" src="https://github.com/user-attachments/assets/d730a0ea-1a2d-4187-bc18-5505bca80fa5" />

## Quick Start

### 1. Download and Run

Download the latest release for your platform from the [Releases](https://github.com/yatesdr/waralert/releases) page.

```bash
# Linux / macOS
tar -xzf waralert-linux-amd64.tar.gz
./waralert
```

```powershell
# Windows
# Extract waralert-windows-amd64.zip, then run:
.\waralert.exe
```

Or build from source:

```bash
make build
./waralert
```

On first launch WarAlert will:
- Create a default `waralert.yaml` if one does not exist
- Generate a self-signed TLS certificate (`cert.pem` / `key.pem`)
- Start the web UI on `https://0.0.0.0:8082`

Open the URL in your browser and create your admin account on the setup page.

### 2. Add a Source

Go to the **Sources** page and add a source:

| Type | Purpose |
|------|---------|
| **WarLink** | Polls a [WarLink](https://github.com/yatesdr/warlink) instance for PLC tags |
| **Ping** | Monitors host availability via ICMP or TCP |

Point the WarLink source URL at your WarLink instance (e.g. `http://192.168.1.100:8080/api`). WarLink handles all PLC communication — WarAlert reads tag values through the WarLink REST API.

### 3. Configure SMS Provider (SMS-Gate Local Mode)

See [SMS-Gate Setup](docs/sms-gate-setup.md) for the full walkthrough. The short version:

1. Go to **Providers** and select **SMS-Gate** / **Local Phone (LAN)**
2. Enter your SMS-Gate base URL, username, and password
3. Click **Test Connection**
4. Set your **External URL** to your LAN IP (e.g. `https://192.168.1.50:8082`)
5. Click **Request Certificate** to get a trusted TLS cert from the SMS-Gate CA
6. Click **Register Webhook** so SMS-Gate can deliver incoming messages
7. Save the provider config

### 4. Create an Alert Chain

See [Chains and Gates](docs/chains-and-gates.md) for full details. A chain consists of **gate** blocks (conditions that must be true) and **action** blocks (notifications to send):

1. Go to **Chains** and click **Add Chain**
2. Add a **gate** with conditions (e.g. PLC tag `AlarmBit == true`)
3. Add an **action** (e.g. SMS to topic `FIRE` with message `Fire alarm active`)
4. Enable the chain

The chain monitors continuously. When the gate condition transitions from false to true (rising edge), it fires the action blocks.

### 5. SMS Subscriber Commands

Subscribers manage their own subscriptions by texting commands to the SMS-Gate phone number. See [SMS Commands](docs/sms-commands.md) for the full reference.

| Command | Action |
|---------|--------|
| `SUB FIRE` | Subscribe to the FIRE topic |
| `UNSUB FIRE` | Unsubscribe from FIRE (and subtopics) |
| `UNSUB` | Unsubscribe from all topics |
| `LIST` | Show your current subscriptions |
| `TOPICS` | List all available topics |
| `STOP` | Stop all messages |
| `HELP` | Show command help |

### Topic Hierarchy

Topics use `-` as a separator to form parent-child relationships. A subscriber to a parent topic receives alerts for all subtopics:

```
SUB SERVERDOWN          -- receives SERVERDOWN, SERVERDOWN-SERVER1, SERVERDOWN-SERVER2, etc.
SUB SERVERDOWN-SERVER1  -- receives only SERVERDOWN-SERVER1
```

When an alert chain fires to topic `SERVERDOWN-SERVER1`, both subscribers above are notified. When it fires to `SERVERDOWN`, only the first subscriber is notified. `UNSUB SERVERDOWN` removes `SERVERDOWN` and all `SERVERDOWN-*` subtopics.

## CLI Flags

```
-config string    Path to config file (default "waralert.yaml")
-p int            Override web server port
-host string      Override web server host
-log string       Path to log file (default: stderr)
-debug            Enable debug logging
-admin-user       Create/update admin user and exit
-admin-pass       Password for admin user (requires -admin-user)
-version          Print version and exit
```

### Creating an Admin User from the Command Line

```bash
./waralert -admin-user admin -admin-pass secret123
```

This creates (or updates) the admin account in the config file and exits. Useful for automated deployments or password recovery.

## TLS Certificates

WarAlert always runs over HTTPS. On first start it generates a self-signed certificate locally — no internet access is required for initial startup.

Android requires valid TLS certificates for webhook delivery, so SMS-Gate **will not** forward incoming messages to WarAlert over a self-signed cert. You must request a CA-signed certificate through the web UI (**Providers > Request Certificate**). This contacts the SMS-Gate CA to issue a certificate valid for 1 year.

The CA process is automated as much as possible, but it can be a tripping point — the request must be approved in the SMS-Gate app on the phone, and the WarAlert server needs outbound HTTPS access to the CA.

**Firewall requirements:** If WarAlert is on an isolated network, IT will need to allow outbound HTTPS (port 443) to:

```
https://ca.sms-gate.app
```

This is needed for initial certificate requests and for auto-renewal (checked every 12 hours, renews within 30 days of expiry). No other external endpoints are required.

WarAlert handles certificate lifecycle automatically:
- **Hot-reload**: New certificates take effect immediately with no restart
- **Auto-renewal**: Renews automatically when within 30 days of expiry — requires outbound access to the CA URL above
- **Event log**: Certificate status, renewals, and failures are logged to the Event Log page

See [TLS Certificates](docs/tls-certificates.md) for details.

## Documentation

| Document | Description |
|----------|-------------|
| [SMS-Gate Setup](docs/sms-gate-setup.md) | Setting up SMS-Gate local mode, webhooks, and certificates |
| [Chains and Gates](docs/chains-and-gates.md) | Alert chain logic, gate conditions, actions, and timing |
| [SMS Commands](docs/sms-commands.md) | SMS command reference for subscribers |
| [TLS Certificates](docs/tls-certificates.md) | Certificate management, hot-reload, and auto-renewal |
| [Sources](docs/sources.md) | WarLink and ping source configuration |

## License

Copyright (c) 2024-2026 Derek Yates. All Rights Reserved. See [LICENSE](LICENSE) for terms.
