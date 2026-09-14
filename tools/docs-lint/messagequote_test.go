package main

import (
	"strings"
	"testing"
)

// templatesFromSnippet reads the messages a function body can print, as the
// linter reads a file of bb's source.
func templatesFromSnippet(t *testing.T, body string) []messageTemplate {
	t.Helper()

	source := "package snippet\n\nfunc snippet() {\n" + body + "\n}\n"
	patterns, err := templatesFromGo("snippet.go", []byte(source))
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, source)
	}

	templates := make([]messageTemplate, 0, len(patterns))
	for _, tokens := range patterns {
		templates = append(templates, newMessageTemplate(tokens))
	}

	return templates
}

func TestMessageQuoteMatching(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		quote  string
		want   bool
	}{
		{
			name:   "a quote is part of a literal message",
			source: `_ = errors.New("no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'")`,
			quote:  "no Bitbucket host configured",
			want:   true,
		},
		{
			name:   "an ellipsis stands where the source formats a value",
			source: `_ = fmt.Sprintf("update_trusted_root is invalid: %q", path)`,
			quote:  "update_trusted_root is invalid: ...",
			want:   true,
		},
		{
			name:   "the unicode ellipsis is a placeholder too",
			source: `_ = fmt.Sprintf("expected %d arguments, received %d", want, got)`,
			quote:  "expected … arguments, received …",
			want:   true,
		},
		{
			name:   "the quotes %q prints are absorbed by the verb",
			source: `_ = fmt.Sprintf("host %q is not permitted by administrative policy; allowed hosts: %s", host, list)`,
			quote:  `host "..." is not permitted by administrative policy`,
			want:   true,
		},
		{
			name:   "words an argument supplies fall on the verb",
			source: `_ = fmt.Sprintf("the %s at %s could not be read. %s", what, path, remedy)`,
			quote:  "the stored configuration at ... could not be read",
			want:   true,
		},
		{
			name:   "a wrapped cause falls on the verb that wraps it",
			source: `_ = fmt.Errorf("read CA bundle: %w", err)`,
			quote:  "read CA bundle: open ...: no such file or directory",
			want:   true,
		},
		{
			name:   "non-literal operands of a concatenation are placeholders",
			source: `_ = errors.New(nameFor(cert) + " and " + nameFor(key) + " must be set together")`,
			quote:  "BB_CLIENT_CERT and BB_CLIENT_KEY must be set together",
			want:   true,
		},
		{
			name:   "a parenthesised chain is one message",
			source: `_ = errors.New(("keyring " + state) + " is unavailable and storage is required")`,
			quote:  "keyring ... is unavailable and storage is required",
			want:   true,
		},
		{
			name:   "an ellipsis may elide literal words",
			source: `_ = fmt.Sprintf("the %s at %s could not be read. %s", what, path, remedy)`,
			quote:  "the ... could not be read",
			want:   true,
		},
		{
			name:   "a doubled percent is a literal percent",
			source: `_ = fmt.Sprintf("coverage fell below 80%% of %s", scope)`,
			quote:  "coverage fell below 80% of ...",
			want:   true,
		},
		{
			name:   "a raw string literal is read like any other",
			source: "_ = errors.New(`self-update is disabled in this build; update bb using your package manager`)",
			quote:  "self-update is disabled in this build",
			want:   true,
		},
		{
			name:   "a reworded message no longer matches",
			source: `_ = fmt.Sprintf("the %s at %s is unreadable. %s", what, path, remedy)`,
			quote:  "the stored configuration at ... could not be read",
			want:   false,
		},
		{
			name:   "a quote longer than the message does not match",
			source: `_ = errors.New("no Bitbucket host configured")`,
			quote:  "no Bitbucket host configured for this workspace",
			want:   false,
		},
		{
			name:   "words in the wrong order do not match",
			source: `_ = errors.New("insecure TLS verification is disabled by administrative policy")`,
			quote:  "TLS verification is insecure",
			want:   false,
		},
		{
			name: "templates made of verbs cannot vouch for a message that is gone",
			source: `_ = fmt.Sprintf("%s: %s", a, b)
_ = fmt.Sprintf("no %s configured", what)
_ = fmt.Sprintf("invalid %s", what)`,
			quote: "no Bitbucket host configured",
			want:  false,
		},
		{
			name:   "a placeholder cannot stand in for words the source does not have",
			source: `_ = fmt.Sprintf("host %q is not permitted", host)`,
			quote:  "host ... is not permitted by administrative policy",
			want:   false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			templates := templatesFromSnippet(t, testCase.source)
			if got := quoteMatches(quotePattern(testCase.quote), templates); got != testCase.want {
				t.Fatalf("quote %q against\n%s\nmatched = %v, want %v", testCase.quote, testCase.source, got, testCase.want)
			}
		})
	}
}

