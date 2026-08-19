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
		{"git@bitbucket.example.com:PROJ/repo.git", "bitbucket.example.com", "PROJ", "repo", true},
		{"ssh://git@bitbucket.example.com:7999/PROJ/repo.git", "bitbucket.example.com", "PROJ", "repo", true},
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

func TestBuildBitbucketCloneURL(t *testing.T) {
	url, err := BuildBitbucketCloneURL("https://bitbucket.example.com", "PROJ", "my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://bitbucket.example.com/scm/PROJ/my-repo.git"
	if url != want {
		t.Fatalf("got %q, want %q", url, want)
	}
}

func TestIsNonRepositoryError(t *testing.T) {
	if !IsNonRepositoryError(errors.New("fatal: not a git repository (or any of the parent directories): .git")) {
		t.Fatalf("expected true for not a git repository")
	}
	if IsNonRepositoryError(errors.New("something else")) {
		t.Fatalf("expected false for other error")
	}
}
