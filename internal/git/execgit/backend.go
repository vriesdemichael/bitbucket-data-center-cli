package execgit

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
)

var (
	urlCredRegex    = regexp.MustCompile(`(https?://)([^:@\s]*):([^@\s]+)@`)
	authHeaderRegex = regexp.MustCompile(`(?i)(Authorization:\s*)(Bearer|Basic)\s+([^\s"']+)`)
)

func redact(s string) string {
	// Redact URL credentials: replace password/token with ***
	s = urlCredRegex.ReplaceAllString(s, "${1}${2}:***@")
	// Redact Authorization headers: replace value/token with ***
	s = authHeaderRegex.ReplaceAllString(s, "${1}${2} ***")
	return s
}

const defaultTimeout = 60 * time.Second

type Backend struct {
	Timeout time.Duration
}

func New() *Backend {
	return &Backend{Timeout: defaultTimeout}
}

func (backend *Backend) Version(ctx context.Context) (string, error) {
	result, err := backend.run(ctx, runOptions{args: []string{"--version"}})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result.stdout), nil
}

func (backend *Backend) Clone(ctx context.Context, repositoryURL string, options git.CloneOptions) error {
	if strings.TrimSpace(repositoryURL) == "" {
		return apperrors.New(apperrors.KindValidation, "repository URL cannot be empty", nil)
	}

	if strings.TrimSpace(options.Directory) == "" {
		return apperrors.New(apperrors.KindValidation, "clone directory cannot be empty", nil)
	}

	// Credentials are supplied for the duration of this one command only, and
	// are never written into the resulting repository.
	//
	// This previously set http.extraHeader both on the clone invocation and
	// persistently in the new repository's .git/config. That put a live token
	// on disk in plaintext, and because an unscoped extraHeader is attached to
	// every HTTP request git makes from that repository, adding any unrelated
	// HTTP remote caused the Bitbucket token to be sent to that host too.
	//
	// A credential helper cannot be used here because it would have to be
	// configured before the repository exists. Instead the header is passed
	// with -c so it lives only in this process's argv, and the persistent
	// credential path is set up afterwards by `bb auth setup-git`.
	var headerVal string
	if options.AuthToken != "" {
		headerVal = fmt.Sprintf("Authorization: Bearer %s", options.AuthToken)
	} else if options.AuthUsername != "" && options.AuthPassword != "" {
		auth := options.AuthUsername + ":" + options.AuthPassword
		headerVal = fmt.Sprintf("Authorization: Basic %s", base64.StdEncoding.EncodeToString([]byte(auth)))
	}

	var args []string
	if headerVal != "" {
		// Scope the header to the host being cloned from. An unscoped
		// http.extraHeader applies to every host git contacts, including any
		// redirect target.
		if scope := httpConfigScope(repositoryURL); scope != "" {
			args = append(args, "-c", fmt.Sprintf("http.%s.extraHeader=%s", scope, headerVal))
		} else {
			args = append(args, "-c", fmt.Sprintf("http.extraHeader=%s", headerVal))
		}
	}
	args = append(args, "clone")
	if options.Branch != "" {
		args = append(args, "--branch", options.Branch)
	}
	if options.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", options.Depth))
	}
	if len(options.ExtraArgs) > 0 {
		args = append(args, options.ExtraArgs...)
	}
	args = append(args, repositoryURL, options.Directory)

	if _, err := backend.run(ctx, runOptions{args: args}); err != nil {
		return err
	}

	return nil
}

// httpConfigScope returns the scheme://host[:port]/ prefix git uses to scope
// http.* configuration, or an empty string when the URL cannot be parsed.
func httpConfigScope(repositoryURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(repositoryURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/"
}

func (backend *Backend) Fetch(ctx context.Context, repositoryDirectory string, options git.FetchOptions) error {
	if strings.TrimSpace(repositoryDirectory) == "" {
		return apperrors.New(apperrors.KindValidation, "repository directory cannot be empty", nil)
	}

	args := []string{"fetch"}
	if strings.TrimSpace(options.Remote) != "" {
		args = append(args, options.Remote)
	}

	_, err := backend.run(ctx, runOptions{cwd: repositoryDirectory, args: args})
	return err
}

func (backend *Backend) AddRemote(ctx context.Context, repositoryDirectory string, remote git.Remote) error {
	if strings.TrimSpace(repositoryDirectory) == "" {
		return apperrors.New(apperrors.KindValidation, "repository directory cannot be empty", nil)
	}

	name := strings.TrimSpace(remote.Name)
	if name == "" {
		return apperrors.New(apperrors.KindValidation, "remote name cannot be empty", nil)
	}

	remoteURL := strings.TrimSpace(remote.URL)
	if remoteURL == "" {
		return apperrors.New(apperrors.KindValidation, "remote URL cannot be empty", nil)
	}

	_, err := backend.run(ctx, runOptions{cwd: repositoryDirectory, args: []string{"remote", "add", name, remoteURL}})
	return err
}

func (backend *Backend) Checkout(ctx context.Context, repositoryDirectory string, options git.CheckoutOptions) error {
	if strings.TrimSpace(repositoryDirectory) == "" {
		return apperrors.New(apperrors.KindValidation, "repository directory cannot be empty", nil)
	}

	if strings.TrimSpace(options.Ref) == "" {
		return apperrors.New(apperrors.KindValidation, "checkout ref cannot be empty", nil)
	}

	_, err := backend.run(ctx, runOptions{cwd: repositoryDirectory, args: []string{"checkout", options.Ref}})
	return err
}

func (backend *Backend) RepositoryRoot(ctx context.Context, workingDirectory string) (string, error) {
	result, err := backend.run(ctx, runOptions{cwd: strings.TrimSpace(workingDirectory), args: []string{"rev-parse", "--show-toplevel"}})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result.stdout), nil
}

