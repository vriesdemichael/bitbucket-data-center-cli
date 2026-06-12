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

type Backend interface {
	Version(ctx context.Context) (string, error)
	Clone(ctx context.Context, repositoryURL string, options CloneOptions) error
	AddRemote(ctx context.Context, repositoryDirectory string, remote Remote) error
	Fetch(ctx context.Context, repositoryDirectory string, options FetchOptions) error
	Checkout(ctx context.Context, repositoryDirectory string, options CheckoutOptions) error
	RepositoryRoot(ctx context.Context, workingDirectory string) (string, error)
	ListRemotes(ctx context.Context, repositoryDirectory string) ([]Remote, error)
}
