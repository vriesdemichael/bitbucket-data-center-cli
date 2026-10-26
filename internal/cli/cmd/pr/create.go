package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prompt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	codeowners "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestCreateCommand(deps Dependencies, repository *string) *cobra.Command {
	var createFromRef string
	var createFromRepo string
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
		Long: "Create a pull request.\n\n" +
			"Run inside a checkout, bb works out what it is not told: --from-ref is the branch that is checked out, " +
			"--to-ref is the repository's default branch, and --title is the subject of the branch's commit when it " +
			"holds exactly one. At a terminal each is offered for you to accept or change. With nobody to ask they are " +
			"used as they are, and --title is still required for a branch that holds several commits. The checked-out " +
			"branch is only taken unasked when the repository itself came from the checkout rather than from --repo.",
		Example: "  # Inside a checkout: from the checked-out branch into the repository's default branch\n" +
			"  bb pr create\n\n" +
			"  # Create a pull request (automatically includes default reviewers and CODEOWNERS)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" \\\n" +
			"    --description \"Why it is needed, and how to test it.\"\n\n" +
			"  # Create a draft pull request\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --draft\n\n" +
			"  # Create a pull request with reviewers and reviewer groups (repeatable or comma-separated)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" \\\n" +
			"    --reviewers alice,@backend-team --reviewer-group qa-team\n\n" +
			"  # Create a pull request without default reviewers or CODEOWNERS\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" \\\n" +
			"    --no-default-reviewers --no-codeowners\n\n" +
			"  # Create a pull request from a branch of a fork into the repository it was forked from\n" +
			"  bb pr create --repo PROJ/repo --from-repo SANDBOX/repo-experiment --from-ref feature/x \\\n" +
			"    --to-ref main --title \"My change\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before the configuration is loaded, because MarkFlagRequired ran
			// before RunE and this replaces it. Loading first would report "no
			// Bitbucket host configured" to someone whose real problem is a
			// missing --title, which is the round trip this is meant to remove.
			// FillMissing needs nothing from the config, and an inference that
			// does loads it for itself.
			inference := &createInference{
				ctx: cmd.Context(), deps: deps, repository: *repository,
				fromRepo: createFromRepo, fromRef: &createFromRef, toRef: &createToRef,
			}
			if strings.TrimSpace(createFromRef) == "" || strings.TrimSpace(createToRef) == "" || strings.TrimSpace(createTitle) == "" {
				if err := inference.requireSourceBranch(); err != nil {
					return err
				}
			}
			if err := prompt.FillMissing(prompt.RequestFor(cmd, deps.JSONEnabled()), []prompt.Missing{
				{Flag: "--from-ref", Question: "Source branch", Value: &createFromRef, Infer: inference.sourceBranch},
				{Flag: "--to-ref", Question: "Target branch", Value: &createToRef, Infer: inference.targetBranch},
				{Flag: "--title", Question: "Title", Value: &createTitle, Infer: inference.title},
			}); err != nil {
				return err
			}

			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(*repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			// Resolved here rather than beside the create call, because the
			// default reviewer lookup below needs it too: that query names the
			// source repository, and giving it the target instead made
			// Bitbucket look for the source branch upstream and answer 404.
			// With --default-reviewers defaulting to on, that failed every fork
			// pull request whose author did not think to switch it off.
			fromRepo, err := resolveSourceRepository(createFromRepo, repo)
			if err != nil {
				return err
			}

			if deps.DryRunEnabled() {
				// Read, not write. Bitbucket requires REPO_READ on the repository a
				// pull request targets, and fork contributors legitimately hold only
				// that upstream -- checking for write refused the standard
				// contribution flow outright (#506).
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}
			}
			if err := inference.requireSourceBranch(); err != nil {
				return err
			}

			rawClient := httpclient.NewFromConfig(cfg)
			service := pullrequestservice.NewService(rawClient)

			author := resolveCurrentUsername(cmd.Context(), rawClient, cfg)

			reviewerSvc := reviewerservice.NewService(apiClient)

			var allReviewers []string
			var allGroups []string
			allReviewers = append(allReviewers, createReviewers...)
			allGroups = append(allGroups, createReviewerGroups...)

			warn := cmd.ErrOrStderr()

			if createDefaultReviewers && !createNoDefaultReviewers {
				// `bb pr create` always opens the pull request within one
				// repository, so the source repository needs no separate entry.
				query := reviewerservice.DefaultReviewerQuery{
					SourceRef: createFromRef,
					TargetRef: createToRef,
				}
				if fromRepo != nil {
					query.SourceProjectKey = fromRepo.ProjectKey
					query.SourceSlug = fromRepo.Slug
				}

				defaults, err := reviewerSvc.ResolveDefaultReviewers(cmd.Context(), repo.ProjectKey, repo.Slug, query)
				if err != nil {
					// Default reviewers are frequently mandatory approvers, so a
					// failed lookup must never pass unnoticed. When the user asked
					// for them the command fails; when they are only on by default
					// the pull request is still created, but loudly.
					if cmd.Flags().Changed("default-reviewers") {
						return err
					}
					writeWarning(warn, fmt.Sprintf("could not resolve default reviewers (%v); creating the pull request without them", err))
				} else {
					allReviewers = append(allReviewers, defaults...)
				}
			}

			if createCodeOwners && !createNoCodeOwners {
				codeOwners, err := resolveCodeOwnersReviewers(
					cmd.Context(),
					cfg,
					codeowners.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					codeOwnersSourceRepository(fromRepo),
					createFromRef, createToRef,
					author,
				)
				switch {
				case err == nil:
					allReviewers = append(allReviewers, codeOwners...)
				case openapi.IsRouteMissing(err):
					// This server does not offer the endpoint -- a Bitbucket
					// without the code-owners module. A repository that simply
					// has no CODEOWNERS file is not this case: that answers 200
					// with an empty list and arrives above.
				case cmd.Flags().Changed("codeowners"):
					return err
				default:
					writeWarning(warn, fmt.Sprintf("could not resolve code owners (%v); creating the pull request without them", err))
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
					MaxResults:   pullrequestservice.AllResults,
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

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "fromRef": createFromRef, "toRef": createToRef, "title": createTitle, "reviewers": resolvedReviewers, "draft": createDraft},
					Action:          "create",
					PredictedAction: predicted,
					Reason:          reason,
					BlockingReasons: func() []string {
						if predicted == "conflict" {
							return []string{"matching open pull request exists"}
						}
						return nil
					}(),
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			created, err := service.Create(cmd.Context(), repo, pullrequestservice.CreateInput{
				FromRef:        createFromRef,
				ToRef:          createToRef,
				Title:          createTitle,
				Description:    createDescription,
				Reviewers:      resolvedReviewers,
				Draft:          createDraft,
				FromRepository: fromRepo,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created pull request #%d\n", created.ID)
			// On a line of its own: it is what a person wants next, to open or
			// to paste, and a terminal links a bare URL.
			if created.URL != "" {
				fmt.Fprintln(cmd.OutOrStdout(), created.URL)
			}
			return nil
		},
	}
	createCmd.Flags().StringVar(&createFromRef, "from-ref", "", "Source branch (name or refs/heads/name); in a checkout, the checked-out branch")
	createCmd.Flags().StringVar(&createFromRepo, "from-repo", "", "Repository holding --from-ref as PROJECT/slug, for a fork to upstream pull request (defaults to --repo)")
	createCmd.Flags().StringVar(&createToRef, "to-ref", "", "Target branch (name or refs/heads/name); the repository's default branch when left out")
	createCmd.Flags().StringVar(&createTitle, "title", "", "Pull request title; the commit subject when the branch holds exactly one commit")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Pull request description")
	createCmd.Flags().StringSliceVar(&createReviewers, "reviewers", nil, "Reviewer usernames to add (repeatable or comma-separated, accepts @group syntax, e.g. --reviewers alice,@backend-team)")
	createCmd.Flags().StringSliceVar(&createReviewerGroups, "reviewer-group", nil, "Reviewer group name(s) to expand and add (repeatable or comma-separated; a leading @ and a reviewer-group/ prefix are both accepted; alias --reviewer-groups)")
	createCmd.Flags().SetNormalizeFunc(createReviewerFlagAliases)
	createCmd.Flags().BoolVar(&createDefaultReviewers, "default-reviewers", true, "Include default reviewers configured on repository/project; a failed lookup warns, unless this flag is passed explicitly, which makes it fatal")
	createCmd.Flags().BoolVar(&createNoDefaultReviewers, "no-default-reviewers", false, "Do not include default reviewers")
	createCmd.Flags().BoolVar(&createCodeOwners, "codeowners", true, "Assign the code owners Bitbucket reports for the change, the same ones the web interface offers; the CODEOWNERS syntax and its meaning are the server's")
	createCmd.Flags().BoolVar(&createNoCodeOwners, "no-codeowners", false, "Do not assign code owners")
	createCmd.Flags().BoolVar(&createDraft, "draft", false, "Create as a draft pull request")
	// Not MarkFlagRequired: Cobra rejects before RunE, which forecloses asking
	// a person who is there. FillMissing enforces the same requirement and, when
	// nobody is there, produces the same message naming every absent flag at
	// once (ADR-073).
	return createCmd
}
