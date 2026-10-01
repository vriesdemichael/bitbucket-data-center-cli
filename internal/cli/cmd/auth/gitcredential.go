package auth

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
)

// credentialRequest is the parsed form of git's credential helper input: a
// series of key=value lines terminated by a blank line or EOF.
type credentialRequest struct {
	Protocol string
	Host     string
	Path     string
	Username string
}

// URL reassembles the request into the form stored host configuration uses.
// Port is part of Host when git supplies one, so it survives the round trip.
func (request credentialRequest) URL() string {
	scheme := strings.TrimSpace(request.Protocol)
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + strings.TrimSpace(request.Host)
}

// parseCredentialRequest reads git's key=value input. Unknown keys are ignored
// rather than rejected: git adds new ones over time (wwwauth[], capability[]),
// and a helper that fails on them breaks on the next git release.
func parseCredentialRequest(reader io.Reader) (credentialRequest, error) {
	request := credentialRequest{}
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			break
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		switch strings.TrimSpace(key) {
		case "protocol":
			request.Protocol = value
		case "host":
			request.Host = value
		case "path":
			request.Path = value
		case "username":
			request.Username = value
		}
	}

	if err := scanner.Err(); err != nil {
		return credentialRequest{}, err
	}

	return request, nil
}

// newGitCredentialCommand implements git's credential helper protocol so that
// git can ask bb for credentials at the moment it needs them.
//
// This exists so that credentials never have to be written into a repository.
// The previous approach persisted an `http.extraHeader` containing a live token
// into every cloned repository's .git/config, which put the token on disk in
// plaintext and — because an unscoped extraHeader is attached to every HTTP
// request git makes from that repository — sent it to any other HTTP remote the
// user happened to add.
//
// Protocol notes that matter for correctness:
//
//   - stdout carries the protocol. Nothing else may be written there, or git
//     will parse diagnostics as credential fields.
//   - "I cannot help" is empty output and exit 0, not an error. A non-zero exit
//     makes git treat the whole credential lookup as failed rather than falling
//     through to another helper or prompting.
//   - store and erase are accepted and ignored. bb owns its own storage through
//     `bb auth login`; letting git write back would create a second source of
//     truth that silently diverges from the keyring.
//
// --config names the configuration file to answer from, and bb auth setup-git
// always writes it into the helper line. Without it the helper read whichever
// BB_CONFIG_PATH was in git's environment when git ran it, so two logins for
// one host -- a personal account and a service account, kept apart by their
// configuration files since #587 -- could not be told apart by git: it pushed
// as whichever the shell happened to export (#733).
func newGitCredentialCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "git-credential <get|store|erase>",
		Short: "Git credential helper (invoked by git, not run directly)",
		Example: `  # What git runs to ask for a credential; run it yourself to debug a failed push
  printf 'protocol=https\nhost=bitbucket.example.com\n\n' | bb auth git-credential get`,
		Long: `Supply stored Bitbucket credentials to git on demand.

git invokes this; you do not normally run it yourself. Configure it with:

  bb auth setup-git

Credentials are read from the same place ` + "`bb auth login`" + ` stores them, so git and
bb stay in agreement and no token is ever written into a repository.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := strings.TrimSpace(args[0])

			// bb is not the system credential store: git must not be able to
			// write into or clear the keyring through this path.
			if operation == "store" || operation == "erase" {
				return nil
			}

			if operation != "get" {
				return apperrors.New(
					apperrors.KindValidation,
					fmt.Sprintf("unsupported credential operation %q (expected get, store or erase)", operation),
					nil,
				)
			}

			request, err := parseCredentialRequest(cmd.InOrStdin())
			if err != nil {
				return apperrors.New(apperrors.KindValidation, "failed to read credential request from stdin", err)
			}

			if pinned := strings.TrimSpace(configPath); pinned != "" {
				// The helper line names the login it was set up with. A file
				// that has gone away is said on stderr, which git shows, and
				// answered with silence, so git falls through to a prompt
				// rather than to whatever other login the environment names.
				if _, err := os.Stat(pinned); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "bb: this credential helper answers from %s, which cannot be read: %v. Run 'bb auth setup-git' again\n", pinned, err)
					return nil
				}

				// The whole of the lookup reads the file through ConfigPath,
				// the keyring entry's name included, and this process exists
				// to answer this one request. Setenv refuses only a value with
				// a NUL byte in it, and os.Stat has refused that path already.
				_ = os.Setenv("BB_CONFIG_PATH", pinned)
			}

			if strings.TrimSpace(request.Host) == "" {
				// Nothing to look up. Stay silent so git moves on.
				return nil
			}

			username, password, ok, err := resolveGitCredential(request)
			if err != nil {
				// Nothing on stdout and exit 0, so git still falls through to its
				// other helpers or a prompt, as the protocol expects. But said on
				// stderr, which git shows: silence here was "not logged in" again,
				// for a config bb could not read (#567).
				//
				// A plaintext credential refused under the keyring requirement
				// (ADR-047) comes this way too, and is "cannot help" for the same
				// reasons: the helper has nothing it may give, a non-zero exit
				// would end git's lookup instead of letting it ask elsewhere, and
				// silence alone would read as not logged in when the remedy is to
				// log in again where the keyring works.
				fmt.Fprintf(cmd.ErrOrStderr(), "bb: %s\n", apperrors.MessageOf(err))

				return nil
			}
			if !ok {
				// No stored credentials for this host. Silence lets git fall
				// through to its other helpers or prompt the user.
				return nil
			}

			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "username=%s\npassword=%s\n\n", username, password); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to write credential response", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Configuration file holding the login to answer with, in place of BB_CONFIG_PATH")

	return cmd
}

// resolveGitCredential maps stored bb credentials onto the username/password
// pair git expects.
//
// git's credential protocol only carries username and password, so a stored
// personal access token is supplied as the password. Bitbucket Data Center
// accepts an HTTP access token in that position over Basic auth, which is what
// makes this work without the Bearer header the REST client uses.
func resolveGitCredential(request credentialRequest) (string, string, bool, error) {
	stored, ok, err := config.LoadStoredAuthForHostStrict(request.URL())
	if err != nil {
		return "", "", false, err
	}
	if !ok {
		return "", "", false, nil
	}

	if token := strings.TrimSpace(stored.BitbucketToken); token != "" {
		username := strings.TrimSpace(stored.BitbucketUsername)
		if username == "" {
			username = strings.TrimSpace(request.Username)
		}
		if username == "" {
			// Bitbucket ignores the username when the password is a valid
			// access token, but git requires the field to be present.
			username = "x-token-auth"
		}
		return username, token, true, nil
	}

	username := strings.TrimSpace(stored.BitbucketUsername)
	password := stored.BitbucketPassword
	if username != "" && password != "" {
		return username, password, true, nil
	}

	return "", "", false, nil
}

// newSetupGitCommand wires bb in as git's credential helper for a Bitbucket
// host.
//
// The configuration is deliberately scoped to the host URL rather than set as a
// bare credential.helper. A bare helper is consulted for every remote git talks
// to, so a misbehaving or over-eager helper becomes a way to leak Bitbucket
// credentials to unrelated hosts. Scoping means git only ever asks bb about the
// host the credentials belong to.
func newSetupGitCommand(deps Dependencies) *cobra.Command {
	var setupHost string
	var setupGlobal bool
	var setupForce bool

	cmd := &cobra.Command{
		Use:   "setup-git",
		Short: "Configure git to authenticate to Bitbucket through bb",
		Example: `  # Let git push and pull authenticate through bb, in every repository
  bb auth setup-git

  # In the current repository only
  bb auth setup-git --global=false

  # Take over from a credential helper git already has for the host
  bb auth setup-git --force`,
		Long: `Configure git to ask bb for Bitbucket credentials.

