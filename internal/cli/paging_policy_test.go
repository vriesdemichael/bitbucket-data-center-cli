package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestOnlyPagingCommandsAdvertisePagingFlags is #476.
//
// Nine parent commands registered --limit and --all as persistent flags, so
// every descendant advertised them: 113 commands offered a maximum result count
// and ignored it, `repo delete --limit 5` among them. ADR-003 makes --help the
// contract an agent reads, and ADR-054 asks for input that does not apply to be
// refused rather than absorbed.
//
// The check is behavioural rather than structural: a command may advertise the
// flags only if its own source consumes them. That catches a persistent
// registration coming back, and also a leaf that registers them and then does
// not read them.
func TestOnlyPagingCommandsAdvertisePagingFlags(t *testing.T) {
	consuming := commandsConsumingPagingFlags(t)

	root := NewRootCommand()
	advertised := 0

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}

		// Flags(), not InheritedFlags(): a command advertises what its own
		// help prints, which is what a caller reads.
		if cmd.Flags().Lookup("limit") != nil || cmd.Flags().Lookup("all") != nil {
			advertised++

			leaf := cmd.Name()
			if !consuming[leaf] {
				t.Errorf("%s advertises --limit/--all but nothing in the command tree consumes them for %q. "+
					"Register them on the leaf that pages, not on a parent: every other child of that parent "+
					"then offers a maximum result count it ignores (#476).", cmd.CommandPath(), leaf)
			}
		}

		// The symptom a caller sees is the child's help, which prints inherited
		// flags too. Nothing registers these persistently any more, so nothing
		// should inherit them -- and if a persistent registration comes back,
		// this names the commands that started advertising a flag they ignore.
		if cmd.InheritedFlags().Lookup("limit") != nil || cmd.InheritedFlags().Lookup("all") != nil {
			t.Errorf("%s inherits --limit/--all from a parent. They belong on the leaf that pages: "+
				"a persistent registration hands them to every sibling, which is how 113 commands came "+
				"to advertise a maximum result count they ignore (#476).", cmd.CommandPath())
		}

		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	// Guards the walk: if the flags stopped being registered anywhere, every
	// assertion above would pass while --limit did nothing at all.
	if advertised == 0 {
		t.Fatal("no command advertises --limit; has paging.Options.Register stopped being called?")
	}
	t.Logf("%d commands advertise the paging flags", advertised)
}

// commandsConsumingPagingFlags collects the Use names of commands whose source
// reads a paging option.
//
// Source rather than runtime, because consuming the flags happens inside RunE
// and a test cannot run every command against a server. It reads each
// cobra.Command literal and asks whether its body mentions ServiceLimit,
// LimitReached or Truncate.
func commandsConsumingPagingFlags(t *testing.T) map[string]bool {
	t.Helper()

	consuming := map[string]bool{}
	fileSet := token.NewFileSet()
	parsed := 0

	err := filepath.WalkDir("cmd", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		parsed++

		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Command" {
				return true
			}

			var use string
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.Ident)
				if !ok || key.Name != "Use" {
					continue
				}
				if value, ok := pair.Value.(*ast.BasicLit); ok {
					use = strings.Fields(strings.Trim(value.Value, `"`))[0]
				}
			}
			if use == "" {
				return true
			}

			ast.Inspect(literal, func(inner ast.Node) bool {
				identifier, ok := inner.(*ast.Ident)
				if !ok {
					return true
				}
				switch identifier.Name {
				case "ServiceLimit", "LimitReached", "Truncate":
					consuming[use] = true
				}

				return true
			})

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walking cmd: %v", err)
	}
	if parsed == 0 {
		t.Fatal("no command source was parsed")
	}

	return consuming
}
