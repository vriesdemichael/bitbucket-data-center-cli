package auth

import (
	"fmt"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prompt"
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

	var listPaging paging.Options
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

			keys, err := svc.ListGpgKeys(cmd.Context(), listPaging.ServiceLimit())
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
	listPaging.Register(listCmd, 25)
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

			keys, err := svc.AddGpgKey(cmd.Context(), keyContent)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"keys": keys})
			}

			// A block can carry several keys and the server reports each one, so
			// the output names them rather than claiming a single success.
			ids := make([]string, 0, len(keys))
			for _, key := range keys {
				if key.Id != nil {
					ids = append(ids, *key.Id)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "GPG key %s added successfully\n", style.Secondary.Render(strings.Join(ids, ", ")))
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
			// This used to be a bare fmt.Scanln with no guard at all: it read
			// the process's real stdin rather than the command's, so it hung on
			// a CI runner and silently cancelled under an agent. ADR-073 routes
			// every confirmation through one place that knows whether anyone is
			// there to answer.
			request := prompt.RequestFor(cmd, isJSON())
			request.Yes = yesFlag
			request.Flag = "--yes"
			if err := prompt.ConfirmAction(request, "clear all GPG keys"); err != nil {
				return err
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
