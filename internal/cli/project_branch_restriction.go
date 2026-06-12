package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
)

func newProjectBranchRestrictionCommand(options *rootOptions) *cobra.Command {
	restrictionCmd := &cobra.Command{
		Use:   "branch-restriction",
		Short: "Manage project branch restrictions",
	}

	var listType string
	var listMatcherType string
	var listMatcherID string

	listCmd := &cobra.Command{
		Use:   "list <project-key>",
		Short: "List all project branch restrictions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			restrictions, err := service.ListRestrictions(cmd.Context(), args[0], projectservice.RestrictionListOptions{
				Type:        listType,
				MatcherType: listMatcherType,
				MatcherID:   listMatcherID,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": args[0], "restrictions": restrictions})
			}

			if len(restrictions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No restrictions found"))
				return nil
			}

			rows := make([][]string, len(restrictions))
			for i, r := range restrictions {
				matcher := ""
				if r.Matcher != nil && r.Matcher.DisplayId != nil {
					matcher = *r.Matcher.DisplayId
				} else if r.Matcher != nil && r.Matcher.Id != nil {
					matcher = *r.Matcher.Id
				}

				rows[i] = []string{
					style.Secondary.Render(fmt.Sprintf("%d", safeInt32(r.Id))),
					safeString(r.Type),
					matcher,
					fmt.Sprintf("users=%d", len(safeUsers(r.Users))),
					fmt.Sprintf("groups=%d", len(safeStringSlice(r.Groups))),
				}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	listCmd.Flags().StringVar(&listType, "type", "", "Filter by restriction type (read-only, no-deletes, fast-forward-only, pull-request-only, no-creates)")
	listCmd.Flags().StringVar(&listMatcherType, "matcher-type", "", "Filter by matcher type (BRANCH, PATTERN, MODEL_BRANCH, MODEL_CATEGORY)")
	listCmd.Flags().StringVar(&listMatcherID, "matcher-id", "", "Filter by matcher ID value")
	restrictionCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <project-key> <restriction-id>",
		Short: "Get details of a single branch restriction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			restriction, err := service.GetRestriction(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": args[0], "restriction": restriction})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", style.Secondary.Render(fmt.Sprintf("id=%d", safeInt32(restriction.Id))), safeString(restriction.Type))
			return nil
		},
	}
	restrictionCmd.AddCommand(getCmd)

	var createType string
	var createMatcherID string
	var createMatcherType string
	var createMatcherDisplay string
	var createUsers []string
	var createGroups []string
	var createAccessKeyIDs []int

	createCmd := &cobra.Command{
		Use:   "create <project-key>",
		Short: "Create a new project-level restriction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(createAccessKeyIDs)
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				restrictions, err := service.ListRestrictions(cmd.Context(), args[0], projectservice.RestrictionListOptions{Limit: 1000})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "branch restriction will be created"
				for _, r := range restrictions {
					if matchesProjectRestrictionSignature(r, createType, createMatcherType, createMatcherID) {
						predicted = "conflict"
						reason = "matching branch restriction already exists"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.branch-restriction.create",
						Target:          map[string]any{"project": args[0], "type": createType, "matcher_type": createMatcherType, "matcher_id": createMatcherID},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project branch restrictions list"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"matching restriction exists"}
							}
							return nil
						}(),
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.CreateRestriction(cmd.Context(), args[0], projectservice.RestrictionUpsertInput{
				Type:           createType,
				MatcherID:      createMatcherID,
				MatcherType:    createMatcherType,
				MatcherDisplay: createMatcherDisplay,
				Users:          createUsers,
				Groups:         createGroups,
				AccessKeyIDs:   accessKeyIDs,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": args[0], "restriction": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created restriction:"), style.Secondary.Render(fmt.Sprintf("%d", safeInt32(created.Id))))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createType, "type", "", "Restriction type")
	createCmd.Flags().StringVar(&createMatcherID, "matcher-id", "", "Matcher id value")
	createCmd.Flags().StringVar(&createMatcherType, "matcher-type", "BRANCH", "Matcher type")
	createCmd.Flags().StringVar(&createMatcherDisplay, "matcher-display", "", "Matcher display value")
	createCmd.Flags().StringSliceVar(&createUsers, "user", nil, "Allowed user slugs")
	createCmd.Flags().StringSliceVar(&createGroups, "group", nil, "Allowed group names")
	createCmd.Flags().IntSliceVar(&createAccessKeyIDs, "access-key-id", nil, "Allowed SSH access key IDs")
	_ = createCmd.MarkFlagRequired("type")
	_ = createCmd.MarkFlagRequired("matcher-id")
	restrictionCmd.AddCommand(createCmd)

	var updateType string
	var updateMatcherID string
	var updateMatcherType string
	var updateMatcherDisplay string
	var updateUsers []string
	var updateGroups []string
	var updateAccessKeyIDs []int

	updateCmd := &cobra.Command{
		Use:   "update <project-key> <restriction-id>",
		Short: "Update an existing restriction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(updateAccessKeyIDs)
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				current, err := service.GetRestriction(cmd.Context(), args[0], args[1])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "branch restriction will be updated"
				if matchesProjectRestrictionUpdate(current, updateType, updateMatcherType, updateMatcherID, updateUsers, updateGroups, accessKeyIDs) {
					predicted = "no-op"
					reason = "branch restriction already matches requested values"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.branch-restriction.update",
						Target:          map[string]any{"project": args[0], "restriction_id": args[1], "type": updateType, "matcher_type": updateMatcherType, "matcher_id": updateMatcherID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project branch restriction get"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			updated, err := service.UpdateRestriction(cmd.Context(), args[0], args[1], projectservice.RestrictionUpsertInput{
				Type:           updateType,
				MatcherID:      updateMatcherID,
				MatcherType:    updateMatcherType,
				MatcherDisplay: updateMatcherDisplay,
				Users:          updateUsers,
				Groups:         updateGroups,
				AccessKeyIDs:   accessKeyIDs,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": args[0], "restriction": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated restriction:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateType, "type", "", "Restriction type")
	updateCmd.Flags().StringVar(&updateMatcherID, "matcher-id", "", "Matcher id value")
	updateCmd.Flags().StringVar(&updateMatcherType, "matcher-type", "", "Matcher type")
	updateCmd.Flags().StringVar(&updateMatcherDisplay, "matcher-display", "", "Matcher display value")
	updateCmd.Flags().StringSliceVar(&updateUsers, "user", nil, "Allowed user slugs")
	updateCmd.Flags().StringSliceVar(&updateGroups, "group", nil, "Allowed group names")
	updateCmd.Flags().IntSliceVar(&updateAccessKeyIDs, "access-key-id", nil, "Allowed SSH access key IDs")
	restrictionCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <project-key> <restriction-id>",
		Short: "Delete a project restriction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.branch-restriction.delete",
						Target:          map[string]any{"project": args[0], "restriction_id": args[1]},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "branch restriction will be deleted",
						Confidence:      capabilityFull,
						RequiredState:   []string{"project branch restriction get"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.DeleteRestriction(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "project": args[0], "restriction_id": args[1]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted restriction:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	restrictionCmd.AddCommand(deleteCmd)

	return restrictionCmd
}

func matchesProjectRestrictionSignature(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string) bool {
	currentType := strings.TrimSpace(strings.ToLower(safeString(restriction.Type)))
	requestedType := strings.TrimSpace(strings.ToLower(restrictionType))
	if currentType != requestedType {
		return false
	}

	currentMatcherType := ""
	currentMatcherID := ""
	if restriction.Matcher != nil {
		if restriction.Matcher.Type != nil && restriction.Matcher.Type.Id != nil {
			currentMatcherType = strings.TrimSpace(strings.ToUpper(string(*restriction.Matcher.Type.Id)))
		}
		currentMatcherID = strings.TrimSpace(safeString(restriction.Matcher.Id))
	}

	requestedMatcherType := strings.TrimSpace(strings.ToUpper(matcherType))
	requestedMatcherID := strings.TrimSpace(matcherID)
	return currentMatcherType == requestedMatcherType && currentMatcherID == requestedMatcherID
}

func matchesProjectRestrictionUpdate(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string, users, groups []string, accessKeyIDs []int32) bool {
	if restrictionType != "" && !strings.EqualFold(safeString(restriction.Type), restrictionType) {
		return false
	}

	if restriction.Matcher != nil {
		if matcherID != "" && safeString(restriction.Matcher.Id) != matcherID {
			return false
		}
		if matcherType != "" && restriction.Matcher.Type != nil && restriction.Matcher.Type.Id != nil {
			if !strings.EqualFold(string(*restriction.Matcher.Type.Id), matcherType) {
				return false
			}
		}
	}

	currentUsers := make([]string, 0)
	if restriction.Users != nil {
		for _, u := range *restriction.Users {
			if u.Name != nil {
				currentUsers = append(currentUsers, *u.Name)
			}
		}
	}

	currentGroups := make([]string, 0)
	if restriction.Groups != nil {
		currentGroups = *restriction.Groups
	}

	currentAccessKeys := make([]int32, 0)
	if restriction.AccessKeys != nil {
		for _, k := range *restriction.AccessKeys {
			if k.Key != nil && k.Key.Id != nil {
				currentAccessKeys = append(currentAccessKeys, *k.Key.Id)
			}
		}
	}

	normalizedCurrentUsers := make([]string, len(currentUsers))
	for i, u := range currentUsers {
		normalizedCurrentUsers[i] = strings.ToLower(u)
	}
	requestedUsers := make([]string, len(users))
	for i, u := range users {
		requestedUsers[i] = strings.ToLower(u)
	}

	normalizedCurrentGroups := make([]string, len(currentGroups))
	for i, g := range currentGroups {
		normalizedCurrentGroups[i] = strings.ToLower(g)
	}
	requestedGroups := make([]string, len(groups))
	for i, g := range groups {
		requestedGroups[i] = strings.ToLower(g)
	}

	slices.Sort(normalizedCurrentUsers)
	slices.Sort(requestedUsers)
	slices.Sort(normalizedCurrentGroups)
	slices.Sort(requestedGroups)

	requestedAccessKeys := append([]int32(nil), accessKeyIDs...)
	slices.Sort(requestedAccessKeys)
	slices.Sort(currentAccessKeys)

	return slices.Equal(normalizedCurrentUsers, requestedUsers) &&
		slices.Equal(normalizedCurrentGroups, requestedGroups) &&
		slices.Equal(currentAccessKeys, requestedAccessKeys)
}
