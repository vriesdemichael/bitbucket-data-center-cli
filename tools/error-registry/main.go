// Command error-registry folds the responses a live run recorded into a
// committed registry of what Bitbucket actually answers.
//
// The published spec describes request and response shapes. It does not say
// which exception arrives with which status, whether an endpoint answers 204,
// or whether a refusal carries a body at all -- and those are the questions
// bb's error taxonomy is decided from. ADR-079 says a claim about the server is
// proven against the server; this is that proof, gathered by observation.
//
//	BB_ERROR_HARVEST=.tmp/error-harvest.jsonl go test -tags live ./tests/integration/live/
//	go run ./tools/error-registry -in .tmp/error-harvest.jsonl -out docs/quality/bitbucket-error-registry.json
//
// The registry is deduplicated and sorted, so re-running a suite that provokes
// the same errors produces no diff. A diff means Bitbucket answered something
// it did not answer before, which is the point.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type observation struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	Exception  string `json:"exception,omitempty"`
	Message    string `json:"message,omitempty"`
	BodyBytes  int    `json:"bodyBytes"`
	BodyIsJSON bool   `json:"bodyIsJSON"`
}

// entry is one (status, exception) pair, with the endpoints that produced it.
type entry struct {
	Status    int      `json:"status"`
	Exception string   `json:"exception,omitempty"`
	Kind      string   `json:"bbKind"`
	Example   string   `json:"exampleMessage,omitempty"`
	Endpoints []string `json:"endpoints"`
	Count     int      `json:"count"`
	// EmptyBody records that this answer arrived with nothing in it, which is
	// what tells a no-op apart from a payload the client could not decode.
	EmptyBody bool `json:"emptyBody,omitempty"`
}

type registry struct {
	Generated string  `json:"generatedBy"`
	Total     int     `json:"observations"`
	Entries   []entry `json:"entries"`
}

var (
	identifier = regexp.MustCompile(`[0-9a-f]{40}|[0-9]{3,}`)
	projectKey = regexp.MustCompile(`/[A-Z][A-Z0-9]{1,}(?:/|$)`)
	repoSlug   = regexp.MustCompile(`/(?:lt-repo|lt-fork)[a-z0-9-]*`)
)

// normalize turns one request path into the endpoint it belongs to, so a
// hundred seeded projects collapse into one row.
func normalize(path string) string {
	path = identifier.ReplaceAllString(path, "{id}")
	path = repoSlug.ReplaceAllString(path, "/{repo}")
	path = projectKey.ReplaceAllStringFunc(path, func(match string) string {
		if strings.HasSuffix(match, "/") {
			return "/{project}/"
		}
		return "/{project}"
	})

	return path
}

// kindFor mirrors openapi.MapStatusError, so the registry shows what bb decides
// today next to what the server said. A row where those disagree is the work.
func kindFor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "success"
	case status == 400:
		return "validation"
	case status == 401:
		return "authentication"
	case status == 403:
		return "authorization"
	case status == 404:
		return "not_found"
	case status == 409:
		return "conflict"
	case status == 429, status >= 500:
		return "transient"
	default:
		return "permanent"
	}
}

func main() {
	in := flag.String("in", ".tmp/error-harvest.jsonl", "harvest file written by BB_ERROR_HARVEST")
	out := flag.String("out", "docs/quality/bitbucket-error-registry.json", "registry to write")
	flag.Parse()

	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", *in, err)
		os.Exit(1)
	}

	grouped := map[string]*entry{}
	endpoints := map[string]map[string]bool{}
	total := 0

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var seen observation
		if err := json.Unmarshal([]byte(line), &seen); err != nil {
			continue
		}
		total++

		key := fmt.Sprintf("%d|%s", seen.Status, seen.Exception)
		if grouped[key] == nil {
			grouped[key] = &entry{
				Status:    seen.Status,
				Exception: seen.Exception,
				Kind:      kindFor(seen.Status),
				Example:   seen.Message,
				EmptyBody: seen.BodyBytes == 0,
			}
			endpoints[key] = map[string]bool{}
		}
		grouped[key].Count++
		if seen.BodyBytes != 0 {
			grouped[key].EmptyBody = false
		}
		endpoints[key][seen.Method+" "+normalize(seen.Path)] = true
	}

	report := registry{
		Generated: "tools/error-registry from a live suite run; see docs/quality/README.md",
		Total:     total,
	}
	for key, one := range grouped {
		for endpoint := range endpoints[key] {
			one.Endpoints = append(one.Endpoints, endpoint)
		}
		sort.Strings(one.Endpoints)
		// A row listing every endpoint that ever answered this is noise; the
		// question is which answers exist, and a handful of examples is enough
		// to find one again.
		if len(one.Endpoints) > 8 {
			one.Endpoints = append(one.Endpoints[:8], fmt.Sprintf("... and %d more", len(one.Endpoints)-8))
		}
		report.Entries = append(report.Entries, *one)
	}

	sort.Slice(report.Entries, func(i, j int) bool {
		if report.Entries[i].Status != report.Entries[j].Status {
			return report.Entries[i].Status < report.Entries[j].Status
		}
		return report.Entries[i].Exception < report.Entries[j].Exception
	})

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s: %d observations, %d distinct answers\n", *out, total, len(report.Entries))
}
