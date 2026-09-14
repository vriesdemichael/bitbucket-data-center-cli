package repocmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestCloneURLsFromReadsTheCloneLinks(t *testing.T) {
	t.Parallel()

	// The links object as the live instance sent it for a new repository.
	const body = `{"slug":"probe","links":{"clone":[` +
		`{"href":"ssh://git@127.0.0.1:7999/probe602x/probe.git","name":"ssh"},` +
		`{"href":"http://127.0.0.1:7990/scm/probe602x/probe.git","name":"http"}],` +
		`"self":[{"href":"http://127.0.0.1:7990/projects/PROBE602X/repos/probe/browse"}]}}`

	want := []CloneURL{
		{Name: "ssh", URL: "ssh://git@127.0.0.1:7999/probe602x/probe.git"},
		{Name: "http", URL: "http://127.0.0.1:7990/scm/probe602x/probe.git"},
	}
	if got := cloneURLsFrom(decodeRepository(t, body).Links); !reflect.DeepEqual(got, want) {
		t.Fatalf("clone URLs = %+v, want %+v", got, want)
	}

	if got := cloneURLsFrom(nil); got == nil || len(got) != 0 {
		t.Errorf("no links must give an empty list rather than nil, got %#v", got)
	}
}

// links is typed as an open map, so anything can be in it. A clone entry with
// nowhere to clone from is not a clone URL, and publishing one with an empty
// url would hand a caller something git cannot use.
func TestCloneURLsFromSkipsEntriesWithNothingToCloneFrom(t *testing.T) {
	t.Parallel()

	const body = `{"links":{"clone":[` +
		`{"name":"http"},` +
		`"not a link",` +
		`{"href":"   ","name":"ssh"},` +
		`{"href":"http://bitbucket.example/scm/prj/demo.git","name":"http"}]}}`

	want := []CloneURL{{Name: "http", URL: "http://bitbucket.example/scm/prj/demo.git"}}
	if got := cloneURLsFrom(decodeRepository(t, body).Links); !reflect.DeepEqual(got, want) {
		t.Fatalf("clone URLs = %+v, want only the usable one %+v", got, want)
	}
}

func TestReadmeFromKeepsBytesItCannotCarryAsText(t *testing.T) {
	t.Parallel()

	if got := readmeFrom(nil, false); got != nil {
		t.Errorf("no README must be absent, got %+v", got)
	}
	if got := readmeFrom([]byte("# Title\n"), true); got == nil || got.Encoding != "utf-8" || got.Content != "# Title\n" {
		t.Errorf("a UTF-8 README = %+v", got)
	}
	if got := readmeFrom([]byte{0xff, 0xfe}, true); got == nil || got.Encoding != "base64" || got.Content != "//4=" {
		t.Errorf("a README that is not UTF-8 = %+v", got)
	}
	if got := readmeFrom([]byte{}, true); got == nil || got.Content != "" {
		t.Errorf("an empty README is still a README, got %+v", got)
	}
}

func TestWriteRepositoryViewPrintsTheReadmeAsItIs(t *testing.T) {
	t.Parallel()

	view := RepositoryView{
		Repository: result.RepositoryDetail{ProjectKey: "PRJ", Slug: "demo", Name: "Demo", State: "AVAILABLE"},
		CloneURLs:  []CloneURL{{Name: "http", URL: "http://bitbucket.example/scm/prj/demo.git"}},
	}

	var withReadme bytes.Buffer
	writeRepositoryView(&withReadme, view, []byte("# Title\n\nno trailing newline"), true, true)
	for _, want := range []string{"PRJ/demo", "http://bitbucket.example/scm/prj/demo.git", "# Title\n\nno trailing newline\n"} {
		if !strings.Contains(plainText(withReadme), want) {
			t.Errorf("output is missing %q:\n%s", want, withReadme.String())
		}
	}

	var without bytes.Buffer
	writeRepositoryView(&without, view, nil, false, true)
	if !strings.Contains(plainText(without), "No README") {
		t.Errorf("a repository without a README must say so:\n%s", without.String())
	}

	var skipped bytes.Buffer
	writeRepositoryView(&skipped, view, nil, false, false)
	if strings.Contains(plainText(skipped), "README") {
		t.Errorf("--readme=false must leave the README section out:\n%s", skipped.String())
	}
}

