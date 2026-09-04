package api

import (
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
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
			response := &httpclient.RawResponse{Header: http.Header{}}
			if testCase.contentType != "" {
				response.Header.Set("Content-Type", testCase.contentType)
			}

			err := htmlResponseError(response, testCase.path)

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

	t.Run("no response at all", func(t *testing.T) {
		if err := htmlResponseError(nil, "/rest/api/1.0/projects"); err != nil {
			t.Fatalf("expected a nil response to produce no error, got: %v", err)
		}
	})
}
