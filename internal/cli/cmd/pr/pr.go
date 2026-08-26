package prcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	browseservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/browse"
	codeowners "github.com/vriesdemichael/bitbucket-server-cli/internal/services/codeowners"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
	jiraservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
	reviewerservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
}

type nopPermissionChecker struct{}

func (nopPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
	GitBackend          func() git.Backend
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
}

func New(deps Dependencies) *cobra.Command {
	if deps.JSONEnabled == nil {
		deps.JSONEnabled = func() bool { return false }
	}
	if deps.DryRunEnabled == nil {
		deps.DryRunEnabled = func() bool { return false }
	}
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.LoadFromEnv
	}
	if deps.LoadConfigAndClient == nil {
		deps.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
			}
			return cfg, client, nil
		}
	}
	if deps.WriteJSON == nil {
		deps.WriteJSON = jsonoutput.Write
	}
	if deps.WriteJSONList == nil {
		deps.WriteJSONList = jsonoutput.WriteList
	}
	if deps.GitBackend == nil {
		deps.GitBackend = func() git.Backend { return execgit.New() }
	}
	if deps.PermissionChecker == nil {
		deps.PermissionChecker = func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return nopPermissionChecker{}
		}
	}

	var repository string
	var state string
	var listPaging paging.Options
	var start int
	var sourceBranch string
	var targetBranch string
	var noReviewSummary bool
	var listWithReviewStatus bool

	prCmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request commands",
	}
	prCmd.PersistentFlags().StringVar(&repository, "repo", "", "Repository as PROJECT/slug (defaults to inferred repository context; otherwise requires BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG)")

	prCmd.AddCommand(newPullRequestDiffAlias(deps, &repository))
	prCmd.AddCommand(newPullRequestStatusCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestCheckoutCommand(deps, &repository))

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		Long: "List pull requests. Each entry carries the open task and comment counters Bitbucket reports " +
			"with the pull request, so pull requests with outstanding feedback stand out. Pass --with-review-status " +
			"to additionally resolve unresolved comment threads per pull request. That walks each pull request activity " +
			"timeline, so it is markedly slower on a long listing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			pullRequests, err := service.List(cmd.Context(), repo, pullrequestservice.ListOptions{
				State:        state,
				Limit:        listPaging.ServiceLimit(),
				Start:        start,
				SourceBranch: sourceBranch,
				TargetBranch: targetBranch,
			})
			if err != nil {
				return err
			}

			var reviewSummaries []pullrequestservice.ReviewSummary
			if listWithReviewStatus {
				reviewSummaries, err = collectReviewSummaries(cmd.Context(), client, repo, pullRequests)
				if err != nil {
					return err
				}
			}

			if deps.JSONEnabled() {
				payload := map[string]any{
					"repository":    repo,
					"filters":       map[string]any{"state": strings.ToLower(strings.TrimSpace(state)), "start": start, "limit": listPaging.ServiceLimit(), "source_branch": sourceBranch, "target_branch": targetBranch},
					"pull_requests": pullRequests,
				}
				if reviewSummaries != nil {
					payload["review_summaries"] = reviewSummaries
				}
				return deps.WriteJSONList(cmd.OutOrStdout(), payload, paging.LimitReached(listPaging, len(pullRequests)))
			}

			if len(pullRequests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pull requests found")
				return nil
			}

			for index, pullRequest := range pullRequests {
				indicator := formatPullRequestCounts(pullRequest)
				if reviewSummaries != nil {
					indicator = formatReviewStatusIndicator(reviewSummaries[index])
				}
				if indicator != "" {
					indicator = "\t" + indicator
				}

				fmt.Fprintf(
					cmd.OutOrStdout(),
					"#%d\t%s\t%s -> %s\t%s%s\n",
					pullRequest.ID,
					pullRequest.State,
					pullRequest.SourceBranch,
					pullRequest.TargetBranch,
					pullRequest.Title,
					indicator,
				)
			}

			return nil
		},
	}
	listCmd.Flags().BoolVar(&listWithReviewStatus, "with-review-status", false, "Resolve unresolved comment threads per pull request (walks each activity timeline; slower)")
	listCmd.Flags().StringVar(&state, "state", "open", "Pull request state filter: open, closed, all")
	listPaging.Register(listCmd, 25)
	listCmd.Flags().IntVar(&start, "start", 0, "Start offset for Bitbucket pull request list operations")
	listCmd.Flags().StringVar(&sourceBranch, "source-branch", "", "Optional source branch filter")
	listCmd.Flags().StringVar(&targetBranch, "target-branch", "", "Optional target branch filter")
	prCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get pull request details, including outstanding review feedback",
		Long: "Get pull request details. The output carries a review summary describing unresolved comment " +
			"threads, open tasks and reviewers who requested changes, so outstanding feedback is visible without " +
			"a separate lookup.\n\n" +
			"The unresolved thread counts come from the activity timeline, which is paged through; pass " +
			"--no-review-summary to skip it. When the timeline is unavailable the summary falls back to the " +
			"blocker-comment tally, then to the counters Bitbucket ships with the pull request. " +
			"review_summary.counts_source reports which was used.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			pullRequest, err := service.Get(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			counts := pullrequestservice.ReviewCounts{}
			if !noReviewSummary {
				counts, err = resolveReviewCounts(cmd.Context(), client, repo, target.PullRequestID)
				if err != nil {
					return err
				}
			}
			reviewSummary := pullrequestservice.BuildReviewSummary(pullRequest, counts)

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"repository":     repo,
					"pull_request":   pullRequest,
					"review_summary": reviewSummary,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\t%s -> %s\t%s\n", pullRequest.ID, pullRequest.State, pullRequest.SourceBranch, pullRequest.TargetBranch, pullRequest.Title)
			if len(pullRequest.Reviewers) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reviewers: %d\n", len(pullRequest.Reviewers))
			}
			for _, line := range formatReviewSummaryLines(reviewSummary) {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			if pullRequest.Mergeability != nil {
				mergeability := pullRequest.Mergeability
				mergeableLabel := "no"
				if mergeability.Mergeable {
					mergeableLabel = "yes"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Mergeable: %s\n", mergeableLabel)
				if mergeability.Outcome != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Merge outcome: %s\n", mergeability.Outcome)
				}
				if mergeability.Conflicted {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge conflicts: yes")
				}
				if len(mergeability.Blockers) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge blockers:")
					for _, blocker := range mergeability.Blockers {
						message := blocker.Summary
						if message == "" {
							message = blocker.Detail
						} else if blocker.Detail != "" && !strings.EqualFold(blocker.Detail, blocker.Summary) {
							message = fmt.Sprintf("%s (%s)", blocker.Summary, blocker.Detail)
						}
						fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", message)
					}
				}
			}

			return nil
		},
	}
	getCmd.Flags().BoolVar(&noReviewSummary, "no-review-summary", false, "Skip the activity timeline lookup used to count unresolved comment threads")
	prCmd.AddCommand(getCmd)

	var commitsPaging paging.Options
	var commitsStart int
	commitsCmd := &cobra.Command{
		Use:   "commits <id>",
		Short: "List the commits in a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			commits, err := service.ListCommits(cmd.Context(), repo, target.PullRequestID, pullrequestservice.PageOptions{Limit: commitsPaging.ServiceLimit(), Start: commitsStart})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No commits found")
				return nil
			}
			for _, commit := range commits {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", shortCommitID(commit), firstMessageLine(commit.Message))
			}
			return nil
		},
	}
	commitsPaging.Register(commitsCmd, 25)
	commitsCmd.Flags().IntVar(&commitsStart, "start", 0, "Start offset for the pull request commit listing")
	prCmd.AddCommand(commitsCmd)

	var filesPaging paging.Options
	var filesStart int
	filesCmd := &cobra.Command{
		Use:     "files <id>",
		Aliases: []string{"changes"},
		Short:   "List the files changed in a pull request",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			changes, err := service.ListChanges(cmd.Context(), repo, target.PullRequestID, pullrequestservice.PageOptions{Limit: filesPaging.ServiceLimit(), Start: filesStart})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "changes": changes})
			}

			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes found")
				return nil
			}
			for _, change := range changes {
				changeType := change.Type
				if changeType == "" {
					changeType = "MODIFY"
				}
				line := fmt.Sprintf("%s\t%s", changeType, change.Path)
				if change.SrcPath != "" && change.SrcPath != change.Path {
					line += fmt.Sprintf(" (from %s)", change.SrcPath)
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
	filesPaging.Register(filesCmd, 25)
	filesCmd.Flags().IntVar(&filesStart, "start", 0, "Start offset for the pull request change listing")
	prCmd.AddCommand(filesCmd)

	mergeBaseCmd := &cobra.Command{
		Use:   "merge-base <id>",
		Short: "Show the common ancestor commit of a pull request's source and target branches",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			commit, err := service.GetMergeBase(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "merge_base": commit})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", shortCommitID(commit), firstMessageLine(commit.Message))
			return nil
		},
	}
	prCmd.AddCommand(mergeBaseCmd)

	var createFromRef string
	var createToRef string
	var createTitle string
	var createDescription string
	var createReviewers []string
	var createReviewerGroups []string
	var createDefaultReviewers bool
	var createNoDefaultReviewers bool
	var createCodeOwners bool
	var createNoCodeOwners bool
	var createDraft bool
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
		Example: "  # Create a pull request (automatically includes default reviewers and CODEOWNERS)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\"\n\n" +
			"  # Create a draft pull request (Bitbucket DC 8.0+)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --draft\n\n" +
			"  # Create a pull request and assign explicit reviewers (repeatable or comma-separated)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --reviewers alice,bob\n\n" +
			"  # Create a pull request with reviewers and reviewer groups (Bitbucket DC 7.13+)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --reviewers alice,@backend-team --reviewer-group qa-team\n\n" +
			"  # Create a pull request without default reviewers or CODEOWNERS\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --no-default-reviewers --no-codeowners",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}
			}

			rawClient := httpclient.NewFromConfig(cfg)
			service := pullrequestservice.NewService(rawClient)

			author := strings.TrimSpace(cfg.BitbucketUsername)
			if author == "" {
				if slug, err := rawClient.CurrentUserSlug(cmd.Context()); err == nil {
					author = strings.TrimSpace(slug)
				}
			}

			reviewerSvc := reviewerservice.NewService(apiClient)

			var allReviewers []string
			var allGroups []string
			allReviewers = append(allReviewers, createReviewers...)
			allGroups = append(allGroups, createReviewerGroups...)

			if createDefaultReviewers && !createNoDefaultReviewers {
				defaults, err := reviewerSvc.ResolveDefaultReviewers(cmd.Context(), repo.ProjectKey, repo.Slug, createFromRef, createToRef)
				if err == nil {
					allReviewers = append(allReviewers, defaults...)
				}
			}

			if createCodeOwners && !createNoCodeOwners {
				codeOwners, err := resolveCodeOwnersReviewers(
					cmd.Context(),
					apiClient,
					cfg,
					reviewerSvc,
					repo.ProjectKey, repo.Slug,
					createFromRef, createToRef,
					"",
					author,
				)
				if err == nil {
					allReviewers = append(allReviewers, codeOwners...)
				} else if !apperrors.IsKind(err, apperrors.KindNotFound) && !openapi.IsRouteMissing(err) {
					return err
				}
			}

			resolvedReviewers, err := resolveReviewersAndGroups(
				cmd.Context(),
				reviewerSvc,
				repo.ProjectKey, repo.Slug,
				allReviewers,
				allGroups,
				author,
			)
			if err != nil {
				return err
			}

			var filtered []string
			for _, r := range resolvedReviewers {
				if author != "" && strings.EqualFold(r, author) {
					continue
				}
				filtered = append(filtered, r)
			}
			resolvedReviewers = filtered

			if deps.DryRunEnabled() {
				existing, err := service.List(cmd.Context(), repo, pullrequestservice.ListOptions{
					State:        "open",
					Limit:        200,
					SourceBranch: createFromRef,
					TargetBranch: createToRef,
				})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "pull request will be created"
				for _, pullRequest := range existing {
					if strings.EqualFold(strings.TrimSpace(pullRequest.SourceBranch), strings.TrimSpace(createFromRef)) &&
						strings.EqualFold(strings.TrimSpace(pullRequest.TargetBranch), strings.TrimSpace(createToRef)) {
						predicted = "conflict"
						reason = "an open pull request already exists for the same source and target branches"
						break
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "from_ref": createFromRef, "to_ref": createToRef, "title": createTitle, "reviewers": resolvedReviewers, "draft": createDraft},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"open pull requests"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"matching open pull request exists"}
							}
							return nil
						}(),
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			created, err := service.Create(cmd.Context(), repo, pullrequestservice.CreateInput{
				FromRef:     createFromRef,
				ToRef:       createToRef,
				Title:       createTitle,
				Description: createDescription,
				Reviewers:   resolvedReviewers,
				Draft:       createDraft,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created pull request #%d\n", created.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&createFromRef, "from-ref", "", "Source branch (name or refs/heads/name)")
	createCmd.Flags().StringVar(&createToRef, "to-ref", "", "Target branch (name or refs/heads/name)")
	createCmd.Flags().StringVar(&createTitle, "title", "", "Pull request title")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Pull request description")
	createCmd.Flags().StringSliceVar(&createReviewers, "reviewers", nil, "Reviewer usernames to add (repeatable or comma-separated, accepts @group syntax, e.g. --reviewers alice,@backend-team)")
	createCmd.Flags().StringSliceVar(&createReviewerGroups, "reviewer-group", nil, "Reviewer group name(s) to expand and add (repeatable or comma-separated, Bitbucket Data Center 7.13+)")
	createCmd.Flags().StringSliceVar(&createReviewerGroups, "reviewer-groups", nil, "Alias for --reviewer-group")
	createCmd.Flags().BoolVar(&createDefaultReviewers, "default-reviewers", true, "Include default reviewers configured on repository/project (defaults to true)")
	createCmd.Flags().BoolVar(&createNoDefaultReviewers, "no-default-reviewers", false, "Do not include default reviewers")
	createCmd.Flags().BoolVar(&createCodeOwners, "codeowners", true, "Assign code owners matching pull request diff from .bitbucket/CODEOWNERS (defaults to true, Bitbucket Data Center 8.14+)")
	createCmd.Flags().BoolVar(&createNoCodeOwners, "no-codeowners", false, "Do not include code owners from .bitbucket/CODEOWNERS")
	createCmd.Flags().BoolVar(&createDraft, "draft", false, "Create as a draft pull request (Bitbucket DC 8.0+)")
	_ = createCmd.MarkFlagRequired("from-ref")
	_ = createCmd.MarkFlagRequired("to-ref")
	_ = createCmd.MarkFlagRequired("title")
	prCmd.AddCommand(createCmd)

	var updateTitle string
	var updateDescription string
	var updateVersion int
	var updateDraft bool
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update pull request metadata",
		Example: "  # Update title and description\n" +
			"  bb pr update 42 --repo PROJ/repo --version 1 --title \"New title\"\n\n" +
			"  # Mark a draft PR as ready for review\n" +
			"  bb pr update 42 --repo PROJ/repo --version 1 --draft=false\n\n" +
			"  # Convert an open PR to draft\n" +
			"  bb pr update 42 --repo PROJ/repo --version 1 --draft",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			var draft *bool
			if cmd.Flags().Changed("draft") {
				draft = &updateDraft
			}

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				draftChanged := draft != nil && current.Draft != *draft
				predicted := "update"
				reason := "pull request metadata will be updated"
				if strings.EqualFold(strings.TrimSpace(current.Title), strings.TrimSpace(updateTitle)) &&
					strings.EqualFold(strings.TrimSpace(current.Description), strings.TrimSpace(updateDescription)) &&
					!draftChanged {
					predicted = "no-op"
					reason = "pull request already matches requested metadata"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "title": updateTitle, "description": updateDescription, "version": updateVersion, "draft": draft},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			updated, err := service.Update(cmd.Context(), repo, target.PullRequestID, pullrequestservice.UpdateInput{
				Title:       updateTitle,
				Description: updateDescription,
				Version:     updateVersion,
				Draft:       draft,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull request #%d\n", updated.ID)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateTitle, "title", "", "Updated pull request title")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "Updated pull request description")
	updateCmd.Flags().IntVar(&updateVersion, "version", 0, "Expected pull request version")
	updateCmd.Flags().BoolVar(&updateDraft, "draft", false, "Set draft state: --draft to mark as draft, --draft=false to mark as ready for review")
	_ = updateCmd.MarkFlagRequired("version")
	prCmd.AddCommand(updateCmd)

	var transitionVersion int
	mergeCmd := &cobra.Command{
		Use:   "merge <id>",
		Short: "Merge a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be merged"
				blocking := []string{}
				if strings.EqualFold(strings.TrimSpace(current.State), "MERGED") {
					predicted = "no-op"
					reason = "pull request is already merged"
				} else if !strings.EqualFold(strings.TrimSpace(current.State), "OPEN") {
					predicted = "blocked"
					reason = "pull request is not open"
					blocking = []string{"pull request is not open"}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.merge",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
						BlockingReasons: blocking,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			merged, err := service.Merge(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": merged})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Merged pull request #%d\n", merged.ID)
			return nil
		},
	}
	mergeCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(mergeCmd)

	declineCmd := &cobra.Command{
		Use:   "decline <id>",
		Short: "Decline a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be declined"
				if strings.EqualFold(strings.TrimSpace(current.State), "DECLINED") {
					predicted = "no-op"
					reason = "pull request is already declined"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.decline",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			declined, err := service.Decline(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": declined})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Declined pull request #%d\n", declined.ID)
			return nil
		},
	}
	declineCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(declineCmd)

	reopenCmd := &cobra.Command{
		Use:   "reopen <id>",
		Short: "Reopen a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be reopened"
				if strings.EqualFold(strings.TrimSpace(current.State), "OPEN") {
					predicted = "no-op"
					reason = "pull request is already open"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.reopen",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			reopened, err := service.Reopen(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": reopened})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Reopened pull request #%d\n", reopened.ID)
			return nil
		},
	}
	reopenCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(reopenCmd)

	reviewCmd := &cobra.Command{Use: "review", Short: "Pull request review commands"}

	reviewApproveCmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "update"
				reason := "pull request approval will be added"
				if currentUser != "" && reviewerApprovedByUser(current.Reviewers, currentUser) {
					predicted = "no-op"
					reason = "current user has already approved this pull request"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.review.approve",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.Approve(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Approved pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewApproveCmd)

	reviewUnapproveCmd := &cobra.Command{
		Use:   "unapprove <id>",
		Short: "Remove pull request approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "update"
				reason := "pull request approval will be removed"
				if currentUser != "" && !reviewerApprovedByUser(current.Reviewers, currentUser) {
					predicted = "no-op"
					reason = "current user has not approved this pull request"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.review.unapprove",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.Unapprove(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed approval for pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewUnapproveCmd)

	reviewerCmd := &cobra.Command{Use: "reviewer", Short: "Manage pull request reviewers"}
	var reviewerUsers []string
	var reviewerGroups []string
	var reviewerDefaultReviewers bool
	var reviewerCodeOwners bool
	reviewerAddCmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add reviewers to a pull request",
		Example: "  # Add a single reviewer\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user alice\n\n" +
			"  # Add multiple reviewers (repeatable or comma-separated)\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user alice --user bob\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --users alice,bob\n\n" +
			"  # Add a reviewer group (Bitbucket DC 7.13+)\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --reviewer-group core-team\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user @core-team\n\n" +
			"  # Add default reviewers configured on repository/project\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --default-reviewers\n\n" +
			"  # Add reviewers matching .bitbucket/CODEOWNERS (Bitbucket DC 8.14+)\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --codeowners",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(reviewerUsers) == 0 && len(reviewerGroups) == 0 && !reviewerDefaultReviewers && !reviewerCodeOwners {
				return apperrors.New(apperrors.KindValidation, "at least one reviewer (--user), reviewer group (--reviewer-group), --default-reviewers, or --codeowners is required", nil)
			}

			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}
			}

			current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			author := current.AuthorUsername
			if author == "" {
				author = current.Author
			}

			reviewerSvc := reviewerservice.NewService(apiClient)

			var allUsers []string
			var allGroups []string
			allUsers = append(allUsers, reviewerUsers...)
			allGroups = append(allGroups, reviewerGroups...)

			if reviewerDefaultReviewers {
				defaults, err := reviewerSvc.ResolveDefaultReviewers(
					cmd.Context(),
					repo.ProjectKey, repo.Slug,
					current.SourceBranch, current.TargetBranch,
				)
				if err != nil {
					return err
				}
				allUsers = append(allUsers, defaults...)
			}

			if reviewerCodeOwners {
				codeOwners, err := resolveCodeOwnersReviewers(
					cmd.Context(),
					apiClient,
					cfg,
					reviewerSvc,
					repo.ProjectKey, repo.Slug,
					current.SourceBranch, current.TargetBranch,
					target.PullRequestID,
					author,
				)
				if err != nil {
					return err
				}
				allUsers = append(allUsers, codeOwners...)
			}

			resolvedReviewers, err := resolveReviewersAndGroups(
				cmd.Context(),
				reviewerSvc,
				repo.ProjectKey, repo.Slug,
				allUsers,
				allGroups,
				author,
			)
			if err != nil {
				return err
			}

			if deps.DryRunEnabled() {
				if len(resolvedReviewers) == 0 {
					preview := dryrunpreview.Preview{
						DryRun:       true,
						PlanningMode: dryrunpreview.PlanningModeStateful,
						Capability:   dryrunpreview.CapabilityFull,
						Items: []dryrunpreview.Item{{
							Intent:          "pr.review.reviewer.add",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
							Action:          "update",
							PredictedAction: "no-op",
							Supported:       true,
							Reason:          "no eligible reviewers to add",
							Confidence:      dryrunpreview.CapabilityFull,
							RequiredState:   []string{"pull request"},
						}},
						Summary: dryrunpreview.Summary{Total: 1, Supported: 1, NoopCount: 1},
					}
					return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
				}

				var items []dryrunpreview.Item
				updateCount := 0
				noopCount := 0

				for _, u := range resolvedReviewers {
					predicted := "update"
					reason := "reviewer will be added"
					if isAuthor(current.Author, current.AuthorUsername, u) {
						predicted = "no-op"
						reason = "pull request author cannot be reviewer"
						noopCount++
					} else if hasReviewer(current.Reviewers, u) {
						predicted = "no-op"
						reason = "reviewer already present"
						noopCount++
					} else {
						updateCount++
					}

					items = append(items, dryrunpreview.Item{
						Intent:          "pr.review.reviewer.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "user": u},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					})
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items:        items,
					Summary: dryrunpreview.Summary{
						Total:       len(items),
						Supported:   len(items),
						UpdateCount: updateCount,
						NoopCount:   noopCount,
					},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var addedReviewers []string
			var skippedAuthor []string
			var alreadyPresent []string
			latestPR := current

			for _, u := range resolvedReviewers {
				if isAuthor(current.Author, current.AuthorUsername, u) {
					skippedAuthor = append(skippedAuthor, u)
					continue
				}
				if hasReviewer(current.Reviewers, u) {
					alreadyPresent = append(alreadyPresent, u)
					continue
				}
				pr, err := service.AddReviewer(cmd.Context(), repo, target.PullRequestID, u)
				if err != nil {
					return err
				}
				latestPR = pr
				addedReviewers = append(addedReviewers, u)
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"repository":      repo,
					"pull_request":    latestPR,
					"added":           addedReviewers,
					"skipped_author":  skippedAuthor,
					"already_present": alreadyPresent,
				})
			}

			if len(addedReviewers) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Added %s to pull request #%d\n", formatReviewerList(addedReviewers), latestPR.ID)
			}
			if len(skippedAuthor) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Skipped %s (pull request author)\n", strings.Join(skippedAuthor, ", "))
			}
			if len(alreadyPresent) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reviewers already present: %s\n", strings.Join(alreadyPresent, ", "))
			}
			if len(addedReviewers) == 0 && len(skippedAuthor) == 0 && len(alreadyPresent) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No eligible reviewers to add")
			}
			return nil
		},
	}
	reviewerAddCmd.Flags().StringSliceVar(&reviewerUsers, "user", nil, "Reviewer username(s) (repeatable or comma-separated, accepts @group syntax)")
	reviewerAddCmd.Flags().StringSliceVar(&reviewerUsers, "users", nil, "Alias for --user")
	reviewerAddCmd.Flags().StringSliceVar(&reviewerUsers, "reviewers", nil, "Alias for --user")
	reviewerAddCmd.Flags().StringSliceVar(&reviewerGroups, "reviewer-group", nil, "Reviewer group name(s) to expand and add (repeatable or comma-separated, Bitbucket Data Center 7.13+)")
	reviewerAddCmd.Flags().StringSliceVar(&reviewerGroups, "reviewer-groups", nil, "Alias for --reviewer-group")
	reviewerAddCmd.Flags().BoolVar(&reviewerDefaultReviewers, "default-reviewers", false, "Assign default reviewers configured on repository/project for this pull request")
	reviewerAddCmd.Flags().BoolVar(&reviewerCodeOwners, "codeowners", false, "Assign code owners matching pull request diff from .bitbucket/CODEOWNERS (Bitbucket Data Center 8.14+)")
	reviewerCmd.AddCommand(reviewerAddCmd)

	var removeReviewerUsername string
	reviewerRemoveCmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a reviewer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				predicted := "delete"
				reason := "reviewer will be removed"
				if !hasReviewer(current.Reviewers, removeReviewerUsername) {
					predicted = "no-op"
					reason = "reviewer is not present"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.review.reviewer.remove",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "user": removeReviewerUsername},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.RemoveReviewer(cmd.Context(), repo, target.PullRequestID, removeReviewerUsername)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed reviewer %s from pull request #%d\n", removeReviewerUsername, pullRequest.ID)
			return nil
		},
	}
	reviewerRemoveCmd.Flags().StringVar(&removeReviewerUsername, "user", "", "Reviewer username")
	_ = reviewerRemoveCmd.MarkFlagRequired("user")
	reviewerCmd.AddCommand(reviewerRemoveCmd)

	reviewCmd.AddCommand(reviewerCmd)

	reviewGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Retrieve current draft review details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			response, err := client.GetReviewWithResponse(cmd.Context(), repo.ProjectKey, repo.Slug, target.PullRequestID, nil)
			if err != nil {
				return err
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return err
			}

			var comments []openapigenerated.RestComment
			if response.ApplicationjsonCharsetUTF8200 != nil && response.ApplicationjsonCharsetUTF8200.Values != nil {
				comments = *response.ApplicationjsonCharsetUTF8200.Values
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "comments": comments})
			}

			if len(comments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No draft comments found in review")
				return nil
			}

			for _, comment := range comments {
				fmt.Fprintln(cmd.OutOrStdout(), formatCommentSummary(comment))
			}
			return nil
		},
	}
	reviewCmd.AddCommand(reviewGetCmd)

	var reviewCompleteStatus string
	var reviewCompleteComment string
	reviewCompleteCmd := &cobra.Command{
		Use:   "complete <id>",
		Short: "Publish draft comments and optionally submit a status change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			var body openapigenerated.RestPullRequestFinishReviewRequest
			if reviewCompleteStatus != "" {
				s := strings.ToUpper(reviewCompleteStatus)
				body.ParticipantStatus = &s
			}
			if reviewCompleteComment != "" {
				c := reviewCompleteComment
				body.CommentText = &c
			}

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.review.complete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "status": reviewCompleteStatus, "comment": reviewCompleteComment},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "pull request review will be completed",
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			response, err := client.FinishReviewWithResponse(cmd.Context(), repo.ProjectKey, repo.Slug, target.PullRequestID, nil, body)
			if err != nil {
				return err
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "status": "completed"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Completed review for pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	reviewCompleteCmd.Flags().StringVar(&reviewCompleteStatus, "status", "", "Pull request status change (APPROVED, NEEDS_WORK, UNAPPROVED)")
	reviewCompleteCmd.Flags().StringVar(&reviewCompleteComment, "comment", "", "Review completion comment text")
	reviewCmd.AddCommand(reviewCompleteCmd)

	reviewDiscardCmd := &cobra.Command{
		Use:   "discard <id>",
		Short: "Discard all draft comments and cancel review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.review.discard",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "pull request review will be discarded",
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			response, err := client.DiscardReviewWithResponse(cmd.Context(), repo.ProjectKey, repo.Slug, target.PullRequestID)
			if err != nil {
				return err
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "status": "discarded"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Discarded review for pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewDiscardCmd)

	prCmd.AddCommand(reviewCmd)

	jiraCmd := &cobra.Command{
		Use:   "jira <id>",
		Short: "List Jira issues associated with a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			jiraService := jiraservice.NewService(httpclient.NewFromConfig(cfg))
			issues, err := jiraService.GetPRIssues(cmd.Context(), jiraservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "issues": issues})
			}

			if len(issues) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No Jira issues associated with pull request")
				return nil
			}

			rows := make([][]string, len(issues))
			for i, issue := range issues {
				rows[i] = []string{style.Secondary.Render(issue.Key), issue.URL}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	prCmd.AddCommand(jiraCmd)

	commentCmd := &cobra.Command{Use: "comment", Short: "Pull request comment commands"}

	var commentPath string
	var commentPaging paging.Options
	var commentBlocker bool
	var commentState string
	var commentUnresolved bool
	var commentTasksOnly bool
	var commentWithReplies bool
	var commentFull bool
	commentListCmd := &cobra.Command{
		Use:   "list <id>",
		Short: "List comment threads for a pull request, unresolved first",
		Long: "List pull request comment threads. Bitbucket models a task as a blocker comment, so this " +
			"returns reviewer comments and tasks in one view, each with its resolution state, anchor and reply count.\n\n" +
			"Without --path this uses the pull request activity timeline to return the aggregate comment view. " +
			"With --path it uses the path-scoped comments endpoint. With --blocker it lists blocker comments.\n\n" +
			"Use --unresolved to show only threads still waiting on someone. Use --full to emit the raw Bitbucket " +
			"comment payload instead of the summarised thread view.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			trimmedCommentPath := strings.TrimSpace(commentPath)

			if commentUnresolved && cmd.Flags().Changed("state") && !strings.EqualFold(strings.TrimSpace(commentState), "open") {
				return apperrors.New(apperrors.KindValidation, "--unresolved cannot be combined with a --state other than open", nil)
			}
			if commentUnresolved {
				commentState = "open"
			}
			normalizedState, err := pullrequestactivityservice.NormalizeThreadState(commentState)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, err.Error(), nil)
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			threadOptions := pullrequestactivityservice.ThreadOptions{
				State:         normalizedState,
				TasksOnly:     commentTasksOnly,
				WithReplies:   commentWithReplies,
				BaseURL:       cfg.BitbucketURL,
				ProjectKey:    repo.ProjectKey,
				Slug:          repo.Slug,
				PullRequestID: target.PullRequestID,
			}

			source := "comments"
			comments := make([]openapigenerated.RestComment, 0)
			var threads []pullrequestactivityservice.Thread
			var summary pullrequestactivityservice.Summary

			if commentBlocker {
				source = "blocker_comments"
				service := commentservice.NewService(client)
				comments, err = service.List(cmd.Context(), commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					PullRequestID: target.PullRequestID,
					Blocker:       true,
				}, "", commentPaging.ServiceLimit())
				if err != nil {
					return err
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			} else if trimmedCommentPath == "" {
				source = "activities"
				activityService := pullrequestactivityservice.NewService(client)
				activities, listErr := activityService.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID, pullrequestactivityservice.ListOptions{Limit: commentPaging.ServiceLimit()})
				if listErr != nil {
					return listErr
				}
				comments = pullrequestactivityservice.ExtractComments(activities)
				threads, summary = pullrequestactivityservice.ExtractThreads(activities, threadOptions)
			} else {
				service := commentservice.NewService(client)
				comments, err = service.List(cmd.Context(), commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					PullRequestID: target.PullRequestID,
				}, trimmedCommentPath, commentPaging.ServiceLimit())
				if err != nil {
					return err
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			}

			if commentFull {
				if deps.JSONEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
						"repository":      repo,
						"pull_request_id": target.PullRequestID,
						"source":          source,
						"path":            trimmedCommentPath,
						"comments":        comments,
					})
				}

				if len(comments) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No comments found")
					return nil
				}
				for _, comment := range comments {
					fmt.Fprintln(cmd.OutOrStdout(), formatCommentSummary(comment))
				}

				return nil
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"repository":      repo,
					"pull_request_id": target.PullRequestID,
					"source":          source,
					"path":            trimmedCommentPath,
					"state":           normalizedState,
					"summary":         summary,
					"threads":         threads,
				})
			}

			if summary.TotalThreads == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments found")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatThreadCounts(summary))

			if len(threads) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments match the current filter")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout())
			for _, thread := range threads {
				fmt.Fprintln(cmd.OutOrStdout(), formatThread(thread))
			}

			return nil
		},
	}
	commentListCmd.Flags().StringVar(&commentPath, "path", "", "Optional file path for path-scoped pull request comment listing")
	commentPaging.Register(commentListCmd, 25)
	commentListCmd.Flags().BoolVar(&commentBlocker, "blocker", false, "List pull request blocker comments")
	commentListCmd.Flags().StringVar(&commentState, "state", "all", "Filter threads by resolution state: open, resolved, pending, all")
	commentListCmd.Flags().BoolVar(&commentUnresolved, "unresolved", false, "Show only unresolved threads (shorthand for --state open)")
	commentListCmd.Flags().BoolVar(&commentTasksOnly, "tasks-only", false, "Show only threads Bitbucket tracks as tasks (blocker comments)")
	commentListCmd.Flags().BoolVar(&commentWithReplies, "with-replies", false, "Include the full text of every reply instead of only the most recent one")
	commentListCmd.Flags().BoolVar(&commentFull, "full", false, "Emit the raw Bitbucket comment payload instead of the summarised thread view")
	commentCmd.AddCommand(commentListCmd)

	commentGetCmd := &cobra.Command{
		Use:   "get <pr-id> <comment-id>",
		Short: "Get a pull request comment",
		Long:  "Get a single pull request comment by id. This is the authoritative single-comment view and is better suited than list output when you need the full rendered comment payload.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			service := commentservice.NewService(client)
			comment, err := service.Get(cmd.Context(), commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: target.PullRequestID,
			}, args[1])
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "comment": comment})
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatCommentDetail(comment))
			return nil
		},
	}
	commentCmd.AddCommand(commentGetCmd)

	var commentAddText string
	var commentAddBlocker bool
	var commentAddPending bool
	commentAddCmd := &cobra.Command{
		Use:   "add <pr-id>",
		Short: "Add a comment to a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			cmtTarget := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: target.PullRequestID,
				Blocker:       commentAddBlocker,
				Pending:       commentAddPending,
			}

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.comment.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "text": commentAddText, "blocker": commentAddBlocker, "pending": commentAddPending},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "pull request comment will be created",
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request reference"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			created, err := service.Create(cmd.Context(), cmtTarget, commentAddText)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "comment": created, "blocker": commentAddBlocker, "pending": commentAddPending})
			}

			commentID := ""
			if created.Id != nil {
				commentID = strconv.Itoa(int(*created.Id))
			}
			blockerStr := ""
			if commentAddBlocker {
				blockerStr = " blocker"
			}
			pendingStr := ""
			if commentAddPending {
				pendingStr = " pending"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created%s%s comment %s\n", blockerStr, pendingStr, commentID)
			return nil
		},
	}
	commentAddCmd.Flags().StringVar(&commentAddText, "text", "", "Comment text")
	commentAddCmd.Flags().BoolVar(&commentAddBlocker, "blocker", false, "Mark the comment as a blocker")
	commentAddCmd.Flags().BoolVar(&commentAddPending, "pending", false, "Mark the comment as pending (draft)")
	_ = commentAddCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(commentAddCmd)

	var commentReactRemove bool
	commentReactCmd := &cobra.Command{
		Use:   "react <pr-id> <comment-id> <emoji>",
		Short: "Add or remove a reaction on a pull request comment",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			prID := target.PullRequestID
			commentID := args[1]
			emoticon := normalizeEmoticon(args[2])

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				action := "update"
				predicted := "update"
				intent := "pr.comment.react"
				reason := "reaction will be added"
				if commentReactRemove {
					action = "delete"
					predicted = "delete"
					reason = "reaction will be removed"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          intent,
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "pr_id": prID, "comment_id": commentID, "emoticon": emoticon},
						Action:          action,
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request comment"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if commentReactRemove {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.UpdateCount = 1
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			if commentReactRemove {
				err = service.UnReact(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, emoticon)
				if err != nil {
					return err
				}

				if deps.JSONEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "action": "removed", "repository": repo, "pull_request_id": prID, "comment_id": commentID, "emoticon": emoticon})
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Removed reaction :%s: from comment %s\n", emoticon, commentID)
				return nil
			}

			reaction, err := service.React(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, emoticon)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": prID, "comment_id": commentID, "reaction": reaction})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added reaction :%s: to comment %s\n", emoticon, commentID)
			return nil
		},
	}
	commentReactCmd.Flags().BoolVar(&commentReactRemove, "remove", false, "Remove the reaction instead of adding it")
	commentCmd.AddCommand(commentReactCmd)

	for _, stateCommand := range newPullRequestCommentStateCommands(deps, &repository) {
		commentCmd.AddCommand(stateCommand)
	}

	var commentSuggestionMsg string
	var commentSuggestionIdx int32
	var commentSuggestionCommentVer int32
	var commentSuggestionPrVer int32
	commentApplySuggestionCmd := &cobra.Command{
		Use:   "apply-suggestion <pr-id> <comment-id>",
		Short: "Apply a suggested change from a comment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			prID := target.PullRequestID
			commentID := args[1]

			req := openapigenerated.RestApplySuggestionRequest{
				Message: fmt.Sprintf("Apply suggestion from comment %s", commentID),
			}
			if strings.TrimSpace(commentSuggestionMsg) != "" {
				req.Message = commentSuggestionMsg
			}
			if cmd.Flags().Changed("index") {
				req.SuggestionIndex = commentSuggestionIdx
			}
			if cmd.Flags().Changed("comment-version") {
				req.CommentVersion = commentSuggestionCommentVer
			}
			if cmd.Flags().Changed("pr-version") {
				req.PullRequestVersion = commentSuggestionPrVer
			}

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.comment.apply-suggestion",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "pr_id": prID, "comment_id": commentID, "suggestion_index": commentSuggestionIdx},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "comment suggestion will be applied",
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request comment suggestion"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			err = service.ApplySuggestion(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, req)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "pull_request_id": prID, "comment_id": commentID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Applied suggestion on comment %s for pull request %s\n", commentID, prID)
			return nil
		},
	}
	commentApplySuggestionCmd.Flags().StringVar(&commentSuggestionMsg, "commit-message", "", "Commit message for the applied suggestion (default: names the comment)")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionIdx, "index", 0, "Optional index of the suggestion in the comment (default 0)")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionCommentVer, "comment-version", 0, "Optional expected version of the comment")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionPrVer, "pr-version", 0, "Optional expected version of the pull request")
	commentCmd.AddCommand(commentApplySuggestionCmd)

	prCmd.AddCommand(commentCmd)

	activityCmd := &cobra.Command{
		Use:   "activity",
		Short: "Pull request activity commands",
		Long:  "Pull request activity commands. This is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
	}

	var activityPaging paging.Options
	activityListCmd := &cobra.Command{
		Use:   "list <id>",
		Short: "List raw pull request activity items",
		Long:  "List raw pull request activity items. This output is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			service := pullrequestactivityservice.NewService(client)
			activities, err := service.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID, pullrequestactivityservice.ListOptions{Limit: activityPaging.ServiceLimit()})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "activities": activities})
			}

			if len(activities) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No activities found")
				return nil
			}

			for _, activity := range activities {
				fmt.Fprintln(cmd.OutOrStdout(), formatPullRequestActivitySummary(activity))
			}

			return nil
		},
	}
	activityPaging.Register(activityListCmd, 25)
	activityCmd.AddCommand(activityListCmd)
	prCmd.AddCommand(activityCmd)

	var buildPaging paging.Options
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Pull request build status commands",
	}
	buildPaging.RegisterPersistent(buildCmd, 25)

	buildStatusCmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show build statuses for a pull request's source commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			statuses, err := service.GetBuildStatuses(cmd.Context(), repo, target.PullRequestID, buildPaging.ServiceLimit())
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"repository":   repo,
					"pull_request": target.PullRequestID,
					"statuses":     statuses,
				})
			}

			if len(statuses) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No build statuses found")
				return nil
			}

			for _, s := range statuses {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s.Key, s.State, s.URL)
			}

			return nil
		},
	}
	buildCmd.AddCommand(buildStatusCmd)
	prCmd.AddCommand(buildCmd)

	autoMergeCmd := &cobra.Command{
		Use:   "auto-merge",
		Short: "Pull request auto-merge commands (Bitbucket DC 8.0+)",
	}

	autoMergeGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get auto-merge configuration for a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			autoMerge, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "auto_merge": autoMerge})
			}

			if !autoMerge.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "Auto-merge: disabled")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-merge: enabled (strategy=%s)\n", autoMerge.StrategyID)
			return nil
		},
	}
	autoMergeCmd.AddCommand(autoMergeGetCmd)

	var autoMergeStrategy string
	autoMergeEnableCmd := &cobra.Command{
		Use:   "enable <id>",
		Short: "Enable auto-merge on a pull request",
		Example: "  # Enable auto-merge with the default strategy (no-ff)\n" +
			"  bb pr auto-merge enable 42 --repo PROJ/repo\n\n" +
			"  # Enable auto-merge with a specific strategy\n" +
			"  bb pr auto-merge enable 42 --repo PROJ/repo --strategy rebase-ff-only",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "auto-merge will be enabled"
				if current.Enabled && strings.EqualFold(strings.TrimSpace(current.StrategyID), strings.TrimSpace(autoMergeStrategy)) {
					predicted = "no-op"
					reason = "auto-merge is already enabled with the same strategy"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.auto-merge.enable",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "strategy": autoMergeStrategy},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request auto-merge"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			autoMerge, err := service.EnableAutoMerge(cmd.Context(), repo, target.PullRequestID, autoMergeStrategy)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "auto_merge": autoMerge})
			}

			if autoMerge.MergedImmediately {
				fmt.Fprintf(cmd.OutOrStdout(), "Merged pull request #%s immediately (strategy=%s): its checks already passed, so there was nothing to wait for\n", target.PullRequestID, autoMerge.StrategyID)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Enabled auto-merge on pull request #%s (strategy=%s)\n", target.PullRequestID, autoMerge.StrategyID)
			return nil
		},
	}
	autoMergeEnableCmd.Flags().StringVar(&autoMergeStrategy, "strategy", "no-ff", "Merge strategy: no-ff, ff-only, rebase-no-ff, rebase-ff-only, squash, squash-ff-only")
	autoMergeCmd.AddCommand(autoMergeEnableCmd)

	autoMergeDisableCmd := &cobra.Command{
		Use:   "disable <id>",
		Short: "Disable auto-merge on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "delete"
				reason := "auto-merge will be disabled"
				if !current.Enabled {
					predicted = "no-op"
					reason = "auto-merge is not enabled"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.auto-merge.disable",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request auto-merge"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.DisableAutoMerge(cmd.Context(), repo, target.PullRequestID); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "pull_request_id": target.PullRequestID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Disabled auto-merge on pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	autoMergeCmd.AddCommand(autoMergeDisableCmd)
	prCmd.AddCommand(autoMergeCmd)

	watchCmd := &cobra.Command{
		Use:   "watch <id>",
		Short: "Watch a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				if _, err := service.Get(cmd.Context(), repo, target.PullRequestID); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.watch",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			err = service.Watch(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "watched": true})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watching pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	prCmd.AddCommand(watchCmd)

	unwatchCmd := &cobra.Command{
		Use:   "unwatch <id>",
		Short: "Unwatch a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				if _, err := service.Get(cmd.Context(), repo, target.PullRequestID); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.unwatch",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			err = service.Unwatch(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": target.PullRequestID, "watched": false})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Unwatching pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	prCmd.AddCommand(unwatchCmd)

	var rebaseVersion int
	rebaseCmd := &cobra.Command{
		Use:   "rebase <id>",
		Short: "Rebase a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			target, err := prsel.Resolve(cmd.Context(), args[0], repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				rebaseability, err := service.CanRebase(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be rebased"
				blocking := []string{}
				if rebaseability != nil && rebaseability.Vetoes != nil && len(*rebaseability.Vetoes) > 0 {
					predicted = "blocked"
					reason = "rebase is vetoed"
					for _, veto := range *rebaseability.Vetoes {
						msg := ""
						if veto.SummaryMessage != nil {
							msg = *veto.SummaryMessage
						}
						if veto.DetailedMessage != nil {
							if msg != "" {
								msg += ": "
							}
							msg += *veto.DetailedMessage
						}
						if msg != "" {
							blocking = append(blocking, msg)
						}
					}
					if len(blocking) > 0 {
						reason = strings.Join(blocking, "; ")
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "pr.rebase",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request"},
						BlockingReasons: blocking,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &rebaseVersion
			}

			result, err := service.Rebase(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "rebase_result": result})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Rebased pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	rebaseCmd.Flags().IntVar(&rebaseVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(rebaseCmd)

	var searchFilter string
	participantsCmd := &cobra.Command{
		Use:   "participants",
		Short: "Search pull request participants across a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repoProj, repoSlug, err := reposel.Resolve(repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			participants, err := service.SearchParticipants(cmd.Context(), repo, searchFilter)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "participants": participants})
			}

			if len(participants) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No participants found"))
				return nil
			}

			rows := make([][]string, len(participants))
			for i, p := range participants {
				activeStr := "active"
				if !p.Active {
					activeStr = "inactive"
				}
				rows[i] = []string{p.Name, p.DisplayName, p.EmailAddress, activeStr}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	participantsCmd.Flags().StringVar(&searchFilter, "search", "", "Query filter (checks username, name, or email)")
	_ = participantsCmd.MarkFlagRequired("search")
	prCmd.AddCommand(participantsCmd)

	var defaultReviewersSourceRepoId string
	var defaultReviewersTargetRepoId string
	var defaultReviewersSourceRef string
	var defaultReviewersTargetRef string

	defaultReviewersCmd := &cobra.Command{
		Use:   "default-reviewers",
		Short: "List default reviewers and matching conditions for repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(repository, cfg)
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			var sourceRepoIdPtr *string
			var targetRepoIdPtr *string
			var sourceRefPtr *string
			var targetRefPtr *string

			if defaultReviewersSourceRepoId != "" {
				sourceRepoIdPtr = &defaultReviewersSourceRepoId
			}
			if defaultReviewersTargetRepoId != "" {
				targetRepoIdPtr = &defaultReviewersTargetRepoId
			}
			if defaultReviewersSourceRef != "" {
				sourceRefPtr = &defaultReviewersSourceRef
			}
			if defaultReviewersTargetRef != "" {
				targetRefPtr = &defaultReviewersTargetRef
			}

			conditions, err := service.GetDefaultReviewers(cmd.Context(), repoProj, repoSlug, sourceRepoIdPtr, targetRepoIdPtr, sourceRefPtr, targetRefPtr)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"default_reviewers": conditions})
			}

			printDefaultReviewers(cmd, conditions)
			return nil
		},
	}

	defaultReviewersCmd.Flags().StringVar(&defaultReviewersSourceRepoId, "source-repo-id", "", "The ID of the repository in which the source ref exists")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersTargetRepoId, "target-repo-id", "", "The ID of the repository in which the target ref exists")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersSourceRef, "source-ref", "", "The ID of the source ref (e.g. refs/heads/feature)")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersTargetRef, "target-ref", "", "The ID of the target ref (e.g. refs/heads/master)")

	prCmd.AddCommand(defaultReviewersCmd)

	return prCmd
}

