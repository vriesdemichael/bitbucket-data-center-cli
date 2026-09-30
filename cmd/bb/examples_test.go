package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// exampleFiles are the files the examples name, with something in each that
// its command can read.
var exampleFiles = map[string]string{
	"public-key.asc":      "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmDMEZexample\n-----END PGP PUBLIC KEY BLOCK-----\n",
	"deploy-key.pub":      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl deploy\n",
	".ssh/id_ed25519.pub": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl alice\n",
	"condition.json":      `{"requiredApprovals":1}`,
	"body.json":           `{"name":"feature/test","startPoint":"main"}`,
	"notes.md":            "First notes\n",
}

// examplesThatNeedACheckout are the examples that read the repository and the
// branch from the git checkout they are run in. The test runs in an empty
// directory, where each is refused for the lack of one, and an entry bb does
// not refuse there is an excuse for nothing.
var examplesThatNeedACheckout = map[string]bool{
	"bb pr create": true,
}

// TestEveryExampleIsAnInvocationBBAccepts runs each example a command's help
// carries, under --dry-run, and fails on one bb refuses as invalid.
//
// docs-lint already holds every example to the command tree: the command
// exists, its flags exist and parse, its arguments are the right number and
// its required flags are there. What it cannot see is what a command checks
// for itself once it runs -- a value outside the ones it accepts, two flags it
// will not take together, a flag that needs another. An example wrong in one
// of those ways parses, reads as correct, and fails for whoever copies it.
//
// The examples are written by hand, which is why they are run. They name
// things that do not exist, PROJ/repo and pull request 42, against a host the
// test process cannot reach, so none of them gets an answer from Bitbucket; a
// command stopped at the transport has accepted what it was given, and one
// stopped before it has not.
func TestEveryExampleIsAnInvocationBBAccepts(t *testing.T) {
	sealEnvironment(t)

	// A host and a credential, so a command gets as far as its own arguments:
	// with neither, every one stops at the missing host and proves nothing.
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_TOKEN", "not-a-token")

	home, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for name, content := range exampleFiles {
		path := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	ran := 0
	neededACheckout := map[string]bool{}
	for _, command := range commandsWithExamples(cli.NewRootCommand()) {
		path := command.CommandPath()
		invocations := invocationsOf(command.Example, home)
		if len(invocations) == 0 {
			t.Errorf("%s: its examples hold no bb invocation", path)
			continue
		}

		showsItself := false
		for _, invocation := range invocations {
			line := "bb " + strings.Join(invocation, " ")

			target, _, err := cli.NewRootCommand().Find(invocation)
			if err != nil {
				t.Errorf("%s: example %q names no command: %v", path, line, err)
				continue
			}
			if target.CommandPath() == path {
				showsItself = true
			}

			switch {
			case target.CommandPath() == "bb ai mcp serve":
				// Does not take --dry-run, and without it would serve until stopped.
				continue
			case target.CommandPath() == "bb browse" && !contains(invocation, "--no-browser"):
				// Runs as usual under --dry-run, which is to open a browser.
				continue
			case examplesThatNeedACheckout[line]:
				neededACheckout[line] = true
				if kind, output := dryRunKind(t, invocation); kind != "validation" {
					t.Errorf("%q is excused for needing a checkout, and bb accepts it without one:\n%s", line, output)
				}
				continue
			}

			t.Run(line, func(t *testing.T) {
				t.Parallel()

				kind, output := dryRunKind(t, invocation)
				if kind == "validation" {
					t.Errorf("bb refuses its own example as invalid:\n%s", output)
				}
			})
			ran++
		}

		if !showsItself {
			t.Errorf("%s: none of its examples runs %s", path, path)
		}
	}

	for line := range examplesThatNeedACheckout {
		if !neededACheckout[line] {
			t.Errorf("%q is excused for needing a checkout, and no command has that example", line)
		}
	}

	if ran < 250 {
		t.Fatalf("ran only %d examples; the walk has stopped reaching the tree", ran)
	}
}

// TestEveryCommandHasAnExample: the reference shows a command's examples, and
// a command without one leaves its reader to work the invocation out from the
// usage line.
func TestEveryCommandHasAnExample(t *testing.T) {
	t.Parallel()

	counted := 0

	var walk func(parent *cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			// Cobra's own commands carry the help Cobra wrote for them.
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			if child.Runnable() {
				counted++
				if strings.TrimSpace(child.Example) == "" {
					t.Errorf("%s has no example", child.CommandPath())
				}
			}
			walk(child)
		}
	}
	walk(cli.NewRootCommand())

	if counted < 200 {
		t.Fatalf("checked only %d commands; the walk has stopped reaching the tree", counted)
	}
}

// commandsWithExamples returns every command a person can run that declares
// examples.
func commandsWithExamples(root *cobra.Command) []*cobra.Command {
	var found []*cobra.Command
	var walk func(parent *cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			if child.Hidden {
				continue
			}
			if strings.TrimSpace(child.Example) != "" {
				found = append(found, child)
			}
			walk(child)
		}
	}
	walk(root)

	return found
}