This replaces the need to embed credentials in a repository or in a remote URL.
The configuration is scoped to the Bitbucket host, so git never offers these
credentials to any other remote.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			host := strings.TrimSpace(setupHost)
			if host == "" {
				cfg, err := deps.LoadConfig()
				if err != nil {
					return err
				}
				host = strings.TrimSpace(cfg.BitbucketURL)
			}

			if host == "" {
				return apperrors.New(
					apperrors.KindValidation,
					"no Bitbucket host configured: pass --host or run 'bb auth login <host>' first",
					nil,
				)
			}

			parsed, err := url.Parse(host)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return apperrors.New(
					apperrors.KindValidation,
					fmt.Sprintf("invalid Bitbucket host %q: expected a URL such as https://bitbucket.example.com", host),
					err,
				)
			}

			executable, err := os.Executable()
			if err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to resolve the bb executable path", err)
			}

			configPath, err := pinnedConfigPath()
			if err != nil {
				return err
			}

			// Absolute path rather than a bare "bb": git resolves the helper
			// through a shell whose PATH may not match the user's, and a helper
			// that silently fails to launch looks like missing credentials.
			//
			// The configuration file is named too, so git authenticates as
			// the login this was set up with and not as whichever file
			// BB_CONFIG_PATH names when git runs it (#733).
			helper := fmt.Sprintf("!%q auth git-credential --config %q", executable, configPath)
			scope := parsed.Scheme + "://" + parsed.Host
			key := fmt.Sprintf("credential.%s.helper", scope)

			if err := deps.ConfigureGitCredentialHelper(cmd.Context(), key, helper, setupGlobal, setupForce); err != nil {
				return err
			}

			if isJSONEnabled(deps) {
				return deps.WriteJSON(cmd.OutOrStdout(), GitCredentialSetup{
					Host:   scope,
					Key:    key,
					Helper: helper,
					Scope:  gitScopeName(setupGlobal),
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Configured git to authenticate to %s through bb (%s), as the login in %s.\n", scope, gitScopeName(setupGlobal), configPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&setupHost, "host", "", "Bitbucket host URL (defaults to the configured host)")
	cmd.Flags().BoolVar(&setupGlobal, "global", true, "Write to global git config rather than the current repository")
	cmd.Flags().BoolVar(&setupForce, "force", false, "Overwrite an existing credential helper for this host")

	return cmd
}

// defaultConfigureGitCredentialHelper writes the helper configuration through
// the git backend.
//
// Refusing to overwrite an existing helper without --force is deliberate: the
// value may be another credential manager the user relies on, and silently
// replacing it would break authentication in a way that is hard to attribute.
func defaultConfigureGitCredentialHelper(ctx context.Context, key, value string, global, force bool) error {
	workingDirectory := ""
	if !global {
		current, err := os.Getwd()
		if err != nil {
			return apperrors.New(apperrors.KindInternal, "failed to determine the current directory", err)
		}
		workingDirectory = current
	}

	return configureGitCredentialHelperIn(ctx, workingDirectory, key, value, global, force)
}

// configureGitCredentialHelperIn takes the working directory explicitly rather
// than reading it from the process.
//
// Tests can therefore point local-scope behaviour at a repository they created
// without calling t.Chdir, which moves the working directory for the whole test
// binary: anything running afterwards that shells out to git relative to the
// process directory then operates on whatever repository it lands in. Doing that
// once wrote core.bare=true into this project's own configuration and broke
// every worktree.
func configureGitCredentialHelperIn(ctx context.Context, workingDirectory, key, value string, global, force bool) error {
	backend := execgit.New()

	options := git.ConfigOptions{Scope: git.ConfigScopeGlobal, Key: key, Value: value}
	if !global {
		root, err := backend.RepositoryRoot(ctx, workingDirectory)
		if err != nil {
			return apperrors.New(
				apperrors.KindValidation,
				"not inside a git repository: run from a repository or use --global",
				err,
			)
		}

		options.Scope = git.ConfigScopeLocal
		options.Directory = root
	}

	existing, err := backend.GetConfig(ctx, options)
	if err != nil {
		return err
	}

	if !replaceableHelper(existing, value) && !force {
		return apperrors.New(
			apperrors.KindConflict,
			fmt.Sprintf("a credential helper is already configured for this host (%s = %s); pass --force to replace it", key, existing),
			nil,
		)
	}

	// credential.<url>.helper is multi-valued and git asks every configured
	// helper in turn, so simply adding bb is not enough: a helper inherited from
	// a broader scope — a system credential manager, or an entry in the global
	// config when writing locally — would still be consulted first and could
	// answer with a stale credential.
	//
	// Setting the key to an empty value resets the list for this host, and the
	// entry after it is then the only helper git will consult. This mirrors what
	// `gh auth setup-git` does for the same reason.
	reset := options
	reset.Value = ""
	reset.Append = false
	if err := backend.SetConfig(ctx, reset); err != nil {
		return err
	}

	options.Append = true
	return backend.SetConfig(ctx, options)
}

// pinnedConfigPath is the configuration file the helper line names: the one
// this run of bb reads, as an absolute path, since git runs the helper from
// wherever the repository is.
func pinnedConfigPath() (string, error) {
	path, err := config.ConfigPath()
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to resolve the configuration file", err)
	}

	return path, nil
}

// unpinnedHelper matches a helper line that runs bb's helper with no --config:
// the one an earlier bb wrote, with its executable quoted, or one written by
// hand naming bb on PATH.
var unpinnedHelper = regexp.MustCompile(`^!(?:"[^"]*"|\S+) auth git-credential$`)

// replaceableHelper reports whether setup-git may write value over existing
// without --force: nothing is there, it is the line already, or it is bb's own
// line from before the helper named its configuration. That line answered for
// whichever login git's environment named, so pinning one replaces no identity
// anybody chose.
func replaceableHelper(existing, value string) bool {
	trimmed := strings.TrimSpace(existing)

	return trimmed == "" || existing == value || unpinnedHelper.MatchString(trimmed)
}

func gitScopeName(global bool) string {
	if global {
		return "global"
	}
	return "local"
}

func isJSONEnabled(deps Dependencies) bool {
	if deps.JSONEnabled == nil {
		return false
	}
	return deps.JSONEnabled()
}
