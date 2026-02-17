// Package whatsapp provides WhatsApp messaging via the whatsmeow library.
package whatsapp

import (
	"context"
	"fmt"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"google.golang.org/protobuf/proto"
)

func init() {
	store.SetOSInfo("Chrome", [3]uint32{131, 0, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
}

// Client wraps a whatsmeow client for sending/receiving WhatsApp messages.
type Client struct {
	cli        *whatsmeow.Client
	container  *sqlstore.Container
	logFn      func(string, ...interface{})
	mu         sync.RWMutex
	msgHandler func(replyJID, phone, pushName, message string)
}

// NewClient creates a new WhatsApp client backed by a SQLite database at dbPath.
func NewClient(dbPath string, logFn func(string, ...interface{})) (*Client, error) {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}

	container, err := sqlstore.New(context.Background(), "sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: open store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("whatsapp: get device: %w", err)
	}

	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	c := &Client{
		cli:       cli,
		container: container,
		logFn:     logFn,
	}

	cli.AddEventHandler(c.eventHandler)

	return c, nil
}

// Connect connects to WhatsApp using stored credentials.
// Returns an error if the device has not been paired yet.
func (c *Client) Connect() error {
	if c.cli.Store.ID == nil {
		return fmt.Errorf("whatsapp: not paired, use StartPairing first")
	}
	return c.cli.Connect()
}

// StartPairing initiates QR code pairing and returns a channel of QR code strings.
// The channel receives successive QR codes until pairing succeeds or the context
// is cancelled. On success, the final value sent is the empty string.
func (c *Client) StartPairing(ctx context.Context) (<-chan string, error) {
	// If already paired, caller should not try to pair again.
	if c.cli.Store.ID != nil {
		return nil, fmt.Errorf("already paired — logout first to re-pair")
	}

	// Disconnect any existing connection before starting QR pairing.
	c.cli.Disconnect()

	// Get a fresh device store in case the old one was invalidated by logout.
	deviceStore, err := c.container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: get device: %w", err)
	}
	c.cli = whatsmeow.NewClient(deviceStore, waLog.Noop)
	c.cli.AddEventHandler(c.eventHandler)

	qrChan, _ := c.cli.GetQRChannel(ctx)
	if err := c.cli.Connect(); err != nil {
		return nil, fmt.Errorf("whatsapp: connect for pairing: %w", err)
	}

	out := make(chan string, 8)
	go func() {
		defer close(out)
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				select {
				case out <- evt.Code:
				case <-ctx.Done():
					return
				}
			case "success":
				c.logFn("whatsapp: pairing successful")
				select {
				case out <- "":
				case <-ctx.Done():
				}
				return
			case "timeout":
				c.logFn("whatsapp: pairing timed out")
				return
			}
		}
	}()

	return out, nil
}

// Disconnect cleanly disconnects from WhatsApp.
func (c *Client) Disconnect() {
	c.cli.Disconnect()
}

// IsConnected returns true if the client is currently connected.
func (c *Client) IsConnected() bool {
	return c.cli.IsConnected()
}

// IsPaired returns true if the client has stored device credentials.
func (c *Client) IsPaired() bool {
	return c.cli.Store.ID != nil
}

// SendMessage sends a text message to a WhatsApp user by phone number.
// The phone number is sanitized to a bare international format (digits only, no + prefix).
func (c *Client) SendMessage(ctx context.Context, phone, message string) error {
	clean := sanitizePhone(phone)
	if clean == "" {
		return fmt.Errorf("whatsapp: invalid phone number %q", phone)
	}
	jid := types.NewJID(clean, types.DefaultUserServer)
	_, err := c.cli.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		return fmt.Errorf("whatsapp: send to %s: %w", clean, err)
	}
	c.logFn("whatsapp: sent to %s", clean)
	return nil
}

// sanitizePhone strips all non-digit characters and leading "+" from a phone number.
func sanitizePhone(phone string) string {
	var digits []byte
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			digits = append(digits, phone[i])
		}
	}
	return string(digits)
}

// SendReply sends a text message to a full JID string (e.g. "1234@s.whatsapp.net"
// or "5678@lid"). Use this for replying to incoming messages.
func (c *Client) SendReply(ctx context.Context, jidStr, message string) error {
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return fmt.Errorf("whatsapp: parse JID %q: %w", jidStr, err)
	}
	_, err = c.cli.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		return fmt.Errorf("whatsapp: send to %s: %w", jidStr, err)
	}
	c.logFn("whatsapp: sent reply to %s", jidStr)
	return nil
}

// SetMessageHandler sets the callback for incoming messages.
// The callback receives:
//   - replyJID: full JID string for sending replies (may be a LID like "123@lid")
//   - phone: the sender's real phone number (for display and subscriptions)
//   - pushName: the sender's WhatsApp display name
//   - message: the message text
func (c *Client) SetMessageHandler(fn func(replyJID, phone, pushName, message string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgHandler = fn
}

// Logout removes stored device credentials, effectively unpairing.
func (c *Client) Logout() error {
	return c.cli.Logout(context.Background())
}

func (c *Client) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		c.mu.RLock()
		handler := c.msgHandler
		c.mu.RUnlock()

		if handler == nil {
			return
		}

		// Use the full sender JID for replies (handles both LID and PN addressing).
		replyJID := v.Info.Sender.ToNonAD().String()

		// Extract the real phone number: if the sender is a LID, the phone
		// number is in SenderAlt; otherwise the sender itself has it.
		phone := v.Info.Sender.User
		if v.Info.Sender.Server == types.HiddenUserServer {
			// Sender is a LID — get phone from SenderAlt
			if !v.Info.SenderAlt.IsEmpty() && v.Info.SenderAlt.Server == types.DefaultUserServer {
				phone = v.Info.SenderAlt.User
			} else {
				// SenderAlt missing — try the LID store
				pn, err := c.cli.Store.LIDs.GetPNForLID(context.Background(), v.Info.Sender.ToNonAD())
				if err == nil && !pn.IsEmpty() {
					phone = pn.User
				} else {
					c.logFn("whatsapp: could not resolve phone for LID %s", v.Info.Sender.User)
				}
			}
		}

		pushName := v.Info.PushName
		text := ""
		if v.Message.GetConversation() != "" {
			text = v.Message.GetConversation()
		} else if v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}
		if text == "" {
			return
		}

		handler(replyJID, phone, pushName, text)
	}
}
