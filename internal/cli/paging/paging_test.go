package paging

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestServiceLimit(t *testing.T) {
	testCases := []struct {
		name     string
		options  Options
		expected int
	}{
		{name: "explicit limit", options: Options{limit: 50}, expected: 50},
		{name: "zero falls back to the default", options: Options{limit: 0}, expected: DefaultLimit},
		{name: "negative falls back to the default", options: Options{limit: -1}, expected: DefaultLimit},
		{name: "all removes the cap", options: Options{all: true}, expected: unlimitedLimit},
		// --all wins even with a limit set, so a caller cannot silently get a
		// capped result from a flag combination the command tree rejects anyway.
		{name: "all beats an explicit limit", options: Options{limit: 5, all: true}, expected: unlimitedLimit},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.options.ServiceLimit(); got != testCase.expected {
				t.Fatalf("ServiceLimit() = %d, want %d", got, testCase.expected)
			}
		})
	}
}

// TestUnlimitedLimitStaysWithinFloat32 guards the reason it is not math.MaxInt.
//
// Services derive a per-page size from the remaining count and the generated
// client carries page limits as float32, so a value that cannot round-trip
// through float32 would be silently altered on the wire.
func TestUnlimitedLimitStaysWithinFloat32(t *testing.T) {
	if int(float32(unlimitedLimit)) != unlimitedLimit {
		t.Fatalf("unlimitedLimit %d does not survive a float32 round trip", unlimitedLimit)
	}
}

func newListCommand(t *testing.T, register func(*Options, *cobra.Command)) (*cobra.Command, *Options) {
	t.Helper()

	options := &Options{}
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	parent := &cobra.Command{Use: "thing"}
	list := &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}

	parent.AddCommand(list)
	root.AddCommand(parent)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	register(options, parent)

	return root, options
}

func TestRegisterPersistentAppliesToSubcommands(t *testing.T) {
	root, options := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.RegisterPersistent(parent, 0)
	})

	root.SetArgs([]string{"thing", "list", "--limit", "7"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if options.ServiceLimit() != 7 {
		t.Fatalf("expected the subcommand to see --limit 7, got %d", options.ServiceLimit())
	}
}

func TestRegisterUsesTheSuppliedDefault(t *testing.T) {
	// Listings that deliberately start higher — file trees, permission lists —
	// keep their default while gaining the shared wording.
	root, options := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.Register(parent, 1000)
	})

	root.SetArgs([]string{"thing"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if options.ServiceLimit() != 1000 {
		t.Fatalf("expected the supplied default, got %d", options.ServiceLimit())
	}
}

func TestRegisterFallsBackToDefaultLimit(t *testing.T) {
	root, options := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.Register(parent, 0)
	})

	root.SetArgs([]string{"thing"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if options.ServiceLimit() != DefaultLimit {
		t.Fatalf("expected %d, got %d", DefaultLimit, options.ServiceLimit())
	}
}

func TestAllRemovesTheCap(t *testing.T) {
	root, options := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.RegisterPersistent(parent, 0)
	})

	root.SetArgs([]string{"thing", "list", "--all"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if options.ServiceLimit() != unlimitedLimit {
		t.Fatalf("expected --all to remove the cap, got %d", options.ServiceLimit())
	}
}

// TestLimitAndAllAreMutuallyExclusive is the reason the rule lives in the
// helper: declared once, it applies to every list command without a check
// repeated in 32 RunE bodies.
func TestLimitAndAllAreMutuallyExclusive(t *testing.T) {
	root, _ := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.RegisterPersistent(parent, 0)
	})

	root.SetArgs([]string{"thing", "list", "--all", "--limit", "5"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected --all with --limit to be rejected")
	}
	if !strings.Contains(err.Error(), "all") || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected the error to name both flags, got %q", err)
	}
}

func TestRegisteredFlagWording(t *testing.T) {
	// The wording is the point of the package: before it, 32 registrations
	// described --limit 21 different ways.
	root, _ := newListCommand(t, func(options *Options, parent *cobra.Command) {
		options.RegisterPersistent(parent, 0)
	})

	parent, _, err := root.Find([]string{"thing"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	limitFlag := parent.PersistentFlags().Lookup("limit")
	if limitFlag == nil {
		t.Fatal("--limit was not registered")
	}
	if limitFlag.Usage != "Maximum number of results to return" {
		t.Fatalf("unexpected --limit usage %q", limitFlag.Usage)
	}

	allFlag := parent.PersistentFlags().Lookup("all")
	if allFlag == nil {
		t.Fatal("--all was not registered")
	}
	if !strings.Contains(allFlag.Usage, "every result") {
		t.Fatalf("unexpected --all usage %q", allFlag.Usage)
	}
}

func TestLimitReached(t *testing.T) {
	testCases := []struct {
		name     string
		options  Options
		count    int
		expected bool
	}{
		{name: "under the limit", options: Options{limit: 25}, count: 10, expected: false},
		{name: "at the limit", options: Options{limit: 25}, count: 25, expected: true},
		{name: "over the limit", options: Options{limit: 25}, count: 26, expected: true},
		{name: "empty", options: Options{limit: 25}, count: 0, expected: false},
		{name: "default applies when unset", options: Options{}, count: DefaultLimit, expected: true},
		// --all has no cap to reach, so it can never be at one.
		{name: "all never reports", options: Options{all: true}, count: 1_000_000, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := LimitReached(testCase.options, testCase.count); got != testCase.expected {
				t.Fatalf("LimitReached(%d) = %v, want %v", testCase.count, got, testCase.expected)
			}
		})
	}
}
