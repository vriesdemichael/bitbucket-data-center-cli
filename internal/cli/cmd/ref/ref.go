package refcmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
)

type Dependencies struct {
	JSONEnabled         func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		}
	}
	if d.LoadConfigAndClient == nil {
		d.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := d.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		}
	}
	if d.WriteJSON == nil {
		d.WriteJSON = jsonoutput.Write
	}
	return d
}

func resolveRefRepositoryReference(selector string, cfg config.AppConfig) (commitservice.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return commitservice.RepositoryRef{}, err
	}
	return commitservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRefRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := commitservice.NewService(client)

			refs, err := service.ListTagsAndBranches(cmd.Context(), repo, filterText)
			if err != nil {
				return err
			}

			reported := Refs{
				Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				Refs:       result.RefsFrom(refs),
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
			}

			if len(reported.Refs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No refs found"))
				return nil
			}

			rows := make([][]string, len(reported.Refs))
			for i, ref := range reported.Refs {
				rows[i] = []string{style.Resource.Render(ref.DisplayID), ref.Type, style.Secondary.Render(ref.ID)}
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRefRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := commitservice.NewService(client)

			refs, err := service.ListTagsAndBranches(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			// Find exact match
			var foundRef *result.Ref

			for _, ref := range refs {
				if ref.DisplayId != nil && *ref.DisplayId == args[0] {
					converted := result.RefFrom(ref)
					foundRef = &converted
					break
				}
			}

			if foundRef == nil {
				err := apperrors.New(apperrors.KindNotFound, fmt.Sprintf("ref not found: %s", args[0]), nil)
				return err
			}

			reported := Resolution{
				Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				Ref:        *foundRef,
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
			}
			style.WriteTable(cmd.OutOrStdout(), [][]string{{style.Resource.Render(reported.Ref.DisplayID), reported.Ref.Type, style.Secondary.Render(reported.Ref.ID)}})
			return nil
		},
	}
	refCmd.AddCommand(resolveCmd)

	return refCmd
}
