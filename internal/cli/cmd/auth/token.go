package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/token"
)

type tokenScope struct {
	user    string
	project string
	repo    string
}

func resolveTokenScope(ctx context.Context, cfg config.AppConfig, newUsersClient func(config.AppConfig) (usersClient, error), scope tokenScope) (token.ScopeType, string, error) {
	count := 0
	if scope.user != "" {
		count++
	}
	if scope.project != "" {
		count++
	}
	if scope.repo != "" {
		count++
	}

	if count > 1 {
		return "", "", apperrors.New(apperrors.KindValidation, "only one of --user, --project, or --repo scope can be specified", nil)
	}

	if scope.project != "" {
		return token.ScopeProject, scope.project, nil
	}
	if scope.repo != "" {
		return token.ScopeRepo, scope.repo, nil
	}

	// Default to user scope
	userSlug := scope.user
	if userSlug == "" {
		// Attempt to resolve the current authenticated user slug
		identity, err := resolveIdentity(ctx, cfg, newUsersClient)
		if err != nil {
			return "", "", apperrors.New(apperrors.KindValidation, "failed to resolve current user slug; please specify --user slug explicitly", err)
		}
		userSlug = identity.Slug
	}

	return token.ScopeUser, userSlug, nil
}

func newTokenCommand(deps Dependencies) *cobra.Command {
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Manage HTTP access tokens",
	}

	isJSON := func() bool {
		if deps.JSONEnabled == nil {
			return false
		}
		return deps.JSONEnabled()
	}

	var scope tokenScope
	var limit int

	// Define persistent scope flags on the parent token command so they are inherited by all subcommands
	tokenCmd.PersistentFlags().StringVar(&scope.user, "user", "", "User slug for personal access token scope")
	tokenCmd.PersistentFlags().StringVar(&scope.project, "project", "", "Project key for project access token scope")
	tokenCmd.PersistentFlags().StringVar(&scope.repo, "repo", "", "Repository reference (projectKey/repositorySlug) for repository access token scope")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List HTTP access tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := token.NewService(client)

			scopeType, target, err := resolveTokenScope(cmd.Context(), cfg, deps.NewUsersClient, scope)
			if err != nil {
				return err
			}

			tokens, err := svc.List(cmd.Context(), scopeType, target, limit)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), tokens)
			}

			if len(tokens) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No HTTP access tokens found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-30s %-25s\n", "ID", "NAME", "CREATED DATE")
			for _, t := range tokens {
				id := ""
				if t.Id != nil {
					id = *t.Id
				}
				name := ""
				if t.Name != nil {
					name = *t.Name
				}
				created := ""
				if t.CreatedDate != nil {
					created = t.CreatedDate.Format(time.RFC3339)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-30s %-25s\n", id, name, created)
			}
			return nil
		},
	}
	listCmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of tokens to list")
	tokenCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get an HTTP access token by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := token.NewService(client)

			scopeType, target, err := resolveTokenScope(cmd.Context(), cfg, deps.NewUsersClient, scope)
			if err != nil {
				return err
			}

			t, err := svc.Get(cmd.Context(), scopeType, target, args[0])
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), t)
			}

			id := ""
			if t.Id != nil {
				id = *t.Id
			}
			name := ""
			if t.Name != nil {
				name = *t.Name
			}
			created := ""
			if t.CreatedDate != nil {
				created = t.CreatedDate.Format(time.RFC3339)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:           %s\n", id)
			fmt.Fprintf(cmd.OutOrStdout(), "Name:         %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "Created Date: %s\n", created)
			return nil
		},
	}
	tokenCmd.AddCommand(getCmd)

	var createName string
	var createPerms []string
	var createExpiry int
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create an HTTP access token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := createName
			if len(args) > 0 {
				name = args[0]
			}
			if strings.TrimSpace(name) == "" {
				return apperrors.New(apperrors.KindValidation, "token name is required; pass as an argument or via --name", nil)
			}

			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := token.NewService(client)

			scopeType, target, err := resolveTokenScope(cmd.Context(), cfg, deps.NewUsersClient, scope)
			if err != nil {
				return err
			}

			t, err := svc.Create(cmd.Context(), scopeType, target, name, createPerms, createExpiry)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), t)
			}

			secret := ""
			if t.Token != nil {
				secret = *t.Token
			}
			id := ""
			if t.Id != nil {
				id = *t.Id
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Token created successfully!\n")
			fmt.Fprintf(cmd.OutOrStdout(), "ID:     %s\n", id)
			fmt.Fprintf(cmd.OutOrStdout(), "Secret: %s\n", secret)
			fmt.Fprintln(cmd.OutOrStdout(), "Warning: This is the only time the token secret will be displayed. Store it securely.")
			return nil
		},
	}
	createCmd.Flags().StringVar(&createName, "name", "", "Name of the access token")
	createCmd.Flags().StringSliceVar(&createPerms, "permission", nil, "Permissions to grant (repeat flag or separate with commas)")
	createCmd.Flags().IntVar(&createExpiry, "expiry-days", 0, "Number of days before the token expires (0 for never)")
	tokenCmd.AddCommand(createCmd)

	var updateName string
	var updatePerms []string
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an HTTP access token name or permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := token.NewService(client)

			scopeType, target, err := resolveTokenScope(cmd.Context(), cfg, deps.NewUsersClient, scope)
			if err != nil {
				return err
			}

			t, err := svc.Update(cmd.Context(), scopeType, target, args[0], updateName, updatePerms)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), t)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Token %s updated successfully\n", args[0])
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "New name for the access token")
	updateCmd.Flags().StringSliceVar(&updatePerms, "permission", nil, "New permissions to set (repeat flag or separate with commas)")
	tokenCmd.AddCommand(updateCmd)

	revokeCmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an HTTP access token by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := token.NewService(client)

			scopeType, target, err := resolveTokenScope(cmd.Context(), cfg, deps.NewUsersClient, scope)
			if err != nil {
				return err
			}

			if err := svc.Revoke(cmd.Context(), scopeType, target, args[0]); err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Token %s revoked successfully\n", args[0])
			return nil
		},
	}
	tokenCmd.AddCommand(revokeCmd)

	return tokenCmd
}
