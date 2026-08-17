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
	// Refspecs are passed after the remote, overriding whatever the remote is
	// configured to fetch.
	//
	// A pull request checkout needs this: the branch it wants may live in a
	// fork the local repository has never fetched from, and even for a
	// same-repository pull request the configured refspec may not have been
	// fetched recently enough to contain the head.
	Refspecs []string
	// Credentials, when set, are supplied to this one fetch.
	//
	// Without them a fetch into a repository that has no credential helper
	// configured stops to prompt for a username, which in a non-interactive
	// context is a hang or a failure rather than a question. A clone made by
	// bb leaves no credential behind by design, so its own later fetches have
	// to bring their own.
	Credentials *Credentials
}

// Credentials are handed to a single git invocation and never persisted.
//
// URL is required and is what scopes them: git applies an unscoped
// http.extraHeader to every host it contacts, including redirect targets, so
// an unscoped credential for one host leaks to any other remote in the same
// repository. See ADR-044.
type Credentials struct {
	URL      string
	Token    string
	Username string
	Password string
}

type CheckoutOptions struct {
	// Ref is what to check out, or what to start a new branch from when
	// NewBranch is set.
	Ref string
	// NewBranch creates a branch of this name at Ref rather than checking out
	// Ref itself. Creating a branch that already exists is an error, which is
	// deliberate: silently moving someone's branch is not a checkout.
	NewBranch string
	// Detach checks out Ref without putting HEAD on a branch. Useful for
	// reading a pull request without adopting it.
	Detach bool
	// Force discards local modifications that would be overwritten.
	Force bool
}

// WorkingTreeStatus describes how far a repository is from a clean checkout.
type WorkingTreeStatus struct {
	// Dirty reports staged or unstaged modifications to tracked files.
	//
	// Untracked files deliberately do not count. They cannot be overwritten by
	// a branch switch, so refusing a checkout for them would block the common
	// case of build output or scratch files sitting in the tree.
	Dirty bool
	// Entries are the porcelain status lines behind Dirty, so an error can name
	// what is in the way instead of only asserting that something is.
	Entries []string
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
	// WorkingTreeState reports modifications to tracked files, so a command
	// that is about to move HEAD can refuse before git does and say what is in
	// the way.
	WorkingTreeState(ctx context.Context, repositoryDirectory string) (WorkingTreeStatus, error)
	// BranchExists reports whether a local branch of this name is already
	// present. Checking first is what lets a caller reuse a branch it created
	// earlier instead of failing on the second run.
	BranchExists(ctx context.Context, repositoryDirectory string, branch string) (bool, error)
	// FastForward advances the checked-out branch to ref, refusing when ref is
	// not a descendant.
	//
	// --ff-only rather than a plain merge or a reset: silently creating a merge
	// commit and silently discarding local commits are both worse than an
	// error telling the caller their branch has diverged.
	FastForward(ctx context.Context, repositoryDirectory string, ref string) error
	ListRemotes(ctx context.Context, repositoryDirectory string) ([]Remote, error)
	// GetConfig returns the value of a configuration key, or an empty string
	// when the key is unset. An unset key is not an error.
	GetConfig(ctx context.Context, options ConfigOptions) (string, error)
	SetConfig(ctx context.Context, options ConfigOptions) error
	// UnsetConfig removes a configuration key. Removing a key that is already
	// absent is not an error, so remediation can run unconditionally.
	UnsetConfig(ctx context.Context, options ConfigOptions) error
}
