package prcmd

import (
	"context"
	"strings"
	"sync"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prompt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// createInference works out what bb pr create was not told, from where it is
// run: the branch that is checked out, the repository's default branch, and the
// subject of the branch's commit when it holds exactly one.
//
// A person is offered each and can change it. With nobody there, the target
// branch and a single commit's subject stand in for their flags, because
// neither leaves a choice; the checked-out branch does only when the
// repository was itself taken from this checkout, since with --repo naming
// another the branch underfoot says nothing about it. Whatever cannot be
// worked out infers nothing, and the flag is asked for as before.
type createInference struct {
	ctx        context.Context
	deps       Dependencies
	repository string
	fromRepo   string
	fromRef    *string
	toRef      *string

	load    sync.Once
	service *pullrequestservice.Service
	repo    pullrequestservice.RepositoryRef
	loaded  bool
}

// target is the repository the pull request would be opened in. The
// configuration is loaded only when something has to be inferred, so a
// complete invocation still reports a missing host as its own problem.
func (inference *createInference) target() (*pullrequestservice.Service, pullrequestservice.RepositoryRef, bool) {
	inference.load.Do(func() {
		cfg, err := inference.deps.LoadConfig()
		if err != nil {
			return
		}
		projectKey, slug, err := reposel.Resolve(inference.repository, cfg)
		if err != nil {
			return
		}

		inference.service = pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
		inference.repo = pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}
		inference.loaded = true
	})

	return inference.service, inference.repo, inference.loaded
}

func (inference *createInference) sourceBranch() prompt.Inferred {
	branch, err := currentGitBranch(inference.ctx, inference.deps)
	if err != nil || branch == "" {
		return prompt.Inferred{}
	}

	return prompt.Inferred{
		Value:      branch,
		Source:     "the checked-out branch",
		Unattended: inference.deps.RepositoryWasInferred != nil && inference.deps.RepositoryWasInferred(),
	}
}

func (inference *createInference) targetBranch() prompt.Inferred {
	service, repo, ok := inference.target()
	if !ok {
		return prompt.Inferred{}
	}

	branch, err := service.DefaultBranch(inference.ctx, repo)
	if err != nil || branch == "" {
		return prompt.Inferred{}
	}
	// A pull request from the default branch into itself is nothing to open,
	// so standing on it says nothing about where the change should go.
	if strings.TrimSpace(inference.fromRepo) == "" && branchName(*inference.fromRef) == branch {
		return prompt.Inferred{}
	}

	return prompt.Inferred{Value: branch, Source: "the repository's default branch", Unattended: true}
}

func (inference *createInference) title() prompt.Inferred {
	fromRef, toRef := strings.TrimSpace(*inference.fromRef), strings.TrimSpace(*inference.toRef)
	if fromRef == "" || toRef == "" {
		return prompt.Inferred{}
	}
	service, repo, ok := inference.target()
	if !ok {
		return prompt.Inferred{}
	}
	fromRepository, err := resolveSourceRepository(inference.fromRepo, repo)
	if err != nil {
		return prompt.Inferred{}
	}

	subject, only, err := service.OnlyCommitSubject(inference.ctx, repo, fromRepository, fromRef, toRef)
	if err != nil || !only {
		return prompt.Inferred{}
	}

	return prompt.Inferred{Value: subject, Source: "the branch's only commit", Unattended: true}
}

// branchName is a branch as it is named, whichever way the ref was written.
func branchName(ref string) string {
	return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
}
