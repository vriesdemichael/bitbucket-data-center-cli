package doctorcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func skillNamed(t *testing.T, name string) ai.Skill {
	t.Helper()

	for _, skill := range ai.Skills {
		if skill.Name == name {
			return skill
		}
	}
	t.Fatalf("bb carries no skill %q", name)

	return ai.Skill{}
}

func locationNamed(t *testing.T, name string) ai.SkillLocation {
	t.Helper()

	for _, location := range ai.SkillLocations {
		if location.Name == name {
			return location
		}
	}
	t.Fatalf("bb knows no skill location %q", name)

	return ai.SkillLocation{}
}

// TestSkillsAreReportedInEveryPlaceAnAgentReads covers the facts: a skill not
// installed, one exactly as bb ai skill install writes it, and the
// repository's copy that npx skills add installs -- here with the CRLF a
// Windows checkout gives a committed file. None of them fails the run.
func TestSkillsAreReportedInEveryPlaceAnAgentReads(t *testing.T) {
	t.Parallel()

	work, home := t.TempDir(), t.TempDir()
	bb := skillNamed(t, "bb")
	agents, claude := locationNamed(t, "agents"), locationNamed(t, "claude")

	put(t, bb.Path(work, agents), bb.Rendered(testVersion))
	put(t, bb.Path(work, claude), strings.ReplaceAll(bb.Repository(), "\n", "\r\n"))
	put(t, bb.Path(home, claude), bb.Rendered(testVersion))
	machine := (&fakeMachine{home: home, work: work}).machine()

	output, _, err := runDoctorOn(t, machine, config.Diagnosis{}, false)
	if err != nil {
		t.Fatalf("skills with nothing to fix failed the run: %v\n%s", err, output)
	}
	want := strings.Join([]string{
		"Agent skills",
		"  bb  installed in " + bb.Path(work, agents),
		"      installed from the repository in " + bb.Path(work, claude),
		"      installed in " + bb.Path(home, claude),
		"",
		"No issues found.",
		"",
	}, "\n")
	if !strings.HasSuffix(output, want) {
		t.Errorf("the report does not end with\n%s\nin\n%s", want, output)
	}

	output, _, err = runDoctorOn(t, machine, config.Diagnosis{}, true)
	if err != nil {
		t.Fatalf("skills with nothing to fix failed the --json run: %v", err)
	}
	wantSkills := []AgentSkill{
		{Skill: "bb", Scope: scopeProject, Location: "agents", Path: bb.Path(work, agents), State: skillCurrent},
		{Skill: "bb", Scope: scopeProject, Location: "claude", Path: bb.Path(work, claude), State: skillRepository},
		{Skill: "bb", Scope: scopeGlobal, Location: "agents", Path: bb.Path(home, agents), State: skillNotInstalled},
		{Skill: "bb", Scope: scopeGlobal, Location: "claude", Path: bb.Path(home, claude), State: skillCurrent},
	}
	if report := decodeReport(t, output); !reflect.DeepEqual(report.Skills, wantSkills) {
		t.Errorf("skills:\n got %+v\nwant %+v", report.Skills, wantSkills)
	}
}

