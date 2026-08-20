package giturl

import (
	"errors"
	"testing"
)

func TestParseBitbucketRemote(t *testing.T) {
	tests := []struct {
		url      string
		wantHost string
		wantProj string
		wantSlug string
		wantOK   bool
	}{
		{"https://bitbucket.example.com/scm/PROJ/repo.git", "bitbucket.example.com", "PROJ", "repo", true},
		{"http://localhost:7990/bitbucket/scm/PROJ/repo.git", "localhost", "PROJ", "repo", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo", "bitbucket.example.com", "PROJ", "repo", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/browse", "bitbucket.example.com", "PROJ", "repo", true},
		{"http://localhost:7990/bitbucket/projects/PROJ/repos/repo/browse/path/to/file", "localhost", "PROJ", "repo", true},
		{"https://bitbucket.example.com/users/jdoe/repos/my-repo", "bitbucket.example.com", "~jdoe", "my-repo", true},
		{"https://bitbucket.example.com/users/jdoe/repos/my-repo/browse", "bitbucket.example.com", "~jdoe", "my-repo", true},
		{"git@bitbucket.example.com:PROJ/repo.git", "bitbucket.example.com", "PROJ", "repo", true},
		{"ssh://git@bitbucket.example.com:7999/PROJ/repo.git", "bitbucket.example.com", "PROJ", "repo", true},
		{"git@bitbucket.example.com:no-colon", "", "", "", false},
		{"not-a-valid-url", "", "", "", false},
		{"", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			host, proj, slug, ok := ParseBitbucketRemote(tt.url)
			if ok != tt.wantOK {
				t.Fatalf("ParseBitbucketRemote(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}
			if ok {
				if host != tt.wantHost || proj != tt.wantProj || slug != tt.wantSlug {
					t.Fatalf("ParseBitbucketRemote(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.url, host, proj, slug, tt.wantHost, tt.wantProj, tt.wantSlug)
				}
			}
		})
	}
}

func TestParseBitbucketPR(t *testing.T) {
	tests := []struct {
		url      string
		wantHost string
		wantProj string
		wantSlug string
		wantID   string
		wantOK   bool
	}{
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42/overview", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42/diff", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"http://localhost:7990/bitbucket/projects/PROJ/repos/repo/pull-requests/101", "localhost", "PROJ", "repo", "101", true},
		{"https://bitbucket.example.com/users/jdoe/repos/my-repo/pull-requests/7", "bitbucket.example.com", "~jdoe", "my-repo", "7", true},
		{"https://bitbucket.example.com/users/jdoe/repos/my-repo/pull-requests/7/overview", "bitbucket.example.com", "~jdoe", "my-repo", "7", true},
		{"https://bitbucket.example.com/projects/~jdoe/repos/my-repo/pull-requests/99", "bitbucket.example.com", "~jdoe", "my-repo", "99", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42?commentId=101#comment-101", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42?commentId=101", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"/projects/PROJ/repos/repo/pull-requests/42", "", "PROJ", "repo", "42", true},
		{"bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42", "bitbucket.example.com", "PROJ", "repo", "42", true},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/not-a-number", "", "", "", "", false},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests", "", "", "", "", false},
		{"https://bitbucket.example.com/users/jdoe/repos/my-repo/pull-requests", "", "", "", "", false},
		{"https://bitbucket.example.com/projects/PROJ/repos/repo", "", "", "", "", false},
		{"", "", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			host, proj, slug, id, ok := ParseBitbucketPR(tt.url)
			if ok != tt.wantOK {
				t.Fatalf("ParseBitbucketPR(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}
			if ok {
				if host != tt.wantHost || proj != tt.wantProj || slug != tt.wantSlug || id != tt.wantID {
					t.Fatalf("ParseBitbucketPR(%q) = (%q, %q, %q, %q), want (%q, %q, %q, %q)", tt.url, host, proj, slug, id, tt.wantHost, tt.wantProj, tt.wantSlug, tt.wantID)
				}
			}
		})
	}
}

func TestBuildBitbucketCloneURL(t *testing.T) {
	url, err := BuildBitbucketCloneURL("https://bitbucket.example.com", "PROJ", "my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://bitbucket.example.com/scm/PROJ/my-repo.git"
	if url != want {
		t.Fatalf("got %q, want %q", url, want)
	}

	// Invalid baseURL
	if _, err := BuildBitbucketCloneURL("invalid-url", "PROJ", "my-repo"); err == nil {
		t.Fatal("expected error for invalid base URL")
	}

	// Empty project or slug
	if _, err := BuildBitbucketCloneURL("https://bitbucket.example.com", "", "my-repo"); err == nil {
		t.Fatal("expected error for empty project")
	}
	if _, err := BuildBitbucketCloneURL("https://bitbucket.example.com", "PROJ", ""); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestNormalizeHTTPCloneHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://bitbucket.example.com/prefix/scm/PROJ/repo.git", "http://bitbucket.example.com"},
		{"https://user:token@bitbucket.example.com:7990/context", "https://bitbucket.example.com:7990"},
		{"ssh://bitbucket.example.com", "https://bitbucket.example.com"},
		{"invalid host without scheme or colon", "invalid host without scheme or colon"},
	}

	for _, tt := range tests {
		got := NormalizeHTTPCloneHost(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeHTTPCloneHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNonRepositoryError(t *testing.T) {
	if !IsNonRepositoryError(errors.New("fatal: not a git repository (or any of the parent directories): .git")) {
		t.Fatalf("expected true for not a git repository")
	}
	if !IsNonRepositoryError(errors.New("fatal: this operation must be run in a work tree")) {
		t.Fatalf("expected true for must be run in a work tree")
	}
	if IsNonRepositoryError(errors.New("something else")) {
		t.Fatalf("expected false for other error")
	}
	if IsNonRepositoryError(nil) {
		t.Fatalf("expected false for nil error")
	}
}

func FuzzParseBitbucketRemote(f *testing.F) {
	f.Add("https://bitbucket.example.com/scm/PROJ/repo.git")
	f.Add("http://localhost:7990/bitbucket/scm/PROJ/repo.git")
	f.Add("git@bitbucket.example.com:PROJ/repo.git")
	f.Add("ssh://git@bitbucket.example.com:7999/PROJ/repo.git")
	f.Add("invalid")
	f.Add("")

	f.Fuzz(func(t *testing.T, rawURL string) {
		host, proj, slug, ok := ParseBitbucketRemote(rawURL)
		if ok {
			if host == "" || proj == "" || slug == "" {
				t.Fatalf("ParseBitbucketRemote(%q) returned ok=true with empty fields: host=%q, proj=%q, slug=%q", rawURL, host, proj, slug)
			}
		}
	})
}

func FuzzParseBitbucketPath(f *testing.F) {
	f.Add("/scm/PROJ/repo.git")
	f.Add("/bitbucket/scm/PROJ/repo.git")
	f.Add("PROJ/repo.git")
	f.Add("/projects/PROJ/repos/repo/browse")
	f.Add("")

	f.Fuzz(func(t *testing.T, path string) {
		proj, slug, ok := ParseBitbucketPath(path)
		if ok {
			if proj == "" || slug == "" {
				t.Fatalf("ParseBitbucketPath(%q) returned ok=true with empty fields: proj=%q, slug=%q", path, proj, slug)
			}
		}
	})
}
