package sms

import (
	"fmt"
	"strings"
)

// CommandType identifies the kind of incoming SMS command.
type CommandType string

const (
	CmdSubscribe   CommandType = "subscribe"
	CmdUnsubscribe CommandType = "unsubscribe"
	CmdList        CommandType = "list"
	CmdStop        CommandType = "stop"
	CmdUnknown     CommandType = "unknown"
)

// Command represents a parsed incoming SMS command.
type Command struct {
	Type  CommandType
	Topic string
}

// ParseCommand parses a raw SMS text into a Command.
// Supported formats (case-insensitive):
//
//	SUBSCRIBE <topic> / SUB <topic>
//	UNSUBSCRIBE <topic> / UNSUB <topic>
//	LIST
//	STOP
func ParseCommand(text string) Command {
	text = strings.TrimSpace(text)
	if text == "" {
		return Command{Type: CmdUnknown}
	}

	parts := strings.Fields(text)
	verb := strings.ToUpper(parts[0])

	switch verb {
	case "SUBSCRIBE", "SUB":
		if len(parts) < 2 {
			return Command{Type: CmdUnknown}
		}
		return Command{Type: CmdSubscribe, Topic: strings.ToUpper(parts[1])}

	case "UNSUBSCRIBE", "UNSUB":
		if len(parts) < 2 {
			return Command{Type: CmdUnknown}
		}
		return Command{Type: CmdUnsubscribe, Topic: strings.ToUpper(parts[1])}

	case "LIST":
		return Command{Type: CmdList}

	case "STOP":
		return Command{Type: CmdStop}

	default:
		return Command{Type: CmdUnknown}
	}
}

// ExecuteCommand runs the parsed command against the manager and returns a
// human-readable reply message.
func ExecuteCommand(mgr *Manager, phone string, cmd Command) string {
	switch cmd.Type {
	case CmdSubscribe:
		if err := mgr.Subscribe(phone, cmd.Topic); err != nil {
			mgr.logFn("sms: subscribe error phone=%s topic=%s: %v", phone, cmd.Topic, err)
			return fmt.Sprintf("Error subscribing to %s. Please try again.", cmd.Topic)
		}
		return fmt.Sprintf("Subscribed to %s", strings.ToUpper(cmd.Topic))

	case CmdUnsubscribe:
		if err := mgr.Unsubscribe(phone, cmd.Topic); err != nil {
			mgr.logFn("sms: unsubscribe error phone=%s topic=%s: %v", phone, cmd.Topic, err)
			return fmt.Sprintf("Error unsubscribing from %s. Please try again.", cmd.Topic)
		}
		return fmt.Sprintf("Unsubscribed from %s", strings.ToUpper(cmd.Topic))

	case CmdList:
		topics := mgr.ListTopics(phone)
		if len(topics) == 0 {
			return "No subscriptions"
		}
		upper := make([]string, len(topics))
		for i, t := range topics {
			upper[i] = strings.ToUpper(t)
		}
		return fmt.Sprintf("Your topics: %s", strings.Join(upper, ", "))

	case CmdStop:
		if err := mgr.UnsubscribeAll(phone); err != nil {
			mgr.logFn("sms: stop error phone=%s: %v", phone, err)
			return "Error removing subscriptions. Please try again."
		}
		return "All subscriptions removed"

	default:
		return "Unknown command. Reply SUBSCRIBE <topic>, UNSUBSCRIBE <topic>, LIST, or STOP"
	}
}
