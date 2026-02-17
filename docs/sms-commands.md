# SMS Commands

Subscribers manage their subscriptions by texting commands to the SMS-Gate phone number. All commands are **case-insensitive**.

## Command Reference

| Command | Aliases | Description |
|---------|---------|-------------|
| `SUB <topic>` | `S`, `SUBSCRIBE` | Subscribe to a topic |
| `UNSUB <topic>` | `U`, `UNSUBSCRIBE` | Unsubscribe from a topic and its subtopics |
| `UNSUB` | `U`, `UNSUBSCRIBE` | Unsubscribe from all topics |
| `LIST` | `SUBSCRIPTIONS` | List your current subscriptions |
| `TOPICS` | | List all available topics on the server |
| `STOP` | | Deactivate your subscription (stop all messages) |
| `HELP` | | Show command usage |

## Topic Hierarchy

Topics support a parent-child hierarchy using the `-` separator:

- `FIRE` is a parent topic
- `FIRE-ZONE1` and `FIRE-ZONE2` are subtopics of `FIRE`

**Subscribing to a parent covers all subtopics.** If you subscribe to `FIRE`, you will receive alerts for `FIRE`, `FIRE-ZONE1`, `FIRE-ZONE2`, etc. You do not need to subscribe to each subtopic individually.

**Unsubscribing from a parent removes subtopics.** Sending `UNSUB FIRE` removes `FIRE` and any `FIRE-*` subtopics from your subscription.

**Parent replaces children.** If you are subscribed to `FIRE-ZONE1` and then send `SUB FIRE`, the `FIRE-ZONE1` subscription is replaced by the broader `FIRE` subscription.

## Examples

```
SUB FIRE           -> "Subscribed to FIRE"
S TORNADO          -> "Subscribed to TORNADO"
LIST               -> "Your topics: FIRE, TORNADO"
TOPICS             -> "Available topics: FIRE, TORNADO, SAFETY"
UNSUB FIRE         -> "Unsubscribed from FIRE"
U                  -> "Unsubscribed from all topics."
STOP               -> "All subscriptions removed. Reply S <topic> to resubscribe."
HELP               -> "Commands: SUB <topic> to subscribe, ..."
```

## Missing Topic

If you send `SUB` or `S` without a topic name, the reply includes the list of available topics:

```
SUB                -> "Topic required. Usage: SUB <topic>
                      Available topics: FIRE, TORNADO, SAFETY"
```

## New Subscribers

When a new phone number sends a `SUB` command, WarAlert automatically creates a subscriber record. The subscriber appears on the **Subscribers** page in the web UI where admins can view and manage them.

## Reactivation

Sending `STOP` deactivates a subscriber but does not delete them. Sending any `SUB` command reactivates the subscriber.
