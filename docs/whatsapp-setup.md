# WhatsApp Setup

WarAlert can send and receive WhatsApp messages using a built-in WhatsApp Web client. This works by linking WarAlert as a companion device to an existing WhatsApp account, the same way WhatsApp Web or WhatsApp Desktop works.

## Important Warnings

### Use a Dedicated Phone Number

**Do not pair WarAlert with your primary personal WhatsApp number.** WhatsApp monitors automated messaging activity and may temporarily or permanently ban accounts that send high volumes of automated messages. Use a secondary or dedicated number for WarAlert.

Recommended options:
- A cheap prepaid SIM in a spare phone
- A business line that is not your daily driver
- A Google Voice or similar VoIP number registered with WhatsApp

### Risk of Account Restrictions

WhatsApp's Terms of Service prohibit unofficial or automated use of their platform. While WarAlert uses the standard WhatsApp Web protocol, it is not an officially sanctioned integration. Be aware of the following risks:

- **Temporary bans**: Sending too many messages in a short period can trigger rate limiting or temporary account suspension (typically 24-72 hours).
- **Permanent bans**: Repeated violations or high-volume automated messaging can result in a permanent account ban with no appeal.
- **No guarantee of delivery**: WhatsApp may silently drop messages or delay delivery at their discretion.
- **Protocol changes**: WhatsApp may change their protocol at any time, which could break connectivity until WarAlert is updated.

**WarAlert includes a configurable rate limiter** (default: 30 messages/minute) to reduce the risk of triggering automated messaging detection. Keep this at a reasonable level.

### Not a Replacement for SMS

WhatsApp should be used as a **supplemental** notification channel, not a sole alerting method for safety-critical systems. SMS delivery is carrier-guaranteed and does not depend on internet connectivity or third-party platform policies. For critical alerts, use SMS as the primary channel and WhatsApp as an additional reach option.

## Setup

### 1. Pair a Device

1. Go to **Providers** in the WarAlert web UI
2. Find the **WhatsApp** card
3. Click **Pair Device** — a QR code will appear
4. On your phone, open WhatsApp > **Settings** > **Linked Devices** > **Link a Device**
5. Scan the QR code displayed in WarAlert
6. Wait for the pairing confirmation (the page will update automatically)

The pairing persists across WarAlert restarts. You do not need to re-pair unless you explicitly log out or remove the linked device from your phone.

### 2. Enable and Save

1. Check the **Enabled** checkbox
2. Optionally adjust the **Rate Limit** (messages per minute, default 30)
3. Click **Save**

### 3. Add Subscribers

WhatsApp subscribers are managed separately from SMS subscribers. There are two ways to add them:

**Self-service (recommended):** Have users send any command (e.g. `SUB FIRE`) to the paired WhatsApp number. WarAlert will automatically create a subscriber record.

**Manual:** Go to the **Subscribers** page, scroll to the **WhatsApp Subscribers** section, and click **Add WA Subscriber**. Enter the phone number in international format (e.g. `15551234567`) and assign topics.

### 4. Create a WhatsApp Action in a Chain

1. Go to **Chains** and edit (or create) a chain
2. Add an action block with type **Send WhatsApp**
3. Set the **Topic** and **Message** (supports the same template variables as SMS)
4. Save the chain

When the chain's gate condition triggers, the WhatsApp action will send the message to all active WhatsApp subscribers for that topic.

## Subscriber Commands

WhatsApp subscribers use the same commands as SMS subscribers by messaging the paired number:

| Command | Description |
|---------|-------------|
| `SUB <topic>` | Subscribe to a topic |
| `UNSUB <topic>` | Unsubscribe from a topic |
| `LIST` | Show current subscriptions |
| `TOPICS` | List available topics |
| `STOP` | Stop all messages |
| `HELP` | Show command help |

See [SMS Commands](sms-commands.md) for the full reference — all commands and aliases are identical.

## Configuration

WhatsApp settings are stored in `waralert.yaml` under `providers.whatsapp`:

```yaml
providers:
  whatsapp:
    enabled: true
    db_path: whatsapp.db       # SQLite database for session data
    global_rate_per_min: 30    # Max messages per minute
```

WhatsApp subscribers are stored under `wa_subscribers`:

```yaml
wa_subscribers:
  - phone: "15551234567"
    active: true
    topics:
      - FIRE
      - TORNADO
```

## Troubleshooting

### QR code doesn't scan
- Make sure the QR code is fully visible and not cut off
- Try refreshing the page and clicking **Pair Device** again to get a fresh QR code
- Ensure your phone has internet connectivity

### Messages not delivered
- Check that the WhatsApp provider is **Enabled** in the Providers page
- Verify the subscriber is **Active** and subscribed to the correct topic
- Check the **Event Log** for error details
- Ensure the paired phone has an active internet connection (WhatsApp Web requires the phone to be online)
- Phone numbers must be in international format without `+` or dashes (e.g. `15551234567`)

### "Not paired" error
- The linked device may have been removed from the phone. Go to **Providers** and click **Pair Device** to re-pair.

### Account temporarily banned
- Stop WarAlert and wait for the ban to expire (usually 24-72 hours)
- Lower the rate limit when you restart
- Reduce the number of subscribers or frequency of alerts
