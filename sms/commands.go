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
	CmdTopics      CommandType = "topics"
	CmdStop        CommandType = "stop"
	CmdHelp        CommandType = "help"
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
//	S/SUB/SUBSCRIBE <topic>
//	U/UNSUB/UNSUBSCRIBE <topic>
//	LIST/SUBSCRIPTIONS
//	TOPICS
//	STOP
//	HELP
func ParseCommand(text string) Command {
	text = strings.TrimSpace(text)
	if text == "" {
		return Command{Type: CmdUnknown}
	}

	parts := strings.Fields(text)
	verb := strings.ToUpper(parts[0])

	switch verb {
	case "S", "SUB", "SUBSCRIBE":
		if len(parts) < 2 {
			return Command{Type: CmdSubscribe}
		}
		return Command{Type: CmdSubscribe, Topic: strings.ToUpper(parts[1])}

	case "U", "UNSUB", "UNSUBSCRIBE":
		if len(parts) < 2 {
			return Command{Type: CmdUnsubscribe}
		}
		return Command{Type: CmdUnsubscribe, Topic: strings.ToUpper(parts[1])}

	case "LIST", "SUBSCRIPTIONS":
		return Command{Type: CmdList}

	case "TOPICS":
		return Command{Type: CmdTopics}

	case "STOP":
		return Command{Type: CmdStop}

	case "HELP":
		return Command{Type: CmdHelp}

	default:
		return Command{Type: CmdUnknown}
	}
}

const helpText = "Commands: SUB <topic> to subscribe, UNSUB <topic> to unsubscribe, LIST for your topics, TOPICS to list available topics, STOP to stop all messages, HELP for this message."

func missingTopicReply(mgr *Manager, usage string) string {
	topics := mgr.ListAllTopics()
	if len(topics) == 0 {
		return fmt.Sprintf("Topic required. Usage: %s", usage)
	}
	return fmt.Sprintf("Topic required. Usage: %s\nAvailable topics: %s", usage, strings.Join(topics, ", "))
}

// ExecuteCommand runs the parsed command against the manager and returns a
// human-readable reply message.
func ExecuteCommand(mgr *Manager, phone string, cmd Command) string {
	switch cmd.Type {
	case CmdSubscribe:
		if cmd.Topic == "" {
			return missingTopicReply(mgr, "SUB <topic>")
		}
		if err := mgr.Subscribe(phone, cmd.Topic); err != nil {
			mgr.logFn("sms: subscribe error phone=%s topic=%s: %v", phone, cmd.Topic, err)
			return fmt.Sprintf("Error subscribing to %s. Please try again.", cmd.Topic)
		}
		return fmt.Sprintf("Subscribed to %s", cmd.Topic)

	case CmdUnsubscribe:
		if cmd.Topic == "" {
			if err := mgr.UnsubscribeAll(phone); err != nil {
				mgr.logFn("sms: unsubscribe-all error phone=%s: %v", phone, err)
				return "Error removing subscriptions. Please try again."
			}
			return "Unsubscribed from all topics."
		}
		if err := mgr.Unsubscribe(phone, cmd.Topic); err != nil {
			mgr.logFn("sms: unsubscribe error phone=%s topic=%s: %v", phone, cmd.Topic, err)
			return fmt.Sprintf("Error unsubscribing from %s. Please try again.", cmd.Topic)
		}
		return fmt.Sprintf("Unsubscribed from %s", cmd.Topic)

	case CmdList:
		topics := mgr.ListTopics(phone)
		if len(topics) == 0 {
			return "No subscriptions. Reply TOPICS to see available topics."
		}
		return fmt.Sprintf("Your topics: %s", strings.Join(topics, ", "))

	case CmdTopics:
		topics := mgr.ListAllTopics()
		if len(topics) == 0 {
			return "No topics available."
		}
		return fmt.Sprintf("Available topics: %s", strings.Join(topics, ", "))

	case CmdStop:
		if err := mgr.UnsubscribeAll(phone); err != nil {
			mgr.logFn("sms: stop error phone=%s: %v", phone, err)
			return "Error removing subscriptions. Please try again."
		}
		return "All subscriptions removed. Reply S <topic> to resubscribe."

	case CmdHelp:
		return helpText

	default:
		return helpText
	}
}
