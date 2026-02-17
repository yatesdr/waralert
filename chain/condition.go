package chain

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"waralert/config"
)

// Operator represents a comparison operator.
type Operator string

const (
	OpEqual        Operator = "=="
	OpNotEqual     Operator = "!="
	OpGreater      Operator = ">"
	OpLess         Operator = "<"
	OpGreaterEqual Operator = ">="
	OpLessEqual    Operator = "<="
)

// ParseOperator converts a string to an Operator.
func ParseOperator(s string) (Operator, error) {
	switch s {
	case "==":
		return OpEqual, nil
	case "!=":
		return OpNotEqual, nil
	case ">":
		return OpGreater, nil
	case "<":
		return OpLess, nil
	case ">=":
		return OpGreaterEqual, nil
	case "<=":
		return OpLessEqual, nil
	default:
		return "", fmt.Errorf("unknown operator: %s", s)
	}
}

// ValidOperators returns a list of valid operator strings.
func ValidOperators() []string {
	return []string{"==", "!=", ">", "<", ">=", "<="}
}

// toFloat64 converts a value to float64 if possible.
// Handles all Go numeric types, bool (true=1, false=0), and numeric strings.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	case bool:
		if val {
			return 1, true
		}
		return 0, true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// Condition represents a single PLC tag condition with an operator and target value.
type Condition struct {
	Operator Operator
	Value    interface{}
}

// Evaluate checks if the given value satisfies the condition.
func (c *Condition) Evaluate(value interface{}) (bool, error) {
	targetFloat, targetIsNum := toFloat64(c.Value)
	valueFloat, valueIsNum := toFloat64(value)

	if targetIsNum && valueIsNum {
		return c.compareFloat(valueFloat, targetFloat), nil
	}

	// Non-numeric: only equality operators are supported.
	switch c.Operator {
	case OpEqual:
		return reflect.DeepEqual(value, c.Value), nil
	case OpNotEqual:
		return !reflect.DeepEqual(value, c.Value), nil
	default:
		return false, fmt.Errorf("operator %s not supported for non-numeric types", c.Operator)
	}
}

func (c *Condition) compareFloat(value, target float64) bool {
	switch c.Operator {
	case OpEqual:
		return value == target
	case OpNotEqual:
		return value != target
	case OpGreater:
		return value > target
	case OpLess:
		return value < target
	case OpGreaterEqual:
		return value >= target
	case OpLessEqual:
		return value <= target
	default:
		return false
	}
}

// TimeBetweenCondition checks if the current time falls between two times of day.
// Supports overnight spans (e.g., 22:00-06:00).
type TimeBetweenCondition struct {
	From time.Time // Only hour/minute used
	To   time.Time // Only hour/minute used
}

// NewTimeBetweenCondition parses "HH:MM" strings into a TimeBetweenCondition.
func NewTimeBetweenCondition(from, to string) (*TimeBetweenCondition, error) {
	fromTime, err := time.Parse("15:04", from)
	if err != nil {
		return nil, fmt.Errorf("invalid time_from %q: %w", from, err)
	}
	toTime, err := time.Parse("15:04", to)
	if err != nil {
		return nil, fmt.Errorf("invalid time_to %q: %w", to, err)
	}
	return &TimeBetweenCondition{From: fromTime, To: toTime}, nil
}

// Evaluate returns true if the current time is between From and To.
func (tc *TimeBetweenCondition) Evaluate(now time.Time) bool {
	nowMinutes := now.Hour()*60 + now.Minute()
	fromMinutes := tc.From.Hour()*60 + tc.From.Minute()
	toMinutes := tc.To.Hour()*60 + tc.To.Minute()

	if fromMinutes <= toMinutes {
		// Same-day span: e.g., 08:00-17:00
		return nowMinutes >= fromMinutes && nowMinutes < toMinutes
	}
	// Overnight span: e.g., 22:00-06:00
	return nowMinutes >= fromMinutes || nowMinutes < toMinutes
}

// WeekdayCondition checks if today is in the allowed list of weekdays.
type WeekdayCondition struct {
	Days []time.Weekday
}