// TestASkillAnEarlierBbInstalledOrSomebodyEditedFailsTheRun covers the
// issue, keyed by the place so the copy for Claude Code and the one for every
// other agent in one scope are told apart, with the command that replaces it.
func TestASkillAnEarlierBbInstalledOrSomebodyEditedFailsTheRun(t *testing.T) {
	t.Parallel()

	work, home := t.TempDir(), t.TempDir()
	bb := skillNamed(t, "bb")
	agents, claude := locationNamed(t, "agents"), locationNamed(t, "claude")

	put(t, bb.Path(work, agents), bb.Rendered("1.0.0"))
	put(t, bb.Path(home, agents), bb.Rendered(testVersion))
	put(t, bb.Path(home, claude), bb.Repository()+"\nA line somebody added.\n")
	// A directory where the file should be cannot be read, by bb doctor or by
	// an agent.
	if err := os.MkdirAll(bb.Path(work, claude), 0o755); err != nil {
		t.Fatal(err)
	}
	machine := (&fakeMachine{home: home, work: work}).machine()

	const notThisBbs = "not what this bb installs, but an earlier bb's or an edited copy; "

	output, _, err := runDoctorOn(t, machine, config.Diagnosis{}, false)
	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("issues must exit 1 as permanent, got %v\n%s", err, output)
	}
	// Three issues, one skill: the summary names each thing once.
	if want := "3 issues to fix: the bb skill"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}

	details := apperrors.DetailsOf(err)
	want := map[string]string{
		"skill/bb/project/agents": bb.Path(work, agents) + ": " + notThisBbs + "bb ai skill install bb replaces it",
		"skill/bb/global/claude":  bb.Path(home, claude) + ": " + notThisBbs + "bb ai skill install bb --global replaces it",
	}
	unreadable := details["skill/bb/project/claude"]
	delete(details, "skill/bb/project/claude")
	if !reflect.DeepEqual(details, want) {
		t.Errorf("details:\n got %#v\nwant %#v", details, want)
	}
	if !strings.HasPrefix(unreadable, bb.Path(work, claude)+": could not be read: ") {
		t.Errorf("skill/bb/project/claude = %q", unreadable)
	}

	for _, line := range []string{
		"  bb  installed in " + bb.Path(work, agents) + "\n" +
			"      problem: " + notThisBbs + "bb ai skill install bb replaces it\n" +
			"      installed in " + bb.Path(work, claude) + "\n" +
			"      problem: could not be read: ",
		"      installed in " + bb.Path(home, agents) + "\n" +
			"      installed in " + bb.Path(home, claude) + "\n" +
			"      problem: " + notThisBbs + "bb ai skill install bb --global replaces it\n",
		"3 issues to fix.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}

	output, _, err = runDoctorOn(t, machine, config.Diagnosis{}, true)
	if output != "" {
		t.Errorf("a run with issues wrote a report beside its failure:\n%s", output)
	}
	written := &bytes.Buffer{}
	if writeErr := jsonoutput.WriteError(written, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope jsonoutput.ErrorEnvelope
	if decodeErr := json.Unmarshal(written.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("not one JSON document: %v\n%s", decodeErr, written)
	}
	for key := range want {
		if envelope.Error.Details[key] != want[key] {
			t.Errorf("the envelope's %s = %q, want %q", key, envelope.Error.Details[key], want[key])
		}
	}
}

// TestRunFromTheHomeDirectoryASkillIsReportedOnce: there the project's skills
// and the user's are the same files, and one stale file is one issue.
func TestRunFromTheHomeDirectoryASkillIsReportedOnce(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	bb := skillNamed(t, "bb")
	agents := locationNamed(t, "agents")
	put(t, bb.Path(home, agents), bb.Rendered("1.0.0"))
	machine := (&fakeMachine{home: home, work: filepath.Join(home, ".")}).machine()

	output, _, err := runDoctorOn(t, machine, config.Diagnosis{}, false)
	details := apperrors.DetailsOf(err)
	if len(details) != 1 || details["skill/bb/global/agents"] == "" {
		t.Errorf("details = %#v, want only skill/bb/global/agents", details)
	}
	if strings.Count(output, bb.Path(home, agents)) != 1 {
		t.Errorf("the report names %s more than once:\n%s", bb.Path(home, agents), output)
	}

	put(t, bb.Path(home, agents), bb.Rendered(testVersion))
	output, _, err = runDoctorOn(t, machine, config.Diagnosis{}, true)
	if err != nil {
		t.Fatalf("a current skill failed the run: %v", err)
	}
	for _, install := range decodeReport(t, output).Skills {
		if install.Scope != scopeGlobal {
			t.Errorf("run from the home directory, a skill was reported for the project: %+v", install)
		}
	}
}

// TestTheSummaryNamesEveryIssueWhereverItIs: one run over a broken
// configuration, a saved script that has fallen behind and an edited skill
// names all of them, and error.details has an entry for each.
func TestTheSummaryNamesEveryIssueWhereverItIs(t *testing.T) {
	t.Parallel()

	work, home := t.TempDir(), t.TempDir()
	bb := skillNamed(t, "bb")
	put(t, bb.Path(home, locationNamed(t, "claude")), "an edited skill\n")
	put(t, filepath.Join(home, ".config", "fish", "completions", "bb.fish"), "# fish completion for bb                   -*- shell-script -*-\n")
	machine := (&fakeMachine{home: home, work: work}).machine()

	_, _, err := runDoctorOn(t, machine, brokenDiagnosis(), true)

	want := "8 issues to fix: 3 in " + storedPath + ", 1 in " + systemPath + ", retry_count, the keyring, fish completion, the bb skill"
	if apperrors.MessageOf(err) != want {
		t.Errorf("message:\n got %q\nwant %q", apperrors.MessageOf(err), want)
	}

	details := apperrors.DetailsOf(err)
	for key := range brokenDetails {
		if details[key] == "" {
			t.Errorf("error.details lacks %s", key)
		}
	}
	for _, key := range []string{"completion/fish/user", "skill/bb/global/claude"} {
		if details[key] == "" {
			t.Errorf("error.details lacks %s: %#v", key, details)
		}
	}
	if len(details) != len(brokenDetails)+2 {
		t.Errorf("error.details has %d entries, want %d: %#v", len(details), len(brokenDetails)+2, details)
	}
}

// TestAProjectsSkillIsFoundFromASubdirectory covers bb doctor run below the
// repository's root, which is where a skill installed for the project usually
// is and where agents read it. The command that replaces it has to run there,
// because bb ai skill install writes where it runs, so the issue says where.
func TestAProjectsSkillIsFoundFromASubdirectory(t *testing.T) {
	t.Parallel()

	root, home := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	bb, agents := skillNamed(t, "bb"), locationNamed(t, "agents")
	put(t, bb.Path(root, agents), bb.Rendered("1.0.0"))
	machine := (&fakeMachine{home: home, work: work}).machine()

	output, _, err := runDoctorOn(t, machine, config.Diagnosis{}, false)
	if !strings.Contains(output, "installed in "+bb.Path(root, agents)) {
		t.Errorf("the skill at the repository's root was not reported:\n%s", output)
	}

	want := bb.Path(root, agents) + ": not what this bb installs, but an earlier bb's or an edited copy; " +
		"bb ai skill install bb, run in " + root + ", replaces it"
	if got := apperrors.DetailsOf(err)["skill/bb/project/agents"]; got != want {
		t.Errorf("the issue says\n%q\nwant\n%q", got, want)
	}
}

// TestProjectDirectoriesStopAtTheRepositoryRoot holds the walk to the
// repository: nothing above its root is the project's.
func TestProjectDirectoriesStopAtTheRepositoryRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	work := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	// A linked worktree's .git is a file, not a directory.
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := []string{work, filepath.Join(root, "a"), root}
	if got := projectDirectories(work); !reflect.DeepEqual(got, want) {
		t.Errorf("projectDirectories = %q, want %q", got, want)
	}
}

