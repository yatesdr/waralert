# SMS-Gate Setup (Local Mode)

This guide covers setting up WarAlert with SMS-Gate in **local mode**, where SMS-Gate runs on the same LAN as WarAlert and connects directly via HTTP basic auth.

## Prerequisites

- SMS-Gate installed and running on an Android phone on your LAN
- The phone's LAN IP address (e.g. `192.168.1.200`)
- SMS-Gate credentials (username and password)
- WarAlert's LAN IP address (e.g. `192.168.1.50`)

## Step 1: Configure the SMS Provider

1. Go to **Providers** in the WarAlert web UI
2. Under SMS Provider, select **SMS-Gate** as the type
3. Select **Local Phone (LAN)** as the mode
4. Enter the SMS-Gate base URL: `http://192.168.1.200:8080` (adjust for your phone's IP and port)
5. Enter the SMS-Gate username and password
6. Click **Test Connection** to verify connectivity
7. Click **Save**

## Step 2: Set the External URL

WarAlert needs to know its own LAN address so it can tell SMS-Gate where to send incoming messages.

1. In the **Providers** section, find the **External URL** field
2. The UI shows detected LAN IPs as quick-select buttons — click the correct one
3. Or manually enter: `https://192.168.1.50:8082`
4. Click **Save**

## Step 3: Request a TLS Certificate

SMS-Gate requires HTTPS for webhook delivery. WarAlert starts with a self-signed cert, but SMS-Gate needs a cert signed by its own CA.

1. Click **Request Certificate**
2. The request is sent to the SMS-Gate CA at `ca.sms-gate.app`
3. Approve the request in the SMS-Gate app on your phone
4. WarAlert downloads the signed certificate automatically (polls for up to 2 minutes)
5. The new certificate takes effect immediately — no restart needed

The CA-issued certificate is valid for 1 year. WarAlert automatically renews it when it is within 30 days of expiry. Certificate events are logged in the **Event Log** page.

## Step 4: Register the Webhook

1. Click **Register Webhook**
2. This tells SMS-Gate to forward incoming SMS messages to WarAlert at `https://192.168.1.50:8082/api/sms/incoming/smsgate`
3. The webhook status badge shows whether registration succeeded

## Step 5: Configure the Webhook Secret (Optional)

For security, you can configure an HMAC-SHA256 webhook secret:

1. Generate a secret in the SMS-Gate app
2. Enter the same secret in the **Webhook Secret** field in WarAlert
3. Save the provider config

When set, WarAlert validates the `X-Signature` and `X-Timestamp` headers on every incoming webhook. Messages with invalid signatures are rejected.

## Step 6: Verify

Send a text message to the SMS-Gate phone number with the text `HELP`. You should receive a reply listing available commands. Check the **Event Log** page to confirm the message was received and processed.

## Troubleshooting

**Connection test fails:**
- Verify the phone's IP and port are correct
- Ensure WarAlert and the phone are on the same network
- Check that SMS-Gate is running on the phone

**Certificate request times out:**
- The SMS-Gate CA requires manual approval on the phone — check the SMS-Gate app
- Ensure the phone has internet access to reach `ca.sms-gate.app`

**Webhook not receiving messages:**
- Verify the external URL is correct and reachable from the phone
- Check that the TLS certificate is CA-signed (not self-signed)
- Re-register the webhook after changing the external URL
- Use **Clean Old Webhooks** to remove stale registrations

**Signature validation failures:**
- Ensure the webhook secret matches exactly between SMS-Gate and WarAlert
- Clear the webhook secret field in both apps if you want to disable validation