// NewWeekdayCondition parses short day name strings (mon, tue, wed, etc.) into a WeekdayCondition.
func NewWeekdayCondition(days []string) (*WeekdayCondition, error) {
	parsed := make([]time.Weekday, 0, len(days))
	for _, d := range days {
		wd, err := parseWeekday(d)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, wd)
	}
	return &WeekdayCondition{Days: parsed}, nil
}

// Evaluate returns true if the given time falls on one of the allowed weekdays.
func (wc *WeekdayCondition) Evaluate(now time.Time) bool {
	today := now.Weekday()
	for _, d := range wc.Days {
		if d == today {
			return true
		}
	}
	return false
}

func parseWeekday(s string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sun", "sunday":
		return time.Sunday, nil
	case "mon", "monday":
		return time.Monday, nil
	case "tue", "tuesday":
		return time.Tuesday, nil
	case "wed", "wednesday":
		return time.Wednesday, nil
	case "thu", "thursday":
		return time.Thursday, nil
	case "fri", "friday":
		return time.Friday, nil
	case "sat", "saturday":
		return time.Saturday, nil
	default:
		return 0, fmt.Errorf("unknown weekday: %q", s)
	}
}

// EvaluateCondition dispatches condition evaluation by type.
// Supported types: "plc_tag", "ping", "time_between", "weekday".
func EvaluateCondition(cfg config.ConditionConfig, tagReader TagReader) (bool, error) {
	switch cfg.Type {
	case "plc_tag":
		return evaluatePLCTag(cfg, tagReader)
	case "ping":
		return evaluatePing(cfg, tagReader)
	case "time_between":
		return evaluateTimeBetween(cfg)
	case "weekday":
		return evaluateWeekday(cfg)
	default:
		return false, fmt.Errorf("unknown condition type: %q", cfg.Type)
	}
}

func evaluatePLCTag(cfg config.ConditionConfig, tagReader TagReader) (bool, error) {
	if cfg.PLC == "" || cfg.Tag == "" {
		return false, fmt.Errorf("plc_tag condition requires plc and tag fields")
	}
	op, err := ParseOperator(cfg.Operator)
	if err != nil {
		return false, fmt.Errorf("plc_tag condition: %w", err)
	}

	value, err := tagReader.ReadTagValue(cfg.PLC, cfg.Tag)
	if err != nil {
		return false, fmt.Errorf("failed to read tag %s/%s: %w", cfg.PLC, cfg.Tag, err)
	}

	cond := &Condition{Operator: op, Value: cfg.Value}
	return cond.Evaluate(value)
}

func evaluatePing(cfg config.ConditionConfig, tagReader TagReader) (bool, error) {
	if cfg.PLC == "" {
		return false, fmt.Errorf("ping condition requires plc (source name) field")
	}
	op, err := ParseOperator(cfg.Operator)
	if err != nil {
		return false, fmt.Errorf("ping condition: %w", err)
	}

	value, err := tagReader.ReadTagValue(cfg.PLC, "online")
	if err != nil {
		return false, fmt.Errorf("failed to read ping status for %s: %w", cfg.PLC, err)
	}

	cond := &Condition{Operator: op, Value: cfg.Value}
	return cond.Evaluate(value)
}

func evaluateTimeBetween(cfg config.ConditionConfig) (bool, error) {
	if cfg.TimeFrom == "" || cfg.TimeTo == "" {
		return false, fmt.Errorf("time_between condition requires time_from and time_to fields")
	}
	tc, err := NewTimeBetweenCondition(cfg.TimeFrom, cfg.TimeTo)
	if err != nil {
		return false, err
	}
	return tc.Evaluate(time.Now()), nil
}

func evaluateWeekday(cfg config.ConditionConfig) (bool, error) {
	if len(cfg.Days) == 0 {
		return false, fmt.Errorf("weekday condition requires at least one day")
	}
	wc, err := NewWeekdayCondition(cfg.Days)
	if err != nil {
		return false, err
	}
	return wc.Evaluate(time.Now()), nil
}
