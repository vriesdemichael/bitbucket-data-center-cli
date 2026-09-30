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
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
)

func newMCPCommand(deps Dependencies) *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server, and list the tools it offers",
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
	var highlightTemplates bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		Example: `  # Serve every tool over stdio, for an MCP client to launch
  bb ai mcp serve

  # Only the tools that read, confined to one repository
  bb ai mcp serve --read-only --repo PROJ/repo

  # Name the tools to expose, and record every call
  bb ai mcp serve --tools get_pull_request,list_pull_requests,get_pr_diff \
    --audit-file /var/log/bb/mcp-audit.jsonl`,
		// The literal matches cli.annotationNoAmbientRepoInference (this package
		// cannot import internal/cli); the test of that name pins them together.
		// Ambient inference fills --repo from the git remote of the working
		// directory, which is a convenience for commands that merely target a
		// repository. Here --repo is a confinement decision (ADR-062): it must
		// come from the operator's flags, not from where the server starts.
		Annotations: map[string]string{"bb/no-ambient-repo-inference": "true"},
		Long: `Start the bb MCP server on stdio, for an MCP client to run.

Configure the client to run bb ai mcp serve, with a token of the server's own in
the client's env block:

  "bb": {
    "command": "bb",
    "args": ["ai", "mcp", "serve"],
    "env": { "BITBUCKET_TOKEN": "${BB_MCP_TOKEN}" }
  }

Every MCP client has that block: Claude Code and Claude Desktop (.mcp.json,
claude_desktop_config.json), VS Code, Codex ([mcp_servers.bb.env] in
config.toml, or codex mcp add --env) and Antigravity (mcp_config.json). The
${VAR} form keeps the agent on a PAT of its own, which can be read-only while
yours can write, and writes neither into the file. No flag takes a token: a
flag's value is readable in the process list for as long as the server runs.

Tools that decide whether or when a pull request merges ask the person first,
as an MCP elicitation in the client: merging, enabling or disabling
auto-merge, submitting a review, reporting a build status, creating a tag, and
changing a pull request's draft flag. A client that cannot ask gets error
-32021 for them, and nothing reaches Bitbucket. bb ai mcp tools lists which
tools ask.

--read-only exposes only the tools that read, for a client you do not trust
with those confirmations. --tools exposes only the tools you name, and
--exclude leaves tools out; neither brings back a tool that --read-only or a
scope withholds. refresh_view and suggest_form_values, which only the views of
show call, go with show.

--project or --repo confines the server to one project or repository, and a
call aimed elsewhere is refused. Build statuses, which hang off a commit rather
than a project, are withheld while a scope is set. The server is never scoped
by the directory it starts in. --host is required when more than one Bitbucket
instance is configured.

A view is drawn from show's result and has no network of its own: its buttons
call this server's tools through the client, with their confirmations, the
scope and the audit trail. Views highlight code, but of a Svelte, ERB, PHTML,
Go HTML or Jinja template only the markup, because a crafted file can keep the
lexers for the code in it busy for seconds. --highlight-templates highlights
that code too.

--audit-file records every tool call as JSON Lines, to a path or to stderr for
a log collector that reads the process streams. When a record cannot be
written the call is refused; --audit-failure=warn relaxes that. The trail
covers this server only: an agent that can run shell commands can run bb
directly. What holds either way is the token, so give this server a narrower
PAT than your own.`,
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

			// The server's requests say they are the server's, so an
			// administrator can tell an agent's traffic from a person's.
			network.SetSurface("mcp")
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

			serveReadOnly, err := readOnlyByPolicy(readOnly)
			if err != nil {
				return err
			}

			s := bbmcp.NewServer(bbmcp.ServerOptions{
				Name:               "bb",
				Version:            deps.Version(),
				Clients:            clients,
				Allow:              splitCSV(toolsFlag),
				Exclude:            splitCSV(excludeFlag),
				ReadOnly:           serveReadOnly,
				Scope:              scope,
				Audit:              audit,
				AuditFailure:       failureMode,
				HighlightTemplates: highlightTemplates,
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
	cmd.Flags().BoolVar(&highlightTemplates, "highlight-templates", false, "In views, highlight the code inside templates too, not only their markup")

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
		Example: `  # Every tool, with whether it changes anything and whether it asks first
  bb ai mcp tools

  # The tools a read-only server exposes
  bb ai mcp tools --read-only`,
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

// readOnlyByPolicy is whether the server runs read-only: when --read-only
// asks, or when the read_only policy lever says so, whatever the client's
// configuration passes (ADR-100).
func readOnlyByPolicy(flag bool) (bool, error) {
	restrictions, err := config.LoadRestrictions()
	if err != nil {
		return false, err
	}

	return flag || restrictions.ReadOnly, nil
}

// warnDeprecatedFlags prints the registered warning for each deprecated flag
// the invocation passed, once, on stderr so a --json document is unchanged
// (ADR-084). The warning comes from the registry, so it says what the reports
// listing outstanding deprecations say.
func warnDeprecatedFlags(cmd *cobra.Command, command string, flags ...string) {
	deprecation.WarnFlags(cmd.ErrOrStderr(), cmd.Flags().Changed, command, flags...)
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