// TestASkillBbNoLongerCarriesIsReportedWhereItWasLeft covers a retired skill:
// no bb command removes one any more, so every copy left behind is an issue
// that names the directory to delete, and a place without one says nothing.
func TestASkillBbNoLongerCarriesIsReportedWhereItWasLeft(t *testing.T) {
	t.Parallel()

	var retired ai.RetiredSkill
	for _, candidate := range ai.RetiredSkills {
		if candidate.Name == "bb-bulk" {
			retired = candidate
		}
	}
	if retired.Name == "" {
		t.Fatal("bb-bulk is not among the retired skills")
	}

	work, home := t.TempDir(), t.TempDir()
	agents, claude := locationNamed(t, "agents"), locationNamed(t, "claude")
	put(t, retired.Path(work, claude), "# an earlier bb's skill\n")
	put(t, retired.Path(home, agents), "# an earlier bb's skill\n")
	machine := (&fakeMachine{home: home, work: work}).machine()

	output, _, err := runDoctorOn(t, machine, config.Diagnosis{}, false)
	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("a retired skill left behind must exit 1 as permanent, got %v\n%s", err, output)
	}
	if want := "2 issues to fix: the bb-bulk skill"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}

	deletion := func(path string) string {
		return path + ": " + retired.Why + "; delete " + filepath.Dir(path)
	}
	want := map[string]string{
		"skill/bb-bulk/project/claude": deletion(retired.Path(work, claude)),
		"skill/bb-bulk/global/agents":  deletion(retired.Path(home, agents)),
	}
	if details := apperrors.DetailsOf(err); !reflect.DeepEqual(details, want) {
		t.Errorf("details:\n got %#v\nwant %#v", details, want)
	}

	if line := "  bb-bulk  installed in " + retired.Path(work, claude) + "\n"; !strings.Contains(output, line) {
		t.Errorf("the report lacks %q:\n%s", line, output)
	}
	if strings.Contains(output, retired.Path(work, agents)) || strings.Contains(output, retired.Path(home, claude)) {
		t.Errorf("a place with no copy of the retired skill was reported:\n%s", output)
	}
}
