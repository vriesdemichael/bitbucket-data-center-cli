package cli

import (
	"github.com/spf13/cobra"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// refuseByPolicy applies the administrator's levers to the command about to
// run (ADR-100): disable_bb refuses it, disable_mcp_server refuses the MCP
// server, and read_only refuses what changes Bitbucket.
//
// It is the one place every command passes, so a command added later is
// covered by its dry-run classification rather than by remembering. Cobra
// answers --help and --version before this runs, and the root answers
// --describe before calling it, so those stay available; so does what
// unrefusable names. A completion refuses on its own, silently, through its
// Refusal dependency. --dry-run changes nothing, so it runs under read_only.
// bb api decides by its method, which it resolves itself, and bb ai mcp serve
// under read_only runs read-only rather than being refused.
func refuseByPolicy(cmd *cobra.Command, dryRun bool) error {
	switch cmd.Name() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return nil
	}

	path := dryRunCommandPath(cmd)
	if unrefusable[path] {
		return nil
	}

	restrictions, err := config.LoadRestrictions()
	if err != nil {
		return err
	}

	switch {
	case restrictions.DisableBB:
		return config.DisabledError()
	case restrictions.DisableMCPServer && path == "ai mcp serve":
		return config.MCPServerDisabledError()
	case restrictions.ReadOnly && !dryRun && path != "api" && classifyCommand(path) == classificationMutating:
		return config.ReadOnlyError("bb " + path)
	}

	return nil
}

// unrefusable are the commands no lever refuses, even when the policy itself
// cannot be read. Each contacts nothing and changes nothing. bb doctor and
// help are how a person finds out why bb refuses, and which file is broken; a
// completion script is what a package manager prints while it installs bb.
var unrefusable = map[string]bool{
	"doctor":                true,
	"help":                  true,
	"completion bash":       true,
	"completion zsh":        true,
	"completion fish":       true,
	"completion powershell": true,
}

// completionRefusal is the refusal a completion answers with: nothing, and the
// reason as Active Help, under disable_bb.
func completionRefusal() error {
	restrictions, err := config.LoadRestrictions()
	if err != nil {
		return err
	}
	if restrictions.DisableBB {
		return config.DisabledError()
	}

	return nil
}
