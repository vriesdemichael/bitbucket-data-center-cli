package gateparity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// harnessDockerfile is the one place a Bitbucket release is recorded (ADR-042).
const harnessDockerfile = "docker/harness/Dockerfile"

// harnessTagPattern matches that base image tag.
var harnessTagPattern = regexp.MustCompile(`(?m)^FROM\s+atlassian/bitbucket:(\d+)\.(\d+)(?:\.(\d+))*`)

// qualifiedVersionPattern matches a version literal introduced by the product
// it belongs to: "Bitbucket 9.4.16", "Atlassian Bitbucket Data Center 9.4",
// "Atlassian 9.4". That is how a record naturally names a release, and it is
// the form ADR-027 used in all three of its normative fields.
//
// The product word has to lead, and only the product's own qualifiers may sit
// between it and the number. That is what keeps "golangci-lint v2.6.2" and
// "oapi-codegen v2.4.1" out: a bare version belongs to whatever introduced it,
// and flagging every one would make this a guard people work around.
var qualifiedVersionPattern = regexp.MustCompile(
	`(?i)\b(?:atlassian|bitbucket)\b(?:[\s,]+(?:atlassian|bitbucket|data|centre|center|server|version|release))*[\s,]+v?(\d+\.\d+(?:\.\d+)*)\b`,
)

// bareVersionPattern matches any version-shaped token, used only for the
// narrower second rule below.
var bareVersionPattern = regexp.MustCompile(`\b(\d+\.\d+(?:\.\d+)*)\b`)

// normativeRecord is the part of a decision record that instructs a reader.
//
// rationale and rejected_alternatives are deliberately absent. They are history
// and argument: ADR-042's rationale has to say that the 9.4.16 pin drifted, and
// ADR-045's has to say which release removed an endpoint. Naming a release
// there is the record doing its job. Naming one in the decision or the
// instructions is the record asserting something that stops being true.
type normativeRecord struct {
	Path              string
	Status            string `yaml:"status"`
	Title             string `yaml:"title"`
	Decision          string `yaml:"decision"`
	AgentInstructions string `yaml:"agent_instructions"`
}

// TestAcceptedRecordsDoNotNameABitbucketVersion guards the drift that left
// ADR-027 accepted, naming 9.4, while the harness ran 10.4.x.
//
// ADR-042 records the Bitbucket release in exactly one place, the harness base
// image tag, and ADR-068 keeps the vendored OpenAPI reference derived from it.
// A record that restates a release in its decision or its agent instructions
// has made a copy of that fact, and the copy is what goes stale: ADR-027 sat
// two majors behind for two releases while telling agents -- a first-class
// audience under ADR-003 -- to prefer a version this project does not vendor.
//
// Superseded records are exempt. Their text is a historical statement of what
// was decided, and the status is what tells a reader not to act on it.
func TestAcceptedRecordsDoNotNameABitbucketVersion(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	harness := harnessVersions(t, filepath.Join(root, harnessDockerfile))
	records := decisionRecords(t, filepath.Join(root, "docs", "decisions"))
	if len(records) < 20 {
		t.Fatalf("found %d decision records; the glob or the parser has stopped matching", len(records))
	}

	var offenders []string
	for _, record := range records {
		for _, named := range versionsNamedIn(record, harness) {
			offenders = append(offenders, filepath.Base(record.Path)+": "+named)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"%d decision record field(s) name a Bitbucket version:\n  %s\n\n"+
				"The release under test lives in %s and nowhere else (ADR-042), and the vendored\n"+
				"OpenAPI reference is derived from it (ADR-068). A version restated in a decision or\n"+
				"in agent_instructions is a copy that goes stale. Move it to rationale, where it is\n"+
				"history, or drop it.",
			len(offenders), strings.Join(offenders, "\n  "), harnessDockerfile,
		)
	}
}

