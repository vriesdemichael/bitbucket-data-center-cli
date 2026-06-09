package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/sshkey"
)

func newSshKeyCommand(options *rootOptions) *cobra.Command {
	sshCmd := &cobra.Command{
		Use:   "ssh-key",
		Short: "Manage personal SSH keys",
	}

	var limit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List personal SSH keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			keys, err := svc.ListUserKeys(cmd.Context(), limit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), keys)
			}

			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No SSH keys found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-50s\n", "ID", "LABEL", "FINGERPRINT")
			for _, k := range keys {
				id := ""
				if k.Id != nil {
					id = fmt.Sprintf("%d", *k.Id)
				}
				label := ""
				if k.Label != nil {
					label = *k.Label
				}
				fingerprint := ""
				if k.Fingerprint != nil {
					fingerprint = *k.Fingerprint
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-50s\n", id, label, fingerprint)
			}
			return nil
		},
	}
	listCmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of SSH keys to list")
	sshCmd.AddCommand(listCmd)

	var labelFlag string
	addCmd := &cobra.Command{
		Use:   "add <key-file-or-text>",
		Short: "Add a personal SSH key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
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

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), key)
			}

			id := 0
			if key.Id != nil {
				id = int(*key.Id)
			}
			lbl := ""
			if key.Label != nil {
				lbl = *key.Label
			}
			fmt.Fprintf(cmd.OutOrStdout(), "SSH key %d (%s) added successfully\n", id, lbl)
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
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			if err := svc.RemoveUserKey(cmd.Context(), args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH key %s removed successfully\n", args[0])
			return nil
		},
	}
	sshCmd.AddCommand(removeCmd)

	return sshCmd
}

func newRepoSshKeyCommand(options *rootOptions) *cobra.Command {
	repoSshCmd := &cobra.Command{
		Use:   "ssh-key",
		Short: "Manage project or repository SSH access keys",
	}

	var projectFlag string
	var repoFlag string
	var limit int

	// Define flags on the group command so they apply to all subcommands
	repoSshCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "Project key for project-level SSH keys")
	repoSshCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "Repository reference (projectKey/repositorySlug) for repository-level SSH keys")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List project or repository SSH access keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			var keys []openapigenerated.RestSshAccessKey
			if isProj {
				keys, err = svc.ListProjectKeys(cmd.Context(), proj, limit)
			} else {
				keys, err = svc.ListRepoKeys(cmd.Context(), proj, repo, limit)
			}
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), keys)
			}

			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No SSH access keys found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-15s %-50s\n", "ID", "LABEL", "PERMISSION", "FINGERPRINT")
			for _, k := range keys {
				id := ""
				label := ""
				fingerprint := ""
				if k.Key != nil {
					if k.Key.Id != nil {
						id = fmt.Sprintf("%d", *k.Key.Id)
					}
					if k.Key.Label != nil {
						label = *k.Key.Label
					}
					if k.Key.Fingerprint != nil {
						fingerprint = *k.Key.Fingerprint
					}
				}
				permission := ""
				if k.Permission != nil {
					permission = string(*k.Permission)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-15s %-50s\n", id, label, permission, fingerprint)
			}
			return nil
		},
	}
	listCmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of SSH access keys to list")
	repoSshCmd.AddCommand(listCmd)

	var labelFlag string
	var permissionFlag string
	var readOnlyFlag bool
	var readWriteFlag bool

	addCmd := &cobra.Command{
		Use:   "add <key-file-or-text>",
		Short: "Add a project or repository SSH access key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			keyContent, err := readPublicKey(args[0])
			if err != nil {
				return err
			}

			// Determine permission string
			permission := ""
			if readWriteFlag {
				if isProj {
					permission = "PROJECT_WRITE"
				} else {
					permission = "REPO_WRITE"
				}
			} else if readOnlyFlag || strings.ToLower(permissionFlag) == "read-only" {
				if isProj {
					permission = "PROJECT_READ"
				} else {
					permission = "REPO_READ"
				}
			} else if strings.ToLower(permissionFlag) == "read-write" {
				if isProj {
					permission = "PROJECT_WRITE"
				} else {
					permission = "REPO_WRITE"
				}
			} else {
				// Default to read-only
				if isProj {
					permission = "PROJECT_READ"
				} else {
					permission = "REPO_READ"
				}
			}

			var added openapigenerated.RestSshAccessKey
			if isProj {
				added, err = svc.AddProjectKey(cmd.Context(), proj, labelFlag, keyContent, permission)
			} else {
				added, err = svc.AddRepoKey(cmd.Context(), proj, repo, labelFlag, keyContent, permission)
			}
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), added)
			}

			id := 0
			lbl := ""
			if added.Key != nil {
				if added.Key.Id != nil {
					id = int(*added.Key.Id)
				}
				if added.Key.Label != nil {
					lbl = *added.Key.Label
				}
			}
			perm := ""
			if added.Permission != nil {
				perm = string(*added.Permission)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH access key %d (%s) with permission %s added successfully\n", id, lbl, perm)
			return nil
		},
	}
	addCmd.Flags().StringVar(&labelFlag, "label", "", "Label/comment for the SSH key")
	addCmd.Flags().StringVar(&permissionFlag, "permission", "read-only", "Permission level (read-only or read-write)")
	addCmd.Flags().BoolVar(&readOnlyFlag, "read-only", false, "Add as read-only access key")
	addCmd.Flags().BoolVar(&readWriteFlag, "read-write", false, "Add as read-write access key")
	repoSshCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <key-id>",
		Short: "Remove a project or repository SSH access key by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			if isProj {
				err = svc.RemoveProjectKey(cmd.Context(), proj, args[0])
			} else {
				err = svc.RemoveRepoKey(cmd.Context(), proj, repo, args[0])
			}
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH access key %s removed successfully\n", args[0])
			return nil
		},
	}
	repoSshCmd.AddCommand(removeCmd)

	return repoSshCmd
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

func resolveRepoSshKeyScope(projectFlag, repoFlag string) (string, string, bool, error) {
	if projectFlag != "" && repoFlag != "" {
		return "", "", false, apperrors.New(apperrors.KindValidation, "only one of --project or --repo can be specified", nil)
	}
	if projectFlag != "" {
		return projectFlag, "", true, nil
	}
	if repoFlag != "" {
		parts := strings.Split(repoFlag, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return "", "", false, apperrors.New(apperrors.KindValidation, "--repo must be in projectKey/repositorySlug format", nil)
		}
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), false, nil
	}
	return "", "", false, apperrors.New(apperrors.KindValidation, "either --project or --repo is required", nil)
}
