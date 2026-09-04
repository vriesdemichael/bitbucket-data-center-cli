package reviewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestNormalizeRefID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "main", want: "refs/heads/main"},
		{in: "feature/x", want: "refs/heads/feature/x"},
		{in: "  release/1.0  ", want: "refs/heads/release/1.0"},
		{in: "refs/heads/main", want: "refs/heads/main"},
		{in: "refs/tags/v1", want: "refs/tags/v1"},
		{in: "", want: ""},
		{in: "   ", want: ""},
	}

	for _, testCase := range tests {
		if got := NormalizeRefID(testCase.in); got != testCase.want {
			t.Fatalf("NormalizeRefID(%q) = %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

// A group whose membership cannot be read is not an empty group. Reporting it as
// empty made the caller quietly assign nobody.
func TestResolveReviewerGroupUsersSurfacesMembershipFailure(t *testing.T) {
	t.Run("repository group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
				_, _ = w.Write([]byte(`{"values":[{"id":10,"name":"core-team"}]}`))
			case strings.HasSuffix(r.URL.Path, "/reviewer-groups/10/users"):
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")

		users, err := NewService(client).ResolveReviewerGroupUsers(context.Background(), "PRJ", "demo", "core-team")
		if err == nil {
			t.Fatalf("expected an error, got users %v", users)
		}
		if !strings.Contains(err.Error(), "core-team") {
			t.Fatalf("error should name the group, got %v", err)
		}
	})

	t.Run("project group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
				_, _ = w.Write([]byte(`{"values":[{"id":20,"name":"arch-team"}]}`))
			case r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/20":
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")

		users, err := NewService(client).ResolveReviewerGroupUsers(context.Background(), "PRJ", "", "arch-team")
		if err == nil {
			t.Fatalf("expected an error, got users %v", users)
		}
		if !strings.Contains(err.Error(), "arch-team") {
			t.Fatalf("error should name the group, got %v", err)
		}
	})
}

func TestRepositoryIDValidation(t *testing.T) {
	client, _ := openapigenerated.NewClientWithResponses("http://example.invalid/rest")
	service := NewService(client)

	if _, err := service.RepositoryID(context.Background(), "", "demo"); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected a validation error for a missing project key, got %v", err)
	}
	if _, err := service.RepositoryID(context.Background(), "PRJ", ""); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected a validation error for a missing repository slug, got %v", err)
	}
}

func TestRepositoryID(t *testing.T) {
	t.Run("returns the numeric id", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":77,"slug":"demo"}`))
		}))
		defer server.Close()

		client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")

		id, err := NewService(client).RepositoryID(context.Background(), "PRJ", "demo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "77" {
			t.Fatalf("id = %q, want %q", id, "77")
		}
	})

	t.Run("errors when the response carries no id", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"slug":"demo"}`))
		}))
		defer server.Close()

		client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")

		if _, err := NewService(client).RepositoryID(context.Background(), "PRJ", "demo"); err == nil {
			t.Fatal("expected an error when the repository response has no id")
		}
	})
}

func TestSelectMembers(t *testing.T) {
	members := []string{"alice", "bob", "carol", "dave"}
	busy := map[string]int{"alice": 5, "bob": 0, "carol": 3, "dave": 1}

	t.Run("excludes the author regardless of case", func(t *testing.T) {
		got := SelectMembers(members, "ALICE", "all", 0, nil)
		for _, name := range got {
			if strings.EqualFold(name, "alice") {
				t.Fatalf("author should have been excluded, got %v", got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("got %v, want the three non-author members", got)
		}
	})

	t.Run("a count of zero selects everyone", func(t *testing.T) {
		if got := SelectMembers(members, "", "random", 0, nil); len(got) != 4 {
			t.Fatalf("got %v, want all four members", got)
		}
	})

	t.Run("least_busy picks the lowest counts", func(t *testing.T) {
		got := SelectMembers(members, "", "least_busy", 2, busy)
		want := []string{"bob", "dave"}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("least_busy without counts keeps group order", func(t *testing.T) {
		got := SelectMembers(members, "", "least_busy", 2, nil)
		if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
			t.Fatalf("got %v, want [alice bob]", got)
		}
	})

	t.Run("random returns the requested number of distinct members", func(t *testing.T) {
		got := SelectMembers(members, "", "random", 2, nil)
		if len(got) != 2 {
			t.Fatalf("got %v, want two members", got)
		}
		if got[0] == got[1] {
			t.Fatalf("got duplicate members: %v", got)
		}
		for _, name := range got {
			if !strings.Contains(strings.Join(members, ","), name) {
				t.Fatalf("got unknown member %q", name)
			}
		}
	})
}

// TestResolveReviewerGroupUsersStripsReviewerGroupPrefix is #503.
//
// Three callers pass a group token through verbatim -- a CODEOWNERS entry,
// --reviewers @<name> and --reviewer-group. All three accept Bitbucket's own
// "reviewer-group/<name>" spelling, and all three used to carry the prefix into
// the lookup, which then found nothing.
func TestResolveReviewerGroupUsersStripsReviewerGroupPrefix(t *testing.T) {
	for _, token := range []string{
		"@reviewer-group/cog_product",
		"reviewer-group/cog_product",
		"cog_product",
	} {
		t.Run(token, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
					_, _ = w.Write([]byte(`{"values":[{"id":10,"name":"cog_product"}]}`))
				case "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/10/users":
					_, _ = w.Write([]byte(`[{"name":"alice"}]`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")

			users, err := NewService(client).ResolveReviewerGroupUsers(context.Background(), "PRJ", "demo", token)
			if err != nil {
				t.Fatalf("resolving %q: %v", token, err)
			}
			if len(users) != 1 || users[0] != "alice" {
				t.Fatalf("resolving %q gave %v, want [alice]", token, users)
			}
		})
	}

	// The prefix on its own names no group, and must not resolve to every
	// group or to the empty name silently.
	t.Run("prefix alone is rejected", func(t *testing.T) {
		if _, err := NewService(nil).ResolveReviewerGroupUsers(context.Background(), "PRJ", "demo", "@reviewer-group/"); err == nil {
			t.Fatal("expected a validation error for a bare prefix")
		}
	})
}
