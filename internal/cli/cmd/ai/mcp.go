package ai

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
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
	var toolsFlag string
	var excludeFlag string
	var readOnly bool
	var yolo bool
	var projectScope string
	var repoScope string
	var auditFile string
	var auditFailure string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		// The literal matches cli.annotationNoAmbientRepoInference (this package
		// cannot import internal/cli); the test of that name pins them together.
		// Ambient inference fills --repo from the git remote of the working
		// directory, which is a convenience for commands that merely target a
		// repository. Here --repo is a confinement decision (ADR-062): it must
		// come from the operator's flags, not from where the server starts.
		Annotations: map[string]string{"bb/no-ambient-repo-inference": "true"},
		Long: `Start the bb MCP server using stdio transport for IDE integration.

Configure your IDE's MCP client to run:

  bb ai mcp serve

VS Code (settings.json):
  "mcp": {
    "servers": {
      "bb": { "type": "stdio", "command": "bb", "args": ["ai", "mcp", "serve"] }
    }
  }

Give the server its own credential through the client's env block, which every
MCP client supports -- Claude Code and Claude Desktop (.mcp.json /
claude_desktop_config.json), Codex ([mcp_servers.bb.env] in config.toml, or
codex mcp add --env), and Antigravity (mcp_config.json):

  "bb": {
    "command": "bb",
    "args": ["ai", "mcp", "serve"],
    "env": { "BITBUCKET_TOKEN": "${BB_MCP_TOKEN}" }
  }

The ${VAR} form is worth using deliberately: it keeps the agent on a different
PAT from your own, so the token you use interactively can carry write rights
while the one the agent gets is read-only, and neither is written into the
config file. Scoping the server this way replaces the old --token flag, which
put the credential in the process argument list for as long as the server ran
-- world-readable on Linux, unlike the process environment.

Tools that change whether or when a pull request merges ask the person to
confirm each call in the MCP client before they run: merging, enabling or
disabling auto-merge, submitting a review, reporting a build status, creating a
tag, and changing a pull request's draft flag. The confirmation is an MCP
elicitation, and the tool acts only when it is accepted. A client that cannot
show one gets error -32021 for those tools, and nothing reaches Bitbucket.
bb ai mcp tools lists which tools ask.

Use --read-only to expose only the tools that read. It is for a client you do
not trust with the tool annotations and those confirmations: a client you cannot
trust with them should not make changes in Bitbucket, so make them yourself.

Use --tools to expose only the tools you name, and --exclude to suppress
individual tools. Neither exposes a tool that --read-only or a scope withholds.

When more than one Bitbucket instance is configured the --host flag is required.

Use --project or --repo to confine the server to one project or repository. Any
tool call aimed elsewhere is refused. Tools that address a resource Bitbucket
does not scope to a project — build statuses, which hang off a commit SHA — are
withheld entirely while a scope is set, because there is no argument to bound.
Confinement applies only when the flags are passed explicitly: the server is
never scoped by the repository of the directory it starts in.

Use --audit-file to record every tool call as JSON Lines for SIEM collection.
Pass a path, or 'stderr' for a containerised deployment whose log collector
reads the process streams. Auditing is off by default. When it is on and a
record cannot be written the call is refused; --audit-failure=warn relaxes that.

The audit trail covers this server only. An agent that can run shell commands
can invoke bb directly and bypass it, along with every other control here; the
control that survives that is the token itself, which binds at the Bitbucket
server -- give this server a narrower PAT than your own through env.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Accepted, and inert: nothing is withheld for them to expose.
			warnDeprecatedFlags(cmd, "bb ai mcp serve", "yolo", "allow-writes")

			// Multi-instance host enforcement.
			if strings.TrimSpace(host) == "" {
				contexts, err := config.ListServerContexts()
				if err != nil {
					// Already classified, and naming the file; wrapping it as internal
					// said bb was broken when a config file was (#567).
					return err
				}
				if len(contexts) > 1 {
					return apperrors.New(apperrors.KindValidation,
						"multiple Bitbucket instances configured — use --host to specify which one to target", nil)
				}
			}

			// The credential comes from the environment, which the MCP client
			// sets for this process alone through its env block. The flag that
			// used to carry it put the secret in the process argument list for
			// the whole life of a long-running server -- world-readable on
			// Linux, where /proc/<pid>/environ is not (#464). The earlier
			// reasoning here worried about an exported token reaching child
			// processes; this server starts none.
			cfg, err := deps.LoadConfig(config.Overrides{
				Host: strings.TrimSpace(host),
			})
			if err != nil {
				return err
			}

			clients, err := bbmcp.ClientsFromConfig(cfg)
			if err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to create API clients", err)
			}

			scope, err := bbmcp.ParseScope(projectScope, repoScope)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, err.Error(), nil)
			}

			// Administrative policy can mandate auditing, in which case an
			// operator may not omit it or redirect it. A compliance control the
			// person being audited can switch off is not a control (ADR-058).
			resolvedAuditFile, err := resolveAuditFile(auditFile)
			if err != nil {
				return err
			}

			failureMode, err := parseAuditFailure(auditFailure)
			if err != nil {
				return err
			}

			audit, err := bbmcp.NewAuditLogger(resolvedAuditFile)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, err.Error(), nil)
			}
			if audit != nil {
				defer func() { _ = audit.Close() }()
				audit.Identity = cfg.BitbucketUsername
				audit.Host = cfg.BitbucketURL
				audit.Scope = scope.String()
			}

			s := bbmcp.NewServer(bbmcp.ServerOptions{
				Name:         "bb",
				Version:      deps.Version(),
				Clients:      clients,
				Allow:        splitCSV(toolsFlag),
				Exclude:      splitCSV(excludeFlag),
				ReadOnly:     readOnly,
				Scope:        scope,
				Audit:        audit,
				AuditFailure: failureMode,
				// stdout is the protocol channel, so operational messages go to
				// stderr like every other diagnostic (ADR-046).
				Warn: func(message string) { fmt.Fprintln(cmd.ErrOrStderr(), message) },
			})

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
	cmd.Flags().StringVar(&toolsFlag, "tools", "", "Comma-separated allowlist of tool names to expose")
	cmd.Flags().StringVar(&excludeFlag, "exclude", "", "Comma-separated denylist of tool names to suppress")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Expose only the tools that read, for a client you do not trust to make changes")
	// Deprecated (ADR-084): still accepted, so an existing client configuration
	// keeps starting, and hidden, since there is nothing left for them to do.
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Deprecated: has no effect")
	cmd.Flags().BoolVar(&yolo, "allow-writes", false, "Deprecated: has no effect")
	_ = cmd.Flags().MarkHidden("yolo")
	_ = cmd.Flags().MarkHidden("allow-writes")
	cmd.Flags().StringVar(&projectScope, "project", "", "Confine the server to this project key; calls aimed elsewhere are refused")
	cmd.Flags().StringVar(&repoScope, "repo", "", "Confine the server to one repository, as PROJECT/slug (or a slug alongside --project)")
	cmd.Flags().StringVar(&auditFile, "audit-file", "", "Append a JSON Lines audit record per tool call to this path, or to 'stderr'")
	enumflag.Register(cmd.Flags(), &auditFailure, "audit-failure", string(bbmcp.AuditFailureDeny), auditFailureModes, "What to do when an audit record cannot be written")

	return cmd
}

// resolveAuditFile applies administrative policy to the --audit-file flag.
//
// Policy wins, in both directions: it supplies the path when the flag is
// omitted, and it rejects a flag that points somewhere else. Both matter for
// the same reason — the person whose agent is being audited is the person
// running this command, so a policy they can override by editing an IDE config
// is documentation rather than enforcement.
func resolveAuditFile(flagValue string) (string, error) {
	policy, err := config.LoadPolicy()
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to load administrative policy", err)
	}

	mandated := strings.TrimSpace(policy.MCPAuditFile)
	requested := strings.TrimSpace(flagValue)

	if mandated == "" {
		return requested, nil
	}
	if requested == "" {
		return mandated, nil
	}
	if !strings.EqualFold(filepath.Clean(requested), filepath.Clean(mandated)) {
		return "", apperrors.New(apperrors.KindAuthorization,
			fmt.Sprintf("the MCP audit log destination is governed by administrative policy; mandated path: %s", mandated), nil)
	}
	return mandated, nil
}

// parseAuditFailure validates the --audit-failure flag.
func parseAuditFailure(value string) (bbmcp.AuditFailureMode, error) {
	switch bbmcp.AuditFailureMode(strings.ToLower(strings.TrimSpace(value))) {
	case "", bbmcp.AuditFailureDeny:
		return bbmcp.AuditFailureDeny, nil
	case bbmcp.AuditFailureWarn:
		return bbmcp.AuditFailureWarn, nil
	default:
		return "", apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("--audit-failure must be %q or %q, got %q", bbmcp.AuditFailureDeny, bbmcp.AuditFailureWarn, value), nil)
	}
}

func newMCPToolsCommand(deps Dependencies) *cobra.Command {
	var readOnly bool
	var safeOnly bool

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List MCP tools with what they change and whether they ask",
		Long: `Print every MCP tool the serve command exposes.

