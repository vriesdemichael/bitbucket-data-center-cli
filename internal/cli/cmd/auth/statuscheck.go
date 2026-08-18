package auth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
)

// statusProbeTimeout bounds the one network call bb auth status makes.
const statusProbeTimeout = 10 * time.Second

// statusCheck is one thing bb auth status verified, rather than merely
// reported.
//
// The distinction is the point of this type. Before it, `bb auth status`
// printed the configured target and how the credential was stored and stopped
// there — so an expired token, an unreachable host and a working setup all
// produced the same confident output. What the reader wanted to know, in every
// case where they were running it at all, was which of the three they had.
type statusCheck struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Advisory marks a check whose failure does not mean the setup is broken.
	//
	// The git credential helper is the case that forced this: it is needed to
	// git push, and irrelevant to anything that only calls the API. Counting it
	// as a failure would have made bb auth status --check fail on every CI
	// pipeline and every agent that never runs git at all — reporting a broken
	// setup to people whose setup is fine, which is the failure mode this whole
	// command exists to remove.
	Advisory bool `json:"advisory,omitempty"`
	// Detail is the finding: who you are, or what went wrong.
	Detail string `json:"detail,omitempty"`
	// Remedy is what to do about it, and is only set when OK is false. A
	// failure that names no fix leaves the reader where it found them.
	Remedy string `json:"remedy,omitempty"`
}

// gitCredentialHelperState reports whether git is configured to ask bb for
// credentials on this host.
//
// Its absence is the single most common cause of "git prompts for a password
// after bb works fine", because bb authenticates itself and plain git does
// not go through bb at all. Nothing previously surfaced it.
func gitCredentialHelperState(ctx context.Context, backend git.Backend, bitbucketURL string) statusCheck {
	check := statusCheck{Name: "git credential helper", Advisory: true}

	parsed, err := url.Parse(strings.TrimSpace(bitbucketURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		check.Detail = "no usable host to check"
		return check
	}

	scope := parsed.Scheme + "://" + parsed.Host
	key := fmt.Sprintf("credential.%s.helper", scope)

	value, err := backend.GetConfig(ctx, git.ConfigOptions{Scope: git.ConfigScopeGlobal, Key: key})
	if err != nil {
		check.Detail = "could not read git configuration: " + err.Error()
		check.Remedy = "run 'bb auth setup-git'"
		return check
	}

	if strings.TrimSpace(value) == "" {
		check.Detail = fmt.Sprintf("not configured for %s", scope)
		check.Remedy = "run 'bb auth setup-git' so git push and git pull authenticate too"
		return check
	}

	check.OK = true
	check.Detail = fmt.Sprintf("configured for %s", scope)
	return check
}

// defaultGitBackend is a seam so the helper check can be exercised without
// touching the developer's real git configuration.
var defaultGitBackend = func() git.Backend { return execgit.New() }

// identityState reaches the configured Bitbucket and reports who the stored
// credential authenticates as.
//
// This is the check that turns a claim into a fact: it is the only one that
// proves the host is reachable, the TLS chain is trusted, any proxy is working,
// and the credential is still valid — all at once, and all as one line.
func identityState(ctx context.Context, cfg config.AppConfig, newUsersClient func(config.AppConfig) (usersClient, error)) statusCheck {
	check := statusCheck{Name: "authentication"}

	// Bounded separately from the global request timeout, which defaults to 20s
	// and is retried: an unreachable-but-routable host would otherwise leave a
	// status command hanging for a minute. Status is something people run when
	// they are already confused, so it has to answer quickly even when the
	// answer is bad news.
	probeCtx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()

	identity, err := resolveIdentity(probeCtx, cfg, newUsersClient)
	if err != nil {
		check.Detail = err.Error()
		check.Remedy = remedyForAuthFailure(err, cfg.BitbucketURL)
		return check
	}

	check.OK = true
	check.Detail = identityHumanSummary(identity)
	return check
}

// remedyForAuthFailure turns the error kind into the next thing to try.
//
// Kind rather than message text: the taxonomy in ADR-011 already distinguishes
// "your credential is wrong" from "the host did not answer", and those have
// genuinely different fixes. Matching on wording would re-derive that
// distinction badly.
func remedyForAuthFailure(err error, bitbucketURL string) string {
	switch {
	case apperrors.IsKind(err, apperrors.KindAuthentication), apperrors.IsKind(err, apperrors.KindAuthorization):
		return fmt.Sprintf("the credential was rejected; run 'bb auth login %s' again", strings.TrimSpace(bitbucketURL))
	case apperrors.IsKind(err, apperrors.KindTransient):
		return "the host did not answer; check the URL, and see 'Networks, Proxies and TLS' in the docs if you are behind a proxy or an internal CA"
	default:
		return "check BITBUCKET_URL and network reachability"
	}
}
