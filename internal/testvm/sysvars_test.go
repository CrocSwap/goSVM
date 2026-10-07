package testvm

import (
	"encoding/json"
	"strings"
	"testing"
)

const completeClock = `{"slot":18446744073709551615,"epoch_start_timestamp":-9223372036854775808,"epoch":7,"leader_schedule_epoch":9,"unix_timestamp":-123456}`
const completeRent = `{"lamports_per_byte_year":777,"exemption_threshold":1.5,"burn_percent":17}`

func TestSysvarParsingPreservesIntegerLimitsAndPartialControls(t *testing.T) {
	controls, err := ParseSysvars([]byte(`{"clock":` + completeClock + `,"rent":` + completeRent + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if controls.Clock.Slot != ^uint64(0) || controls.Clock.EpochStartTimestamp != -9223372036854775808 || controls.Rent.ExemptionThreshold != 1.5 {
		t.Fatal("sysvar precision lost")
	}
	for _, raw := range []string{`{"clock":` + completeClock + `}`, `{"rent":` + completeRent + `}`, `{}`} {
		if _, err := ParseSysvars([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(controls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "18446744073709551615") || !strings.Contains(string(data), "-9223372036854775808") {
		t.Fatal("sysvar report precision lost")
	}
}

func TestSysvarParsingRejectsIncompleteAmbiguousAndOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{} {}`, `{"clock":null}`, `{"clock":{"slot":1}}`, `{"other":{}}`,
		`{"clock":` + completeClock + `,"clock":` + completeClock + `}`,
		`{"clock":` + strings.Replace(completeClock, `"epoch":7`, `"epoch":7,"epoch":8`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `"epoch":7`, `"epoch":null`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `"epoch":7`, `"epoch":-1`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `"epoch":7`, `"epoch":1.5`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `"epoch":7`, `"unknown":7`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `18446744073709551615`, `18446744073709551616`, 1) + `}`,
		`{"clock":` + strings.Replace(completeClock, `-9223372036854775808`, `-9223372036854775809`, 1) + `}`,
		`{"rent":` + strings.Replace(completeRent, `"burn_percent":17`, `"burn_percent":101`, 1) + `}`,
		`{"rent":` + strings.Replace(completeRent, `"exemption_threshold":1.5`, `"exemption_threshold":-0.1`, 1) + `}`,
		`{"rent":` + strings.Replace(completeRent, `"exemption_threshold":1.5`, `"exemption_threshold":1e999`, 1) + `}`,
	} {
		if _, err := ParseSysvars([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid sysvars: %s", raw)
		}
	}
}
