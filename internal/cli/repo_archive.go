package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newRepoArchiveCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var format string
	var output string
	var at string
	var prefix string
	var path string

	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Download repository archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			if format == "" {
				format = "zip"
			}

			var pathParam *string
			if path != "" {
				pathParam = &path
			}
			var atParam *string
			if at != "" {
				atParam = &at
			}
			var prefixParam *string
			if prefix != "" {
				prefixParam = &prefix
			}
			var formatParam *string
			if format != "" {
				formatParam = &format
			}

			params := &openapigenerated.GetArchiveParams{
				Path:   pathParam,
				At:     atParam,
				Prefix: prefixParam,
				Format: formatParam,
			}

			resp, err := client.GetArchive(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, params)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				bodyBytes, _ := io.ReadAll(resp.Body)
				return openapi.MapStatusError(resp.StatusCode, bodyBytes)
			}

			var writer io.Writer
			var targetMsg string

			if output == "-" {
				writer = cmd.OutOrStdout()
				targetMsg = "stdout"
			} else {
				filename := output
				if filename == "" {
					filename = fmt.Sprintf("%s.%s", repoRef.Slug, format)
				}

				file, err := os.Create(filename)
				if err != nil {
					return err
				}
				defer file.Close()
				writer = file
				absPath, _ := filepath.Abs(filename)
				targetMsg = absPath
			}

			_, err = io.Copy(writer, resp.Body)
			if err != nil {
				return err
			}

			if output != "-" {
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]string{
						"status": "success",
						"file":   targetMsg,
					})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Successfully downloaded repository archive to %s\n", targetMsg)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&format, "format", "zip", "The format to stream the archive in: zip, tar, tar.gz, tgz")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output filename (use '-' for stdout, defaults to <repo-slug>.<format>)")
	cmd.Flags().StringVar(&at, "at", "", "The commit to stream an archive of")
	cmd.Flags().StringVar(&prefix, "prefix", "", "A prefix to apply to all entries in the streamed archive")
	cmd.Flags().StringVar(&path, "path", "", "Paths to include in the streamed archive")

	return cmd
}