func newPullRequestDiffAlias(deps Dependencies, repositorySelector *string) *cobra.Command {
	var patch bool
	var stat bool
	var nameOnly bool

	command := &cobra.Command{
		Use:   "diff <id>",
		Short: "Diff a pull request (alias for bb diff pr)",
		Long:  "Diff a pull request.\n\nAlias for bb diff pr, which is where the command reference documents it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repositorySelector, cfg, nil)
			if err != nil {
				return err
			}
			repo := diffservice.RepositoryRef{ProjectKey: target.ProjectKey, Slug: target.RepoSlug}

			service := diffservice.NewService(client)
			outputMode, err := resolveDiffOutputMode(patch, stat, nameOnly)
			if err != nil {
				return err
			}

			result, err := service.DiffPR(cmd.Context(), diffservice.DiffPRInput{
				Repository:    repo,
				PullRequestID: target.PullRequestID,
				Output:        outputMode,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), deps.JSONEnabled(), outputMode, result, deps.WriteJSON)
		},
	}

	command.Flags().BoolVar(&patch, "patch", false, "Output unified patch stream")
	command.Flags().BoolVar(&stat, "stat", false, "Output structured diff stats")
	command.Flags().BoolVar(&nameOnly, "name-only", false, "Output only changed file names")

	return command
}

