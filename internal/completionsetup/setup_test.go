package completionsetup

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// fakeSystem is a machine described rather than probed: which programs are
// installed, what each prints when asked, and the environment.
type fakeSystem struct {
	goos      string
	home      string
	env       map[string]string
	installed map[string]string
	answers   map[string]string
	failures  map[string]error

	// The PowerShells are asked at the same time.
	mutex sync.Mutex
	asked []string
}

func (fake *fakeSystem) system() System {
	return System{
		GOOS:    fake.goos,
		Getenv:  func(name string) string { return fake.env[name] },
		HomeDir: func() (string, error) { return fake.home, nil },
		LookPath: func(name string) (string, error) {
			if path, ok := fake.installed[name]; ok {
				return path, nil
			}

			return "", exec.ErrNotFound
		},
		Run: func(_ context.Context, executable string, args ...string) (string, error) {
			fake.mutex.Lock()
			fake.asked = append(fake.asked, executable+" "+strings.Join(args, " "))
			fake.mutex.Unlock()

			if err, ok := fake.failures[executable]; ok {
				return "", err
			}

			return fake.answers[executable], nil
		},
	}
}

func TestTheUserTargetsFollowWhereEachShellLooks(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	elsewhere := t.TempDir()

	for _, testCase := range []struct {
		name  string
		shell Shell
		env   map[string]string
		want  string
	}{
		{name: "bash by default", shell: Bash, want: filepath.Join(home, ".local", "share", "bash-completion", "completions", "bb")},
		{name: "bash under XDG_DATA_HOME", shell: Bash, env: map[string]string{"XDG_DATA_HOME": elsewhere},
			want: filepath.Join(elsewhere, "bash-completion", "completions", "bb")},
		{name: "bash-completion's own variable wins", shell: Bash, env: map[string]string{"XDG_DATA_HOME": home, "BASH_COMPLETION_USER_DIR": elsewhere},
			want: filepath.Join(elsewhere, "completions", "bb")},
		// The XDG specification says a relative value is to be ignored.
		{name: "a relative XDG_DATA_HOME is ignored", shell: Bash, env: map[string]string{"XDG_DATA_HOME": "relative"},
			want: filepath.Join(home, ".local", "share", "bash-completion", "completions", "bb")},
		{name: "zsh by default", shell: Zsh, want: filepath.Join(home, ".zshrc")},
		{name: "zsh under ZDOTDIR", shell: Zsh, env: map[string]string{"ZDOTDIR": elsewhere}, want: filepath.Join(elsewhere, ".zshrc")},
		{name: "fish by default", shell: Fish, want: filepath.Join(home, ".config", "fish", "completions", "bb.fish")},
		{name: "fish under XDG_CONFIG_HOME", shell: Fish, env: map[string]string{"XDG_CONFIG_HOME": elsewhere},
			want: filepath.Join(elsewhere, "fish", "completions", "bb.fish")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeSystem{goos: "linux", home: home, env: testCase.env}
			targets, err := Targets(context.Background(), fake.system(), testCase.shell, CurrentUser)
			if err != nil {
				t.Fatalf("Targets failed: %v", err)
			}
			if len(targets) != 1 || targets[0].Path != testCase.want {
				t.Fatalf("targets = %+v, want one at %s", targets, testCase.want)
			}
			if targets[0].Shared != (testCase.shell == Zsh) {
				t.Errorf("a %s setup was shared=%v", testCase.shell, targets[0].Shared)
			}
		})
	}
}

