# Git authentication

`bb` talks to Bitbucket's REST API using the credentials you stored with
`bb auth login`. Plain `git` — `git push`, `git pull`, `git fetch` in a cloned
repository — does not go through `bb` at all, so it needs credentials of its own.

This page explains how to give git those credentials without writing a token
into a repository.

## Set it up

```bash
bb auth setup-git
```

That is the whole setup. It configures git to ask `bb` for a credential whenever
it contacts your Bitbucket host, using the credentials already in your keyring.

```bash
bb auth setup-git --host https://bitbucket.example.com   # a specific host
bb auth setup-git --global=false                         # this repository only
bb auth setup-git --force                                # replace an existing helper
```

By default the configuration is written to your global git config, so a single
setup covers every clone of that host. Pass `--global=false` from inside a
repository to scope it to that repository instead.

## How it works

Git supports **credential helpers**: when it needs a username and password it
runs a program, asks it, and uses the answer. Nothing is written to disk.

`bb auth setup-git` writes one line of git configuration:

```
credential.https://bitbucket.example.com.helper = !"/usr/local/bin/bb" auth git-credential
```

When git contacts that host it runs `bb auth git-credential`, which looks the
host up in the same place `bb auth login` stored it and hands the credential
back for that one request.

You never run `bb auth git-credential` yourself. Git runs it.

Two properties are worth knowing:

**The configuration is scoped to your Bitbucket host.** It is not a bare
`credential.helper`, which git consults for *every* remote. Git only ever asks
`bb` about the host the credentials belong to, and `bb` answers only for hosts
you have configured — so these credentials are never offered to GitHub, GitLab,
or anywhere else.

**Nothing is stored.** Rotating or revoking a token takes effect immediately,
because git asks again every time. There is no copy to go stale.

## Credentials are never written into a repository

`bb repo clone` supplies credentials to git for the duration of the clone only.
The resulting repository contains no credential:

```bash
bb repo clone PROJECT/repo
grep -i 'extraheader\|password' repo/.git/config    # no matches
```

If you have clones made by an older version of `bb`, they may contain an
`http.extraHeader` entry holding a live token. Check with:

```bash
git config --local --get http.extraHeader
```

If that prints anything, remove it and set up the helper instead:

```bash
git config --local --unset-all http.extraHeader
bb auth setup-git
```

That entry is worth removing even if authentication is working. It is a token
in plaintext in a file that gets copied and archived with the working tree, and
because it is not scoped to a host, git sends it to **any** HTTP remote in that
repository — so adding an unrelated remote would transmit your Bitbucket token
to that host.

## Tokens and SSH

A personal access token works as the password for git over HTTPS, which is what
the helper supplies. You do not need a separate credential for git.

If you clone over SSH instead, none of this applies: SSH authenticates with your
SSH key, and git never asks for a username or password. Manage those keys with
`bb ssh-key`.

## Troubleshooting

**Git still prompts for a password.** Check the helper is configured for the
exact host git is contacting, including scheme and port:

```bash
git config --get-all credential.https://bitbucket.example.com.helper
```

**Check what the helper returns.** It speaks git's protocol on stdin:

```bash
printf 'protocol=https\nhost=bitbucket.example.com\n\n' | bb auth git-credential get
```

Credentials mean it is working. **No output means `bb` has nothing stored for
that host** — run `bb auth login` for it. That silence is deliberate: it lets git
fall through to another helper or prompt you, rather than failing outright.

**Another credential manager answers first.** `bb auth setup-git` resets the
helper list for your Bitbucket host before adding `bb`, so an inherited helper
such as Git Credential Manager no longer answers for it. If you configured
things by hand, make sure `bb` is the only helper for that host.

## See also

- [ADR-044](../adr/044-git-credential-helper-instead-of-persisted-credentials.md) — why credentials are not persisted
- [ADR-021](../adr/021-persistent-auth-configuration-and-keyring-storage.md) — where `bb` stores credentials
