package repocmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestCloneURLsFromReadsTheCloneLinks(t *testing.T) {
	t.Parallel()

	// The links object as the live instance sent it for a new repository.
	const body = `{"slug":"probe","links":{"clone":[` +
		`{"href":"ssh://git@127.0.0.1:7999/probe602x/probe.git","name":"ssh"},` +
		`{"href":"http://127.0.0.1:7990/scm/probe602x/probe.git","name":"http"}],` +
		`"self":[{"href":"http://127.0.0.1:7990/projects/PROBE602X/repos/probe/browse"}]}}`

	var upstream openapigenerated.RestRepository
	if err := json.Unmarshal([]byte(body), &upstream); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []CloneURL{
		{Name: "ssh", URL: "ssh://git@127.0.0.1:7999/probe602x/probe.git"},
		{Name: "http", URL: "http://127.0.0.1:7990/scm/probe602x/probe.git"},
	}
	if got := cloneURLsFrom(upstream.Links); !reflect.DeepEqual(got, want) {
		t.Fatalf("clone URLs = %+v, want %+v", got, want)
	}

	if got := cloneURLsFrom(nil); got == nil || len(got) != 0 {
		t.Errorf("no links must give an empty list rather than nil, got %#v", got)
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
		if !strings.Contains(withReadme.String(), want) {
			t.Errorf("output is missing %q:\n%s", want, withReadme.String())
		}
	}

	var without bytes.Buffer
	writeRepositoryView(&without, view, nil, false, true)
	if !strings.Contains(without.String(), "No README") {
		t.Errorf("a repository without a README must say so:\n%s", without.String())
	}

	var skipped bytes.Buffer
	writeRepositoryView(&skipped, view, nil, false, false)
	if strings.Contains(skipped.String(), "README") {
		t.Errorf("--readme=false must leave the README section out:\n%s", skipped.String())
	}
}
