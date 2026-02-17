package chain

import (
	"fmt"

	"waralert/config"
)

// Gate represents a logic gate block that combines multiple conditions using AND/OR logic.
type Gate struct {
	Name       string
	LogicMode  string // "and" or "or"
	Conditions []config.ConditionConfig
}

// NewGate creates a Gate from a block config. LogicMode defaults to "and" if empty.
func NewGate(cfg config.BlockConfig) *Gate {
	mode := cfg.LogicMode
	if mode == "" {
		mode = "and"
	}
	return &Gate{
		Name:       cfg.Name,
		LogicMode:  mode,
		Conditions: cfg.Conditions,
	}
}

// Evaluate evaluates all conditions using the gate's logic mode.
// AND mode: all conditions must be true (short-circuits on first false).
// OR mode: at least one condition must be true (short-circuits on first true).
// A gate with no conditions evaluates to true.
func (g *Gate) Evaluate(tagReader TagReader) (bool, error) {
	if len(g.Conditions) == 0 {
		return true, nil
	}

	switch g.LogicMode {
	case "and":
		return g.evaluateAnd(tagReader)
	case "or":
		return g.evaluateOr(tagReader)
	default:
		return false, fmt.Errorf("unknown logic_mode %q for gate %q", g.LogicMode, g.Name)
	}
}

func (g *Gate) evaluateAnd(tagReader TagReader) (bool, error) {
	for _, cond := range g.Conditions {
		result, err := EvaluateCondition(cond, tagReader)
		if err != nil {
			return false, fmt.Errorf("gate %q condition error: %w", g.Name, err)
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

func (g *Gate) evaluateOr(tagReader TagReader) (bool, error) {
	for _, cond := range g.Conditions {
		result, err := EvaluateCondition(cond, tagReader)
		if err != nil {
			return false, fmt.Errorf("gate %q condition error: %w", g.Name, err)
		}
		if result {
			return true, nil
		}
	}
	return false, nil
}