func TestEveryPowerShellInstalledIsAskedForItsOwnProfile(t *testing.T) {
	t.Parallel()

	fake := &fakeSystem{
		goos:      "windows",
		installed: map[string]string{"pwsh": "pwsh.exe", "powershell": "powershell.exe"},
		answers: map[string]string{
			// Where OneDrive has moved Documents, and in Dutch: nothing to
			// work out, only something to ask.
			"pwsh.exe": "C:\\Users\\me\\OneDrive\\Documenten\\PowerShell\\profile.ps1\r\nRemoteSigned\r\n",
			// Windows PowerShell can start its output with a byte order mark.
			"powershell.exe": string(rune(0xFEFF)) + "C:\\Users\\me\\OneDrive\\Documenten\\WindowsPowerShell\\profile.ps1\r\nRestricted\r\n",
		},
	}

	targets, err := Targets(context.Background(), fake.system(), PowerShell, CurrentUser)
	if err != nil {
		t.Fatalf("Targets failed: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected one target per edition, got %+v", targets)
	}

	byEdition := map[string]Target{}
	for _, target := range targets {
		byEdition[target.Edition] = target
		if !target.Shared || target.Shell != PowerShell {
			t.Errorf("a PowerShell target was not a shared block: %+v", target)
		}
	}

	seven := byEdition["PowerShell 7"]
	if seven.Path != "C:\\Users\\me\\OneDrive\\Documenten\\PowerShell\\profile.ps1" || seven.Blocked != "" {
		t.Errorf("PowerShell 7 = %+v", seven)
	}
	five := byEdition["Windows PowerShell 5.1"]
	if five.Path != "C:\\Users\\me\\OneDrive\\Documenten\\WindowsPowerShell\\profile.ps1" {
		t.Errorf("Windows PowerShell 5.1 = %+v", five)
	}
	if !strings.Contains(five.Blocked, "Restricted") {
		t.Errorf("a Restricted execution policy did not block the target: %q", five.Blocked)
	}

	for _, question := range fake.asked {
		if !strings.Contains(question, "$PROFILE.CurrentUserAllHosts") || !strings.Contains(question, "-NoProfile") {
			t.Errorf("asked %q", question)
		}
	}
}

func TestAllUsersAsksPowerShellForTheAllUsersProfile(t *testing.T) {
	t.Parallel()

	fake := &fakeSystem{
		goos:      "linux",
		installed: map[string]string{"pwsh": "/usr/bin/pwsh", "powershell": "/should/not/be/asked"},
		answers:   map[string]string{"/usr/bin/pwsh": "/opt/microsoft/powershell/7/profile.ps1\nUnrestricted\n"},
	}

	targets, err := Targets(context.Background(), fake.system(), PowerShell, AllUsers)
	if err != nil {
		t.Fatalf("Targets failed: %v", err)
	}
	// Windows PowerShell exists only on Windows, whatever PATH says.
	if len(targets) != 1 || targets[0].Path != "/opt/microsoft/powershell/7/profile.ps1" || targets[0].Scope != AllUsers {
		t.Fatalf("targets = %+v", targets)
	}
	if len(fake.asked) != 1 || !strings.Contains(fake.asked[0], "$PROFILE.AllUsersAllHosts") {
		t.Errorf("asked %v", fake.asked)
	}
}

func TestAPowerShellThatCannotAnswerBlocksOnlyItself(t *testing.T) {
	t.Parallel()

	fake := &fakeSystem{
		goos:      "windows",
		installed: map[string]string{"pwsh": "pwsh.exe", "powershell": "powershell.exe"},
		answers:   map[string]string{"pwsh.exe": "C:\\p\\profile.ps1\nAllSigned\n", "powershell.exe": "only one line\n"},
	}

	targets, err := Targets(context.Background(), fake.system(), PowerShell, CurrentUser)
	if err != nil {
		t.Fatalf("Targets failed: %v", err)
	}
	for _, target := range targets {
		if target.Blocked == "" {
			t.Errorf("%s was not blocked: %+v", target.Edition, target)
		}
	}

	fake.failures = map[string]error{"pwsh.exe": errors.New("exit status 1")}
	targets, _ = Targets(context.Background(), fake.system(), PowerShell, CurrentUser)
	if !strings.Contains(targets[0].Blocked, "could not ask it") {
		t.Errorf("a PowerShell that failed was %+v", targets[0])
	}

	none := &fakeSystem{goos: "linux"}
	if _, err := Targets(context.Background(), none.system(), PowerShell, CurrentUser); apperrors.KindOf(err) != apperrors.KindValidation {
		t.Errorf("no PowerShell at all gave %v", err)
	}
}