func resolveDiffOutputMode(patch, stat, nameOnly bool) (diffservice.OutputKind, error) {
	selected := 0
	if patch {
		selected++
	}
	if stat {
		selected++
	}
	if nameOnly {
		selected++
	}
	if selected > 1 {
		return "", apperrors.New(apperrors.KindValidation, "choose only one output mode: --patch, --stat, or --name-only", nil)
	}

	if patch {
		return diffservice.OutputKindPatch, nil
	}
	if stat {
		return diffservice.OutputKindStat, nil
	}
	if nameOnly {
		return diffservice.OutputKindNameOnly, nil
	}

	return diffservice.OutputKindRaw, nil
}

func writeDiffResult(writer io.Writer, asJSON bool, mode diffservice.OutputKind, result diffservice.Result, writeJSON func(io.Writer, any) error) error {
	if asJSON {
		switch mode {
		case diffservice.OutputKindNameOnly:
			return writeJSON(writer, map[string]any{"names": result.Names})
		case diffservice.OutputKindStat:
			return writeJSON(writer, map[string]any{"stats": result.Stats})
		default:
			return writeJSON(writer, map[string]any{"patch": result.Patch})
		}
	}

	switch mode {
	case diffservice.OutputKindNameOnly:
		for _, name := range result.Names {
			fmt.Fprintln(writer, name)
		}
		return nil
	case diffservice.OutputKindStat:
		return writeJSON(writer, result.Stats)
	default:
		fmt.Fprint(writer, result.Patch)
		if result.Patch != "" && !strings.HasSuffix(result.Patch, "\n") {
			fmt.Fprintln(writer)
		}
		return nil
	}
}

