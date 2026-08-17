package git

import "context"

type CloneOptions struct {
	Directory    string
	Branch       string
	Depth        int
	ExtraArgs    []string
	AuthToken    string
	AuthUsername string
	AuthPassword string
}

type FetchOptions struct {
	Remote string
}

type CheckoutOptions struct {
	Ref string
}

type Remote struct {
	Name string
	URL  string
}

// ConfigScope selects which git configuration file an operation targets.
type ConfigScope string

const (
	// ConfigScopeLocal targets a single repository's .git/config and requires
	// Directory to be set.
	ConfigScopeLocal ConfigScope = "local"
	// ConfigScopeGlobal targets the user's ~/.gitconfig.
	ConfigScopeGlobal ConfigScope = "global"
)

// ConfigOptions describes a single git configuration read or write.
type ConfigOptions struct {
	// Directory is the repository to operate on. Required for ConfigScopeLocal
	// and ignored for ConfigScopeGlobal. Operations are always explicitly
	// scoped with -C so they cannot fall through to whichever repository the
	// process happens to be standing in.
	Directory string
	Scope     ConfigScope
	Key       string
	Value     string
	// Append adds a value to a multi-valued key instead of replacing the key's
	// existing values. credential.<url>.helper is multi-valued: git consults
	// every configured helper in order, so replacing and appending are
	// meaningfully different operations.
	Append bool
}

type Backend interface {
	Version(ctx context.Context) (string, error)
	Clone(ctx context.Context, repositoryURL string, options CloneOptions) error
	AddRemote(ctx context.Context, repositoryDirectory string, remote Remote) error
	Fetch(ctx context.Context, repositoryDirectory string, options FetchOptions) error
	Checkout(ctx context.Context, repositoryDirectory string, options CheckoutOptions) error
	RepositoryRoot(ctx context.Context, workingDirectory string) (string, error)
	// CurrentBranch returns the checked-out branch name, or an empty string
	// when HEAD is detached. Detached HEAD is not an error: it is a repository
	// state with no branch to report, and callers that wanted one simply have
	// nothing to work with.
	CurrentBranch(ctx context.Context, repositoryDirectory string) (string, error)
	ListRemotes(ctx context.Context, repositoryDirectory string) ([]Remote, error)
	// GetConfig returns the value of a configuration key, or an empty string
	// when the key is unset. An unset key is not an error.
	GetConfig(ctx context.Context, options ConfigOptions) (string, error)
	SetConfig(ctx context.Context, options ConfigOptions) error
	// UnsetConfig removes a configuration key. Removing a key that is already
	// absent is not an error, so remediation can run unconditionally.
	UnsetConfig(ctx context.Context, options ConfigOptions) error
}
