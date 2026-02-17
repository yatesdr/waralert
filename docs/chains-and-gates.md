# Chains and Gates

Alert chains are the core of WarAlert's notification logic. A chain monitors conditions and fires actions when those conditions are met.

## Chain Structure

A chain is an ordered list of **blocks**. There are two block types:

- **Gate**: A set of conditions that must evaluate to true for the chain to continue
- **Action**: A notification to send (SMS, email, or webhook)

Blocks are evaluated top to bottom. If a gate evaluates to false, the chain stops and no further blocks are processed. If a gate passes, the chain continues to the next block.

A typical chain looks like:

```
Gate:   AlarmBit == true
Action: SMS to topic FIRE — "Fire alarm triggered"
Action: Email to ops@example.com — "Fire alarm triggered"
```

## Chain Lifecycle

Chains operate as a state machine:

```
Armed -> Firing -> WaitingClear -> Cooldown -> Armed
```

### Armed

The chain monitors the **first gate** for a **rising edge** — a transition from false to true. The chain does not fire on every poll tick where the gate is true; it only fires when the condition *becomes* true.

### Firing

When a rising edge is detected, the chain walks all blocks in order:
- Gates are evaluated; if any gate fails, the walk stops
- Actions are executed; if an action fails, the error is logged but remaining actions still execute

### WaitingClear

After firing, the chain waits for the first gate to go back to false. This prevents repeated firing while the condition remains true.

### Cooldown

After the gate clears, an optional cooldown period (`cooldown_sec`) prevents the chain from re-arming immediately. This avoids rapid re-triggering from flapping signals.

## Gate Logic

Each gate has a **logic mode** and a list of **conditions**:

- `AND` (default): All conditions must be true
- `OR`: At least one condition must be true

An empty gate (no conditions) always evaluates to true.

## Condition Types

### PLC Tag

Compares a PLC tag value against a target:

| Field | Description |
|-------|-------------|
| Source | The PLC or WarLink source name |
| Tag | The tag name |
| Operator | `==`, `!=`, `>`, `<`, `>=`, `<=` |
| Value | The comparison target |

Numeric values are compared as numbers. Booleans are treated as `1` (true) and `0` (false). String values support only `==` and `!=`.

### Ping

Checks whether a ping source is online:

| Field | Description |
|-------|-------------|
| Source | The ping source name |
| Operator | `==` or `!=` |
| Value | `true` (host is up) or `false` (host is down) |

### Time Between

True when the current time is within a range:

| Field | Description |
|-------|-------------|
| From | Start time in `HH:MM` 24-hour format |
| To | End time in `HH:MM` format |

Supports overnight spans (e.g. `22:00` to `06:00`).

### Weekday

True on specific days of the week:

| Field | Description |
|-------|-------------|
| Days | List of day names (`mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`) |

## Timing Controls

### Debounce (`debounce_sec`)

After detecting a rising edge, the chain waits this many seconds and re-evaluates. If the condition has cleared during the debounce period, the chain does not fire. Use this to filter out momentary glitches.

### Duration (`duration_min`)

A gate-level on-delay timer (TON). The gate conditions must remain continuously true for this many minutes before the gate evaluates to true. If the conditions go false at any point, the timer resets.

### Cooldown (`cooldown_sec`)

After the chain fires and the gate clears, the chain waits this many seconds before re-arming. Prevents rapid re-triggering from flapping conditions.

## Action Types

### SMS

Sends an SMS to all active subscribers of a topic.

| Field | Description |
|-------|-------------|
| Topic | The topic name (e.g. `FIRE`). Resolves to subscriber phone numbers. |
| Message | Message text. Supports Go templates with `{{Tag "source" "tag"}}`. |

### Email

Sends an email to specified recipients.

| Field | Description |
|-------|-------------|
| To | List of email addresses |
| Subject | Email subject. Supports Go templates. |
| Body | Email body. Supports Go templates. |

### Webhook

Sends an HTTP request to a URL.

| Field | Description |
|-------|-------------|
| URL | Target URL |
| Method | HTTP method (default `POST`) |
| Body | Request body. Supports Go templates. |
| Content-Type | Default `application/json` |
| Auth | Optional: `bearer`, `basic`, or `custom_header` |

## Message Templates

Action messages support Go `text/template` syntax. Use `{{Tag "source" "tag"}}` to embed live tag values:

```
Fire alarm active in zone {{Tag "MainPLC" "ZoneNumber"}}. Temperature: {{Tag "MainPLC" "Temp"}}F.
```

## Test Fire

The **Test Fire** button in the web UI bypasses all gates and executes every action block in the chain. All outgoing messages are prefixed with `[TEST ONLY - INITIATED FROM WEBUI]`. Test fire results are logged to the Event Log.

## Rate Limiting

SMS actions are rate-limited by a token-bucket limiter. The default rate is 30 messages per minute, configurable in the SMS provider settings. This prevents runaway alert chains from exhausting SMS credits.