// The optional details appear when the repository has them and are left out,
// label and all, when it does not: an empty "Description:" reads as a
// description that is blank rather than one nobody wrote.
func TestWriteRepositoryViewShowsOnlyTheDetailsTheRepositoryHas(t *testing.T) {
	t.Parallel()

	fork := RepositoryView{Repository: result.RepositoryDetail{
		ProjectKey:    "PRJ",
		Slug:          "demo",
		Name:          "Demo",
		Description:   "Payments service",
		DefaultBranch: "refs/heads/main",
		State:         "AVAILABLE",
		Public:        true,
		Forkable:      true,
		Origin:        &result.Repository{ProjectKey: "UP", Slug: "demo"},
	}}

	var full bytes.Buffer
	writeRepositoryView(&full, fork, nil, false, false)
	for _, want := range []string{
		"Description: Payments service",
		"Default branch: refs/heads/main",
		"Public: yes",
		"Forkable: yes",
		"Archived: no",
		"Forked from: UP/demo",
	} {
		if !strings.Contains(plainText(full), want) {
			t.Errorf("output is missing %q:\n%s", want, plainText(full))
		}
	}

	bare := RepositoryView{Repository: result.RepositoryDetail{ProjectKey: "PRJ", Slug: "demo", State: "AVAILABLE", Archived: true}}

	var minimal bytes.Buffer
	writeRepositoryView(&minimal, bare, nil, false, false)
	for _, absent := range []string{"Description", "Default branch", "Forked from", "Clone ("} {
		if strings.Contains(plainText(minimal), absent) {
			t.Errorf("output names %q for a repository that has none:\n%s", absent, plainText(minimal))
		}
	}
	for _, want := range []string{"Public: no", "Archived: yes"} {
		if !strings.Contains(plainText(minimal), want) {
			t.Errorf("output is missing %q:\n%s", want, plainText(minimal))
		}
	}
}

// The failures bb repo get decides before it asks Bitbucket anything, each
// returned as the error it is. The client points at a closed port, so a command
// that went on to the network would fail with a transport error instead.
func TestRepoGetReportsFailuresBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	run := func(deps Dependencies, args ...string) error {
		root := &cobra.Command{Use: "bb"}
		root.AddCommand(New(deps))
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(append([]string{"repo", "get"}, args...))

		return root.Execute()
	}

	t.Run("configuration that cannot be loaded", func(t *testing.T) {
		t.Parallel()

		broken := errors.New("no Bitbucket host is configured")
		err := run(Dependencies{
			LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
				return config.AppConfig{}, nil, broken
			},
		}, "--repo", "PRJ/demo")
		if !errors.Is(err, broken) {
			t.Fatalf("got %v, want the configuration failure", err)
		}
	})

	t.Run("a repository that is not PROJECT/slug", func(t *testing.T) {
		t.Parallel()

		err := run(Dependencies{
			LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
				client, err := openapigenerated.NewClientWithResponses(testsupport.RefusedURL)
				return config.AppConfig{BitbucketURL: testsupport.RefusedURL}, client, err
			},
		}, "--repo", "not-a-selector")
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("got %v, want a validation error for the selector", err)
		}
	})
}

func decodeRepository(t *testing.T, body string) openapigenerated.RestRepository {
	t.Helper()

	var upstream openapigenerated.RestRepository
	if err := json.Unmarshal([]byte(body), &upstream); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return upstream
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plainText is the output without styling, which a terminal may or may not
// have been given.
func plainText(output bytes.Buffer) string {
	return ansiEscape.ReplaceAllString(output.String(), "")
}