func TestAllUsersGoesWhereEachShellReadsForEveryone(t *testing.T) {
	t.Parallel()

	fake := &fakeSystem{
		goos:      "linux",
		installed: map[string]string{"zsh": "/usr/bin/zsh", "fish": "/usr/bin/fish"},
		answers: map[string]string{
			"/usr/bin/zsh":  "/usr/local/share/zsh/site-functions\n/usr/share/zsh/vendor-completions\n",
			"/usr/bin/fish": "/etc/fish\n",
		},
	}

	for shell, want := range map[Shell]string{
		Bash: "/usr/local/share/bash-completion/completions/bb",
		Zsh:  "/usr/local/share/zsh/site-functions/_bb",
		Fish: "/etc/fish/completions/bb.fish",
	} {
		targets, err := Targets(context.Background(), fake.system(), shell, AllUsers)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		if len(targets) != 1 || targets[0].Path != want || targets[0].Shared || targets[0].Scope != AllUsers {
			t.Errorf("%s: targets = %+v, want a file of bb's own at %s", shell, targets, want)
		}
	}
}

func TestAllUsersRefusesWhereNoShellReadsForEveryone(t *testing.T) {
	t.Parallel()

	windows := &fakeSystem{goos: "windows", installed: map[string]string{"zsh": "zsh.exe"}}
	for _, shell := range []Shell{Bash, Zsh, Fish} {
		if _, err := Targets(context.Background(), windows.system(), shell, AllUsers); apperrors.KindOf(err) != apperrors.KindValidation {
			t.Errorf("%s for all users on Windows gave %v", shell, err)
		}
	}

	// A zsh built to read nothing under /usr/local.
	elsewhere := &fakeSystem{
		goos:      "linux",
		installed: map[string]string{"zsh": "/usr/bin/zsh"},
		answers:   map[string]string{"/usr/bin/zsh": "/usr/share/zsh/site-functions\n"},
	}
	_, err := Targets(context.Background(), elsewhere.system(), Zsh, AllUsers)
	if apperrors.KindOf(err) != apperrors.KindValidation || !strings.Contains(err.Error(), "/usr/local/share/zsh/site-functions") {
		t.Errorf("a zsh that does not read /usr/local gave %v", err)
	}

	missing := &fakeSystem{goos: "linux"}
	if _, err := Targets(context.Background(), missing.system(), Fish, AllUsers); apperrors.KindOf(err) != apperrors.KindValidation {
		t.Errorf("fish for all users with no fish installed gave %v", err)
	}
}

func TestDetectShellReadsTheShellBbWasStartedFrom(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		goos  string
		shell string
		want  Shell
	}{
		{goos: "linux", shell: "/bin/bash", want: Bash},
		{goos: "darwin", shell: "/bin/zsh", want: Zsh},
		{goos: "linux", shell: "/usr/bin/fish", want: Fish},
		{goos: "windows", shell: "C:\\Program Files\\Git\\usr\\bin\\bash.exe", want: Bash},
		{goos: "windows", shell: "", want: PowerShell},
	} {
		fake := &fakeSystem{goos: testCase.goos, env: map[string]string{"SHELL": testCase.shell}}
		got, err := DetectShell(fake.system())
		if err != nil || got != testCase.want {
			t.Errorf("DetectShell(%s, %q) = %q, %v; want %q", testCase.goos, testCase.shell, got, err, testCase.want)
		}
	}

	unknown := &fakeSystem{goos: "linux", env: map[string]string{"SHELL": "/bin/tcsh"}}
	if _, err := DetectShell(unknown.system()); apperrors.KindOf(err) != apperrors.KindValidation {
		t.Errorf("an unknown shell gave %v", err)
	}
}

func TestParseShellKnowsTheFourShells(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"bash", "Zsh", " fish ", "POWERSHELL"} {
		if _, err := ParseShell(name); err != nil {
			t.Errorf("ParseShell(%q) = %v", name, err)
		}
	}
	if _, err := ParseShell("cmd"); apperrors.KindOf(err) != apperrors.KindValidation {
		t.Errorf("ParseShell(cmd) = %v", err)
	}
}

