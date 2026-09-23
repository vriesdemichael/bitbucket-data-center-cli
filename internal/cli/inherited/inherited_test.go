package inherited

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestFromProjectReadsTheScopeAsBitbucketSpellsIt(t *testing.T) {
	t.Parallel()

	for scope, want := range map[string]bool{
		"PROJECT":    true,
		"project":    true,
		" PROJECT ":  true,
		"REPOSITORY": false,
		// An endpoint that does not say is answering about the level it was
		// asked about.
		"": false,
	} {
		if got := FromProject(scope); got != want {
			t.Errorf("FromProject(%q) = %v, want %v", scope, got, want)
		}
	}
}

func TestARefusalNamesTheCommandThatChangesItForTheProject(t *testing.T) {
	t.Parallel()

	err := Refusal("branch restriction", "25", "PROJ", "delete", "bb project branch-restriction delete PROJ 25")

	if apperrors.KindOf(err) != apperrors.KindValidation || apperrors.ExitCode(err) != 2 {
		t.Errorf("a refusal is a validation failure, exit 2; got %v", err)
	}
	for _, want := range []string{"branch restriction 25", "inherited from project PROJ", "every repository in PROJ", "bb project branch-restriction delete PROJ 25"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

func TestLabelMarksOnlyWhatTheProjectDefines(t *testing.T) {
	t.Parallel()

	if got := Label("PROJECT", "PROJ"); got != "inherited from PROJ" {
		t.Errorf("an inherited entry is labelled %q", got)
	}
	if got := Label("REPOSITORY", "PROJ"); got != "" {
		t.Errorf("the repository's own entry is labelled %q", got)
	}
}
