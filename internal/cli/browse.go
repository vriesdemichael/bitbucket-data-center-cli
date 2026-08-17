package cli

import (
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	browseservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/browse"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newRepoBrowseCommand(options *rootOptions) *cobra.Command {
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
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
				At:    treeAt,
				Limit: treePaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "path": path, "files": files})
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			content, err := service.Raw(cmd.Context(), repo, args[0], rawAt)
			if err != nil {
				return err
			}

			// For raw, we always output raw bytes even if --json is set, since it's "raw"
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
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

			if options.JSON {
				// Parse and format json for nicer output
				var parsed any
				if err := json.Unmarshal(content, &parsed); err == nil {
					return writeJSON(cmd.OutOrStdout(), parsed)
				}
				_, _ = cmd.OutOrStdout().Write(content)
				return nil
			}

			// Unmarshal specifically to print lines
			var parsed struct {
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.Unmarshal(content, &parsed); err == nil {
				for _, line := range parsed.Lines {
					fmt.Fprintln(cmd.OutOrStdout(), line.Text)
				}
			} else {
				_, _ = cmd.OutOrStdout().Write(content)
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
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

			if options.JSON {
				var parsed any
				if err := json.Unmarshal(content, &parsed); err == nil {
					return writeJSON(cmd.OutOrStdout(), parsed)
				}
				_, _ = cmd.OutOrStdout().Write(content)
				return nil
			}

			var parsed struct {
				Blame struct {
					Author map[string]string `json:"author"`
				} `json:"blame"`
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.Unmarshal(content, &parsed); err == nil {
				author := "unknown"
				if name, ok := parsed.Blame.Author["name"]; ok {
					author = name
				}
				for _, line := range parsed.Lines {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", author, line.Text)
				}
				return nil
			}

			_, _ = cmd.OutOrStdout().Write(content)
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

			commits, err := service.List(cmd.Context(), repo, commitservice.ListOptions{Limit: historyPaging.ServiceLimit(), Path: args[0]})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "path": args[0], "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commit history found"))
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
	historyPaging.Register(historyCmd, 25)
	browseCmd.AddCommand(historyCmd)

	return browseCmd
}
