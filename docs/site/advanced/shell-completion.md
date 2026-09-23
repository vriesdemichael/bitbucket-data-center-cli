# Shell Completion

`bb` completes commands, flags and the values they take. Pressing tab after
`bb pr merge ` lists the open pull requests of the repository you are in, by
number and title; after `bb branch delete ` it lists that repository's
branches, each with its latest commit message and the default one first.

## Enabling it

Homebrew and the `.deb` and `.rpm` packages set completion up for bash, zsh and
fish when they install `bb`, and Scoop sets it up for PowerShell. Anywhere else,
one command does it:

```bash
bb completion install
```

It sets up the shell you run it from; `--shell bash`, `zsh`, `fish` or
`powershell` names another. What it writes loads the script from `bb` every
time, so an upgrade of `bb` needs nothing done here. Running it again changes
nothing, and `bb completion remove` takes it out.

| Shell | Where it goes |
|---|---|
| bash | `~/.local/share/bash-completion/completions/bb`; needs the `bash-completion` package |
| zsh | a marked block at the end of `~/.zshrc`, after `compinit` |
| fish | `~/.config/fish/completions/bb.fish` |
| PowerShell | a marked block in the profile of every PowerShell installed, Windows PowerShell 5.1 and PowerShell 7 alike |

Windows PowerShell 5.1 runs no profile at all under its default execution
policy, `Restricted`. `bb` says so rather than write one that would not run;
`Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`, in Windows PowerShell,
allows it.

### For every user

`--all-users` sets it up for everyone on the machine, and needs an administrator.
Each shell has its own place for that, and on Windows only PowerShell has one:

| Shell | Where it goes |
|---|---|
| bash | `/usr/local/share/bash-completion/completions/bb` |
| zsh | `/usr/local/share/zsh/site-functions/_bb`, when zsh reads that directory |
| fish | `completions/bb.fish` in fish's configuration directory, `/etc/fish` on Linux |
| PowerShell | the all-users profile of every PowerShell installed |

### By hand

For a startup file you keep yourself, this is the line to add. It generates the
script each time, like the setup above:

=== "Bash"

    Needs the `bash-completion` package. In `~/.bashrc`:

    ```bash
    source <(bb completion bash)
    ```

=== "Zsh"

    In `~/.zshrc`, after `compinit` has run:

    ```zsh
    source <(bb completion zsh)
    ```

=== "Fish"

    In `~/.config/fish/config.fish`:

    ```fish
    bb completion fish | source
    ```

=== "PowerShell"

    In your profile, the file `$PROFILE` names:

    ```powershell
    bb completion powershell | Out-String | Invoke-Expression
    ```

Command Prompt (`cmd.exe`) has no programmable completion; use PowerShell.

`bb completion <shell> --help` shows how to install the script as a file
instead, which saves running `bb` when a shell starts. A file does not follow an
upgrade, so generate it again after updating `bb`. To list values without their
descriptions, add `--no-descriptions` to `bb completion <shell>`.

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
| This machine | the instances you are logged in to, for `--host`, and the bulk runs saved here, for `bb bulk status` |

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
branch. In bash and zsh, a reason you can act on — a credential the server
refused, a repository that could not be resolved — appears as a hint under the
prompt.

**It completes for the repository the command would act on.** The same
resolution: `--repo` if you passed one, otherwise the git remote of the
checkout you are standing in, otherwise `BITBUCKET_PROJECT_KEY` and
`BITBUCKET_REPO_SLUG`.

**It prefers your checkout.** Branches, tags, commits and paths come from
local git when the repository being completed is the one you are standing in,
which costs no request at all. Everything else, and every repository that is
not this one, comes from the server.

## When nothing is offered

`bb doctor` shows where completion is set up for each shell, and reports a
setup the shell will not run and a saved script that has fallen behind `bb`.

To see why one press comes back empty, run the completion by hand with
`BB_COMPLETION_DEBUG` set. Give the words after
`bb` as you typed them, ending with the word being completed, or an empty one
for a new word:

=== "Bash, zsh, fish"

    ```bash
    BB_COMPLETION_DEBUG=1 bb __complete pr merge ""
    ```

=== "PowerShell"

    ```powershell
    $env:BB_COMPLETION_DEBUG = 1; bb __complete pr merge ''
    ```

Each value it would offer comes out on a line of its own. A line starting
`completion:` says why there is none: a credential the server refused, a
repository that could not be resolved, a server that could not be reached, or
one that did not answer in time (`context deadline exceeded`), which a larger
`BB_COMPLETION_TIMEOUT` may fix. The last line is an instruction for the shell.

## Settings

| Variable | Effect |
|---|---|
| `BB_COMPLETION_TIMEOUT` | How long a press may take, as a Go duration. The default is `1200ms`. |
| `BB_COMPLETION_DEBUG` | Any value prints why a completion came back empty. Shells discard it; see [When nothing is offered](#when-nothing-is-offered). |
| `BB_ACTIVE_HELP` | `0` turns off the hints bash and zsh show under the prompt. |

## Where the shells differ

A few behaviours come from the shell rather than from `bb`:

- **PowerShell:** with nothing typed yet, a completion with no matches falls back
  to listing the current directory. PowerShell accepts no value that would leave
  the line untouched, and an error at the prompt is worse. Descriptions appear in
  the completion menu rather than beside each value.
- **fish:** an argument that takes a directory offers files as well, because
  fish's completion script does not filter to directories.
