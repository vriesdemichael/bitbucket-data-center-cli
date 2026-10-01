# Configuration

Where `bb` takes each setting from, which files it reads, and what each file can
hold. `bb doctor` shows the result on one machine: every effective setting, and
the flag, variable or file it came from.

```bash
bb doctor
```

## The order

For each setting, the first of these that has a value decides:

1. A flag on the command line.
2. The environment, including `.env` files, which `bb` reads without overriding
   a variable that is already set. See [Environment Variables](environment.md).
3. The workspace file, `.bb/config.yaml`, in the repository you are in.
4. Your own file, the `config.yaml` that `bb auth login` writes.
5. The system file an administrator writes.
6. The built-in default.

[System policy](system-policy.md) stands outside this order. A setting an
administrator mandates applies whatever a flag, a variable or your own file
says, and a value that contradicts it is refused rather than used.

Some settings take fewer sources, or add one:

| Setting | Sources, first one wins |
|---|---|
| Host | `--host` on a command that takes it, the host of the git checkout's remote when the repository comes from it, `BITBUCKET_URL`, then `default_host` in the workspace file, your own file and the system file. A host `allowed_hosts` does not list is refused. |
| Repository | `--repo`, then the repository of the git checkout you are in, then `BITBUCKET_PROJECT_KEY` and `BITBUCKET_REPO_SLUG`. The workspace file's `project_key` stands in for the project. |
| Credential | A credential in the environment: `BITBUCKET_TOKEN`, or `BITBUCKET_USERNAME` and `BITBUCKET_PASSWORD`. Otherwise the one stored for the host the command talks to, looked up in your own file, then the system file. A host profile in the workspace file can name a username, but never a stored credential or a client certificate. No flag takes a credential. |
| CA bundle | `--ca-file`, `BB_CA_FILE`, then the system file's `ca_file`, which is a mandate: a different bundle is refused. |
| Release mirror | `--base-url` on `bb update`, `BB_UPDATE_BASE_URL`, then `update_base_url` in the workspace file, your own file and the system file. `bb update` reads no `.env`. |

A stored credential goes only to the host it was stored for, whichever file holds
it and whatever named the host. The token or password itself is in the operating
system's keyring, or in the file's `insecure_secrets` where there is no keyring and
the login passed `--allow-insecure-storage`:
see [where credentials are stored](../installation-and-quickstart.md#where-credentials-are-stored).

## The files

| File | Where | Written by |
|---|---|---|
| Your own | `%AppData%\bb\config.yaml` on Windows, `~/Library/Application Support/bb/config.yaml` on macOS, `~/.config/bb/config.yaml` on Linux. `BB_CONFIG_PATH` names another. | `bb auth login`, `bb auth logout`, `bb auth server use` and `bb auth alias` |
| Workspace | `.bb/config.yaml` in the working directory or the nearest parent that has one, up to the root of the repository. `BB_WORKSPACE_CONFIG_PATH` names another. | You, by hand, usually committed with a repository so everyone working in it gets the same host and project |
| System | `/etc/bb/config.yaml` on Linux and macOS, `%ProgramData%\bb\config.yaml` on Windows, and on Windows the registry under `HKEY_LOCAL_MACHINE\Software\Policies\bb` | An administrator |

`BB_DISABLE_STORED_CONFIG=1` leaves your own file unread, credentials included,
which keeps a file left on a shared CI runner out of a build.

A workspace file for a repository whose team works on one Bitbucket project:

```yaml
default_host: https://bitbucket.example.com
project_key: PAY
```

!!! warning "A workspace file arrives with a clone"

    Like a `.env`, it is a file in a repository, so anyone who can change the
    repository can change it, and the next `bb` command run in the checkout reads
    it. It can point `bb` at another host, but a stored credential is never sent
    to a host it was not stored for.

## What each file holds

The keys every file accepts are in the [configuration schema](schemas.md#configuration-file-schema),
but not every file reads every key. A key in a file that does not read it is
ignored, and `bb doctor` names it.

| Key | Your own | Workspace | System | What it sets |
|---|---|---|---|---|
| `default_host` | read | read | read | The host a command talks to when nothing earlier in the order names one. A URL, or a key of `hosts`. |
| `hosts` | read | read | read | Servers by their URL: `url`, `aliases` (other host names the server answers to, such as its SSH host), `username`, `auth_mode` (`token` or `basic`), `client_cert` and `client_key`. A workspace file's profile gives a host its `url` and `username` only: it never releases a stored credential, and its `client_cert` and `client_key` are ignored. |
| `project_key` | | read | | The project a command uses when no repository names one. |
| `insecure_secrets` | read | | read | Tokens and passwords by host, where no keyring could hold them and the login passed `--allow-insecure-storage`. |
| `update_base_url` | read | read | read | The release mirror `bb update` fetches from. |
| The [policy keys](system-policy.md#keys) | | | read | Mandates, at the top level of the file or under `policies:` or `policy:`. |

`$schema` is read by every file and ignored by `bb`: it is there for an editor.
