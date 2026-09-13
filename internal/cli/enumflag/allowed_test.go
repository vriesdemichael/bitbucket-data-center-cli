package enumflag

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestAllowedAnswersOnlyForItsOwnFlags(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var state string
	Register(flags, &state, "state", "open", []string{"open", "closed"}, "State filter")
	flags.String("plain", "", "Not an enum")

	values, ok := Allowed(flags.Lookup("state"))
	if !ok || len(values) != 2 || values[0] != "open" || values[1] != "closed" {
		t.Fatalf("Allowed(--state) = %v, %v", values, ok)
	}

	// A copy: a caller changing it cannot change what the flag accepts.
	values[0] = "changed"
	if again, _ := Allowed(flags.Lookup("state")); again[0] != "open" {
		t.Fatalf("changing the returned values changed the flag: %v", again)
	}

	if _, ok := Allowed(flags.Lookup("plain")); ok {
		t.Fatal("a plain string flag was reported as an enum")
	}
	if _, ok := Allowed(nil); ok {
		t.Fatal("a missing flag was reported as an enum")
	}
}