func TestInstallAndRemoveAFileOfBbsOwn(t *testing.T) {
	t.Parallel()

	for _, shell := range []Shell{Bash, Fish, Zsh} {
		t.Run(string(shell), func(t *testing.T) {
			t.Parallel()

			target := Target{Shell: shell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "nested", "bb")}

			outcome, err := Install(target)
			if err != nil || outcome.Status != Installed {
				t.Fatalf("first install = %+v, %v", outcome, err)
			}
			if written := read(t, target.Path); written != ownFile(shell) {
				t.Fatalf("wrote %q", written)
			}
			if state, _ := Inspect(target); !state.Present || !state.Current {
				t.Errorf("Inspect after install = %+v", state)
			}

			outcome, err = Install(target)
			if err != nil || outcome.Status != Unchanged {
				t.Fatalf("second install = %+v, %v", outcome, err)
			}

			// An older loader of bb's is brought up to date.
			write(t, target.Path, ownMarker+"\nan older loader\n")
			if state, _ := Inspect(target); !state.Present || state.Current {
				t.Errorf("Inspect of an older loader = %+v", state)
			}
			if outcome, err := Install(target); err != nil || outcome.Status != Updated {
				t.Fatalf("install over an older loader = %+v, %v", outcome, err)
			}

			outcome, err = Remove(target)
			if err != nil || outcome.Status != Removed {
				t.Fatalf("remove = %+v, %v", outcome, err)
			}
			if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the file is still there: %v", err)
			}

			if outcome, err := Remove(target); err != nil || outcome.Status != NotFound {
				t.Errorf("second remove = %+v, %v", outcome, err)
			}
		})
	}
}

func TestTheZshFileIsAFunctionZshCanAutoload(t *testing.T) {
	t.Parallel()

	// compinit reads the first line for the command a file completes, so
	// bb's marker has to come after it and still be found.
	content := ownFile(Zsh)
	if !strings.HasPrefix(content, "#compdef bb\n") {
		t.Errorf("the zsh file does not start with #compdef: %q", content)
	}
	if !isOwnFile([]byte(content)) {
		t.Error("bb's zsh file is not recognised as bb's")
	}
}

func TestAScriptSavedFromAnEarlierBbIsReplacedAndRemoved(t *testing.T) {
	t.Parallel()

	for shell, script := range map[Shell]string{
		Bash: "# bash completion V2 for bb                                  -*- shell-script -*-\n\n__bb_debug() {}\n",
		Fish: "# fish completion for bb                                   -*- shell-script -*-\n",
		Zsh:  "#compdef bb\ncompdef _bb bb\n\n# zsh completion for bb                                    -*- shell-script -*-\n",
	} {
		target := Target{Shell: shell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "bb")}
		write(t, target.Path, script)

		if state, _ := Inspect(target); !state.Script || state.Present {
			t.Errorf("%s: Inspect of a saved script = %+v", shell, state)
		}

		outcome, err := Install(target)
		if err != nil || outcome.Status != Updated || outcome.Note == "" {
			t.Fatalf("%s: install over a saved script = %+v, %v", shell, outcome, err)
		}

		write(t, target.Path, script)
		if outcome, err := Remove(target); err != nil || outcome.Status != Removed {
			t.Errorf("%s: remove of a saved script = %+v, %v", shell, outcome, err)
		}
	}
}

func TestAFileBbDidNotWriteIsLeftAlone(t *testing.T) {
	t.Parallel()

	target := Target{Shell: Fish, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "bb.fish")}
	const theirs = "complete -c bb -f -a 'something of my own'\n"
	write(t, target.Path, theirs)

	_, err := Install(target)
	if apperrors.KindOf(err) != apperrors.KindConflict || !strings.Contains(err.Error(), target.Path) {
		t.Errorf("install over somebody's file gave %v", err)
	}

	outcome, err := Remove(target)
	if err != nil || outcome.Status != NotFound || outcome.Note == "" {
		t.Errorf("remove of somebody's file = %+v, %v", outcome, err)
	}
	if read(t, target.Path) != theirs {
		t.Error("a file bb did not write was changed")
	}
}

