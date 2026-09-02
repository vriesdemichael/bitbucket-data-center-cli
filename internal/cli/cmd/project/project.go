package projectcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
)

type PermissionChecker interface {
	CheckProjectCreate(ctx context.Context) error
	CheckProjectAdmin(ctx context.Context, projectKey string) error
	InspectProjectPermissions(ctx context.Context, projectKey string) (map[string]bool, error)
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
}

func (d Dependencies) withDefaults() Dependencies {
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
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
		d.WriteJSON = func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		}
	}
	if d.WriteJSONList == nil {
		d.WriteJSONList = func(w io.Writer, v any, limitReached bool) error {
			return jsonoutput.WriteList(w, v, limitReached)
		}
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var listPaging paging.Options
	var start int

	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Project administration commands",
	}

	listPaging.RegisterPersistent(projectCmd, 25)
	projectCmd.PersistentFlags().IntVar(&start, "start", 0, "Start offset for list operations")

	var listName string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projects, err := service.List(cmd.Context(), projectservice.ListOptions{
				MaxResults: listPaging.ServiceLimit(),
				Start:      start,
				Name:       listName,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Projects{Projects: projectsFrom(projects)})
			}

			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No projects found"))
				return nil
			}

			rows := make([][]string, len(projects))
			for i, p := range projects {
				rows[i] = []string{style.Resource.Render(safeString(p.Key)), safeString(p.Name)}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&listName, "name", "", "Filter projects by name")
	projectCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			project, err := service.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleProject{Project: projectFrom(project)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Key:"), safeString(project.Key))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Name:"), safeString(project.Name))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Description:"), safeString(project.Description))
			return nil
		},
	}
	projectCmd.AddCommand(getCmd)

	var createName string
	var createDesc string
	createCmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.ProjectCreate(cmd.Context(), d.PermissionChecker, client); err != nil {
					return err
				}

				_, err := service.Get(cmd.Context(), args[0])
				predicted := "create"
				reason := "project will be created"
				if err == nil {
					predicted = "conflict"
					reason = "project key already exists"
				} else if apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.create",
						Target:          map[string]any{"project": args[0], "name": createName, "description": createDesc},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"project get"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"project key exists"}
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

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			created, err := service.Create(cmd.Context(), projectservice.CreateInput{
				Key:         args[0],
				Name:        createName,
				Description: createDesc,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleProject{Project: projectFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created project"), style.Resource.Render(safeString(created.Key)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createName, "name", "", "Project name (required)")
	createCmd.Flags().StringVar(&createDesc, "description", "", "Project description")
	_ = createCmd.MarkFlagRequired("name")
	projectCmd.AddCommand(createCmd)

	var updateName string
	var updateDesc string
	updateCmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Update project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, args[0]); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "project details will be updated"
				currentName := strings.TrimSpace(safeString(current.Name))
				currentDesc := strings.TrimSpace(safeString(current.Description))
				if strings.EqualFold(currentName, strings.TrimSpace(updateName)) && strings.EqualFold(currentDesc, strings.TrimSpace(updateDesc)) {
					predicted = "no-op"
					reason = "project already matches requested values"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.update",
						Target:          map[string]any{"project": args[0], "name": updateName, "description": updateDesc},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"project get"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			updated, err := service.Update(cmd.Context(), args[0], projectservice.UpdateInput{
				Name:        updateName,
				Description: updateDesc,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleProject{Project: projectFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated project"), style.Resource.Render(safeString(updated.Key)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "Project name")
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "Project description")
	projectCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, args[0]); err != nil {
					return err
				}

				_, err := service.Get(cmd.Context(), args[0])
				predicted := "delete"
				reason := "project will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "project was not found"
					} else {
						return err
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.delete",
						Target:          map[string]any{"project": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"project get"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), ProjectDeletion{Status: result.OK(), Project: args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted project"), style.Resource.Render(args[0]))
			return nil
		},
	}
	projectCmd.AddCommand(deleteCmd)

	projectCmd.AddCommand(newProjectPermissionsCommand(d))
	projectCmd.AddCommand(newProjectWebhookCommand(d))
	projectCmd.AddCommand(newProjectBranchRestrictionCommand(d))
	projectCmd.AddCommand(newProjectDefaultTaskCommand(d))

	return projectCmd
}

func safeString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func safeInt32(ptr *int32) int32 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

func safeUsers(users *[]openapigenerated.RestApplicationUser) []openapigenerated.RestApplicationUser {
	if users == nil {
		return nil
	}
	return *users
}

func safeStringSlice(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
}