func (backend *Backend) ListRemotes(ctx context.Context, repositoryDirectory string) ([]git.Remote, error) {
	trimmedDir := strings.TrimSpace(repositoryDirectory)
	if trimmedDir == "" {
		return nil, apperrors.New(apperrors.KindValidation, "repository directory cannot be empty", nil)
	}

	result, err := backend.run(ctx, runOptions{cwd: trimmedDir, args: []string{"remote", "-v"}})
	if err != nil {
		return nil, err
	}

	lines := strings.Split(result.stdout, "\n")
	seen := map[string]struct{}{}
	remotes := make([]git.Remote, 0)
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		if fields[2] != "(fetch)" {
			continue
		}

		name := strings.TrimSpace(fields[0])
		url := strings.TrimSpace(fields[1])
		if name == "" || url == "" {
			continue
		}

		key := name + "\x00" + url
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		remotes = append(remotes, git.Remote{Name: name, URL: url})
	}

	sort.SliceStable(remotes, func(left, right int) bool {
		if remotes[left].Name == remotes[right].Name {
			return remotes[left].URL < remotes[right].URL
		}
		if remotes[left].Name == "origin" {
			return true
		}
		if remotes[right].Name == "origin" {
			return false
		}
		return remotes[left].Name < remotes[right].Name
	})

	return remotes, nil
}

type runOptions struct {
	cwd  string
	args []string
}

type runResult struct {
	stdout string
	stderr string
}

// configArgs builds the leading arguments for a git config invocation.
// Local-scope operations are addressed with -C so they cannot fall through to
// whichever repository the process happens to be standing in.
func configArgs(options git.ConfigOptions) ([]string, error) {
	if strings.TrimSpace(options.Key) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "git config key cannot be empty", nil)
	}

	switch options.Scope {
	case git.ConfigScopeGlobal:
		return []string{"config", "--global"}, nil
	case git.ConfigScopeLocal, "":
		directory := strings.TrimSpace(options.Directory)
		if directory == "" {
			return nil, apperrors.New(apperrors.KindValidation, "git config directory cannot be empty for local scope", nil)
		}
		return []string{"-C", directory, "config", "--local"}, nil
	default:
		return nil, apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("unsupported git config scope %q", string(options.Scope)),
			nil,
		)
	}
}

func (backend *Backend) GetConfig(ctx context.Context, options git.ConfigOptions) (string, error) {
	args, err := configArgs(options)
	if err != nil {
		return "", err
	}

	// --get-all rather than --get: credential.<url>.helper is multi-valued, and
	// --get exits 2 when several values are present.
	result, err := backend.run(ctx, runOptions{args: append(args, "--get-all", options.Key)})
	if err != nil {
		// git exits 1 when the key is simply absent. That is a normal answer to
		// "is this configured", not a failure, so report it as an empty value.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(result.stderr) == "" {
			return "", nil
		}
		return "", err
	}

	// Report the effective value, which for a multi-valued key is the last one.
	lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

func (backend *Backend) SetConfig(ctx context.Context, options git.ConfigOptions) error {
	args, err := configArgs(options)
	if err != nil {
		return err
	}

	// --replace-all so a plain set is deterministic on a multi-valued key, and
	// --add when the caller is deliberately appending.
	mode := "--replace-all"
	if options.Append {
		mode = "--add"
	}

	_, err = backend.run(ctx, runOptions{args: append(args, mode, options.Key, options.Value)})
	return err
}

func (backend *Backend) UnsetConfig(ctx context.Context, options git.ConfigOptions) error {
	args, err := configArgs(options)
	if err != nil {
		return err
	}

	result, err := backend.run(ctx, runOptions{args: append(args, "--unset-all", options.Key)})
	if err != nil {
		// Exit code 5 means the key was not set. Remediation runs
		// unconditionally, so "already clean" must not be an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 && strings.TrimSpace(result.stderr) == "" {
			return nil
		}
		return err
	}

	return nil
}

func (backend *Backend) run(ctx context.Context, options runOptions) (runResult, error) {
	if len(options.args) == 0 {
		return runResult{}, apperrors.New(apperrors.KindValidation, "git command cannot be empty", nil)
	}

	if backend.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, backend.Timeout)
		defer cancel()
	}

	command := exec.CommandContext(ctx, "git", options.args...)
	if options.cwd != "" {
		command.Dir = options.cwd
	}
	command.Env = ScopeFreeEnv()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := runResult{stdout: redact(stdout.String()), stderr: redact(stderr.String())}
	if err != nil {
		message := strings.TrimSpace(result.stderr)
		if message == "" {
			message = strings.TrimSpace(err.Error())
		}
		redactedArgs := make([]string, len(options.args))
		for i, arg := range options.args {
			redactedArgs[i] = redact(arg)
		}
		redactedMsg := redact(message)
		return result, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("git %s failed: %s", strings.Join(redactedArgs, " "), redactedMsg), err)
	}

	return result, nil
}
