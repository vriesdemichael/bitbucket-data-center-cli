package pullrequest

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// PageOptions controls pagination for pull request inspection listings.
type PageOptions struct {
	Limit int `json:"limit"`
	Start int `json:"start"`
}

// Commit is a commit reachable from a pull request (its commit list or merge base).
type Commit struct {
	ID              string `json:"id"`
	DisplayID       string `json:"display_id,omitempty"`
	Message         string `json:"message,omitempty"`
	Author          string `json:"author,omitempty"`
	AuthorEmail     string `json:"author_email,omitempty"`
	AuthorTimestamp int64  `json:"author_timestamp,omitempty"`
}

// Change is a single file change in a pull request.
type Change struct {
	Path       string `json:"path"`
	SrcPath    string `json:"src_path,omitempty"`
	Type       string `json:"type,omitempty"`
	NodeType   string `json:"node_type,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

// ListCommits returns the commits that make up a pull request, oldest pages first.
func (service *Service) ListCommits(ctx context.Context, repository RepositoryRef, pullRequestID string, options PageOptions) ([]Commit, error) {
	resolvedID, err := service.validateInspectionRequest(repository, pullRequestID, &options)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/%s/commits", pullRequestPath(repository), resolvedID)
	results := make([]Commit, 0)
	start := options.Start

	for {
		var response pagedCommitsResponse
		if err := service.client.GetJSON(ctx, path, pageQuery(options.Limit, start), &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			results = append(results, mapCommit(value))
		}

		if response.IsLastPage || response.NextPageStart == start {
			break
		}
		start = response.NextPageStart
	}

	return results, nil
}

// ListChanges returns the file changes in a pull request.
func (service *Service) ListChanges(ctx context.Context, repository RepositoryRef, pullRequestID string, options PageOptions) ([]Change, error) {
	resolvedID, err := service.validateInspectionRequest(repository, pullRequestID, &options)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/%s/changes", pullRequestPath(repository), resolvedID)
	results := make([]Change, 0)
	start := options.Start

	for {
		var response pagedChangesResponse
		if err := service.client.GetJSON(ctx, path, pageQuery(options.Limit, start), &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			results = append(results, mapChange(value))
		}

		if response.IsLastPage || response.NextPageStart == start {
			break
		}
		start = response.NextPageStart
	}

	return results, nil
}

// GetMergeBase returns the common ancestor commit between the source and target
// branches of a pull request.
func (service *Service) GetMergeBase(ctx context.Context, repository RepositoryRef, pullRequestID string) (Commit, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Commit{}, err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Commit{}, err
	}

	var response commitValue
	if err := service.client.GetJSON(ctx, fmt.Sprintf("%s/%s/merge-base", pullRequestPath(repository), resolvedID), nil, &response); err != nil {
		return Commit{}, err
	}

	return mapCommit(response), nil
}

// validateInspectionRequest validates the repository and pull request id and
// normalizes pagination defaults in place.
func (service *Service) validateInspectionRequest(repository RepositoryRef, pullRequestID string, options *PageOptions) (string, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return "", err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return "", err
	}
	if options.Limit <= 0 {
		options.Limit = 25
	}
	if options.Start < 0 {
		return "", apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}
	return resolvedID, nil
}

func pageQuery(limit, start int) map[string]string {
	return map[string]string{
		"limit": strconv.Itoa(limit),
		"start": strconv.Itoa(start),
	}
}

type pagedCommitsResponse struct {
	Values        []commitValue `json:"values"`
	IsLastPage    bool          `json:"isLastPage"`
	NextPageStart int           `json:"nextPageStart"`
}

type commitValue struct {
	ID              string        `json:"id"`
	DisplayID       string        `json:"displayId"`
	Message         string        `json:"message"`
	Author          *commitAuthor `json:"author"`
	AuthorTimestamp int64         `json:"authorTimestamp"`
}

type commitAuthor struct {
	Name         string `json:"name"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

type pagedChangesResponse struct {
	Values        []changeValue `json:"values"`
	IsLastPage    bool          `json:"isLastPage"`
	NextPageStart int           `json:"nextPageStart"`
}

type changeValue struct {
	Path       *pathValue `json:"path"`
	SrcPath    *pathValue `json:"srcPath"`
	Type       string     `json:"type"`
	NodeType   string     `json:"nodeType"`
	Executable bool       `json:"executable"`
}

type pathValue struct {
	ToString string `json:"toString"`
}

func mapCommit(raw commitValue) Commit {
	commit := Commit{
		ID:              strings.TrimSpace(raw.ID),
		DisplayID:       strings.TrimSpace(raw.DisplayID),
		Message:         strings.TrimSpace(raw.Message),
		AuthorTimestamp: raw.AuthorTimestamp,
	}
	if raw.Author != nil {
		commit.Author = strings.TrimSpace(raw.Author.DisplayName)
		if commit.Author == "" {
			commit.Author = strings.TrimSpace(raw.Author.Name)
		}
		commit.AuthorEmail = strings.TrimSpace(raw.Author.EmailAddress)
	}
	return commit
}

func mapChange(raw changeValue) Change {
	change := Change{
		Type:       strings.TrimSpace(raw.Type),
		NodeType:   strings.TrimSpace(raw.NodeType),
		Executable: raw.Executable,
	}
	if raw.Path != nil {
		change.Path = strings.TrimSpace(raw.Path.ToString)
	}
	if raw.SrcPath != nil {
		change.SrcPath = strings.TrimSpace(raw.SrcPath.ToString)
	}
	return change
}
