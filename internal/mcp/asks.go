package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reposettingsservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
)

// The confirmations the tools that ask show the person.
//
// Each names exactly what the call will do, in words a person reads in a
// dialog. Most come from the call's own arguments. A merge and auto-merge read
// the pull request as well, because the call does not name the branch it
// merges into, and a person confirming a merge needs to see that. Text other
// people wrote, such as a pull request title, is quoted as data.

// prNumber is how a pull request ID reads in a sentence.
func prNumber(id string) string {
	return "#" + strings.TrimPrefix(strings.TrimSpace(id), "#")
}

// repoName is how a repository reads in a sentence.
func repoName(project, repo string) string {
	return project + "/" + repo
}

func askMergePullRequest() ask[MergePullRequestInput] {
	return ask[MergePullRequestInput]{
		confirm: func(ctx context.Context, c Clients, in MergePullRequestInput) (confirmation, error) {
			pr, err := pullRequestToConfirm(ctx, c, in.Project, in.Repo, in.PRID, "merge")
			if err != nil {
				return confirmation{}, err
			}
			if in.Version != nil && *in.Version != pr.Version {
				return confirmation{}, fmt.Errorf("pull request #%d is at version %d, not %d: it changed since version %d was read",
					pr.ID, pr.Version, *in.Version, *in.Version)
			}

			return confirmation{
				Message: fmt.Sprintf("Merge pull request #%d %q in %s from %s into %s, with %s? A merge cannot be undone.",
					pr.ID, pr.Title, repoName(in.Project, in.Repo), pr.SourceBranch, pr.TargetBranch,
					defaultMergeStrategy(ctx, c, in.Project, in.Repo)),
				Label: fmt.Sprintf("Merge #%d into %s", pr.ID, pr.TargetBranch),
				// The version the person was shown. Bitbucket refuses a merge
				// at a stale version, so a pull request that changed after the
				// person accepted fails rather than merging what nobody saw.
				Pin: strconv.Itoa(pr.Version),
			}, nil
		},
		hold: func(in *MergePullRequestInput, pin string) error {
			if in.Version != nil {
				return nil
			}
			version, err := strconv.Atoi(pin)
			if err != nil {
				return fmt.Errorf("the confirmed pull request version %q is not a number", pin)
			}
			in.Version = &version
			return nil
		},
	}
}

func askEnableAutoMerge() ask[EnableAutoMergeInput] {
	return ask[EnableAutoMergeInput]{
		confirm: func(ctx context.Context, c Clients, in EnableAutoMergeInput) (confirmation, error) {
			pr, err := pullRequestToConfirm(ctx, c, in.Project, in.Repo, in.PRID, "set auto-merge on")
			if err != nil {
				return confirmation{}, err
			}
			strategy := in.Strategy
			if strategy == "" {
				strategy = "no-ff"
			}

			return confirmation{
				Message: fmt.Sprintf("Turn on auto-merge for pull request #%d %q in %s, merging %s into %s with the %s strategy once its required checks and approvals pass? If they pass already, it merges at once.",
					pr.ID, pr.Title, repoName(in.Project, in.Repo), pr.SourceBranch, pr.TargetBranch, strategy),
				Label: fmt.Sprintf("Auto-merge #%d into %s", pr.ID, pr.TargetBranch),
			}, nil
		},
	}
}

func askDisableAutoMerge() ask[DisableAutoMergeInput] {
	return ask[DisableAutoMergeInput]{
		confirm: func(_ context.Context, _ Clients, in DisableAutoMergeInput) (confirmation, error) {
			return confirmation{
				Message: fmt.Sprintf("Turn off auto-merge for pull request %s in %s? It will no longer merge by itself.",
					prNumber(in.PRID), repoName(in.Project, in.Repo)),
				Label: fmt.Sprintf("Stop auto-merging %s", prNumber(in.PRID)),
			}, nil
		},
	}
}

func askSubmitPRReview() ask[SubmitPRReviewInput] {
	return ask[SubmitPRReviewInput]{
		confirm: func(_ context.Context, _ Clients, in SubmitPRReviewInput) (confirmation, error) {
			pr, repo := prNumber(in.PRID), repoName(in.Project, in.Repo)
			switch in.Action {
			case "approve":
				return confirmation{
					Message: fmt.Sprintf("Approve pull request %s in %s? An approval counts toward the reviews it needs before it can merge.", pr, repo),
					Label:   fmt.Sprintf("Approve %s", pr),
				}, nil
			case "needs_work":
				return confirmation{
					Message: fmt.Sprintf("Mark pull request %s in %s as needing work? That holds its merge back until you clear it.", pr, repo),
					Label:   fmt.Sprintf("Mark %s as needing work", pr),
				}, nil
			case "unapprove":
				return confirmation{
					Message: fmt.Sprintf("Withdraw your approval of pull request %s in %s? It may then need another review before it can merge.", pr, repo),
					Label:   fmt.Sprintf("Withdraw approval of %s", pr),
				}, nil
			default:
				return confirmation{}, fmt.Errorf("unknown action %q", in.Action)
			}
		},
	}
}

