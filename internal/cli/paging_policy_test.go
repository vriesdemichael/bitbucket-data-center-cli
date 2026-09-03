package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
// contract an agent reads.
//
// Two assertions, and the pairing matters. The first ties each registration to
// the body of the very command it registers on, so a command cannot advertise
// the flags without reading them. The second says no command may inherit them,
// which is the symptom a caller sees, since help prints inherited flags.
func TestOnlyPagingCommandsAdvertisePagingFlags(t *testing.T) {
	t.Run("a registration sits on a command that reads the flags", func(t *testing.T) {
		registrations := 0

		forEachPagingRegistration(t, func(file string, line int, command string, body string) {
			registrations++

			if !mentionsPagingHelper(body) {
				t.Errorf("%s:%d registers --limit/--all on %s, whose body never reads them. "+
					"Register them on the command that pages: a command advertising a maximum "+
					"result count it ignores is the defect #476 closed.", file, line, command)
			}
		})

		// Guards the walk. A parse that found nothing would pass every
		// assertion above while proving nothing.
		if registrations == 0 {
			t.Fatal("no paging registrations were found; has paging.Options.Register been renamed?")
		}
		t.Logf("%d paging registrations checked", registrations)
	})

	t.Run("every paging option that is read is also registered", func(t *testing.T) {
		// The other direction. A paging.Options that is read but never
		// registered is silently the zero value, so --limit does not exist on
		// that command and its default is used whatever the caller asks --
		// which is what moving nine registrations could easily have caused.
		for _, unregistered := range pagingOptionsReadButNotRegistered(t) {
			t.Errorf("%s reads %s but never registers it, so the command has no --limit "+
				"and silently uses the default.", unregistered.file, unregistered.name)
		}
	})

	t.Run("no command inherits the flags from a parent", func(t *testing.T) {
		root := NewRootCommand()
		advertised := 0

		var walk func(*cobra.Command)
		walk = func(cmd *cobra.Command) {
			if cmd.Name() == "help" || cmd.Name() == "completion" {
				return
			}

			if cmd.Flags().Lookup("limit") != nil || cmd.Flags().Lookup("all") != nil {
				advertised++
			}

			// Persistent registration is what handed the flags to 113 commands.
			// Nothing registers them that way now, so nothing should inherit
			// them -- and if one comes back, this names every command that
			// started advertising a flag it ignores.
			if cmd.InheritedFlags().Lookup("limit") != nil || cmd.InheritedFlags().Lookup("all") != nil {
				t.Errorf("%s inherits --limit/--all from a parent. They belong on the leaf that "+
					"pages: a persistent registration hands them to every sibling (#476).", cmd.CommandPath())
			}

			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(root)

		if advertised == 0 {
			t.Fatal("no command advertises --limit; has paging.Options.Register stopped being called?")
		}
		t.Logf("%d commands advertise the paging flags", advertised)
	})
}

// forEachPagingRegistration finds every `<options>.Register(<command>, n)` call
// under internal/cli/cmd and hands back the source of the command literal it
// registers on.
//
// Resolving the command variable is the point. An earlier version of this test
// asked "does any command with this name read the flags", and 36 commands are
// named `list` -- so one command's paging made every namesake pass. Giving
// `bb auth status` the flags did not fail it, because `pr status` pages.
func forEachPagingRegistration(t *testing.T, visit func(file string, line int, command string, body string)) {
	t.Helper()

	fileSet := token.NewFileSet()

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

		// Every `name := &cobra.Command{...}` in this file, by variable name.
		literals := map[string]*ast.CompositeLit{}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			name, ok := assign.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			unary, ok := assign.Rhs[0].(*ast.UnaryExpr)
			if !ok {
				return true
			}
			literal, ok := unary.X.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if selector, ok := literal.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "Command" {
				literals[name.Name] = literal
			}

			return true
		})

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Register" {
				return true
			}
			// paging registrations take (command, defaultLimit); enumflag's
			// Register takes a FlagSet and more arguments.
			if len(call.Args) != 2 {
				return true
			}
			target, ok := call.Args[0].(*ast.Ident)
			if !ok {
				return true
			}
			literal, ok := literals[target.Name]
			if !ok {
				return true
			}

			var body strings.Builder
			ast.Inspect(literal, func(inner ast.Node) bool {
				if identifier, ok := inner.(*ast.Ident); ok {
					body.WriteString(identifier.Name)
					body.WriteString(" ")
				}

				return true
			})

			visit(path, fileSet.Position(call.Pos()).Line, target.Name, body.String())

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walking cmd: %v", err)
	}
}

func mentionsPagingHelper(body string) bool {
	for _, helper := range []string{"ServiceLimit", "LimitReached", "Truncate"} {
		if strings.Contains(body, helper) {
			return true
		}
	}

	return false
}

type unregisteredPagingOption struct {
	file string
	name string
}

// pagingOptionsReadButNotRegistered finds paging.Options variables whose value
// is consumed without ever being registered on a command.
//
// Such an option is the zero value: --limit does not exist on that command, and
// the service is handed the default whatever the caller asks for.
func pagingOptionsReadButNotRegistered(t *testing.T) []unregisteredPagingOption {
	t.Helper()

	var found []unregisteredPagingOption

	err := filepath.WalkDir("cmd", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := readSource(path)
		if err != nil {
			return err
		}

		for _, name := range pagingOptionNames(source) {
			read := strings.Contains(source, name+".ServiceLimit()") ||
				strings.Contains(source, "paging.LimitReached("+name) ||
				strings.Contains(source, "paging.Truncate("+name)
			registered := strings.Contains(source, name+".Register(")

			if read && !registered {
				found = append(found, unregisteredPagingOption{file: path, name: name})
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking cmd: %v", err)
	}

	return found
}

func pagingOptionNames(source string) []string {
	var names []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "var ") || !strings.HasSuffix(trimmed, " paging.Options") {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(trimmed, "var "), " paging.Options"))
	}

	return names
}

func readSource(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}
