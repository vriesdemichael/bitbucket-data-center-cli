package auth

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/gpgkey"
)

func newGpgKeyCommand(deps Dependencies) *cobra.Command {
	gpgCmd := &cobra.Command{
		Use:   "gpg-key",
		Short: "Manage personal GPG keys",
	}

	isJSON := func() bool {
		if deps.JSONEnabled == nil {
			return false
		}
		return deps.JSONEnabled()
	}

	var limit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List personal GPG keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := gpgkey.NewService(client)

			keys, err := svc.ListGpgKeys(cmd.Context(), limit)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), keys)
			}

			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No GPG keys found"))
				return nil
			}

			rows := make([][]string, len(keys))
			for i, k := range keys {
				id := ""
				if k.Id != nil {
					id = *k.Id
				}
				email := ""
				if k.EmailAddress != nil {
					email = *k.EmailAddress
				}
				fingerprint := ""
				if k.Fingerprint != nil {
					fingerprint = *k.Fingerprint
				}
				expiryStr := "never"
				if k.ExpiryDate != nil && *k.ExpiryDate > 0 {
					t := time.Unix(*k.ExpiryDate/1000, 0)
					expiryStr = t.Format("2006-01-02")
				}
				rows[i] = []string{style.Secondary.Render(id), style.Resource.Render(email), expiryStr, fingerprint}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	listCmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of GPG keys to list")
	gpgCmd.AddCommand(listCmd)

	addCmd := &cobra.Command{
		Use:   "add <key-file-or-text>",
		Short: "Add a personal GPG key",
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
			svc := gpgkey.NewService(client)

			keyContent, err := readGpgKey(args[0])
			if err != nil {
				return err
			}

			key, err := svc.AddGpgKey(cmd.Context(), keyContent)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), key)
			}

			id := ""
			if key.Id != nil {
				id = *key.Id
			}
			fmt.Fprintf(cmd.OutOrStdout(), "GPG key %s added successfully\n", style.Secondary.Render(id))
			return nil
		},
	}
	gpgCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <id-or-fingerprint>",
		Short: "Remove a personal GPG key",
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
			svc := gpgkey.NewService(client)

			if err := svc.RemoveGpgKey(cmd.Context(), args[0]); err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "removed": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "GPG key %s removed successfully\n", style.Secondary.Render(args[0]))
			return nil
		},
	}
	gpgCmd.AddCommand(removeCmd)

	var yesFlag bool
	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear all personal GPG keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yesFlag && !isJSON() {
				fmt.Fprint(cmd.OutOrStdout(), "Are you sure you want to clear all GPG keys? (y/N): ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					return apperrors.New(apperrors.KindValidation, "action cancelled", nil)
				}
			}

			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return err
			}
			svc := gpgkey.NewService(client)

			if err := svc.ClearGpgKeys(cmd.Context()); err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
			}

			fmt.Fprintln(cmd.OutOrStdout(), "All GPG keys cleared successfully")
			return nil
		},
	}
	clearCmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Confirm clearing of all GPG keys")
	gpgCmd.AddCommand(clearCmd)

	return gpgCmd
}

func readGpgKey(arg string) (string, error) {
	if _, err := os.Stat(arg); err == nil {
		content, err := os.ReadFile(arg)
		if err != nil {
			return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read key file %s", arg), err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return arg, nil
}