func TestABlockGoesInAndComesOutLeavingTheFileAsItWas(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		shell    Shell
		original string
	}{
		{name: "a .zshrc", shell: Zsh, original: "autoload -Uz compinit && compinit\nexport EDITOR=vim\n"},
		{name: "a file with no final newline", shell: Zsh, original: "alias ll='ls -l'"},
		{name: "a file ending in a blank line", shell: Zsh, original: "setopt autocd\n\n"},
		{name: "an empty file", shell: Zsh, original: ""},
		{name: "a Windows profile", shell: PowerShell, original: "Set-PSReadLineOption -EditMode Emacs\r\nImport-Module posh-git\r\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			target := Target{Shell: testCase.shell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "profile"), Shared: true}
			write(t, target.Path, testCase.original)

			outcome, err := Install(target)
			if err != nil || outcome.Status != Installed {
				t.Fatalf("install = %+v, %v", outcome, err)
			}

			installed := read(t, target.Path)
			if !strings.HasPrefix(installed, testCase.original) {
				t.Errorf("what was in the file changed:\n%q", installed)
			}
			if strings.Contains(testCase.original, "\r\n") && strings.Count(installed, "\r\n") != strings.Count(installed, "\n") {
				t.Errorf("the block did not keep the file's line endings:\n%q", installed)
			}
			if state, _ := Inspect(target); !state.Present || !state.Current {
				t.Errorf("Inspect after install = %+v", state)
			}

			if outcome, err := Install(target); err != nil || outcome.Status != Unchanged {
				t.Errorf("second install = %+v, %v", outcome, err)
			}
			if read(t, target.Path) != installed {
				t.Error("a second install changed the file")
			}

			if outcome, err := Remove(target); err != nil || outcome.Status != Removed {
				t.Fatalf("remove = %+v, %v", outcome, err)
			}
			if got := read(t, target.Path); got != testCase.original && got != testCase.original+"\n" {
				t.Errorf("remove did not restore the file:\ngot  %q\nwant %q", got, testCase.original)
			}
		})
	}
}

func TestAProfileInstallCreatedGoesWithTheBlock(t *testing.T) {
	t.Parallel()

	profile := Target{Shell: PowerShell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "profile.ps1"), Shared: true}
	if outcome, err := Install(profile); err != nil || outcome.Status != Installed {
		t.Fatalf("install = %+v, %v", outcome, err)
	}
	if outcome, err := Remove(profile); err != nil || outcome.Status != Removed {
		t.Fatalf("remove = %+v, %v", outcome, err)
	}
	if _, err := os.Stat(profile.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a profile that held only bb's block is still there: %v", err)
	}

	// zsh starts its new-user wizard where there is no .zshrc, so an empty
	// one stays.
	zshrc := Target{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), ".zshrc"), Shared: true}
	if outcome, err := Install(zshrc); err != nil || outcome.Status != Installed {
		t.Fatalf("install = %+v, %v", outcome, err)
	}
	if outcome, err := Remove(zshrc); err != nil || outcome.Status != Removed {
		t.Fatalf("remove = %+v, %v", outcome, err)
	}
	if content, err := os.ReadFile(zshrc.Path); err != nil || strings.TrimSpace(string(content)) != "" {
		t.Errorf("the .zshrc was not left, empty: %q, %v", content, err)
	}
}

func TestAnOlderBlockIsReplacedWhereItStands(t *testing.T) {
	t.Parallel()

	target := Target{Shell: PowerShell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "profile.ps1"), Shared: true}
	write(t, target.Path, "before\n"+beginMarker+"\nan older block\n"+endMarker+"\nafter\n")

	outcome, err := Install(target)
	if err != nil || outcome.Status != Updated {
		t.Fatalf("install over an older block = %+v, %v", outcome, err)
	}

	got := read(t, target.Path)
	if !strings.HasPrefix(got, "before\n"+beginMarker) || !strings.HasSuffix(got, endMarker+"\nafter\n") || strings.Contains(got, "an older block") {
		t.Errorf("the block was not replaced in place:\n%s", got)
	}
}

func TestABlockSomebodyCutShortIsNotGuessedAt(t *testing.T) {
	t.Parallel()

	target := Target{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), ".zshrc"), Shared: true}
	damaged := "before\n" + beginMarker + "\nsource <(bb completion zsh)\nafter\n"
	write(t, target.Path, damaged)

	if _, err := Install(target); apperrors.KindOf(err) != apperrors.KindConflict {
		t.Errorf("install over a block with no end marker gave %v", err)
	}
	if _, err := Remove(target); apperrors.KindOf(err) != apperrors.KindConflict {
		t.Errorf("remove of a block with no end marker gave %v", err)
	}
	if read(t, target.Path) != damaged {
		t.Error("a damaged block was edited")
	}
}

