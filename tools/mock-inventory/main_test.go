package main

import (
	"os"
	"path/filepath"
	"strings"
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

// A retry test is not a Bitbucket simulation, and reading it as one would send
// it to a live suite that cannot express it: no real server can be asked to
// fail twice and then succeed. It serves a route and returns statuses like any
// other mock, so the thing that tells them apart is answering with a status the
// client is meant to retry, on a handler that counts its calls.
//
// Counting alone is not enough. A paging mock counts calls too, and Bitbucket's
// paging convention is a claim about Bitbucket.
func TestFaultInjectionIsToldApartFromPaging(t *testing.T) {
	retry := preamble + `func TestRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
}`

	paging := preamble + `func TestPages(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/latest/projects" {
			http.NotFound(w, r)
			return
		}
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte("{\"values\":[1],\"isLastPage\":false,\"nextPageStart\":1}"))
			return
		}
		_, _ = w.Write([]byte("{\"values\":[2],\"isLastPage\":true}"))
	}))
	defer server.Close()
}`

	if got := classOf(t, retry); got != ClassTransportFault {
		t.Errorf("a retry sequence classified %q, want %q", got, ClassTransportFault)
	}
	if got := classOf(t, paging); got != ClassBehaviour {
		t.Errorf("a paging sequence classified %q, want %q", got, ClassBehaviour)
	}
}

// A retry mock does not have to count with ++. atomic.Int32.Add is the same
// intent, and missing it left the transport package's retry tests queued for a
// live suite that cannot express them: no real server can be asked to fail
// twice and then succeed.
func TestAnAtomicCounterCountsAttemptsToo(t *testing.T) {
	source := preamble + `func TestRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
}`

	if got := classOf(t, source); got != ClassTransportFault {
		t.Errorf("class = %q, want %q", got, ClassTransportFault)
	}
}

// A directive in the test overrides the classifier, and has to say why.
//
// The classifier reads signals, and a retry test leaves the same ones a routing
// test leaves: serve a path, answer a status. It has already been widened twice
// in that direction, and each widening risks a wrong answer somewhere it was
// previously right. A reviewed judgement recorded at the site ends that, but
// only if it cannot be used to quietly silence an entry -- so an override
// without a stated reason is ignored.
func TestAReviewedDirectiveOverridesTheClassifier(t *testing.T) {
	const behaviourMock = preamble + `
func TestSomething(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/latest/projects" {
			_, _ = w.Write([]byte(` + "`" + `{"values":[],"isLastPage":true}` + "`" + `))
		}
	}))
	defer server.Close()
}
`

	if class := classOf(t, behaviourMock); class != ClassBehaviour {
		t.Fatalf("the sample must classify as behaviour before the override means anything, got %q", class)
	}

	cases := []struct {
		name      string
		directive string
		want      Class
	}{
		{
			name:      "an em dash separator",
			directive: "// mock-inventory: transport-fault — the failure is injected below the API\n",
			want:      ClassTransportFault,
		},
		{
			name:      "a plain hyphen separator",
			directive: "// mock-inventory: transport-fault - the failure is injected below the API\n",
			want:      ClassTransportFault,
		},
		{
			name:      "no reason given",
			directive: "// mock-inventory: transport-fault\n",
			want:      ClassBehaviour,
		},
		{
			name:      "a class that does not exist",
			directive: "// mock-inventory: not-a-class — because I say so\n",
			want:      ClassBehaviour,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := insertDirective(behaviourMock, testCase.directive)
			if class := classOf(t, source); class != testCase.want {
				t.Fatalf("class = %q, want %q", class, testCase.want)
			}
		})
	}
}

// The override's reason replaces the generated one, so the task list says what
// the person said rather than what the classifier would have.
func TestAReviewedDirectiveCarriesItsReason(t *testing.T) {
	const reason = "the failure is injected below the API"
	source := insertDirective(preamble+`
func TestSomething(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/latest/projects" {
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()
}
`, "// mock-inventory: transport-fault — "+reason+"\n")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	entries, err := scan(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("scan: %v (%d entries)", err, len(entries))
	}

	if !entries[0].Reviewed {
		t.Error("expected the entry to be marked reviewed")
	}
	if entries[0].Reason != reason {
		t.Errorf("reason = %q, want %q", entries[0].Reason, reason)
	}
}

func insertDirective(source, directive string) string {
	return strings.Replace(source, "func TestSomething(", directive+"func TestSomething(", 1)
}

// TestTheSharedGuardIsRecognised covers the handler that is a call rather than
// a literal.
//
// handlerFailsTheTest looks inside a func literal, and testsupport.
// UnreachedHandler has none to look inside. Without this the shared helper
// reads as a handler supplied by the caller, and the whole point of moving
// eleven hand-written guards onto it was that they say the same thing --
// including to this tool.
func TestTheSharedGuardIsRecognised(t *testing.T) {
	source := preamble + `func TestX(t *testing.T) {
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	defer server.Close()
}`

	if got := classOf(t, source); got != ClassUnreachedGuard {
		t.Errorf("class = %q, want %q", got, ClassUnreachedGuard)
	}
}

// A guard on one listener does not make the file's other mocks guards. The
// same rule handlerFailsTheTest applies to literals has to hold for the shared
// helper, or a serving mock beside a guarded one would be waved through.
func TestOneSharedGuardDoesNotCoverAServingMock(t *testing.T) {
	source := preamble + `func TestX(t *testing.T) {
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	defer guard.Close()

	serving := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/latest/projects/PRJ" {
			_, _ = w.Write([]byte("{\"key\":\"PRJ\"}"))
		}
	}))
	defer serving.Close()
}`

	for index, got := range classesOf(t, source) {
		if got != ClassBehaviour {
			t.Errorf("mock %d class = %q, want %q", index, got, ClassBehaviour)
		}
	}
}

// classesOf is classOf for a source with more than one listener in it.
func classesOf(t *testing.T, source string) []Class {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	entries, err := scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no mocks found in a source that opens two listeners")
	}

	classes := make([]Class, 0, len(entries))
	for _, entry := range entries {
		classes = append(classes, entry.Class)
	}

	return classes
}
