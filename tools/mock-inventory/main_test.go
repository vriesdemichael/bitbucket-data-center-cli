package main

import (
	"os"
	"path/filepath"
	"testing"
)

// classOf runs the scanner over one synthetic test file and returns the class
// it assigned.
func classOf(t *testing.T, source string) Class {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	entries, err := scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one mock, got %d: %#v", len(entries), entries)
	}

	return entries[0].Class
}

const preamble = `package sample

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

`

// The classifier decides where a test has to live, so each class is pinned to
// the shape that earns it. Getting any of these wrong either hides an
// integration assumption in the unit suite or moves a legitimate unit test out
// of it.
func TestClassification(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
		want Class
	}{
		{
			// Routing on a Bitbucket path is the plainest statement that the
			// mock is standing in for the server.
			name: "serves a Bitbucket route",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo" {
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()
}`,
			want: ClassBehaviour,
		},
		{
			// Reading the request body is asserting what the server accepts,
			// which is the assumption that produced every defect in the sweep.
			name: "asserts the request body",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
}`,
			want: ClassBehaviour,
		},
		{
			// Never looks at the request. The call could be wrong in every
			// particular and this still says yes.
			name: "answers every request the same way",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"status\":\"ok\"}"))
	}))
	defer server.Close()
}`,
			want: ClassCannedResponse,
		},
		{
			// A bare status is still a claim that Bitbucket answers this code
			// here, so it needs live proof -- but it is a different claim from
			// a payload.
			name: "returns a bare status",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
}`,
			want: ClassStatusTaxonomy,
		},
		{
			// The handler exists to prove nothing is sent. Nothing about
			// Bitbucket is assumed because nothing is expected to arrive.
			name: "fails if it is reached",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached: %s", r.URL.Path)
	}))
	defer server.Close()
}`,
			want: ClassUnreachedGuard,
		},
		{
			// http.Error writes a response. Reading it as a test failure would
			// clear a behaviour mock as a guard, which is the direction that
			// hides things.
			name: "http.Error is a response, not a test failure",
			body: `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
}`,
			want: ClassCannedResponse,
		},
		{
			// Opens a listener around a handler it was handed. It states
			// nothing; the caller does.
			name: "harness constructor",
			body: `func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}`,
			want: ClassHarnessConstructor,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classOf(t, preamble+testCase.body); got != testCase.want {
				t.Errorf("class = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A routed mock that also fails on its default branch is an ordinary mock with
// a safety net, not an assertion that nothing is sent. Reading it as a guard
// cleared eight behaviour mocks in one file while the inventory was being
// built.
func TestAServingHandlerIsNotAGuard(t *testing.T) {
	source := preamble + `func TestX(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte("{\"key\":\"PRJ\"}"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
}`

	if got := classOf(t, source); got != ClassBehaviour {
		t.Errorf("class = %q, want %q", got, ClassBehaviour)
	}
}

// Nothing may be left unexamined: an unclassified mock is one nobody has
// decided about, and the whole point of the inventory is that there is no such
// pile.
func TestTheRepositoryHasNoUnclassifiedMocks(t *testing.T) {
	entries, err := scan("../../internal")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no mocks found; has the scanner stopped working?")
	}

	for _, item := range entries {
		if item.Class == ClassUnclear {
			t.Errorf("%s:%d %s is unclassified", item.File, item.Line, item.Function)
		}
	}
}
