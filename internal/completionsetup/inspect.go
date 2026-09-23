package completionsetup

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// OnPath reports whether shell is installed here, which is whether it is on
// PATH: for PowerShell, either edition.
func OnPath(system System, shell Shell) bool {
	if shell == PowerShell {
		return len(powerShellEditions(system)) > 0
	}

	_, err := system.LookPath(string(shell))

	return err == nil
}

// Editions are the names a PowerShell target's Edition can have.
func Editions() []string {
	return []string{"PowerShell 7", "Windows PowerShell 5.1"}
}

// packaged is where a package puts bb's completion script for one shell: the
// .deb and .rpm under /usr/share, and Homebrew under its prefix. zsh has two,
// because Debian reads vendor-completions and Fedora site-functions.
type packaged struct {
	system   []string
	homebrew string
}

var packagedScripts = map[Shell]packaged{
	Bash: {system: []string{"/usr/share/bash-completion/completions/bb"}, homebrew: "etc/bash_completion.d/bb"},
	Zsh: {
		system:   []string{"/usr/share/zsh/vendor-completions/_bb", "/usr/share/zsh/site-functions/_bb"},
		homebrew: "share/zsh/site-functions/_bb",
	},
	Fish: {system: []string{"/usr/share/fish/vendor_completions.d/bb.fish"}, homebrew: "share/fish/vendor_completions.d/bb.fish"},
}

// homebrewPrefixes are where Homebrew lives when HOMEBREW_PREFIX does not say:
// on Apple silicon and on an Intel Mac, and on Linux. Only an Intel Mac's is
// /usr/local, which elsewhere is the administrator's, and where bb completion
// install --all-users writes.
var homebrewPrefixes = map[string][]string{
	"darwin": {"/opt/homebrew", "/usr/local"},
	"linux":  {"/home/linuxbrew/.linuxbrew"},
}

// PackagedScripts are the places a package manager puts bb's completion script
// for shell. The package replaces the script on every upgrade and removes it
// with bb, so it is never bb completion install's to change. None on Windows,
// and none for PowerShell: no package there has a directory a shell reads.
func PackagedScripts(system System, shell Shell) []string {
	places, ok := packagedScripts[shell]
	if !ok || system.GOOS == "windows" {
		return nil
	}

	// Homebrew exists only where paths are written with slashes, so the prefix
	// is read as one of those whatever runs this.
	prefixes := homebrewPrefixes[system.GOOS]
	if prefix := strings.TrimSpace(system.Getenv("HOMEBREW_PREFIX")); path.IsAbs(prefix) {
		prefixes = []string{prefix}
	}

	found := append([]string{}, places.system...)
	for _, prefix := range prefixes {
		found = append(found, path.Join(prefix, places.homebrew))
	}

	return found
}

// StartupFile is a file a shell runs as it starts, where somebody may have
// added `bb completion` by hand.
type StartupFile struct {
	Shell Shell
	Scope Scope
	// Edition is the PowerShell that reads it, empty for the other shells.
	Edition string
	Path    string
}

// consoleProfile is the profile the PowerShell console reads beside its
// all-hosts one. It is what $PROFILE names in a terminal, and so where the
// documentation says to add the line by hand.
const consoleProfile = "Microsoft.PowerShell_profile.ps1"

// StartupFiles are the files shell runs as it starts in which completion may
// be set up by hand: ~/.bashrc and ~/.bash_profile, fish's config.fish, and
// the files among targets bb shares with their owner -- the .zshrc and the
// PowerShell profiles Targets found -- with the console's profile beside each
// PowerShell one.
func StartupFiles(system System, shell Shell, targets []Target) []StartupFile {
	files := []StartupFile{}
	add := func(scope Scope, edition, file string) {
		files = append(files, StartupFile{Shell: shell, Scope: scope, Edition: edition, Path: file})
	}

	for _, target := range targets {
		if target.Shell != shell || !target.Shared || target.Path == "" {
			continue
		}
		add(target.Scope, target.Edition, target.Path)
		if shell == PowerShell {
			add(target.Scope, target.Edition, filepath.Join(filepath.Dir(target.Path), consoleProfile))
		}
	}

	home, err := system.HomeDir()
	if err != nil {
		return files
	}

	switch shell {
	case Bash:
		add(CurrentUser, "", filepath.Join(home, ".bashrc"))
		add(CurrentUser, "", filepath.Join(home, ".bash_profile"))
	case Fish:
		add(CurrentUser, "", filepath.Join(xdg(system, "XDG_CONFIG_HOME", home, ".config"), "fish", "config.fish"))
	}

	return files
}

// handWritten is a line that runs `bb completion <shell>`, however it is fed to
// the shell: source <(bb completion bash), eval "$(bb completion zsh)",
// bb completion fish | source, /usr/local/bin/bb completion bash, or
// bb.exe completion powershell | Out-String | Invoke-Expression.
var handWritten = regexp.MustCompile(`(?:^|[^\w.-])bb(?:\.exe)?\s+completion\s+(?:bash|zsh|fish|powershell)\b`)

// SetUpByHand reports whether the startup file at path runs `bb completion
// <shell>` outside the block bb completion install adds: completion somebody
// set up themselves, which install and remove leave alone. A line that is
// commented out runs nothing and does not count.
func SetUpByHand(path string) (bool, error) {
	file, err := readText(path, "\n")
	if err != nil || !file.exists {
		return false, err
	}

	// A block that has lost its end marker has no extent bb can trust, so the
	// whole file is read instead.
	text := file.text
	if start, end, found, blockErr := findBlock(text); blockErr == nil && found {
		text = text[:start] + text[end:]
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if handWritten.MatchString(line) {
			return true, nil
		}
	}

	return false, nil
}
