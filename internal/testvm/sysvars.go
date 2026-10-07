package testvm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// ParseSysvars checks the complete wire objects before a fixture can execute.
// Numbers stay typed as uint64/int64/float64; nulls and duplicate keys cannot
// silently become Go zero values or disagree with the runner's validation.
func ParseSysvars(data []byte) (Sysvars, error) {
	var result Sysvars
	object, err := sysvarObject(data)
	if err != nil {
		return result, err
	}
	for name, raw := range object {
		var fields []string
		switch name {
		case "clock":
			fields = []string{"slot", "epoch_start_timestamp", "epoch", "leader_schedule_epoch", "unix_timestamp"}
		case "rent":
			fields = []string{"lamports_per_byte_year", "exemption_threshold", "burn_percent"}
		default:
			return result, fmt.Errorf("only clock and rent controls are supported; found %q", name)
		}
		members, err := sysvarObject(raw)
		if err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		if len(members) != len(fields) {
			return result, fmt.Errorf("%s: require all fields %v", name, fields)
		}
		for _, field := range fields {
			value, ok := members[field]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return result, fmt.Errorf("%s: non-null field %q required", name, field)
			}
		}
		if name == "clock" {
			var clock Clock
			if err := json.Unmarshal(raw, &clock); err != nil {
				return result, fmt.Errorf("clock: %w", err)
			}
			result.Clock = &clock
		} else {
			var rent Rent
			if err := json.Unmarshal(raw, &rent); err != nil {
				return result, fmt.Errorf("rent: %w", err)
			}
			if math.IsNaN(rent.ExemptionThreshold) || math.IsInf(rent.ExemptionThreshold, 0) || rent.ExemptionThreshold < 0 || rent.BurnPercent > 100 {
				return result, fmt.Errorf("rent requires finite nonnegative threshold and burn_percent <= 100")
			}
			result.Rent = &rent
		}
	}
	return result, nil
}

func sysvarObject(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil {
		return nil, err
	}
	if first != json.Delim('{') {
		return nil, fmt.Errorf("require a sysvar JSON object")
	}
	object := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name := key.(string)
		if _, found := object[name]; found {
			return nil, fmt.Errorf("duplicate sysvar field %q", name)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		object[name] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("require one sysvar JSON object")
	}
	return object, nil
}
