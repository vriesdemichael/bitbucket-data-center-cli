// Command mock-inventory indexes every mocked Bitbucket server in the unit
// tests and records what each one assumes about the real server.
//
// A mock encodes its author's belief about the API. When that belief is the
// bug, the mock agrees with the code and the test passes, which is how every
// defect in the v4 sweep shipped green. The inventory is the first step out of
// that: before anything is migrated, we need a list of what is actually being
// assumed and where, rather than a sample.
//
// Classification is deliberately conservative. A call site is only called
// harmless when nothing in its function suggests otherwise; anything ambiguous
// is reported for a person to read rather than quietly cleared.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const reportVersion = 1

// Class is what a mock assumes, which decides where its test belongs.
type Class string

const (
	// ClassBehaviour: the mock answers Bitbucket's routes, shapes or request
	// expectations. Every assertion resting on it is an integration claim, and
	// the unit test cannot check it -- the mock and the code agree by
	// construction. These belong in the live suite.
	ClassBehaviour Class = "bitbucket-behaviour"

	// ClassStatusTaxonomy: the mock returns a status with no Bitbucket payload,
	// and the assertion is usually about our own error taxonomy. Still a claim
	// about the server -- "Bitbucket answers 409 for this" -- so it may stay
	// only where a live test proves the server really does answer that way.
	ClassStatusTaxonomy Class = "status-taxonomy"

	// ClassTransportFault: the failure is injected below the API -- a closed
	// listener, a truncated body, a hang. The subject is our client, not
	// Bitbucket, and a real server will not produce these on demand.
	ClassTransportFault Class = "transport-fault"

	// ClassCannedResponse: the handler answers every request the same way. It
	// never looks at the path, the verb or the body, so the request could be
	// wrong in every particular and the mock would still say yes. It produces
	// coverage and verifies nothing, which is worse than an absent test: the
	// line count claims the path is guarded.
	ClassCannedResponse Class = "canned-response"

	// ClassUnclear: nothing decisive was found. Reported rather than cleared,
	// because a mock that looks harmless is exactly the failure mode.
	// ClassHarnessConstructor: opens a listener around a handler it was handed.
	// It assumes nothing itself, so there is nothing here to migrate -- the
	// callers that write the handlers are what carry the claims.
	// ClassUnreachedGuard: the handler fails the test if it is reached, so the
	// mock asserts that no request is made. It assumes nothing about Bitbucket
	// because nothing is expected to reach it.
	// ClassExternalService: the mock stands in for GitHub or Sigstore, not
	// Bitbucket. The same drift risk applies in principle; it is counted apart
	// so the number is visible rather than waved through.
	ClassExternalService Class = "external-service"

	ClassUnreachedGuard Class = "unreached-guard"

	ClassHarnessConstructor Class = "harness-constructor"

	ClassUnclear Class = "unclear"
)

type entry struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Function string   `json:"function"`
	Class    Class    `json:"class"`
	Signals  []string `json:"signals"`
	// Action is what to do with this mock, and Reason says why. The tool
	// proposes; a person confirms. Nothing here is safe to apply blindly,
	// because whether a live test really covers what a unit test asserted is a
	// judgement the classifier cannot make.
	Action Action `json:"action"`
	Reason string `json:"reason"`
}

// Action is the disposition proposed for a mock.
type Action string

const (
	// ActionMoveToLive: the assertion is about Bitbucket, so it is only worth
	// anything against Bitbucket. Write the live test first, then delete this.
	ActionMoveToLive Action = "move-to-live"

	// ActionRemove: the mock asserts nothing that survives the move. Deleting
	// it loses no guarantee, only the coverage it was inflating.
	ActionRemove Action = "remove"

	// ActionKeep: a legitimate unit test under ADR-079.
	ActionKeep Action = "keep"

	// ActionFollowsCallers: nothing to decide here; it goes when the tests that
	// supply its handlers go.
	ActionFollowsCallers Action = "follows-callers"

	// ActionDecide: outside the Bitbucket policy and needs its own call.
	ActionDecide Action = "decide-separately"
)

// disposition proposes what to do with a mock, from what it assumes.
func disposition(class Class) (Action, string) {
	switch class {
	case ClassBehaviour:
		return ActionMoveToLive, "asserts how Bitbucket routes, accepts or answers; only a real server can confirm it"
	case ClassCannedResponse:
		return ActionRemove, "answers every request identically, so it distinguishes a correct call from a wrong one in no way"
	case ClassStatusTaxonomy:
		return ActionMoveToLive, "claims the server answers this status here; may return as a unit test once a live test proves it does"
	case ClassTransportFault:
		return ActionKeep, "injects a fault below the API; the subject is our client and a real server will not produce it on demand"
	case ClassUnreachedGuard:
		return ActionKeep, "asserts that no request is made, which needs no server to be true"
	case ClassHarnessConstructor:
		return ActionFollowsCallers, "opens a listener around a handler it was handed; it assumes nothing itself"
	case ClassExternalService:
		return ActionDecide, "stands in for GitHub or Sigstore, not Bitbucket; the same drift risk applies but ADR-079 does not cover it"
	default:
		return ActionDecide, "unclassified"
	}
}

type report struct {
	Version int `json:"version"`
	Summary struct {
		Servers   int            `json:"mocked_servers"`
		Files     int            `json:"files"`
		Functions int            `json:"functions"`
		ByClass   map[Class]int  `json:"by_class"`
		ByPackage map[string]int `json:"behaviour_by_package"`
	} `json:"summary"`
	Entries []entry `json:"entries"`
}

