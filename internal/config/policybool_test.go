package config

import "testing"

// TestParsePolicyBool covers the spellings an administrator writes.
//
// strconv.ParseBool takes only a few of them, and a policy value it rejected
// was dropped: the control then did not apply, and nothing said so.
func TestParsePolicyBool(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "True": true, "t": true,
		"y": true, "yes": true, "Yes": true, "on": true, "ON": true,
		"enable": true, "enabled": true, "  yes  ": true,
		"0": false, "false": false, "FALSE": false, "f": false,
		"n": false, "no": false, "No": false, "off": false, "OFF": false,
		"disable": false, "disabled": false, " no ": false,
	} {
		got, ok := parsePolicyBool(value)
		if !ok {
			t.Errorf("%q was not recognised", value)

			continue
		}
		if got != want {
			t.Errorf("%q read as %v, want %v", value, got, want)
		}
	}

	for _, value := range []string{"", "  ", "sometimes", "2", "yes please", "null", "-1"} {
		if _, ok := parsePolicyBool(value); ok {
			t.Errorf("%q was read as a boolean", value)
		}
	}
}
