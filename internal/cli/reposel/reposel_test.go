package reposel

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantProject string
		wantSlug    string
		wantErr     bool
	}{
		{"valid selector", "PROJ/my-repo", "PROJ", "my-repo", false},
		{"with spaces", " PROJ / my-repo ", "PROJ", "my-repo", false},
		{"browser url", "https://bitbucket.example.com/projects/PROJ/repos/my-repo", "PROJ", "my-repo", false},
		{"browser url with browse", "https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse", "PROJ", "my-repo", false},
		{"user personal repo url", "https://bitbucket.example.com/users/jdoe/repos/my-repo", "~jdoe", "my-repo", false},
		{"git clone url", "https://bitbucket.example.com/scm/PROJ/my-repo.git", "PROJ", "my-repo", false},
		{"ssh clone url", "ssh://git@bitbucket.example.com:7999/PROJ/my-repo.git", "PROJ", "my-repo", false},
		{"missing project", "/my-repo", "", "", true},
		{"missing slug", "PROJ/", "", "", true},
		{"no slash", "PROJ", "", "", true},
		{"extra parts", "A/B/C", "", "", true},
		{"empty", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, slug, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if proj != tt.wantProject || slug != tt.wantSlug {
					t.Fatalf("Parse(%q) = (%q, %q), want (%q, %q)", tt.input, proj, slug, tt.wantProject, tt.wantSlug)
				}
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Run("explicit selector wins", func(t *testing.T) {
		proj, slug, err := Resolve("FOO/bar", config.AppConfig{ProjectKey: "BAZ"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if proj != "FOO" || slug != "bar" {
			t.Fatalf("got (%q, %q), want (FOO, bar)", proj, slug)
		}
	})

	t.Run("falls back to config and env", func(t *testing.T) {
		_ = os.Setenv("BITBUCKET_REPO_SLUG", "env-slug")
		defer os.Unsetenv("BITBUCKET_REPO_SLUG")

		proj, slug, err := Resolve("", config.AppConfig{ProjectKey: "ENVPROJ"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if proj != "ENVPROJ" || slug != "env-slug" {
			t.Fatalf("got (%q, %q), want (ENVPROJ, env-slug)", proj, slug)
		}
	})

	t.Run("missing both returns validation error", func(t *testing.T) {
		_ = os.Unsetenv("BITBUCKET_REPO_SLUG")
		_, _, err := Resolve("", config.AppConfig{ProjectKey: ""})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error kind, got %v", apperrors.KindOf(err))
		}
	})
}

func FuzzParse(f *testing.F) {
	// Seed initial corpus
	f.Add("PROJ/repo")
	f.Add("PROJ/sub/repo")
	f.Add("/repo")
	f.Add("PROJ/")
	f.Add("")
	f.Add("   ")
	f.Add("PROJECT_KEY/my-awesome-repo")

	f.Fuzz(func(t *testing.T, input string) {
		proj, slug, err := Parse(input)
		if err == nil {
			if proj == "" || slug == "" {
				t.Fatalf("Parse(%q) returned empty components on success: proj=%q, slug=%q", input, proj, slug)
			}
			if len(proj) > len(input) || len(slug) > len(input) {
				t.Fatalf("Parse(%q) returned component larger than input: proj=%q, slug=%q", input, proj, slug)
			}
		}
	})
}
