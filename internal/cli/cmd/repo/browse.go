package repocmd

import (
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newRepoBrowseCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	browseCmd := &cobra.Command{
		Use:   "browse",
		Short: "Repository content browsing commands",
	}

	browseCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var treeAt string
	var treePaging paging.Options
	treeCmd := &cobra.Command{
		Use:   "tree [path]",
		Short: "List repository files in a directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			path := ""
			if len(args) > 0 {
				path = args[0]
			}

			files, err := service.Tree(cmd.Context(), repo, path, browseservice.TreeOptions{
				At:       treeAt,
				PageSize: treePaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Tree{Repository: browseRepositoryOf(repo), Path: path, Files: files})
			}

			if len(files) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No files found"))
				return nil
			}

			for _, file := range files {
				fmt.Fprintln(cmd.OutOrStdout(), file)
			}

			return nil
		},
	}
	treeCmd.Flags().StringVar(&treeAt, "at", "", "Commit ID or ref to browse")
	treePaging.Register(treeCmd, 1000)
	browseCmd.AddCommand(treeCmd)

	var rawAt string
	rawCmd := &cobra.Command{
		Use:   "raw <path>",
		Short: "Get raw file content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			content, err := service.Raw(cmd.Context(), repo, args[0], rawAt)
			if err != nil {
				return err
			}

			// Raw means the bytes, unwrapped -- but only without --json. Under
			// --json stdout is one bb.machine document (ADR-014), and this used
			// to write the file there instead, which is not a document at all.
			// bb repo cat reads the same endpoint and already answered this way.
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), rawFileFrom(browseRepositoryOf(repo), args[0], rawAt, content))
			}

			_, _ = cmd.OutOrStdout().Write(content)
			return nil
		},
	}
	rawCmd.Flags().StringVar(&rawAt, "at", "", "Commit ID or ref")
	browseCmd.AddCommand(rawCmd)

	var fileAt string
	fileCmd := &cobra.Command{
		Use:   "file <path>",
		Short: "Get structured file content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			content, err := service.File(cmd.Context(), repo, args[0], browseservice.FileOptions{
				At:    fileAt,
				Blame: false,
			})
			if err != nil {
				return err
			}

			lines, binary, complete := fileLinesFrom(content)
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), FileContent{
					Repository: browseRepositoryOf(repo),
					Path:       args[0],
					At:         fileAt,
					Binary:     binary,
					Complete:   complete,
					Lines:      lines,
				})
			}

			if binary {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("Binary file; use bb repo cat to read the bytes"))
				return nil
			}

			for _, line := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), line.Text)
			}
			if !complete {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Empty.Render("Truncated: Bitbucket returned one page of a longer file"))
			}

			return nil
		},
	}
	fileCmd.Flags().StringVar(&fileAt, "at", "", "Commit ID or ref")
	browseCmd.AddCommand(fileCmd)

	var blameAt string
	blameCmd := &cobra.Command{
		Use:   "blame <path>",
		Short: "Get file blame",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			content, err := service.File(cmd.Context(), repo, args[0], browseservice.FileOptions{
				At:    blameAt,
				Blame: true,
			})
			if err != nil {
				return err
			}

			lines, binary, complete := fileLinesFrom(content)
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), FileContent{
					Repository: browseRepositoryOf(repo),
					Path:       args[0],
					At:         blameAt,
					Binary:     binary,
					Complete:   complete,
					Lines:      lines,
				})
			}

			if binary {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("Binary file; there is nothing to attribute"))
				return nil
			}

			for _, line := range lines {
				author := line.Author
				if author == "" {
					author = "unknown"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", author, line.Text)
			}
			if !complete {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Empty.Render("Truncated: Bitbucket returned one page of a longer file"))
			}

			return nil
		},
	}
	blameCmd.Flags().StringVar(&blameAt, "at", "", "Commit ID or ref")
	browseCmd.AddCommand(blameCmd)

	var historyPaging paging.Options
	historyCmd := &cobra.Command{
		Use:   "history <path>",
		Short: "List commit history for a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			commits, err := service.List(cmd.Context(), repo, commitservice.ListOptions{MaxResults: historyPaging.ServiceLimit(), Path: args[0]})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), FileHistory{Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, Path: args[0], Commits: result.CommitsFrom(commits)})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commit history found"))
				return nil
			}

			rows := make([][]string, len(commits))
			for i, commit := range commits {
				rows[i] = []string{style.Secondary.Render(safederef.String(commit.DisplayId)), strings.Split(safederef.String(commit.Message), "\n")[0]}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	historyPaging.Register(historyCmd, 25)
	browseCmd.AddCommand(historyCmd)

	return browseCmd
}
