package doctorcmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// issue is one thing to fix, under the key it has in error.details.
type issue struct {
	key     string
	message string
	// summary is what the failure's message calls the issue. Empty for one in
	// the configuration, which the message counts under its file instead.
	summary string
}

// issuesIn lists every issue the configuration section of the report shows, in
// the order it shows them.
//
// Facts are not issues: where a setting came from, what it overrides, and that
// a file holds a plaintext credential, which is the fallback bb uses by design
// where no keyring exists. When keyring-backed storage is required, that
// credential is an issue, and it arrives here as the setting's problem.
func issuesIn(diagnosis config.Diagnosis) []issue {
	issues := []issue{}

	for _, file := range diagnosis.Files {
		name := fileName(file)
		if file.Problem != "" {
			issues = append(issues, issue{key: "file/" + file.Tier, message: name + ": " + file.Problem})
		}
		for _, violation := range file.Violations {
			key := violation.Key
			if key == "" {
				key = "(the file)"
			}
			issues = append(issues, issue{
				key:     "violation/" + file.Tier + jsonPointer(violation.Path),
				message: fmt.Sprintf("%s: %s: %s", located(name, violation.Line), key, violation.Problem),
			})
		}
		for _, ignored := range file.Ignored {
			issues = append(issues, issue{
				key:     "ignored/" + file.Tier + jsonPointer([]string{ignored.Key}),
				message: fmt.Sprintf("%s: %s is read only from the %s configuration", located(name, ignored.Line), ignored.Key, strings.Join(ignored.ReadFrom, " or ")),
			})
		}
	}

	for _, setting := range diagnosis.Settings {
		if setting.Problem != "" {
			issues = append(issues, issue{
				key:     "setting/" + setting.Name,
				message: fmt.Sprintf("%s from %s: %s", setting.Name, describeSource(setting.Source), setting.Problem),
			})
		}
	}

	if keyringUnreachable(diagnosis) {
		issues = append(issues, issue{
			key:     "keyring",
			message: fmt.Sprintf("keyring required by %s: %s", describeSource(diagnosis.Keyring.RequiredBy), diagnosis.Keyring.Problem),
		})
	}

	return issues
}

func keyringUnreachable(diagnosis config.Diagnosis) bool {
	return diagnosis.Keyring.Checked && !diagnosis.Keyring.Reachable
}

func fileName(file config.DiagnosedFile) string {
	if file.Path == "" {
		return "the " + file.Tier + " configuration"
	}

	return file.Path
}

func located(name string, line int) string {
	if line > 0 {
		return fmt.Sprintf("%s:%d", name, line)
	}

	return name
}

// jsonPointer writes a key path as RFC 6901 does, so no two paths share a
// spelling. The dotted form cannot tell hosts."a.b".x from hosts.a."b.x", and a
// host key is a URL, full of dots.
func jsonPointer(path []string) string {
	escape := strings.NewReplacer("~", "~0", "/", "~1")

	var pointer strings.Builder
	for _, segment := range path {
		pointer.WriteString("/")
		pointer.WriteString(escape.Replace(segment))
	}

	return pointer.String()
}

// failureFor is how a run with issues ends, and nil when there is nothing to
// fix.
//
// Permanent, exit 1: what is wrong is in files on this machine, and running bb
// doctor again reads the same files. A failure of bb doctor itself is a bug
// rather than a finding, and keeps whatever kind it has. error.details names
// every issue under its own key, so a caller acts on the envelope without
// parsing the message; two findings at one place, such as a value of the wrong
// type that is also not an allowed one, share that place's entry.
func failureFor(diagnosis config.Diagnosis, issues []issue) error {
	if len(issues) == 0 {
		return nil
	}

	details := make(map[string]string, len(issues))
	for _, found := range issues {
		if earlier, ok := details[found.key]; ok {
			found.message = earlier + "; " + found.message
		}
		details[found.key] = found.message
	}

	failure := apperrors.New(apperrors.KindPermanent, summaryOf(diagnosis, issues), nil)
	failure.Details = details

	return failure
}

func summaryOf(diagnosis config.Diagnosis, issues []issue) string {
	parts := []string{}
	for _, file := range diagnosis.Files {
		inFile := len(file.Violations) + len(file.Ignored)
		if file.Problem != "" {
			inFile++
		}
		if inFile > 0 {
			parts = append(parts, fmt.Sprintf("%d in %s", inFile, fileName(file)))
		}
	}
	for _, setting := range diagnosis.Settings {
		if setting.Problem != "" {
			parts = append(parts, setting.Name)
		}
	}
	if keyringUnreachable(diagnosis) {
		parts = append(parts, "the keyring")
	}
	for _, found := range issues {
		if found.summary != "" && !slices.Contains(parts, found.summary) {
			parts = append(parts, found.summary)
		}
	}

	count := len(issues)

	return fmt.Sprintf("%d %s to fix: %s", count, plural(count, "issue", "issues"), strings.Join(parts, ", "))
}
