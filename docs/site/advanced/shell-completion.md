# Shell Completion

`bb` completes commands, flags and the values they take. Pressing tab after
`bb pr merge ` lists the open pull requests of the repository you are in, by
number and title; after `bb branch delete ` it lists that repository's
branches, marking the default one.

## Enabling it

Generate the script for your shell and load it from your profile.

```bash
bb completion bash > /etc/bash_completion.d/bb
```

```bash
bb completion zsh > "${fpath[1]}/_bb"
```

```bash
bb completion fish > ~/.config/fish/completions/bb.fish
```

For PowerShell, add the output to your profile:

```powershell
bb completion powershell | Out-String | Invoke-Expression
```

`bb completion <shell> --help` prints the paths for each platform.

## What is completed

| Kind | Where |
|---|---|
| Pull requests | every `<pr-id>`, filtered by what the command accepts — `bb pr reopen` offers declined ones |
| Branches, tags, refs, commits | `<branch>`, `<tag>`, `<commit>`, `--from-ref`, `--to-ref`, `--at`, `--start-point` |
| Repositories and projects | `--repo`, `--project`, `<project-key>`, `bb repo clone` |
| Files | paths in the repository, one directory at a time; the `--path` flag of an inline pull request comment offers only the files that pull request changes |
| People | users, groups and reviewer groups, including `@group` inside `--reviewers` |
| Ids | comments, webhooks, branch restrictions, default tasks, reviewer conditions, required builds, keys and tokens — each with what it names beside it |
| Fixed values | every flag with a defined set of values, the log levels, review statuses and permissions |

Values a server cannot know are not completed: a title, a URL, or the name of
something being created.

## How it behaves

**It answers in about a second or not at all.** One deadline covers reading
your credentials, asking git and calling Bitbucket. Whatever has not finished
by then is abandoned, so an instance you cannot reach costs a moment rather
than a hung terminal.

**It says nothing when it cannot answer.** A shell prompt is no place for an
error, so a failed completion offers nothing and prints nothing. It also does
not fall back to listing your working directory for an argument that wanted a
branch. In bash and zsh, a reason you can act on — no credentials for this
instance, a repository that could not be resolved — appears as a hint under
the prompt.

**It completes for the repository the command would act on.** The same
resolution: `--repo` if you passed one, otherwise the git remote of the
checkout you are standing in, otherwise `BITBUCKET_PROJECT_KEY` and
`BITBUCKET_REPO_SLUG`.

**It prefers your checkout.** Branches, tags, commits and paths come from
local git when the repository being completed is the one you are standing in,
which costs no request at all. Everything else, and every repository that is
not this one, comes from the server.

## Settings

| Variable | Effect |
|---|---|
| `BB_COMPLETION_TIMEOUT` | How long a press may take, as a Go duration. The default is `1200ms`. |
| `BB_COMPLETION_DEBUG` | Prints on stderr why a completion came back empty. Shell scripts discard that stream, so set it and run `bb __complete <words>` by hand. |
| `BB_ACTIVE_HELP` | `0` turns off the hints bash and zsh show under the prompt. |

## PowerShell

Two behaviours differ, both in the script PowerShell uses rather than in `bb`:

- With nothing typed yet, a completion with no matches falls back to listing
  the current directory. PowerShell accepts no value that would leave the line
  untouched, and an error at the prompt is worse.
- Descriptions appear in the completion menu rather than beside each value.
