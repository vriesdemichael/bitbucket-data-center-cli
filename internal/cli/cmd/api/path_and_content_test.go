package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Two decisions `bb api` makes before and after the request, taken over their
// inputs rather than through a server.
//
// The versions these replace ran the whole command against a mock and read the
// answer off the request the mock had just received. That put a socket between
// a string function and its assertion, and it left the interesting half
// unasked: whether the path the sanitiser produces is one Bitbucket actually
// serves. TestLiveAPIMangledPathReachesTheEndpoint asks that, against the
// server.

// A path mangled by MSYS2 has to be recovered, because the shell rewrites the
// argument before bb ever sees it and the user did nothing wrong.
func TestSanitizeMangledPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		want      string
		wantFixed bool
	}{
		{
			name:      "msys2 drive with program files git prefix",
			input:     "/C:/Program Files/Git/rest/api/1.0/projects/PROJ/repos/repo",
			want:      "/rest/api/1.0/projects/PROJ/repos/repo",
			wantFixed: true,
		},
		{
			name:      "windows backslash with git prefix",
			input:     `C:\Program Files\Git\rest\api\1.0\projects\PROJ`,
			want:      "/rest/api/1.0/projects/PROJ",
			wantFixed: true,
		},
		{
			name:      "short msys drive prefix",
			input:     "/c/rest/api/1.0/users",
			want:      "/rest/api/1.0/users",
			wantFixed: true,
		},
		{
			name:      "custom plugin with drive letter",
			input:     "/C:/plugins/servlet/custom",
			want:      "/plugins/servlet/custom",
			wantFixed: true,
		},
		// The recovery must not eat real endpoints. `bb api` reaches plugin
		// paths that contain "/rest/" partway through, and a heuristic matching
		// on words like "git" truncated them.
		{name: "a plain rest path", input: "/rest/api/1.0/projects", want: "/rest/api/1.0/projects"},
		{name: "a rest path without its leading slash", input: "rest/api/1.0/projects", want: "rest/api/1.0/projects"},
		{name: "an absolute url", input: "https://bitbucket.example.com/rest/api/1.0/projects", want: "https://bitbucket.example.com/rest/api/1.0/projects"},
		{name: "a plugin path with rest inside it", input: "/rest/git-lfs/admin/projects/PROJ", want: "/rest/git-lfs/admin/projects/PROJ"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, fixed := sanitizeMangledPath(testCase.input)

			if got != testCase.want {
				t.Errorf("sanitizeMangledPath(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
			if fixed != testCase.wantFixed {
				t.Errorf("sanitizeMangledPath(%q) reported fixed=%v, want %v", testCase.input, fixed, testCase.wantFixed)
			}
		})
	}
}

// HTML where JSON was expected means the request was answered by a login page
// or a proxy rather than by the REST API, and saying "invalid JSON" would send
// the caller looking in the wrong place.
//
// This is not a claim about Bitbucket: a real instance answers /rest with JSON,
// and the case arises when something in front of it does not. What is being
// pinned is that bb reads the situation correctly when it happens.
func TestHTMLResponseError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		path        string
		contentType string
		wantError   bool
	}{
		{name: "login page on a rest path", path: "/rest/api/1.0/projects/PRJ", contentType: "text/html;charset=UTF-8", wantError: true},
		{name: "rest path without its leading slash", path: "rest/api/1.0/projects", contentType: "text/html", wantError: true},
		// Plugin and servlet endpoints legitimately render HTML.
		{name: "html outside the rest api", path: "/plugins/servlet/custom", contentType: "text/html;charset=UTF-8"},
		{name: "json on a rest path", path: "/rest/api/1.0/projects", contentType: "application/json"},
		{name: "no content type at all", path: "/rest/api/1.0/projects", contentType: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			header := http.Header{}
			if testCase.contentType != "" {
				header.Set("Content-Type", testCase.contentType)
			}

			err := htmlResponseError(header, testCase.path)

			if !testCase.wantError {
				if err != nil {
					t.Fatalf("expected no error for %s, got: %v", testCase.name, err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected an authentication error")
			}
			if !apperrors.IsKind(err, apperrors.KindAuthentication) {
				t.Errorf("kind = %v, want authentication (%v)", apperrors.KindOf(err), err)
			}
			// The message has to name the content type, or the caller cannot
			// tell this from a malformed payload.
			if !strings.Contains(err.Error(), "text/html") {
				t.Errorf("the message does not name what came back instead: %v", err)
			}
		})
	}

	t.Run("no header at all", func(t *testing.T) {
		if err := htmlResponseError(nil, "/rest/api/1.0/projects"); err != nil {
			t.Fatalf("expected a missing header to produce no error, got: %v", err)
		}
	})
}