func TestAMarkerThatIsNotALineOfItsOwnIsNotTheBlock(t *testing.T) {
	t.Parallel()

	target := Target{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), ".zshrc"), Shared: true}
	quoted := "echo '" + beginMarker + "'\n"
	write(t, target.Path, quoted)

	if outcome, err := Remove(target); err != nil || outcome.Status != NotFound {
		t.Errorf("a quoted marker was taken for the block: %+v, %v", outcome, err)
	}
	if outcome, err := Install(target); err != nil || outcome.Status != Installed {
		t.Errorf("install beside a quoted marker = %+v, %v", outcome, err)
	}
}

func TestAProfileKeepsItsEncoding(t *testing.T) {
	t.Parallel()

	// Windows PowerShell 5.1 writes UTF-16 when a profile is made with > or
	// Out-File. UTF-8 appended to that would ruin the whole file.
	for name, bom := range map[string][]byte{"UTF-16 LE": utf16LEBOM, "UTF-16 BE": utf16BEBOM, "UTF-8 with a BOM": utf8BOM} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			target := Target{Shell: PowerShell, Scope: CurrentUser, Path: filepath.Join(t.TempDir(), "profile.ps1"), Shared: true}
			original := textFile{bom: bom, text: "Write-Host 'héllo'\r\n", newline: "\r\n"}
			switch name {
			case "UTF-16 LE":
				original.order = utf16Order(true)
			case "UTF-16 BE":
				original.order = utf16Order(false)
			}
			if err := original.write(target.Path, CurrentUser); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(target.Path)

			if outcome, err := Install(target); err != nil || outcome.Status != Installed {
				t.Fatalf("install = %+v, %v", outcome, err)
			}

			file, err := readText(target.Path, "\n")
			if err != nil {
				t.Fatal(err)
			}
			if string(file.bom) != string(bom) || !strings.HasPrefix(file.text, "Write-Host 'héllo'\r\n") || !strings.Contains(file.text, endMarker+"\r\n") {
				t.Errorf("the profile did not keep its encoding: bom %x, text %q", file.bom, file.text)
			}

			if outcome, err := Remove(target); err != nil || outcome.Status != Removed {
				t.Fatalf("remove = %+v, %v", outcome, err)
			}
			if after, _ := os.ReadFile(target.Path); string(after) != string(before) {
				t.Errorf("remove did not restore the bytes:\n%x\n%x", after, before)
			}
		})
	}
}

func TestABlockedTargetIsNotWritten(t *testing.T) {
	t.Parallel()

	target := Target{
		Shell:   PowerShell,
		Path:    filepath.Join(t.TempDir(), "profile.ps1"),
		Shared:  true,
		Blocked: "its execution policy is Restricted, so it runs no profile script",
	}

	outcome, err := Install(target)
	if err != nil || outcome.Status != Blocked || outcome.Note != target.Blocked {
		t.Fatalf("install to a blocked target = %+v, %v", outcome, err)
	}
	if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("a blocked target was written")
	}
}

func TestTheLoadersCheckForBbBeforeRunningIt(t *testing.T) {
	t.Parallel()

	// Uninstalling bb without removing its setup must leave a shell that
	// starts quietly, not one that reports a missing command every time.
	for _, target := range []Target{
		{Shell: Bash}, {Shell: Fish}, {Shell: Zsh}, {Shell: Zsh, Shared: true}, {Shell: PowerShell, Shared: true},
	} {
		loader := Loader(target)
		if !strings.Contains(loader, "bb completion "+string(target.Shell)) {
			t.Errorf("%s loader does not run bb completion: %q", target.Shell, loader)
		}
		guarded := strings.Contains(loader, "command -v bb") || strings.Contains(loader, "command -q bb") ||
			strings.Contains(loader, "$+commands[bb]") || strings.Contains(loader, "Get-Command bb")
		if !guarded {
			t.Errorf("%s loader does not check bb is there: %q", target.Shell, loader)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func utf16Order(littleEndian bool) binary.ByteOrder {
	if littleEndian {
		return binary.LittleEndian
	}

	return binary.BigEndian
}
