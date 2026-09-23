package completionsetup

import (
	"encoding/binary"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOnPathFindsEitherPowerShell(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		goos      string
		installed map[string]string
		shell     Shell
		want      bool
	}{
		{name: "bash on PATH", goos: "linux", installed: map[string]string{"bash": "/bin/bash"}, shell: Bash, want: true},
		{name: "fish not on PATH", goos: "linux", installed: map[string]string{"bash": "/bin/bash"}, shell: Fish},
		{name: "PowerShell 7 alone", goos: "linux", installed: map[string]string{"pwsh": "/usr/bin/pwsh"}, shell: PowerShell, want: true},
		{name: "Windows PowerShell alone", goos: "windows", installed: map[string]string{"powershell": "powershell.exe"}, shell: PowerShell, want: true},
		// Windows PowerShell exists only on Windows, whatever PATH says.
		{name: "a powershell elsewhere", goos: "linux", installed: map[string]string{"powershell": "/usr/bin/powershell"}, shell: PowerShell},
	} {
		fake := &fakeSystem{goos: testCase.goos, installed: testCase.installed}
		if got := OnPath(fake.system(), testCase.shell); got != testCase.want {
			t.Errorf("%s: OnPath = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// TestEditionsNameEveryPowerShell holds the published list to the editions
// Targets can report.
func TestEditionsNameEveryPowerShell(t *testing.T) {
	t.Parallel()

	fake := &fakeSystem{goos: "windows", installed: map[string]string{"pwsh": "pwsh.exe", "powershell": "powershell.exe"}}

	found := []string{}
	for _, edition := range powerShellEditions(fake.system()) {
		found = append(found, edition.name)
	}
	if !reflect.DeepEqual(found, Editions()) {
		t.Errorf("Targets reports editions %q, Editions lists %q", found, Editions())
	}
}

func TestPackagedScriptsAreWherePackagesPutThem(t *testing.T) {
	t.Parallel()

	linux := &fakeSystem{goos: "linux"}
	want := map[Shell][]string{
		Bash: {
			"/usr/share/bash-completion/completions/bb",
			"/home/linuxbrew/.linuxbrew/etc/bash_completion.d/bb",
		},
		Zsh: {
			"/usr/share/zsh/vendor-completions/_bb",
			"/usr/share/zsh/site-functions/_bb",
			"/home/linuxbrew/.linuxbrew/share/zsh/site-functions/_bb",
		},
		Fish: {
			"/usr/share/fish/vendor_completions.d/bb.fish",
			"/home/linuxbrew/.linuxbrew/share/fish/vendor_completions.d/bb.fish",
		},
	}
	for shell, paths := range want {
		if got := PackagedScripts(linux.system(), shell); !reflect.DeepEqual(got, paths) {
			t.Errorf("%s: got %q, want %q", shell, got, paths)
		}
	}

	// /usr/local is Homebrew's only on an Intel Mac. On Linux it is where bb
	// completion install --all-users writes, and a script there is bb's own.
	mac := &fakeSystem{goos: "darwin"}
	if got, want := PackagedScripts(mac.system(), Zsh), []string{
		"/usr/share/zsh/vendor-completions/_bb",
		"/usr/share/zsh/site-functions/_bb",
		"/opt/homebrew/share/zsh/site-functions/_bb",
		"/usr/local/share/zsh/site-functions/_bb",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("macOS: got %q, want %q", got, want)
	}

	// HOMEBREW_PREFIX, which brew shellenv sets, is where Homebrew is,
	// wherever it was installed.
	elsewhere := &fakeSystem{goos: "linux", env: map[string]string{"HOMEBREW_PREFIX": "/opt/brew"}}
	if got, want := PackagedScripts(elsewhere.system(), Fish), []string{
		"/usr/share/fish/vendor_completions.d/bb.fish",
		"/opt/brew/share/fish/vendor_completions.d/bb.fish",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("under HOMEBREW_PREFIX: got %q, want %q", got, want)
	}

	relative := &fakeSystem{goos: "linux", env: map[string]string{"HOMEBREW_PREFIX": "brew"}}
	if got := PackagedScripts(relative.system(), Bash); !reflect.DeepEqual(got, want[Bash]) {
		t.Errorf("a relative HOMEBREW_PREFIX was used: %q", got)
	}

	windows := &fakeSystem{goos: "windows"}
	if got := PackagedScripts(windows.system(), Bash); len(got) != 0 {
		t.Errorf("Windows has packaged scripts: %q", got)
	}
	if got := PackagedScripts(linux.system(), PowerShell); len(got) != 0 {
		t.Errorf("PowerShell has packaged scripts: %q", got)
	}
}

func TestStartupFilesAreWhatEachShellRunsAsItStarts(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	elsewhere := t.TempDir()
	fake := &fakeSystem{goos: "linux", home: home, env: map[string]string{"XDG_CONFIG_HOME": elsewhere}}

	paths := func(files []StartupFile) []string {
		found := []string{}
		for _, file := range files {
			found = append(found, file.Path)
		}
		return found
	}

	if got, want := paths(StartupFiles(fake.system(), Bash, nil)), []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}; !reflect.DeepEqual(got, want) {
		t.Errorf("bash: got %q, want %q", got, want)
	}
	if got, want := paths(StartupFiles(fake.system(), Fish, nil)), []string{filepath.Join(elsewhere, "fish", "config.fish")}; !reflect.DeepEqual(got, want) {
		t.Errorf("fish: got %q, want %q", got, want)
	}

	// The .zshrc bb shares with its owner is a startup file; a file of bb's
	// own, such as zsh's for every user, is not.
	zshrc := Target{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(home, ".zshrc"), Shared: true}
	allUsers := Target{Shell: Zsh, Scope: AllUsers, Path: "/usr/local/share/zsh/site-functions/_bb"}
	if got, want := paths(StartupFiles(fake.system(), Zsh, []Target{zshrc, allUsers})), []string{zshrc.Path}; !reflect.DeepEqual(got, want) {
		t.Errorf("zsh: got %q, want %q", got, want)
	}

	// Each PowerShell profile, and the console's beside it. A PowerShell that
	// could not say where its profile is has none.
	profile := filepath.Join(home, "PowerShell", "profile.ps1")
	got := StartupFiles(fake.system(), PowerShell, []Target{
		{Shell: PowerShell, Scope: CurrentUser, Edition: "PowerShell 7", Path: profile, Shared: true},
		{Shell: PowerShell, Scope: CurrentUser, Edition: "Windows PowerShell 5.1", Shared: true, Blocked: "it did not say where its profile is"},
	})
	want := []StartupFile{
		{Shell: PowerShell, Scope: CurrentUser, Edition: "PowerShell 7", Path: profile},
		{Shell: PowerShell, Scope: CurrentUser, Edition: "PowerShell 7", Path: filepath.Join(home, "PowerShell", "Microsoft.PowerShell_profile.ps1")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PowerShell: got %+v, want %+v", got, want)
	}
}

func TestSetUpByHandFindsTheLinesTheDocumentationShows(t *testing.T) {
	t.Parallel()

	block := Loader(Target{Shell: Zsh, Shared: true})
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	cutShort := strings.Join(lines[:len(lines)-1], "\n") + "\n"

	for _, testCase := range []struct {
		name    string
		content string
		want    bool
	}{
		{name: "bash", content: "export EDITOR=vim\nsource <(bb completion bash)\n", want: true},
		{name: "zsh through eval", content: `eval "$(bb completion zsh)"` + "\n", want: true},
		{name: "fish", content: "bb completion fish | source\n", want: true},
		{name: "PowerShell", content: "Import-Module posh-git\r\nbb completion powershell | Out-String | Invoke-Expression\r\n", want: true},
		{name: "a path to bb", content: "source <(/usr/local/bin/bb completion bash --no-descriptions)\n", want: true},
		{name: "bb.exe", content: `& C:\tools\bb.exe completion powershell | Out-String | Invoke-Expression` + "\n", want: true},
		{name: "a quoted path", content: `source <("$HOME/bin/bb" completion bash)` + "\n", want: true},
		{name: "a quoted bb.exe", content: `& "$env:LOCALAPPDATA\bb\bb.exe" completion powershell | Out-String | Invoke-Expression` + "\n", want: true},
		{name: "commented out", content: "  # source <(bb completion bash)\n"},
		{name: "another command", content: "source <(mybb completion bash)\n"},
		{name: "not a script", content: "alias setup='bb completion install'\n"},
		{name: "only bb's block", content: "setopt autocd\n\n" + block},
		{name: "a line beside bb's block", content: "source <(bb completion zsh)\n\n" + block, want: true},
		// With no end marker the block's extent is unknown, so its line counts.
		{name: "a block cut short", content: cutShort, want: true},
	} {
		path := filepath.Join(t.TempDir(), "startup")
		write(t, path, testCase.content)

		got, err := SetUpByHand(path)
		if err != nil || got != testCase.want {
			t.Errorf("%s: SetUpByHand = %v, %v; want %v", testCase.name, got, err, testCase.want)
		}
	}

	if got, err := SetUpByHand(filepath.Join(t.TempDir(), "absent")); got || err != nil {
		t.Errorf("a file that is not there: %v, %v", got, err)
	}
}

// TestSetUpByHandReadsAProfileInUTF16 covers the profile Windows PowerShell
// 5.1 writes with > or Out-File.
func TestSetUpByHandReadsAProfileInUTF16(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Microsoft.PowerShell_profile.ps1")
	profile := textFile{bom: utf16LEBOM, order: binary.LittleEndian, text: "bb completion powershell | Out-String | Invoke-Expression\r\n"}
	if err := profile.write(path, CurrentUser); err != nil {
		t.Fatal(err)
	}

	if got, err := SetUpByHand(path); !got || err != nil {
		t.Errorf("a UTF-16 profile: %v, %v", got, err)
	}
}
