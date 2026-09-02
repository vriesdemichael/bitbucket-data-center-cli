package safederef_test

import (
	"encoding/json"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
)

func TestAbsentReadsAsTheZeroValue(t *testing.T) {
	t.Parallel()

	if got := safederef.String(nil); got != "" {
		t.Errorf("String(nil) = %q", got)
	}
	if got := safederef.Int32(nil); got != 0 {
		t.Errorf("Int32(nil) = %d", got)
	}
	if got := safederef.Int64(nil); got != 0 {
		t.Errorf("Int64(nil) = %d", got)
	}
}

func TestPresentReadsThroughUnchanged(t *testing.T) {
	t.Parallel()

	text, small, large := "value", int32(7), int64(1700000000000)
	if got := safederef.String(&text); got != "value" {
		t.Errorf("String = %q", got)
	}
	if got := safederef.Int32(&small); got != 7 {
		t.Errorf("Int32 = %d", got)
	}
	// Wide enough for an epoch millisecond, which is the reason Int64 exists
	// separately from Int32.
	if got := safederef.Int64(&large); got != 1700000000000 {
		t.Errorf("Int64 = %d", got)
	}
}

// TestAnAbsentSliceMarshalsAsEmptyRatherThanNull is the divergence this package
// settled.
//
// Three copies returned an empty slice and two returned nil. Nothing observed
// the difference, because every caller ranged or counted -- but the two
// marshal differently, and a required list that arrives as null forces a
// consumer to handle a value the command never means to send.
func TestAnAbsentSliceMarshalsAsEmptyRatherThanNull(t *testing.T) {
	t.Parallel()

	got := safederef.StringSlice(nil)
	if got == nil {
		t.Fatal("StringSlice(nil) returned nil")
	}
	if len(got) != 0 {
		t.Fatalf("StringSlice(nil) = %v, want empty", got)
	}

	encoded, err := json.Marshal(struct {
		Values []string `json:"values"`
	}{Values: got})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != `{"values":[]}` {
		t.Errorf("encoded = %s, want an empty array rather than null", encoded)
	}

	values := []string{"a", "b"}
	if got := safederef.StringSlice(&values); len(got) != 2 || got[0] != "a" {
		t.Errorf("StringSlice = %v", got)
	}
}