// TestBitbucketVersionScanDetectsAStaleRecord is the sabotage, written as a
// test so CI re-runs it (ADR-067).
//
// The guard above passes on a clean tree, so on its own it cannot distinguish
// "no record names a version" from "the scan stopped matching". These cases
// exercise both the rule and each of its deliberate exemptions.
func TestBitbucketVersionScanDetectsAStaleRecord(t *testing.T) {
	t.Parallel()

	harness := []string{"10.4", "10.4.2"}

	cases := []struct {
		name   string
		record normativeRecord
		want   bool
	}{
		{
			name: "ADR-027 as it stood: a stale release in both normative fields",
			record: normativeRecord{
				Status:            "accepted",
				Title:             "Atlassian 9.4 docs as API reference source",
				Decision:          "Use Atlassian Bitbucket Data Center 9.4 REST documentation and its published OpenAPI artifact.",
				AgentInstructions: "Use the version-pinned Atlassian 9.4 reference first when implementing endpoints.",
			},
			want: true,
		},
		{
			name: "the current release restated, which is stale at the next bump",
			record: normativeRecord{
				Status:   "accepted",
				Decision: "The vendored reference is 10.4, matching the harness.",
			},
			want: true,
		},
		{
			name: "a patch-level pin",
			record: normativeRecord{
				Status:            "accepted",
				AgentInstructions: "Assume Bitbucket 9.4.16 behaviour as the baseline.",
			},
			want: true,
		},
		{
			name: "the same claim in a superseded record, which is history",
			record: normativeRecord{
				Status:   "superseded",
				Decision: "Use Atlassian Bitbucket Data Center 9.4 REST documentation.",
			},
			want: false,
		},
		{
			name: "a release named in rationale, which is where history belongs",
			record: normativeRecord{
				Status:   "accepted",
				Decision: "Target the newest Bitbucket version that runs in this project's container stack.",
			},
			want: false,
		},
		{
			name: "an unrelated version, which this rule is not about",
			record: normativeRecord{
				Status:            "accepted",
				Decision:          "Pin golangci-lint to v2.6.2 so a linter release cannot turn CI red.",
				AgentInstructions: "Generated code is produced by oapi-codegen v2.4.1.",
			},
			want: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found := versionsNamedIn(testCase.record, harness)
			if got := len(found) > 0; got != testCase.want {
				t.Fatalf("detected=%v want=%v (found %v)", got, testCase.want, found)
			}
		})
	}
}

// versionsNamedIn reports the Bitbucket versions a record states normatively.
//
// Two rules, because a release reaches prose in two shapes. The first catches
// the qualified form, "Bitbucket 10.4", which is how a record introduces a
// product version. The second catches the bare form, "the reference is 10.4",
// but only for the release the harness actually runs -- a bare number is
// otherwise far more likely to be a linter or generator version, and flagging
// those would make the guard something people work around.
func versionsNamedIn(record normativeRecord, harness []string) []string {
	if strings.EqualFold(record.Status, "superseded") || strings.EqualFold(record.Status, "deprecated") {
		return nil
	}

	current := map[string]bool{}
	for _, version := range harness {
		current[version] = true
	}

	named := []string{}
	for field, text := range map[string]string{
		"title":              record.Title,
		"decision":           record.Decision,
		"agent_instructions": record.AgentInstructions,
	} {
		for _, match := range qualifiedVersionPattern.FindAllStringSubmatch(text, -1) {
			named = append(named, field+": "+match[1])
		}
		for _, match := range bareVersionPattern.FindAllStringSubmatch(text, -1) {
			if current[match[1]] {
				named = append(named, field+": "+match[1])
			}
		}
	}

	return normalise(named)
}

// harnessVersions returns the release the harness pins, as both the full tag
// and the major.minor Atlassian publishes a spec for.
func harnessVersions(t *testing.T, dockerfile string) []string {
	t.Helper()

	content, err := os.ReadFile(dockerfile)
	if err != nil {
		t.Fatalf("read %s: %v", dockerfile, err)
	}

	match := harnessTagPattern.FindSubmatch(content)
	if match == nil {
		t.Fatalf("no atlassian/bitbucket base image tag in %s; ADR-042 requires the release to live there", dockerfile)
	}

	versions := []string{string(match[1]) + "." + string(match[2])}
	if len(match[3]) > 0 {
		versions = append(versions, versions[0]+"."+string(match[3]))
	}
	return versions
}

func decisionRecords(t *testing.T, directory string) []normativeRecord {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if err != nil {
		t.Fatalf("glob %s: %v", directory, err)
	}

	records := make([]normativeRecord, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		var record normativeRecord
		if err := yaml.Unmarshal(raw, &record); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		record.Path = path
		records = append(records, record)
	}

	return records
}