Use this output to build --tools and --exclude allowlists and denylists.

ACCESS says whether a tool changes anything in Bitbucket. ASKS says whether a
call asks the person to confirm it in the MCP client before it runs:

  always               every call asks
  when-setting-draft   a call that sets the pull request's draft flag asks
  never                the tool runs when called

A client that cannot show a confirmation gets error -32021 for a call that asks.
Pass --read-only to list just the tools 'bb ai mcp serve --read-only' exposes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Accepted, and inert: every tool is exposed without --yolo, which
			// is the set it used to narrow the listing to.
			warnDeprecatedFlags(cmd, "bb ai mcp tools", "safe-only")

			specs := bbmcp.AllSpecs()
			if readOnly {
				filtered := make([]bbmcp.Spec, 0, len(specs))
				for _, spec := range specs {
					if spec.ReadOnly() {
						filtered = append(filtered, spec)
					}
				}
				specs = filtered
			}

			if deps.jsonEnabled() {
				entries := make([]Tool, len(specs))
				for i, spec := range specs {
					entries[i] = Tool{
						Name:        spec.Tool.Name,
						Description: toolDescription(spec),
						Writes:      toolWrites(spec),
						Asks:        string(spec.Asks),
						// Deprecated: both describe exposure without --yolo,
						// and every tool has it now.
						Safe:     true,
						Exposure: exposureSafe,
					}
				}
				return deps.WriteJSON(cmd.OutOrStdout(), entries)
			}

			const row = "%-40s %-9s %-18s %s\n"
			fmt.Fprintf(cmd.OutOrStdout(), row, "NAME", "ACCESS", "ASKS", "DESCRIPTION")
			for _, spec := range specs {
				fmt.Fprintf(cmd.OutOrStdout(), row, spec.Tool.Name, toolAccess(spec), spec.Asks, toolDescription(spec))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&readOnly, "read-only", false, "List only the tools 'bb ai mcp serve --read-only' exposes")
	// Deprecated (ADR-084): still accepted, and hidden.
	cmd.Flags().BoolVar(&safeOnly, "safe-only", false, "Deprecated: lists every tool")
	_ = cmd.Flags().MarkHidden("safe-only")

	return cmd
}

