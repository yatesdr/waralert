package plcman

import (
	"github.com/yatesdr/plcio/ads"
	"github.com/yatesdr/plcio/driver"
	"github.com/yatesdr/plcio/logix"
	"github.com/yatesdr/plcio/omron"
	"github.com/yatesdr/plcio/s7"
)

// TagValue is a simplified wrapper around a PLC tag reading. It stores the
// decoded Go value along with enough metadata for display and diagnostics.
type TagValue struct {
	Name     string      // Tag name
	DataType uint16      // Native type code (family-specific)
	Family   string      // PLC family ("logix", "s7", "ads", "omron")
	Value    interface{} // Pre-computed Go value
	Error    error       // Per-tag error (nil if successful)
	TypeStr  string      // Pre-computed type name (set by warlink, empty for direct)
}

// GoValue returns the underlying Go value. If there was a read error it
// returns nil.
func (tv TagValue) GoValue() interface{} {
	if tv.Error != nil {
		return nil
	}
	return tv.Value
}

// TypeName returns a human-readable type name by dispatching to the
// appropriate family's TypeName function.
func (tv TagValue) TypeName() string {
	if tv.TypeStr != "" {
		return tv.TypeStr
	}
	switch tv.Family {
	case "logix", "micro800":
		return logix.TypeName(tv.DataType)
	case "s7":
		return s7.TypeName(tv.DataType)
	case "ads", "beckhoff":
		return ads.TypeName(tv.DataType)
	case "omron":
		return omron.TypeName(tv.DataType)
	default:
		// Fall back to logix for unknown families.
		return logix.TypeName(tv.DataType)
	}
}

// FromDriverTagValue creates a TagValue from a driver.TagValue returned by a
// PLC read operation.
func FromDriverTagValue(dtv *driver.TagValue) TagValue {
	if dtv == nil {
		return TagValue{Error: nil}
	}
	return TagValue{
		Name:     dtv.Name,
		DataType: dtv.DataType,
		Family:   dtv.Family,
		Value:    dtv.Value,
		Error:    dtv.Error,
	}
}
