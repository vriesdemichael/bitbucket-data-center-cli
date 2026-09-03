package codeowners

import (
	"reflect"
	"testing"
)

// A path pattern may escape spaces exactly like a reviewer group name does.
// The parser used to unescape only the owner side, so the backslash survived
// into the compiled regular expression and the rule could never match.
func TestEscapedSpacesInPathPatterns(t *testing.T) {
	tests := []struct {
		name    string
		content string
		path    string
		want    []string
	}{
		{
			name:    "single backslash escape in directory segment",
			content: `my\ docs/*.md @writers` + "\n",
			path:    "my docs/guide.md",
			want:    []string{"@writers"},
		},
		{
			name:    "double backslash escape in directory segment",
			content: `my\\ docs/*.md @writers` + "\n",
			path:    "my docs/guide.md",
			want:    []string{"@writers"},
		},
		{
			name:    "escaped space in a trailing directory pattern",
			content: `design\ assets/ @design` + "\n",
			path:    "design assets/logo.svg",
			want:    []string{"@design"},
		},
		{
			name:    "escaped space in an anchored pattern",
			content: `/release\ notes/*.md @release` + "\n",
			path:    "release notes/1.0.md",
			want:    []string{"@release"},
		},
		{
			name:    "unescaped path still matches",
			content: "docs/*.md @writers\n",
			path:    "docs/guide.md",
			want:    []string{"@writers"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := Parse(testCase.content).MatchFile(testCase.path)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("MatchFile(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}

// The pattern and the owner side must agree on what an escape means, otherwise
// a rule written consistently in one file behaves inconsistently.
func TestEscapedSpacesAgreeAcrossPatternAndOwner(t *testing.T) {
	co := Parse(`my\ docs/*.md @backend\ engineers:random(2)` + "\n")

	if len(co.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(co.Rules))
	}

	refs := co.MatchFileRefs("my docs/guide.md")
	if len(refs) != 1 {
		t.Fatalf("expected 1 owner ref, got %d (%v)", len(refs), refs)
	}
	if refs[0].Name != "backend engineers" {
		t.Fatalf("owner name = %q, want %q", refs[0].Name, "backend engineers")
	}
	if refs[0].Strategy != StrategyRandom || refs[0].Count != 2 {
		t.Fatalf("strategy = %v(%d), want random(2)", refs[0].Strategy, refs[0].Count)
	}
}

// MatchFileRefs handed callers the rule's own slice, so a caller that adjusted
// a returned ref silently rewrote the parsed rule set for every later lookup.
func TestMatchFileRefsReturnsIndependentCopy(t *testing.T) {
	co := Parse("*.go @backend:random(2)\n")

	first := co.MatchFileRefs("main.go")
	if len(first) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(first))
	}
	first[0].Name = "mutated"
	first[0].Count = 99

	second := co.MatchFileRefs("main.go")
	if second[0].Name != "backend" {
		t.Fatalf("parsed rule was mutated: name = %q, want %q", second[0].Name, "backend")
	}
	if second[0].Count != 2 {
		t.Fatalf("parsed rule was mutated: count = %d, want 2", second[0].Count)
	}
}

// Deduplication keyed on the bare name alone, so a reviewer group "@alice" and
// an individual "alice" collapsed into whichever was seen first.
func TestUsersAndGroupsDoNotCollide(t *testing.T) {
	co := Parse("*.go alice\n*.md @alice\n")

	refs := co.MatchFileRefsUnion([]string{"main.go", "README.md"})
	if len(refs) != 2 {
		t.Fatalf("expected the user and the group to survive separately, got %d: %+v", len(refs), refs)
	}

	got := co.MatchFiles([]string{"main.go", "README.md"})
	want := []string{"alice", "@alice"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchFiles = %v, want %v", got, want)
	}
}

// A pattern that normalizes away to nothing used to be stored as a rule with a
// nil regex, which could never match and only wasted a slot.
func TestEmptyPatternIsRejected(t *testing.T) {
	co := Parse("/ @nobody\n*.go @backend\n")

	if len(co.Rules) != 1 {
		t.Fatalf("expected the empty pattern to be dropped, got rules: %+v", co.Rules)
	}
	if co.Rules[0].Pattern != "*.go" {
		t.Fatalf("surviving rule = %q, want %q", co.Rules[0].Pattern, "*.go")
	}
}

// A bounded strategy with no usable count means "everyone". Recording it as
// random(0) left an ambiguous value that the union merge then treated as the
// narrowest selection instead of the widest.
func TestBoundedStrategyWithoutCountBecomesAll(t *testing.T) {
	tests := []struct {
		token string
		want  SelectionStrategy
	}{
		{token: "@team:random(0)", want: StrategyAll},
		{token: "@team:random(-1)", want: StrategyAll},
		{token: "@team:random(abc)", want: StrategyAll},
		{token: "@team:least_busy(0)", want: StrategyAll},
		{token: "@team:all", want: StrategyAll},
		{token: "@team", want: StrategyAll},
		{token: "@team:random(3)", want: StrategyRandom},
		{token: "@team:least_busy(3)", want: StrategyLeastBusy},
	}

	for _, testCase := range tests {
		t.Run(testCase.token, func(t *testing.T) {
			ref := parseOwnerRef(testCase.token)
			if ref.Strategy != testCase.want {
				t.Fatalf("strategy = %v, want %v", ref.Strategy, testCase.want)
			}
			if testCase.want == StrategyAll && ref.Count != 0 {
				t.Fatalf("count = %d, want 0 for an unbounded selection", ref.Count)
			}
		})
	}
}

// When one file pulls in a narrow selection and another pulls in a wider one,
// the wider selection has to win regardless of which file is listed first.
func TestUnionWidensSelectionRegardlessOfOrder(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		files        []string
		wantStrategy SelectionStrategy
		wantCount    int
	}{
		{
			name:         "bounded seen before all",
			content:      "*.go @team:random(2)\ndocs/*.md @team:all\n",
			files:        []string{"main.go", "docs/guide.md"},
			wantStrategy: StrategyAll,
			wantCount:    0,
		},
		{
			// Disjoint patterns, so each file resolves to exactly one rule and
			// the merge — not last-match-wins — decides the outcome.
			name:         "all seen before bounded",
			content:      "docs/*.md @team:all\n*.go @team:random(2)\n",
			files:        []string{"docs/guide.md", "main.go"},
			wantStrategy: StrategyAll,
			wantCount:    0,
		},
		{
			name:         "two bounded selections keep the larger count",
			content:      "*.go @team:random(2)\ndocs/*.md @team:random(5)\n",
			files:        []string{"main.go", "docs/guide.md"},
			wantStrategy: StrategyRandom,
			wantCount:    5,
		},
		{
			name:         "larger count first still wins",
			content:      "docs/*.md @team:random(5)\n*.go @team:random(2)\n",
			files:        []string{"docs/guide.md", "main.go"},
			wantStrategy: StrategyRandom,
			wantCount:    5,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			refs := Parse(testCase.content).MatchFileRefsUnion(testCase.files)
			if len(refs) != 1 {
				t.Fatalf("expected 1 merged ref, got %d: %+v", len(refs), refs)
			}
			if refs[0].Strategy != testCase.wantStrategy || refs[0].Count != testCase.wantCount {
				t.Fatalf("merged to %v(%d), want %v(%d)", refs[0].Strategy, refs[0].Count, testCase.wantStrategy, testCase.wantCount)
			}
		})
	}
}

