package mcp

import (
	"reflect"
	"testing"
)

func TestParseCommaList(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: nil},
		{name: "blank only", input: "   ", want: nil},
		{name: "commas only", input: " , , ", want: nil},
		{name: "single", input: "alice", want: []string{"alice"}},
		{name: "multiple", input: "alice,bob", want: []string{"alice", "bob"}},
		{name: "trims and skips blanks", input: " alice , , bob ,", want: []string{"alice", "bob"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCommaList(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCommaList(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}
