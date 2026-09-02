package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

func TestZZListPaths(t *testing.T) {
	if os.Getenv("BB_LIST_PATHS") == "" {
		t.Skip("helper")
	}
	declared := map[string]bool{}
	for _, path := range result.DeclaredPaths() {
		declared[path] = true
	}
	root := NewRootCommand()
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Runnable() && command != root {
			path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), root.Name()))
			if !declared[path] {
				t.Log("TODO " + path)
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}
