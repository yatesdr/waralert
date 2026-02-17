package whatsapp

import (
	"context"

	"waralert/audit"
	"waralert/sms"
)

// Handler processes incoming WhatsApp messages using the shared command system.
type Handler struct {
	mgr      *Manager
	client   *Client
	logFn    func(string, ...interface{})
	auditLog *audit.Logger
}

// NewHandler creates a new incoming WhatsApp message handler.
func NewHandler(mgr *Manager, client *Client, logFn func(string, ...interface{}), auditLog *audit.Logger) *Handler {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Handler{
		mgr:      mgr,
		client:   client,
		logFn:    logFn,
		auditLog: auditLog,
	}
}

// HandleIncoming processes an incoming WhatsApp message.
// replyJID is the full JID string for sending replies (may be a LID).
// phone is the sender's real phone number for subscriptions and display.
func (h *Handler) HandleIncoming(replyJID, phone, pushName, messageText string) {
	h.logFn("whatsapp: incoming phone=%s replyJID=%s (%s) body=%q", phone, replyJID, pushName, messageText)

	cmd := sms.ParseCommand(messageText)
	reply := sms.ExecuteCommand(h.mgr, phone, cmd)

	h.logFn("whatsapp: reply to=%s body=%q", replyJID, reply)

	var sendErr string
	if err := h.client.SendReply(context.Background(), replyJID, reply); err != nil {
		h.logFn("whatsapp: send reply error: %v", err)
		sendErr = err.Error()
	}

	if h.auditLog != nil {
		h.auditLog.Log(audit.Entry{
			EventType: "wa_incoming",
			From:      phone,
			FromName:  pushName,
			Command:   messageText,
			Reply:     reply,
			Success:   sendErr == "",
			Error:     sendErr,
		})
	}
}