// warnDeprecatedFlags prints the registered warning for each deprecated flag
// the invocation passed, once, on stderr so a --json document is unchanged
// (ADR-084). The warning comes from the registry, so it says what the reports
// listing outstanding deprecations say.
func warnDeprecatedFlags(cmd *cobra.Command, command string, flags ...string) {
	for _, flag := range flags {
		if !cmd.Flags().Changed(flag) {
			continue
		}
		if entry, ok := deprecation.Named(command + " --" + flag); ok {
			fmt.Fprintln(cmd.ErrOrStderr(), entry.Warning())
		}
	}
}

// nopWriteCloser adapts the command's output stream to the io.WriteCloser the
// transport wants. Closing is a no-op deliberately: the stream belongs to the
// command, and in the live suite it is a pipe the test still needs to read.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// Exposure labels, part of the --json contract, deprecated with the field that
// carries them. Every tool reports SAFE now; YOLO stays in the published
// vocabulary until the field is removed.
const (
	exposureSafe = "SAFE"
	exposureYolo = "YOLO"
)

// askingValues are the values of the asks field, taken from the server's own
// vocabulary so the listing cannot publish one the server does not use.
var askingValues = []string{
	string(bbmcp.AsksAlways),
	string(bbmcp.AsksWhenSettingDraft),
	string(bbmcp.AsksNever),
}

// toolWrites reports whether a tool changes anything in Bitbucket, read from
// the annotation the server publishes to clients. A tool that does not say it
// is read-only is taken to write.
func toolWrites(spec bbmcp.Spec) bool {
	return spec.Tool.Annotations == nil || !spec.Tool.Annotations.ReadOnlyHint
}

// toolAccess is toolWrites as the text listing shows it.
func toolAccess(spec bbmcp.Spec) string {
	if toolWrites(spec) {
		return "writes"
	}

	return "read-only"
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

// auditFailureModes come from the modes themselves, so the flag cannot offer
// one the middleware does not implement.
var auditFailureModes = []string{
	string(bbmcp.AuditFailureDeny),
	string(bbmcp.AuditFailureWarn),
}