func askSetBuildStatus() ask[SetBuildStatusInput] {
	return ask[SetBuildStatusInput]{
		confirm: func(_ context.Context, _ Clients, in SetBuildStatusInput) (confirmation, error) {
			build := in.Key
			if strings.TrimSpace(in.Name) != "" {
				build = in.Name
			}
			consequence := "A failed or unfinished build that a pull request requires holds its merge back."
			if in.State == "SUCCESSFUL" {
				consequence = "A successful build that a pull request requires can let it merge."
			}

			return confirmation{
				Message: fmt.Sprintf("Report build %q as %s for commit %s? %s", build, in.State, shortCommit(in.CommitID), consequence),
				Label:   fmt.Sprintf("Report %s as %s", in.Key, in.State),
			}, nil
		},
	}
}

func askCreateTag() ask[CreateTagInput] {
	return ask[CreateTagInput]{
		confirm: func(_ context.Context, _ Clients, in CreateTagInput) (confirmation, error) {
			return confirmation{
				Message: fmt.Sprintf("Create tag %q at %s in %s? Release pipelines often act on a new tag, and this server cannot delete one.",
					in.Name, in.StartPoint, repoName(in.Project, in.Repo)),
				Label: fmt.Sprintf("Create tag %s", in.Name),
			}, nil
		},
	}
}

func askUpdatePullRequest() ask[UpdatePullRequestInput] {
	return ask[UpdatePullRequestInput]{
		// Only the draft flag decides whether and when the pull request
		// merges. A title or a description does not.
		when: func(in UpdatePullRequestInput) bool { return in.Draft != nil },
		confirm: func(_ context.Context, _ Clients, in UpdatePullRequestInput) (confirmation, error) {
			pr, repo := prNumber(in.PRID), repoName(in.Project, in.Repo)

			c := confirmation{
				Message: fmt.Sprintf("Mark pull request %s in %s ready for review? It can then be merged, and its reviewers are told.", pr, repo),
				Label:   fmt.Sprintf("Mark %s ready for review", pr),
			}
			if *in.Draft {
				c = confirmation{
					Message: fmt.Sprintf("Make pull request %s in %s a draft? It cannot be merged while it is one, and any auto-merge set on it is cancelled.", pr, repo),
					Label:   fmt.Sprintf("Make %s a draft", pr),
				}
			}

			switch {
			case in.Title != "" && in.Description != "":
				c.Message += " The call also changes its title and description."
			case in.Title != "":
				c.Message += " The call also changes its title."
			case in.Description != "":
				c.Message += " The call also changes its description."
			}

			return c, nil
		},
	}
}

// pullRequestToConfirm reads the pull request a merge or auto-merge acts on,
// and refuses one that cannot be merged, so nobody is asked to confirm a call
// that will fail. Bitbucket refuses to merge a draft, or to set auto-merge on
// one, and its merge-status endpoint does not say so, so the flag is read here.
func pullRequestToConfirm(ctx context.Context, c Clients, project, repo, id, doing string) (pullrequestservice.PullRequest, error) {
	pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, id)
	if err != nil {
		return pullrequestservice.PullRequest{}, fmt.Errorf("reading the pull request to confirm it: %w", err)
	}

	switch {
	case !strings.EqualFold(pr.State, "OPEN"):
		return pullrequestservice.PullRequest{}, fmt.Errorf("pull request #%d is %s, so there is nothing to %s", pr.ID, strings.ToLower(pr.State), doing)
	case pr.Draft:
		return pullrequestservice.PullRequest{}, fmt.Errorf("pull request #%d is a draft, and Bitbucket does not %s a draft: mark it ready for review first", pr.ID, doing)
	}

	return pr, nil
}

// defaultMergeStrategy names the strategy a merge uses: the repository's
// default, since merge_pull_request takes none. When the settings cannot be
// read the confirmation says so rather than failing, since the merge itself
// does not depend on reading them.
func defaultMergeStrategy(ctx context.Context, c Clients, project, repo string) string {
	const unknown = "the repository's default strategy"

	settings, err := reposettingsservice.NewService(c.OpenAPI).GetRepositoryPullRequestSettings(ctx,
		reposettingsservice.RepositoryRef{ProjectKey: project, Slug: repo})
	if err != nil {
		return unknown
	}
	mergeConfig, _ := settings["mergeConfig"].(map[string]any)
	strategy, _ := mergeConfig["defaultStrategy"].(map[string]any)
	if name, _ := strategy["name"].(string); strings.TrimSpace(name) != "" {
		return fmt.Sprintf("the %q strategy", name)
	}
	if id, _ := strategy["id"].(string); strings.TrimSpace(id) != "" {
		return fmt.Sprintf("the %s strategy", id)
	}

	return unknown
}

// shortCommit is how a commit reads in a sentence.
func shortCommit(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
