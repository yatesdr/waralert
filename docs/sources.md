# Sources

Sources provide the data that alert chains evaluate. WarAlert supports three source types.

## WarLink

Polls a [WarLink](https://github.com/yatesdr/warlink) REST API to discover PLCs and read tag values. This is the recommended setup for most installations.

| Setting | Description |
|---------|-------------|
| Name | Display name for this source |
| URL | WarLink API base URL (e.g. `http://192.168.1.100:8080/api`) |
| Poll Rate | How often to poll for tag updates (default: 2 seconds) |

WarLink automatically discovers all connected PLCs and their tags. Tags appear in the chain editor's condition picker as `PLCName.TagName`.

### How It Works

1. WarAlert polls `GET {url}/` to discover connected PLCs and their status
2. For each connected PLC, it polls `GET {url}/{plcName}/tags` to read tag values
3. Tag values are cached in memory and used by chain gate conditions
4. PLC connection status is displayed on the Sources page

## Direct PLC

Connects directly to a PLC over the network without WarLink as an intermediary.

| Setting | Description |
|---------|-------------|
| Name | Display name / tag namespace |
| Address | PLC IP address |
| Family | PLC type (see below) |
| Slot | Backplane slot (Logix only) |
| Tags | List of tags to read |

### Supported PLC Families

| Family | Protocol |
|--------|----------|
| `logix` | Allen-Bradley ControlLogix / CompactLogix (EtherNet/IP) |
| `micro800` | Allen-Bradley Micro800 series |
| `s7` | Siemens S7 (S7comm) |
| `omron` | Omron (FINS) |
| `beckhoff` | Beckhoff (ADS/AMS) |

### Tag Configuration

For direct PLC connections, you must specify which tags to read. Use the tag picker in the web UI:

1. Connect to the PLC from the Sources page
2. Click the tag icon to open the tag manager
3. Add tags by name — the available tags are discovered from the PLC

Tags are polled at the global `poll_rate` (default 500ms).

### Reconnection

If a PLC connection is lost, WarAlert attempts to reconnect with exponential backoff (2s, 4s, 6s, 8s, 10s). After 5 failed attempts the source enters an error state. Reconnection can be triggered manually from the Sources page.

## Ping

Monitors host availability via ICMP ping or TCP port check.

| Setting | Description |
|---------|-------------|
| Name | Display name |
| Host | Hostname or IP address to monitor |
| Port | `0` for ICMP ping, or a TCP port number |
| Interval | How often to check (default: 30 seconds) |

### ICMP vs TCP

- **Port 0** (default): Uses the OS `ping` command. Tests basic network reachability.
- **Port > 0**: Attempts a TCP connection to the specified port. Tests whether a specific service is running.

### Using Ping in Chains

Ping sources expose a single boolean tag called `online`. Use it in gate conditions:

```
Source: Router
Tag: online
Operator: ==
Value: false
```

This gate triggers when the host goes offline — useful for alerting on network failures or equipment going down.