func TestPrintfVerbsReadAsWildcards(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"plain %s here":       "plain \x00 here",
		"width %-10s and %5d": "width \x00 and \x00",
		"index %[1]q":         "index \x00",
		"precision %.2f":      "precision \x00",
		"100%% literal":       "100% literal",
		"adjacent %s%v":       "adjacent \x00",
		"trailing %":          "trailing %",
	}

	for input, want := range cases {
		if got := appendFormat(nil, input).key(); got != want {
			t.Errorf("appendFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMessageOfDirectiveDeclaresHeadingsAndTables(t *testing.T) {
	t.Parallel()

	document := strings.Join([]string{
		"# Troubleshooting",
		"",
		"<!-- docs-lint: message-of bb -->",
		"",
		"## `no Bitbucket host configured`",
		"",
		"## `certificate signed by unknown authority`",
		"",
		"<!-- docs-lint: message-of bb crypto/x509 -->",
		"| Symptom | Cause |",
		"|---|---|",
		"| `host \"...\" is not permitted by administrative policy` | policy |",
		"| Git prompts for a password on `git push` | helper |",
		"| `a \\| b` | escaped pipe |",
		"",
		"```text",
		"<!-- docs-lint: message-of bb -->",
		"```",
	}, "\n")

	quotes, findings := extractMessageQuotes("doc.md", document)

	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}

	want := []messageQuote{
		{line: 5, text: "no Bitbucket host configured", producers: []string{"bb"}},
		{line: 12, text: `host "..." is not permitted by administrative policy`, producers: []string{"bb", "crypto/x509"}},
		{line: 14, text: "a | b", producers: []string{"bb", "crypto/x509"}},
	}
	if len(quotes) != len(want) {
		t.Fatalf("quotes = %+v, want %+v", quotes, want)
	}
	for index, quote := range quotes {
		if quote.line != want[index].line || quote.text != want[index].text || strings.Join(quote.producers, " ") != strings.Join(want[index].producers, " ") {
			t.Errorf("quote[%d] = %+v, want %+v", index, quote, want[index])
		}
	}
}

// TestMessageOfDirectiveMustDeclareAQuote covers every way a directive can end
// up checking nothing. Each one fails, so a heading reworded out of a code span
// or a table moved away from its directive is reported rather than skipped.
func TestMessageOfDirectiveMustDeclareAQuote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		problem  string
	}{
		{
			name:     "prose follows the directive",
			document: "<!-- docs-lint: message-of bb -->\nSome prose.\n",
			problem:  "prose follows it",
		},
		{
			name:     "a code block follows the directive",
			document: "<!-- docs-lint: message-of bb -->\n```text\nno Bitbucket host configured\n```\n",
			problem:  "a code block follows it",
		},
		{
			name:     "the heading is not one code span",
			document: "<!-- docs-lint: message-of bb -->\n## `bb update` says self-update is disabled\n",
			problem:  "must be one code span",
		},
		{
			name:     "the table quotes nothing",
			document: "<!-- docs-lint: message-of bb -->\n| Symptom | Cause |\n|---|---|\n| A hang | proxy |\n",
			problem:  "quotes no message",
		},
		{
			name:     "no producer is named",
			document: "<!-- docs-lint: message-of -->\n## `no Bitbucket host configured`\n",
			problem:  "names no producer",
		},
		{
			name:     "two directives in a row",
			document: "<!-- docs-lint: message-of bb -->\n<!-- docs-lint: message-of bb -->\n## `no Bitbucket host configured`\n",
			problem:  "followed by another directive",
		},
		{
			name:     "the document ends first",
			document: "## `no Bitbucket host configured`\n\n<!-- docs-lint: message-of bb -->\n",
			problem:  "not followed by the heading or table",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, findings := extractMessageQuotes("doc.md", testCase.document)
			if len(findings) != 1 || !strings.Contains(findings[0].Problem, testCase.problem) {
				t.Fatalf("expected one finding containing %q, got %+v", testCase.problem, findings)
			}
		})
	}
}

// TestMessageQuotesAreCheckedAgainstTheirProducers goes through the linter the
// way a document does, with producers read from real source: crypto/x509 from
// the toolchain, and bb from this module.
func TestMessageQuotesAreCheckedAgainstTheirProducers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		problem  string
	}{
		{
			name:     "a message the named standard library package prints",
			document: "<!-- docs-lint: message-of crypto/x509 -->\n## `certificate signed by unknown authority`\n",
		},
		{
			name:     "a message bb has never printed",
			document: "<!-- docs-lint: message-of bb -->\n## `the flux capacitor refused to charge while parked`\n",
			problem:  "no message in bb's source under cmd/ and internal/ prints this quote",
		},
		{
			name:     "a message found only in a producer the directive does not name",
			document: "<!-- docs-lint: message-of bb -->\n## `certificate signed by unknown authority`\n",
			problem:  "no message in bb's source",
		},
		{
			name:     "a producer the toolchain cannot find",
			document: "<!-- docs-lint: message-of bb example.invalid/no/such/package -->\n## `certificate signed by unknown authority`\n",
			problem:  "neither bb nor a Go package the toolchain can find",
		},
		{
			name:     "a producer written as a flag",
			document: "<!-- docs-lint: message-of -toolexec=true -->\n## `certificate signed by unknown authority`\n",
			problem:  "neither bb nor a Go import path",
		},
		{
			name:     "a quote with no words",
			document: "<!-- docs-lint: message-of crypto/x509 -->\n## `...`\n",
			problem:  "has no words",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			findings := lintMessageQuotes("doc.md", testCase.document)

			if testCase.problem == "" {
				if len(findings) != 0 {
					t.Fatalf("expected no findings, got %+v", findings)
				}
				return
			}
			if len(findings) != 1 || !strings.Contains(findings[0].Problem, testCase.problem) {
				t.Fatalf("expected one finding containing %q, got %+v", testCase.problem, findings)
			}
			if findings[0].Line != 2 {
				t.Errorf("finding is on line %d, want the heading on line 2", findings[0].Line)
			}
		})
	}
}