// TestParseOwnerRefRecognisesReviewerGroupPrefix is #503.
//
// Bitbucket's Code Owners plugin spells a reviewer group "@reviewer-group/name".
// parseOwnerRef treated the whole token as the group name, so the lookup missed
// a group that exists and the caller then sent "reviewer-group/cog_product" to
// the reviewers API as a username, which answered 409.
func TestParseOwnerRefRecognisesReviewerGroupPrefix(t *testing.T) {
	for _, testCase := range []struct {
		raw             string
		wantName        string
		wantGroup       bool
		wantReviewerGrp bool
	}{
		{raw: "@reviewer-group/cog_product", wantName: "cog_product", wantGroup: true, wantReviewerGrp: true},
		// The bare form still means a Bitbucket group, and must not be
		// mistaken for the reviewer-group form: the two resolve differently
		// when they cannot be found, one warning and one falling back to a
		// username.
		{raw: "@backend-team", wantName: "backend-team", wantGroup: true, wantReviewerGrp: false},
		{raw: "alice", wantName: "alice", wantGroup: false, wantReviewerGrp: false},
		// A user really named after the prefix is not a group at all, so the
		// prefix must only be stripped behind the "@".
		{raw: "reviewer-group/cog_product", wantName: "reviewer-group/cog_product", wantGroup: false, wantReviewerGrp: false},
	} {
		t.Run(testCase.raw, func(t *testing.T) {
			ref := parseOwnerRef(testCase.raw)

			if ref.Name != testCase.wantName {
				t.Errorf("Name = %q, want %q", ref.Name, testCase.wantName)
			}
			if ref.IsGroup != testCase.wantGroup {
				t.Errorf("IsGroup = %v, want %v", ref.IsGroup, testCase.wantGroup)
			}
			if ref.IsReviewerGroup != testCase.wantReviewerGrp {
				t.Errorf("IsReviewerGroup = %v, want %v", ref.IsReviewerGroup, testCase.wantReviewerGrp)
			}
			if ref.Raw != testCase.raw {
				t.Errorf("Raw = %q, want the token unchanged %q", ref.Raw, testCase.raw)
			}
		})
	}
}
