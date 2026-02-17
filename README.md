# WarAlert

Industrial alert system that monitors PLC tags, ping targets, and WarLink sources, then fires SMS, email, and webhook notifications through configurable alert chains.

## Quick Start

### 1. Build and Run

```bash
make build
./waralert -config waralert.yaml
```

On first launch WarAlert will:
- Create a default `waralert.yaml` if one does not exist
- Generate a self-signed TLS certificate (`cert.pem` / `key.pem`)
- Start the web UI on `https://0.0.0.0:8082`

Open the URL in your browser and create your admin account on the setup page.

### 2. Add a Source

Go to the **Sources** page and add a source. WarAlert supports three source types:

| Type | Purpose |
|------|---------|
| **WarLink** | Polls a [WarLink](https://github.com/yatesdr/warlink) REST API for PLC tags |
| **Direct PLC** | Connects directly to a PLC (Logix, Micro800, S7, Omron, Beckhoff) |
| **Ping** | Monitors host availability via ICMP or TCP |

For a typical setup with WarLink, point the source URL at your WarLink instance (e.g. `http://192.168.1.100:8080/api`).

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

WarAlert always runs over HTTPS. On first start it generates a self-signed certificate valid for 10 years covering `localhost`, `127.0.0.1`, and all detected LAN IPs.

For SMS-Gate integration you should request a CA-signed certificate through the web UI (**Providers > Request Certificate**). This cert is issued by the SMS-Gate CA and is valid for 1 year.

WarAlert handles certificate lifecycle automatically:
- **Hot-reload**: New certificates take effect immediately with no restart
- **Auto-renewal**: A background check runs every 12 hours and renews the certificate automatically when it is within 30 days of expiry
- **Event log**: Certificate status, renewals, and failures are logged to the Event Log page

See [TLS Certificates](docs/tls-certificates.md) for details.

## Documentation

| Document | Description |
|----------|-------------|
| [SMS-Gate Setup](docs/sms-gate-setup.md) | Setting up SMS-Gate local mode, webhooks, and certificates |
| [Chains and Gates](docs/chains-and-gates.md) | Alert chain logic, gate conditions, actions, and timing |
| [SMS Commands](docs/sms-commands.md) | SMS command reference for subscribers |
| [TLS Certificates](docs/tls-certificates.md) | Certificate management, hot-reload, and auto-renewal |
| [Sources](docs/sources.md) | WarLink, direct PLC, and ping source configuration |

## License

Copyright (c) 2024-2026 Derek Yates. All Rights Reserved. See [LICENSE](LICENSE) for terms.
