package ai

import (
	"fmt"
	"io"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	bbmcp "github.com/vriesdemichael/bitbucket-server-cli/internal/mcp"
)

func newMCPCommand(deps Dependencies) *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands",
	}

	mcpCmd.AddCommand(newMCPServeCommand(deps))
	mcpCmd.AddCommand(newMCPToolsCommand(deps))

	return mcpCmd
}

func newMCPServeCommand(deps Dependencies) *cobra.Command {
	var host string
	var token string
	var toolsFlag string
	var excludeFlag string
	var yolo bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		Long: `Start the bb MCP server using stdio transport for IDE integration.

Configure your IDE's MCP client to run:

  bb ai mcp serve

VS Code (settings.json):
  "mcp": {
    "servers": {
      "bb": { "type": "stdio", "command": "bb", "args": ["ai", "mcp", "serve"] }
    }
  }

By default the server runs in safe mode: only tools whose side-effects are
low-blast-radius and easily reversed are exposed (e.g. create_pull_request,
add_pr_comment). Tools that perform irreversible operations such as
merge_pull_request are withheld unless --yolo is set.

Use --tools to expose a specific subset regardless of the safety classification.
Use --exclude to suppress individual tools in any mode.

When more than one Bitbucket instance is configured the --host flag is required.
Use --token to restrict all API calls to the rights of a specific PAT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Multi-instance host enforcement.
			if strings.TrimSpace(host) == "" {
				contexts, err := config.ListServerContexts()
				if err != nil {
					return apperrors.New(apperrors.KindInternal, "failed to list server contexts", err)
				}
				if len(contexts) > 1 {
					return apperrors.New(apperrors.KindValidation,
						"multiple Bitbucket instances configured — use --host to specify which one to target", nil)
				}
			}

			// Passed into the load rather than written to the environment. This
			// process then serves MCP for as long as the client keeps it alive,
			// so an exported BITBUCKET_TOKEN would sit in the environment of
			// every subprocess for the whole session.
			cfg, err := deps.LoadConfig(config.Overrides{
				Host:  strings.TrimSpace(host),
				Token: strings.TrimSpace(token),
			})
			if err != nil {
				return err
			}

			clients, err := bbmcp.ClientsFromConfig(cfg)
			if err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to create API clients", err)
			}

			allow := splitCSV(toolsFlag)
			exclude := splitCSV(excludeFlag)

			s := bbmcp.NewServer("bb", deps.Version(), clients, allow, exclude, yolo)

			// IOTransport over the command's own streams rather than
			// mcp.StdioTransport, which reads os.Stdin and writes os.Stdout
			// directly. Identical in production, where Cobra hands the command
			// the process streams, and it is what lets the live suite drive a
			// real server in-process rather than spawning a binary whose
			// execution no coverage profile can see.
			transport := &mcpsdk.IOTransport{
				Reader: io.NopCloser(cmd.InOrStdin()),
				Writer: nopWriteCloser{Writer: cmd.OutOrStdout()},
			}
			return s.Run(cmd.Context(), transport)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Target Bitbucket instance URL; required when multiple instances are configured")
	cmd.Flags().StringVar(&token, "token", "", "PAT to use; restricts all API calls to this token's rights")
	cmd.Flags().StringVar(&toolsFlag, "tools", "", "Comma-separated allowlist of tool names to expose (overrides safety filter)")
	cmd.Flags().StringVar(&excludeFlag, "exclude", "", "Comma-separated denylist of tool names to suppress")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Expose all tools including unsafe operations like merge_pull_request")
	cmd.Flags().BoolVar(&yolo, "allow-writes", false, "Alias for --yolo")

	return cmd
}

func newMCPToolsCommand(deps Dependencies) *cobra.Command {
	var safeOnly bool

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List available MCP tools with name, exposure and description",
		Long: `Print all MCP tools the serve command can expose.

Use this output to build --tools and --exclude allowlists/denylists.

The EXPOSURE column says when a tool is available:

  SAFE   exposed by default; side-effects are low-blast-radius and easily
         reversed, such as opening a pull request or adding a comment
  YOLO   withheld unless 'bb ai mcp serve --yolo' (or --allow-writes) is set,
         because the operation is irreversible

--tools takes precedence over the safety filter, so naming a YOLO tool in an
allowlist exposes it without --yolo. Pass --safe-only to list just the set the
server exposes by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			specs := bbmcp.AllSpecs()
			if safeOnly {
				filtered := make([]bbmcp.Spec, 0, len(specs))
				for _, spec := range specs {
					if spec.Safe {
						filtered = append(filtered, spec)
					}
				}
				specs = filtered
			}

			if isJSON, _ := cmd.Root().PersistentFlags().GetBool("json"); isJSON {
				type toolEntry struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					// Safe mirrors the server's classification; Exposure is the
					// same fact as a stable string, so a consumer can render it
					// without re-deriving the vocabulary.
					Safe     bool   `json:"safe"`
					Exposure string `json:"exposure"`
				}
				entries := make([]toolEntry, len(specs))
				for i, spec := range specs {
					entries[i] = toolEntry{
						Name:        spec.Tool.Name,
						Description: toolDescription(spec),
						Safe:        spec.Safe,
						Exposure:    toolExposure(spec),
					}
				}
				return deps.WriteJSON(cmd.OutOrStdout(), entries)
			}

			for _, spec := range specs {
				fmt.Fprintf(cmd.OutOrStdout(), "%-40s %-6s %s\n", spec.Tool.Name, toolExposure(spec), toolDescription(spec))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&safeOnly, "safe-only", false, "List only the tools the server exposes without --yolo")

	return cmd
}

// nopWriteCloser adapts the command's output stream to the io.WriteCloser the
// transport wants. Closing is a no-op deliberately: the stream belongs to the
// command, and in the live suite it is a pipe the test still needs to read.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// Exposure labels for the tool listing. These are part of the --json contract,
// so they are constants rather than inline literals.
const (
	exposureSafe = "SAFE"
	exposureYolo = "YOLO"
)

// toolExposure reports when a tool is available, rather than whether it is
// "safe" in the abstract — the question a reader building an allowlist has is
// which tools they get without --yolo.
func toolExposure(spec bbmcp.Spec) string {
	if spec.Safe {
		return exposureSafe
	}

	return exposureYolo
}

// toolDescription extracts the human-readable description from a tool spec.
func toolDescription(spec bbmcp.Spec) string {
	return spec.Tool.Description
}

// splitCSV splits a comma-separated string into a trimmed slice, ignoring empty parts.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