// dryRunKind runs one invocation under --dry-run --json and returns the kind of
// failure it ended in, or "" when it ended in none, with what it printed.
func dryRunKind(t *testing.T, invocation []string) (string, string) {
	t.Helper()

	args := append(append([]string{}, invocation...), "--dry-run", "--json", "--no-input")
	if position := indexOf(args, "--"); position >= 0 {
		// Everything after -- is for git, so bb's own flags go before it.
		args = append(append(append([]string{}, invocation[:position]...), "--dry-run", "--json", "--no-input"), invocation[position:]...)
	}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := cli.NewRootCommand()
	root.SetArgs(args)
	root.SetErr(stderr)
	// What an example pipes in: a secret, a file's content, git's request.
	root.SetIn(strings.NewReader("example\n"))
	executeRootCommand(root, args, stdout, stderr)

	var document struct {
		Error   *struct{ Kind string } `json:"error"`
		Preview *struct {
			Error *struct{ Kind string } `json:"error"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		// A command that writes no document of its own -- bb ai skill show
		// prints the skill -- has nothing to refuse the example with here.
		return "", stdout.String() + stderr.String()
	}

	switch {
	case document.Error != nil:
		return document.Error.Kind, stdout.String()
	case document.Preview != nil && document.Preview.Error != nil:
		return document.Preview.Error.Kind, stdout.String()
	default:
		return "", stdout.String()
	}
}

// invocationsOf returns the arguments of every bb command in a block of
// examples, read the way a shell would: a comment is dropped, a line ending in
// a backslash continues on the next, a pipeline or a chain is split into its
// commands, a redirection is left out, and quotes group words. A leading ~/ is
// the home directory.
func invocationsOf(examples string, home string) [][]string {
	var invocations [][]string

	joined := strings.ReplaceAll(examples, "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		for _, command := range shellCommands(line) {
			if len(command) == 0 || command[0] != "bb" {
				continue
			}
			arguments := command[1:]
			for index, argument := range arguments {
				if rest, found := strings.CutPrefix(argument, "~/"); found {
					arguments[index] = filepath.Join(home, rest)
				}
			}
			invocations = append(invocations, arguments)
		}
	}

	return invocations
}

// shellCommands splits one line into its commands and each command into its
// words.
func shellCommands(line string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	quote := rune(0)
	inWord := false
	redirected := false

	endWord := func() {
		if inWord && !redirected {
			words = append(words, word.String())
		}
		if inWord {
			redirected = false
		}
		word.Reset()
		inWord = false
	}
	endCommand := func() {
		endWord()
		commands = append(commands, words)
		words = nil
		redirected = false
	}

	runes := []rune(line)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			} else {
				word.WriteRune(character)
			}
		case character == '\'' || character == '"':
			quote = character
			inWord = true
		case character == ' ' || character == '\t':
			endWord()
		case character == '|' || character == ';':
			endCommand()
		case character == '&' && index+1 < len(runes) && runes[index+1] == '&':
			endCommand()
			index++
		case character == '>' || character == '<':
			// The word that follows names a file, not an argument.
			endWord()
			redirected = true
		default:
			word.WriteRune(character)
			inWord = true
		}
	}
	endCommand()

	return commands
}

func contains(words []string, word string) bool {
	return indexOf(words, word) >= 0
}

func indexOf(words []string, word string) int {
	for index, each := range words {
		if each == word {
			return index
		}
	}

	return -1
}

func TestShellCommandsReadsALineTheWayAShellDoes(t *testing.T) {
	t.Parallel()

	cases := map[string][][]string{
		`bb pr get 42 --repo PROJ/repo`:                                {{"bb", "pr", "get", "42", "--repo", "PROJ/repo"}},
		`bb project create PROJ --name "Payments platform"`:            {{"bb", "project", "create", "PROJ", "--name", "Payments platform"}},
		`printf '%s' "$TOKEN" | bb auth login https://h --token-stdin`: {{"printf", "%s", "$TOKEN"}, {"bb", "auth", "login", "https://h", "--token-stdin"}},
		`bb pr checks 42 && bb pr merge 42`:                            {{"bb", "pr", "checks", "42"}, {"bb", "pr", "merge", "42"}},
		`bb repo cat a.txt > a.txt`:                                    {{"bb", "repo", "cat", "a.txt"}},
		`bb repo edit a.md --content - --message "Add" < notes.md`:     {{"bb", "repo", "edit", "a.md", "--content", "-", "--message", "Add"}},
		`bb build required create --body '{"a":["b"]}'`:                {{"bb", "build", "required", "create", "--body", `{"a":["b"]}`}},
	}
	for line, want := range cases {
		got := shellCommands(line)
		if len(got) != len(want) {
			t.Errorf("shellCommands(%q) = %q, want %q", line, got, want)
			continue
		}
		for index := range want {
			if strings.Join(got[index], "\x00") != strings.Join(want[index], "\x00") {
				t.Errorf("shellCommands(%q) = %q, want %q", line, got, want)
			}
		}
	}
}