// resolveReviewersAndGroups expands individual reviewer usernames and reviewer group names into a deduplicated
// slice of reviewer usernames, excluding the PR author from expanded groups.
func resolveReviewersAndGroups(
	ctx context.Context,
	reviewerSvc *reviewerservice.Service,
	projectKey, repoSlug string,
	reviewers []string,
	reviewerGroups []string,
	author string,
) ([]string, error) {
	var directUsers []string
	var groupsToResolve []string
	explicitGroups := make(map[string]bool)

	for _, r := range reviewers {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			groupsToResolve = append(groupsToResolve, strings.TrimPrefix(trimmed, "@"))
		} else {
			directUsers = append(directUsers, trimmed)
		}
	}

	for _, g := range reviewerGroups {
		trimmed := strings.TrimPrefix(strings.TrimSpace(g), "@")
		if trimmed != "" {
			groupsToResolve = append(groupsToResolve, trimmed)
			explicitGroups[trimmed] = true
		}
	}

	seenUsers := make(map[string]bool)
	var finalReviewers []string

	// Direct users
	for _, u := range directUsers {
		lower := strings.ToLower(u)
		if !seenUsers[lower] {
			seenUsers[lower] = true
			finalReviewers = append(finalReviewers, u)
		}
	}

	// Group members (or @username fallback): exclude author
	for _, groupName := range groupsToResolve {
		members, err := reviewerSvc.ResolveReviewerGroupUsers(ctx, projectKey, repoSlug, groupName)
		if err != nil {
			if explicitGroups[groupName] {
				return nil, err
			}
			// If not a group, treat as a direct username
			if author != "" && strings.EqualFold(groupName, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(groupName)
			if !seenUsers[lower] {
				seenUsers[lower] = true
				finalReviewers = append(finalReviewers, groupName)
			}
			continue
		}
		for _, member := range members {
			trimmedMember := strings.TrimSpace(member)
			if trimmedMember == "" {
				continue
			}
			if author != "" && strings.EqualFold(trimmedMember, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(trimmedMember)
			if !seenUsers[lower] {
				seenUsers[lower] = true
				finalReviewers = append(finalReviewers, trimmedMember)
			}
		}
	}

	return finalReviewers, nil
}

func fetchCodeOwnersContent(ctx context.Context, apiClient *openapigenerated.ClientWithResponses, cfg config.AppConfig, projectKey, repoSlug, targetRef string) (string, error) {
	if data, err := os.ReadFile(".bitbucket/CODEOWNERS"); err == nil && len(data) > 0 {
		return string(data), nil
	}
	if data, err := os.ReadFile("CODEOWNERS"); err == nil && len(data) > 0 {
		return string(data), nil
	}

	browseSvc := browseservice.NewService(apiClient, httpclient.NewFromConfig(cfg))
	rawBytes, err := browseSvc.Raw(ctx, browseservice.RepositoryRef{ProjectKey: projectKey, Slug: repoSlug}, ".bitbucket/CODEOWNERS", targetRef)
	if err == nil && len(rawBytes) > 0 {
		return string(rawBytes), nil
	}

	rawBytes, err = browseSvc.Raw(ctx, browseservice.RepositoryRef{ProjectKey: projectKey, Slug: repoSlug}, "CODEOWNERS", targetRef)
	if err == nil && len(rawBytes) > 0 {
		return string(rawBytes), nil
	}

	return "", apperrors.New(apperrors.KindNotFound, ".bitbucket/CODEOWNERS not found in repository or target branch", nil)
}

func fetchChangedFiles(ctx context.Context, apiClient *openapigenerated.ClientWithResponses, projectKey, repoSlug, fromRef, toRef, prID string) ([]string, error) {
	diffSvc := diffservice.NewService(apiClient)
	if prID != "" {
		res, err := diffSvc.DiffPR(ctx, diffservice.DiffPRInput{
			Repository:    diffservice.RepositoryRef{ProjectKey: projectKey, Slug: repoSlug},
			PullRequestID: prID,
			Output:        diffservice.OutputKindNameOnly,
		})
		if err != nil {
			return nil, err
		}
		return res.Names, nil
	}

	res, err := diffSvc.DiffRefs(ctx, diffservice.DiffRefsInput{
		Repository: diffservice.RepositoryRef{ProjectKey: projectKey, Slug: repoSlug},
		From:       toRef,
		To:         fromRef,
		Output:     diffservice.OutputKindNameOnly,
	})
	if err != nil {
		return nil, err
	}
	return res.Names, nil
}

func resolveCodeOwnersReviewers(
	ctx context.Context,
	apiClient *openapigenerated.ClientWithResponses,
	cfg config.AppConfig,
	reviewerSvc *reviewerservice.Service,
	projectKey, repoSlug string,
	fromRef, toRef string,
	prID string,
	author string,
) ([]string, error) {
	content, err := fetchCodeOwnersContent(ctx, apiClient, cfg, projectKey, repoSlug, toRef)
	if err != nil {
		return nil, err
	}

	files, err := fetchChangedFiles(ctx, apiClient, projectKey, repoSlug, fromRef, toRef, prID)
	if err != nil {
		return nil, err
	}

	co := codeowners.Parse(content)
	refs := co.MatchFileRefsUnion(files)

	var busyCounts map[string]int
	for _, ref := range refs {
		if ref.IsGroup && ref.Strategy == codeowners.StrategyLeastBusy {
			busyCounts = fetchBusyCounts(ctx, cfg, projectKey, repoSlug)
			break
		}
	}

	var finalUsers []string
	seen := make(map[string]bool)

	for _, ref := range refs {
		if !ref.IsGroup {
			if author != "" && strings.EqualFold(ref.Name, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(ref.Name)
			if !seen[lower] {
				seen[lower] = true
				finalUsers = append(finalUsers, ref.Name)
			}
			continue
		}

		members, err := reviewerSvc.ResolveReviewerGroupUsers(ctx, projectKey, repoSlug, ref.Name)
		if err != nil {
			if author != "" && strings.EqualFold(ref.Name, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(ref.Name)
			if !seen[lower] {
				seen[lower] = true
				finalUsers = append(finalUsers, ref.Name)
			}
			continue
		}

		selected := reviewerservice.SelectMembers(members, author, string(ref.Strategy), ref.Count, busyCounts)
		for _, m := range selected {
			lower := strings.ToLower(m)
			if !seen[lower] {
				seen[lower] = true
				finalUsers = append(finalUsers, m)
			}
		}
	}

	return finalUsers, nil
}

func fetchBusyCounts(ctx context.Context, cfg config.AppConfig, projectKey, repoSlug string) map[string]int {
	counts := make(map[string]int)
	service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
	list, err := service.List(ctx, pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: repoSlug}, pullrequestservice.ListOptions{
		State: "OPEN",
		Limit: 50,
	})
	if err != nil || len(list) == 0 {
		return counts
	}
	for _, pr := range list {
		for _, r := range pr.Reviewers {
			if !r.Approved {
				counts[strings.ToLower(r.Name)]++
			}
		}
	}
	return counts
}

func formatReviewerList(users []string) string {
	if len(users) == 1 {
		return fmt.Sprintf("reviewer %s", users[0])
	}
	return fmt.Sprintf("reviewers %s", strings.Join(users, ", "))
}

func isAuthor(author, authorUsername, username string) bool {
	u := strings.TrimSpace(username)
	if u == "" {
		return false
	}
	return (author != "" && strings.EqualFold(u, strings.TrimSpace(author))) ||
		(authorUsername != "" && strings.EqualFold(u, strings.TrimSpace(authorUsername)))
}