func main() {
	roots := flag.String("roots", "internal,tools,cmd", "Comma-separated directories to scan")
	out := flag.String("out", "docs/quality/unit-test-mock-inventory.json", "Where to write the inventory")
	write := flag.Bool("write", false, "Write the inventory to disk")
	flag.Parse()

	entries := []entry{}
	for _, root := range strings.Split(*roots, ",") {
		found, err := scan(strings.TrimSpace(root))
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan %s: %v\n", root, err)
			os.Exit(1)
		}
		entries = append(entries, found...)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		return entries[i].Line < entries[j].Line
	})

	current := buildReport(entries)
	printSummary(current)

	if *write {
		encoded, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %s\n", *out)

		tasks := strings.TrimSuffix(*out, ".json") + "-tasks.md"
		if err := writeTaskList(tasks, current); err != nil {
			fmt.Fprintf(os.Stderr, "write task list: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %s\n", tasks)
	}
}

func buildReport(entries []entry) report {
	current := report{Version: reportVersion, Entries: entries}
	current.Summary.ByClass = map[Class]int{}
	current.Summary.ByPackage = map[string]int{}

	files := map[string]struct{}{}
	functions := map[string]struct{}{}
	for _, item := range entries {
		current.Summary.ByClass[item.Class]++
		files[item.File] = struct{}{}
		functions[item.File+"#"+item.Function] = struct{}{}
		if item.Class == ClassBehaviour {
			current.Summary.ByPackage[filepath.ToSlash(filepath.Dir(item.File))]++
		}
	}
	current.Summary.Servers = len(entries)
	current.Summary.Files = len(files)
	current.Summary.Functions = len(functions)

	return current
}

func printSummary(current report) {
	fmt.Printf("mocked servers: %d across %d files and %d functions\n",
		current.Summary.Servers, current.Summary.Files, current.Summary.Functions)
	for _, class := range []Class{ClassBehaviour, ClassCannedResponse, ClassStatusTaxonomy, ClassTransportFault, ClassUnreachedGuard, ClassHarnessConstructor, ClassExternalService, ClassUnclear} {
		if count := current.Summary.ByClass[class]; count > 0 {
			fmt.Printf("  %-20s %d\n", class, count)
		}
	}
}

func readFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}

// writeTaskList renders the inventory as work to be done, grouped by the file
// it lives in and ordered by how much of it there is.
//
// The JSON is the record; this is the thing somebody actually works from. It is
// grouped by file because that is the unit of a sitting: one file's mocks share
// a harness and usually a subject, so they move together.
func writeTaskList(path string, current report) error {
	type group struct {
		file     string
		byAction map[Action]int
		total    int
	}

	groups := map[string]*group{}
	for _, item := range current.Entries {
		existing, ok := groups[item.File]
		if !ok {
			existing = &group{file: item.File, byAction: map[Action]int{}}
			groups[item.File] = existing
		}
		existing.byAction[item.Action]++
		existing.total++
	}

	ordered := make([]*group, 0, len(groups))
	for _, item := range groups {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		// Files needing live tests first, then by size: that is the order the
		// work pays off in, because a file with no move-to-live is bookkeeping.
		if left.byAction[ActionMoveToLive] != right.byAction[ActionMoveToLive] {
			return left.byAction[ActionMoveToLive] > right.byAction[ActionMoveToLive]
		}
		if left.total != right.total {
			return left.total > right.total
		}

		return left.file < right.file
	})

	var out strings.Builder
	out.WriteString("# Mock migration task list\n\n")
	out.WriteString("Generated by `tools/mock-inventory`. The decision is ADR-079; this is the work it implies.\n\n")
	out.WriteString("Each proposal is what the mock *assumes*, not a verdict on the test. ")
	out.WriteString("Whether a live test already covers what a unit test asserted is a judgement the classifier cannot make, ")
	out.WriteString("so confirm before deleting anything.\n\n")

	out.WriteString("| action | count | meaning |\n|---|---:|---|\n")
	for _, action := range []Action{ActionMoveToLive, ActionRemove, ActionKeep, ActionFollowsCallers, ActionDecide} {
		count := 0
		for _, item := range current.Entries {
			if item.Action == action {
				count++
			}
		}
		if count == 0 {
			continue
		}
		fmt.Fprintf(&out, "| `%s` | %d | %s |\n", action, count, actionMeaning(action))
	}

	out.WriteString("\n## By file\n\n")
	out.WriteString("| file | total | move-to-live | remove | keep | other |\n|---|---:|---:|---:|---:|---:|\n")
	for _, item := range ordered {
		other := item.byAction[ActionFollowsCallers] + item.byAction[ActionDecide]
		fmt.Fprintf(&out, "| `%s` | %d | %d | %d | %d | %d |\n",
			item.file, item.total,
			item.byAction[ActionMoveToLive], item.byAction[ActionRemove],
			item.byAction[ActionKeep], other)
	}

	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func actionMeaning(action Action) string {
	switch action {
	case ActionMoveToLive:
		return "write the live test, then delete the mock"
	case ActionRemove:
		return "delete; it asserts nothing a live test would not already give"
	case ActionKeep:
		return "legitimate unit test under ADR-079"
	case ActionFollowsCallers:
		return "goes when the tests supplying its handlers go"
	case ActionDecide:
		return "outside the Bitbucket policy; needs its own decision"
	}

	return ""
}
