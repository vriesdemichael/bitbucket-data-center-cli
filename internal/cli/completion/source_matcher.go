package completion

import (
	"context"
	"strings"
)

func init() {
	register(KindMatcherID, matcherSource)
}

// matcherSource offers what --matcher-id accepts, which depends on what
// --matcher-type says it is.
//
// A branch restriction matches a ref in one of four ways, and the id means a
// different thing in each: a branch, one of the branching model's two named
// branches, one of its categories, or a glob the caller writes. Offering
// branches for all of them would be offering values Bitbucket refuses for
// three of the four.
func matcherSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	switch strings.ToUpper(strings.TrimSpace(environment.Flag("matcher-type"))) {
	case "MODEL_BRANCH":
		return Result{Candidates: modelBranches(ctx, environment), KeepOrder: true}, nil
	case "MODEL_CATEGORY":
		return Result{Candidates: modelCategories(ctx, environment), KeepOrder: true}, nil
	case "PATTERN":
		// A glob nobody has written yet. Its own branches are not it, and a
		// pattern that matches one branch is a pattern written badly.
		return Result{}, nil
	default:
		// BRANCH, and the default of `bb branch restriction create`.
		return branchSource(ctx, environment, request)
	}
}

// modelBranchIDs are the two branches a branching model names.
//
// Both are accepted whether or not the repository configures them -- verified
// against 10.4.3, where a restriction on an unconfigured `production` is
// created and comes back active -- so both are offered, and the model is read
// only to say which branch each one currently points at.
var modelBranchIDs = []string{"development", "production"}

// modelCategoryIDs are Bitbucket's branch categories. The prefix each one
// matches is configurable; the ids are not.
var modelCategoryIDs = []string{"FEATURE", "BUGFIX", "HOTFIX", "RELEASE"}

func modelBranches(ctx context.Context, environment *Environment) []Candidate {
	model, read := branchingModel(ctx, environment)

	candidates := make([]Candidate, 0, len(modelBranchIDs))
	for _, id := range modelBranchIDs {
		description := ""
		switch {
		case !read:
			// Nothing is known about either, and "not configured" would be a
			// claim about both.
		case id == "development":
			description = displayOf(model.Development)
		case id == "production":
			description = displayOf(model.Production)
		}

		candidates = append(candidates, Candidate{Value: id, Description: description})
	}

	return candidates
}

func modelCategories(ctx context.Context, environment *Environment) []Candidate {
	model, read := branchingModel(ctx, environment)

	prefixes := map[string]string{}
	for _, category := range model.Types {
		prefixes[strings.ToUpper(strings.TrimSpace(category.ID))] = strings.TrimSpace(category.Prefix)
	}

	candidates := make([]Candidate, 0, len(modelCategoryIDs))
	for _, id := range modelCategoryIDs {
		prefix, enabled := prefixes[id]
		if read && !enabled {
			// The model leaves a category the repository has switched off out
			// altogether rather than listing it as disabled. Said here, so it
			// does not sit blank beside the prefixes of the others.
			prefix = "not enabled"
		}

		candidates = append(candidates, Candidate{Value: id, Description: prefix})
	}

	return candidates
}

// branchingModel is what the repository's model names, and whether it could be
// read: there may be no repository in scope, or no answer.
//
// Only a repository has one: the project-level endpoint is a 404, and a
// project restriction still matches by the same ids. So this enriches the
// descriptions where it can and is silent where it cannot -- the values it
// describes are correct either way.
func branchingModel(ctx context.Context, environment *Environment) (branchModel, bool) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return branchModel{}, false
	}

	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return branchModel{}, false
	}

	var model branchModel
	path := "/rest/branch-utils/latest/projects/" + repository.ProjectKey + "/repos/" + repository.Slug + "/branchmodel"
	if err := client.GetJSON(ctx, path, nil, &model); err != nil {
		debugf("branching model: %v", err)

		return branchModel{}, false
	}

	return model, true
}

// branchModel is the part of the branching model a matcher id names.
type branchModel struct {
	Development *modelRef       `json:"development"`
	Production  *modelRef       `json:"production"`
	Types       []modelCategory `json:"types"`
}

type modelRef struct {
	DisplayID string `json:"displayId"`
	ID        string `json:"id"`
}

type modelCategory struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
}

func displayOf(ref *modelRef) string {
	if ref == nil {
		return "not configured"
	}

	if display := strings.TrimSpace(ref.DisplayID); display != "" {
		return display
	}

	return strings.TrimPrefix(strings.TrimSpace(ref.ID), "refs/heads/")
}
