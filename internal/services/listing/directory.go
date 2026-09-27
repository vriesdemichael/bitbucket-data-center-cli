package listing

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// DirectoryEntries is how many entries of one directory a listing asks for.
//
// Large, because nothing else narrows it. Bitbucket's directory listing takes
// no filter -- a filterText is ignored rather than refused -- so the directory
// in the path is the whole of the query, and the letters typed after the last
// slash can only be matched against what the page already holds. 500 is
// Bitbucket's own default for this listing and more entries than any directory
// somebody completes inside; it is sent rather than inherited, because a
// default is the server's to change.
const DirectoryEntries = 500

// Entry is one child of a directory: its name within the directory, and
// whether it is a directory itself.
type Entry struct {
	Name      string
	Directory bool
}

// Directory asks Bitbucket for the children of one directory at a ref, the
// repository's default branch when at is empty.
//
// The browse endpoint rather than the file listing, which is the difference
// between reading a directory and walking a tree: /files answers with every
// path beneath the one it is given, recursively, which for a repository of any
// size is both the slowest answer available and the least useful. /browse
// answers with the children of that path alone, and takes the page size the
// caller can use.
func Directory(
	ctx context.Context,
	client *httpclient.Client,
	projectKey string,
	slug string,
	at string,
	directory string,
	maxResults int,
) ([]Entry, error) {
	encoded, err := encodeDirectory(directory)
	if err != nil {
		return nil, err
	}

	query := map[string]string{"limit": strconv.Itoa(maxResults)}
	if trimmed := strings.TrimSpace(at); trimmed != "" {
		query["at"] = trimmed
	}

	var response browseResponse
	if err := client.GetJSON(ctx, browsePath(projectKey, slug, encoded), query, &response); err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(response.Children.Values))
	for _, child := range response.Children.Values {
		// The child's path is relative to the directory being browsed, which
		// is the half of it the caller does not already have.
		entries = append(entries, Entry{
			Name:      strings.TrimSpace(child.Path.ToString),
			Directory: strings.EqualFold(strings.TrimSpace(child.Type), "DIRECTORY"),
		})
	}

	return entries, nil
}

// browseResponse is the part of the browse endpoint's answer a directory
// listing needs.
//
// A file path answers with its lines instead of its children, which decodes
// here as no children at all -- correct, and the reason nothing checks first
// whether the path is a directory.
type browseResponse struct {
	Children struct {
		Values []struct {
			Path struct {
				ToString string `json:"toString"`
			} `json:"path"`
			Type string `json:"type"`
		} `json:"values"`
	} `json:"children"`
}

// browsePath is the endpoint for a directory, with the directory already
// escaped.
//
// Kept as a single fmt.Sprintf return so tools/quality-report can resolve the
// endpoints reached through the raw httpclient statically; internal/services
// builds the same path the same way.
func browsePath(projectKey, slug, encodedPath string) string {
	return fmt.Sprintf(
		"/rest/api/latest/projects/%s/repos/%s/browse/%s",
		url.PathEscape(strings.TrimSpace(projectKey)),
		url.PathEscape(strings.TrimSpace(slug)),
		encodedPath,
	)
}

// encodeDirectory escapes a directory a segment at a time.
//
// Whole-path escaping turns the separators into %2F, which the browse endpoint
// does not accept. Per-segment escaping keeps them and still stops a half-typed
// path from carrying a query string, a fragment or a traversal into a request
// for some other endpoint -- a value being typed is the least predictable input
// there is.
func encodeDirectory(directory string) (string, error) {
	encoded := make([]string, 0, 8)

	for _, segment := range strings.Split(strings.TrimSpace(directory), "/") {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" || trimmed == "." {
			continue
		}
		if trimmed == ".." {
			return "", apperrors.New(apperrors.KindValidation, `path must not contain ".." segments`, nil)
		}

		encoded = append(encoded, url.PathEscape(trimmed))
	}

	return strings.Join(encoded, "/"), nil
}
