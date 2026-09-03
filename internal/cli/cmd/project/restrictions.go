package projectcmd

import (
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"math"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
)

func newProjectBranchRestrictionCommand(deps Dependencies) *cobra.Command {
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
			_, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Restrictions{Project: args[0], Restrictions: result.RestrictionsFrom(restrictions)})
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
					style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(r.Id))),
					safederef.String(r.Type),
					matcher,
					fmt.Sprintf("users=%d", len(safeUsers(r.Users))),
					fmt.Sprintf("groups=%d", len(safederef.StringSlice(r.Groups))),
				}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	enumflag.Register(listCmd.Flags(), &listType, "type", "", result.RestrictionTypes, "Filter by restriction type")
	enumflag.Register(listCmd.Flags(), &listMatcherType, "matcher-type", "", openapi.RestrictionMatcherTypes, "Filter by matcher type")
	listCmd.Flags().StringVar(&listMatcherID, "matcher-id", "", "Filter by matcher ID value")
	restrictionCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <project-key> <restriction-id>",
		Short: "Get details of a single branch restriction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			restriction, err := service.GetRestriction(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Project: args[0], Restriction: result.RestrictionFrom(restriction)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", style.Secondary.Render(fmt.Sprintf("id=%d", safederef.Int32(restriction.Id))), safederef.String(restriction.Type))
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
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(createAccessKeyIDs)
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), deps.PermissionChecker, client, args[0]); err != nil {
					return err
				}

				restrictions, err := service.ListRestrictions(cmd.Context(), args[0], projectservice.RestrictionListOptions{MaxResults: projectservice.AllResults})
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "project.branch-restriction.create",
					Target:          map[string]any{"project": args[0], "type": createType, "matcherType": createMatcherType, "matcherId": createMatcherID},
					Action:          "create",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project branch restrictions list"},
					BlockingReasons: func() []string {
						if predicted == "conflict" {
							return []string{"matching restriction exists"}
						}
						return nil
					}(),
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Project: args[0], Restriction: result.RestrictionFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created restriction:"), style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(created.Id))))
			return nil
		},
	}
	enumflag.Register(createCmd.Flags(), &createType, "type", "", result.RestrictionTypes, "Restriction type")
	createCmd.Flags().StringVar(&createMatcherID, "matcher-id", "", "Matcher id value")
	enumflag.Register(createCmd.Flags(), &createMatcherType, "matcher-type", "BRANCH", openapi.RestrictionMatcherTypes, "Matcher type")
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
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(updateAccessKeyIDs)
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), deps.PermissionChecker, client, args[0]); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "project.branch-restriction.update",
					Target:          map[string]any{"project": args[0], "restrictionId": args[1], "type": updateType, "matcherType": updateMatcherType, "matcherId": updateMatcherID},
					Action:          "update",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project branch restriction get"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Project: args[0], Restriction: result.RestrictionFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated restriction:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	enumflag.Register(updateCmd.Flags(), &updateType, "type", "", result.RestrictionTypes, "Restriction type")
	updateCmd.Flags().StringVar(&updateMatcherID, "matcher-id", "", "Matcher id value")
	// Required for the same reason as its branch-scoped twin: an empty value
	// reaches normalizeRestrictionRequestMatcherType, which reads it as BRANCH,
	// so omitting the flag silently rewrote the matcher.
	enumflag.Register(updateCmd.Flags(), &updateMatcherType, "matcher-type", "", openapi.RestrictionMatcherTypes, "Matcher type")
	// The service already rejects an empty type or matcher id, so these were
	// never silent -- but they failed after the request was built rather than
	// at parse time, and the branch-scoped twin requires both (ADR-054).
	_ = updateCmd.MarkFlagRequired("type")
	_ = updateCmd.MarkFlagRequired("matcher-id")
	_ = updateCmd.MarkFlagRequired("matcher-type")
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
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), deps.PermissionChecker, client, args[0]); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "project.branch-restriction.delete",
					Target:          map[string]any{"project": args[0], "restrictionId": args[1]},
					Action:          "delete",
					PredictedAction: "delete",
					Supported:       true,
					Reason:          "branch restriction will be deleted",
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project branch restriction get"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.DeleteRestriction(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), RestrictionDeletion{Status: result.OK(), Project: args[0], RestrictionID: args[1]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted restriction:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	restrictionCmd.AddCommand(deleteCmd)

	return restrictionCmd
}

func normalizeAccessKeyIDs(values []int) ([]int32, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make([]int32, len(values))
	for i, v := range values {
		if v < 0 || v > math.MaxInt32 {
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid access key id: %d", v), nil)
		}
		result[i] = int32(v)
	}
	return result, nil
}

func matchesProjectRestrictionSignature(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string) bool {
	currentType := strings.TrimSpace(strings.ToLower(safederef.String(restriction.Type)))
	requestedType := strings.TrimSpace(strings.ToLower(restrictionType))
	if currentType != requestedType {
		return false
	}

	currentMatcherType := ""
	currentMatcherID := ""
	if restriction.Matcher != nil {
		if restriction.Matcher.Type != nil {
			currentMatcherType = strings.TrimSpace(strings.ToUpper(string(restriction.Matcher.Type.Id)))
		}
		currentMatcherID = strings.TrimSpace(safederef.String(restriction.Matcher.Id))
	}

	requestedMatcherType := strings.TrimSpace(strings.ToUpper(matcherType))
	requestedMatcherID := strings.TrimSpace(matcherID)
	return currentMatcherType == requestedMatcherType && currentMatcherID == requestedMatcherID
}

func matchesProjectRestrictionUpdate(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string, users, groups []string, accessKeyIDs []int32) bool {
	if restrictionType != "" && !strings.EqualFold(safederef.String(restriction.Type), restrictionType) {
		return false
	}

	if restriction.Matcher != nil {
		if matcherID != "" && safederef.String(restriction.Matcher.Id) != matcherID {
			return false
		}
		if matcherType != "" && restriction.Matcher.Type != nil {
			if !strings.EqualFold(string(restriction.Matcher.Type.Id), matcherType) {
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
