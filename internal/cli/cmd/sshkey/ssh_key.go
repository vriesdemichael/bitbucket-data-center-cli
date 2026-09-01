package sshkeycmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/sshkey"
)

type Dependencies struct {
	JSONEnabled         func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
}

func (d Dependencies) withDefaults() Dependencies {
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

	sshCmd := &cobra.Command{
		Use:   "ssh-key",
		Short: "Manage personal SSH keys",
	}

	var listPaging paging.Options
	var start int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List personal SSH keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			keys, err := svc.ListUserKeys(cmd.Context(), listPaging.ServiceLimit(), start)
			if err != nil {
				return err
			}

			reported := keysFrom(keys)

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), reported, paging.LimitReached(listPaging, len(reported)))
			}

			if len(reported) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No SSH keys found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-50s\n", "ID", "LABEL", "FINGERPRINT")
			for _, key := range reported {
				fmt.Fprintf(cmd.OutOrStdout(), "%-8d %-30s %-50s\n", key.ID, key.Label, key.Fingerprint)
			}
			return nil
		},
	}
	listPaging.Register(listCmd, 25)
	listCmd.Flags().IntVar(&start, "start", 0, "Start index for SSH keys listing")
	sshCmd.AddCommand(listCmd)

	var labelFlag string
	addCmd := &cobra.Command{
		Use:   "add <key-file-or-text>",
		Short: "Add a personal SSH key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			keyContent, err := readPublicKey(args[0])
			if err != nil {
				return err
			}

			key, err := svc.AddUserKey(cmd.Context(), labelFlag, keyContent)
			if err != nil {
				return err
			}

			reported := keyFrom(key)

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH key %d (%s) added successfully\n", reported.ID, reported.Label)
			return nil
		},
	}
	addCmd.Flags().StringVar(&labelFlag, "label", "", "Label/comment for the SSH key")
	sshCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <key-id>",
		Short: "Remove a personal SSH key by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			if err := svc.RemoveUserKey(cmd.Context(), args[0]); err != nil {
				return err
			}

			reported := Removal{Status: result.OK(), Key: args[0]}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH key %s removed successfully\n", reported.Key)
			return nil
		},
	}
	sshCmd.AddCommand(removeCmd)

	return sshCmd
}

func readPublicKey(arg string) (string, error) {
	if _, err := os.Stat(arg); err == nil {
		content, err := os.ReadFile(arg)
		if err != nil {
			return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read key file %s", arg), err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return arg, nil
}
