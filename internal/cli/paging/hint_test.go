package paging

import (
	"strings"
	"testing"
)

func TestHint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		options Options
		count   int
		says    string
	}{
		{name: "at the limit", options: Options{limit: 5}, count: 5, says: "limit of 5"},
		{name: "the default when no limit was given", options: Options{}, count: DefaultLimit, says: "limit of 25"},
		{name: "under the limit", options: Options{limit: 5}, count: 4},
		{name: "empty", options: Options{limit: 5}, count: 0},
		// --all has no cap to reach, so it has nothing to hint at.
		{name: "all", options: Options{all: true}, count: 1_000_000},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			Hint(&out, testCase.options, testCase.count)

			if testCase.says == "" {
				if out.Len() != 0 {
					t.Fatalf("printed %q for a list that did not reach its limit", out.String())
				}
				return
			}

			// The hint is only useful if it names the way out.
			for _, want := range []string{testCase.says, "--limit", "--all"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("hint %q does not mention %q", out.String(), want)
				}
			}
		})
	}
}