// TestABodyIsTextOnlyWhenItsHeaderAndItsBytesSaySo is the rule that decides
// what bb api formats and what it writes byte for byte.
func TestABodyIsTextOnlyWhenItsHeaderAndItsBytesSaySo(t *testing.T) {
	t.Parallel()

	notUTF8 := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff}
	for name, testCase := range map[string]struct {
		contentType string
		body        []byte
		want        bool
	}{
		"json":                       {"application/json;charset=UTF-8", []byte(`{"a":1}`), true},
		"a json vendor type":         {"application/vnd.api+json", []byte(`{}`), true},
		"xml":                        {"application/xml", []byte("<a/>"), true},
		"an xml vendor type":         {"application/atom+xml", []byte("<feed/>"), true},
		"plain text":                 {"text/plain; charset=utf-8", []byte("hello\n"), true},
		"any text type":              {"text/csv", []byte("a,b\n"), true},
		"text that is not utf-8":     {"text/plain", notUTF8, false},
		"an image":                   {"image/png", notUTF8, false},
		"octet-stream that is ascii": {"application/octet-stream", []byte("  just ascii  \n"), false},
		"a zip":                      {"application/zip", []byte("PK"), false},
		"no type, utf-8":             {"", []byte(`{"a":1}`), true},
		"no type, not utf-8":         {"", notUTF8, false},
		"an unreadable type, utf-8":  {"not a type;;", []byte("text"), true},
	} {
		header := http.Header{}
		if testCase.contentType != "" {
			header.Set("Content-Type", testCase.contentType)
		}
		if got := textual(header, testCase.body); got != testCase.want {
			t.Errorf("%s: textual = %v, want %v", name, got, testCase.want)
		}
	}
}

// TestABodyThatIsNotTextIsWrittenExactly: bb api trimmed every body and ended
// it with a newline, so a file fetched through it came back changed. Text is
// still formatted that way; anything else is written byte for byte.
func TestABodyThatIsNotTextIsWrittenExactly(t *testing.T) {
	t.Parallel()

	human := Dependencies{JSONEnabled: func() bool { return false }}
	binary := []byte{'\n', 0x89, 'P', 'N', 'G', '\r', '\n', 0x00, 0xff, ' ', '\n'}

	var written bytes.Buffer
	if err := writeResponse(&written, http.Header{"Content-Type": {"application/octet-stream"}}, binary, human); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(written.Bytes(), binary) {
		t.Fatalf("a binary body came out as %q, want %q exactly", written.Bytes(), binary)
	}

	written.Reset()
	if err := writeResponse(&written, http.Header{"Content-Type": {"text/plain"}}, []byte("\n  hello  \n\n"), human); err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.String() != "hello\n" {
		t.Fatalf("a text body came out as %q, want it trimmed and ended with a newline", written.String())
	}

	written.Reset()
	if err := writeResponse(&written, http.Header{"Content-Type": {"application/json"}}, []byte(`{"a":1}`), human); err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("a JSON body came out as %q, want it indented", written.String())
	}
}
