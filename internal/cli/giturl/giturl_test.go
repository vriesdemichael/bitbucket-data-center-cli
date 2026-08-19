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
