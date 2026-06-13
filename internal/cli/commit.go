package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
	jiraservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newCommitCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var limit int

	commitCmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit inspection and compare commands",
	}

	commitCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	commitCmd.PersistentFlags().IntVar(&limit, "limit", 25, "Page size for list operations")

	var listPath string
	var listJira string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository commits",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			// Map to commit service repo ref
			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}

			var commits []openapigenerated.RestCommit
			if strings.TrimSpace(listJira) != "" {
				jiraService := jiraservice.NewService(httpclient.NewFromConfig(cfg))
				commits, err = jiraService.GetIssueCommits(cmd.Context(), strings.TrimSpace(listJira), limit)
				if err != nil {
					return err
				}
			} else {
				service := commitservice.NewService(client)
				commits, err = service.List(cmd.Context(), repo, commitservice.ListOptions{Limit: limit, Path: listPath})
				if err != nil {
					return err
				}
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commits found"))
				return nil
			}

			rows := make([][]string, len(commits))
			for i, commit := range commits {
				rows[i] = []string{style.Secondary.Render(safeString(commit.DisplayId)), strings.Split(safeString(commit.Message), "\n")[0]}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&listPath, "path", "", "Filter commits by file path")
	listCmd.Flags().StringVar(&listJira, "jira", "", "List commits associated with a Jira issue key")
	commitCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a specific commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			commit, err := service.Get(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commit": commit})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Commit:"), style.Secondary.Render(safeString(commit.Id)))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Message:"), safeString(commit.Message))
			return nil
		},
	}
	commitCmd.AddCommand(getCmd)

	compareCmd := &cobra.Command{
		Use:   "compare <from> <to>",
		Short: "Compare two commits or refs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			commits, err := service.Compare(cmd.Context(), repo, commitservice.CompareOptions{
				From:  args[0],
				To:    args[1],
				Limit: limit,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commits found between refs"))
				return nil
			}

			rows := make([][]string, len(commits))
			for i, commit := range commits {
				rows[i] = []string{style.Secondary.Render(safeString(commit.DisplayId)), strings.Split(safeString(commit.Message), "\n")[0]}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	commitCmd.AddCommand(compareCmd)

	prsCmd := &cobra.Command{
		Use:   "prs <commitId>",
		Short: "List pull requests containing a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := pullrequestservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := pullrequestservice.NewService(nil).WithAPIClient(apiClient)

			prs, err := service.ListPullRequestsContainingCommit(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_requests": prs})
			}

			if len(prs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No pull requests containing commit found"))
				return nil
			}

			rows := make([][]string, len(prs))
			for i, pr := range prs {
				idStr := fmt.Sprintf("#%d", pr.ID)
				rows[i] = []string{style.Secondary.Render(idStr), pr.Title, pr.State}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	commitCmd.AddCommand(prsCmd)

	return commitCmd
}

func newRefCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	refCmd := &cobra.Command{
		Use:   "ref",
		Short: "Repository ref resolution and listing commands",
	}

	refCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var filterText string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository refs (branches and tags)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			refs, err := service.ListTagsAndBranches(cmd.Context(), repo, filterText)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "refs": refs})
			}

			if len(refs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No refs found"))
				return nil
			}

			rows := make([][]string, len(refs))
			for i, ref := range refs {
				t := ""
				if ref.Type != nil {
					t = string(*ref.Type)
				}
				rows[i] = []string{style.Resource.Render(safeString(ref.DisplayId)), t, style.Secondary.Render(safeString(ref.Id))}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&filterText, "filter", "", "Filter refs by name")
	refCmd.AddCommand(listCmd)

	resolveCmd := &cobra.Command{
		Use:   "resolve <name>",
		Short: "Resolve a ref by name to its full ref and commit if applicable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			refs, err := service.ListTagsAndBranches(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			// Find exact match
			var foundRef map[string]any

			for _, ref := range refs {
				if ref.DisplayId != nil && *ref.DisplayId == args[0] {
					foundRef = map[string]any{
						"id":        safeString(ref.Id),
						"displayId": safeString(ref.DisplayId),
					}
					if ref.Type != nil {
						foundRef["type"] = string(*ref.Type)
					}
					break
				}
			}

			if foundRef == nil {
				err := apperrors.New(apperrors.KindNotFound, fmt.Sprintf("ref not found: %s", args[0]), nil)
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "ref": foundRef})
			}

			t := ""
			if val, ok := foundRef["type"].(string); ok {
				t = val
			}
			style.WriteTable(cmd.OutOrStdout(), [][]string{{style.Resource.Render(fmt.Sprintf("%v", foundRef["displayId"])), t, style.Secondary.Render(fmt.Sprintf("%v", foundRef["id"]))}})
			return nil
		},
	}
	refCmd.AddCommand(resolveCmd)

	return refCmd
}
