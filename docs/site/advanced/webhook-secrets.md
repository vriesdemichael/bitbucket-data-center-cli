# Webhook Secrets

A Bitbucket webhook can carry two credentials, and `bb` treats both as things it
holds rather than things it prints.

- The **shared secret** signs every delivery. Bitbucket sends it as
  `X-Hub-Signature: sha256=…` so the receiving endpoint can prove the request
  came from Bitbucket and not from someone who learned the URL.
- The **endpoint credentials** are a username and password `bb` never sees the
  second half of. Bitbucket sends them as an `Authorization: Basic …` header on
  every delivery, so the endpoint can authenticate the caller.

## What `bb` publishes, and what it does not

Bitbucket returns the shared secret **in plaintext on every read** of a webhook.
`bb` does not pass it on. What the model publishes instead is whether one is
configured:

```console
$ bb webhook get 42 --json
{
  "data": {
    "webhook": {
      "id": 42,
      "name": "ci",
      "url": "https://ci.example.com/hooks/bitbucket",
      "active": true,
      "events": ["repo:refs_changed"],
      "sslVerificationRequired": true,
      "scopeType": "repository",
      "secretConfigured": true,
      "credentialsUsername": "bitbucket"
    }
  }
}
```

Three details are worth knowing:

- `secretConfigured` is always present. `secret` is not — it appears only with
  `--reveal-secret`.
- `sslVerificationRequired` is **absent** when the server did not report it,
  rather than `false`. Absent and "TLS verification is off" are different
  answers, and this is the field an audit reads.
- There is no field for the endpoint password. Bitbucket never returns it, so
  `bb` has nothing to publish. `credentialsUsername` is the only half that comes
  back.

`bb webhook test` publishes the delivery record Bitbucket produced, and that
record contains the request headers — including `Authorization`. `bb` replaces
the value with `<redacted>`. Base64 is not encryption: the header carries the
endpoint password, and under `--json` that record is the machine contract.

### Recovering a secret on purpose

`--reveal-secret` prints what would otherwise be redacted. It exists so that
losing a secret does not mean going to the database, and it has to be typed:

```bash
bb webhook get 42 --json --reveal-secret          # the shared secret
bb webhook test 42 --json --reveal-secret         # the Authorization header
```

Both write a warning to **stderr** saying a credential went through stdout, so a
log that contains one also contains the note that it does. The warning never
repeats the value.

## Setting a secret

No flag takes a secret as its value ([ADR-047](../adr/047-credential-input-and-keyring-enforcement.md)):
a flag value lands in the process argument list, which is world-readable on
Linux, and in shell history. Two routes are open instead.

### From the environment — the automation path

| Variable | Sets |
|---|---|
| `BB_WEBHOOK_SECRET` | the shared secret |
| `BB_WEBHOOK_PASSWORD` | the endpoint password |

```bash
export BB_WEBHOOK_SECRET="$(vault read -field=value secret/bitbucket/webhook)"
bb webhook create ci https://ci.example.com/hooks/bitbucket \
  --event repo:refs_changed --ssl-verification=true
```

This is the form to reach for in a pipeline: the secret comes from wherever the
runner keeps secrets and never appears in a command line.

An **empty** variable counts as unset, so a typo in a variable name two files
away cannot quietly configure an empty credential.

### From stdin — one secret at a time

```bash
printf '%s' "$SECRET" | bb webhook create ci https://ci.example.com/hooks/bitbucket --secret-stdin
printf '%s' "$PASSWORD" | bb webhook update 42 --credentials-username bitbucket --credentials-password-stdin
```

`--secret-stdin` and `--credentials-password-stdin` cannot both be given: there
is one stdin, and `bb` refuses rather than guessing which secret arrived. Put
the other one in its environment variable.

A secret piped on stdin may not contain spaces, tabs or newlines. A value that
arrived with a stray space is far more likely to be a piping mistake than a real
credential, and the failure it causes shows up much later, at the receiving
endpoint. If your secret genuinely contains whitespace, set it through the
environment instead.

### Precedence

stdin wins over the environment. Piping is the explicit act; a variable can come
from a shell profile the caller has forgotten.

A flag typed on this invocation outranks the environment entirely, which is what
makes `--no-secret` usable on a host that exports `BB_WEBHOOK_SECRET` for
everything:

```bash
bb webhook update 42 --no-secret          # removes it, variable or no variable
bb webhook update 42 --no-credentials     # removes the endpoint credentials
```

Combining `--no-secret` with `--secret-stdin` is refused. One of the two has to
win and neither should.

## What an update leaves alone

Bitbucket's update endpoint **replaces** the webhook rather than patching it: a
field that does not arrive is cleared. `bb` reads the webhook first and sends
back what you did not mention, so `--name` changes the name and nothing else.

This matters most for the two credentials:

- The **shared secret** survives because Bitbucket returns it and `bb` sends it
  straight back. Sending an update without a configuration object clears it.
- The **endpoint password** survives for a different reason. `bb` cannot send it
  back — Bitbucket never returned it — so it sends the credentials object with
  the username alone, and Bitbucket keeps the password it already had. This is
  verified against a live instance by observing the `Authorization` header on a
  real delivery before and after an update, because no API response can show it.

## Dry runs name the variable, not the value

A dry run is written to stdout, is what an operator reads before applying, and is
what gets pasted into a ticket when it looks wrong. It says where the secret will
come from and never what it is:

```console
$ export BB_WEBHOOK_SECRET=…
$ bb webhook create ci https://ci.example.com/hook --dry-run --json
{
  "data": {
    "items": [
      {
        "intent": "repo.webhook.create",
        "target": {
          "repository": "PROJ/repo",
          "name": "ci",
          "url": "https://ci.example.com/hook",
          "secret": "will be set from $BB_WEBHOOK_SECRET"
        },
        "predictedAction": "create"
      }
    ]
  }
}
```

Naming the variable is also the more useful answer: the mistake a plan makes is
reading the wrong one. Removing a credential shows as `"will be removed"`.

## Bulk plans hold a variable name

A bulk plan is a file. It gets written to disk, committed, attached to a change
request and read by whoever reviews it — so a literal secret in one is a secret
in version control, for the same reason ADR-047 keeps secrets off the command
line.

The policy therefore names the **variable**, and `bb bulk apply` reads it at
apply time:

```yaml
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PROJ
operations:
  - type: repo.webhook.create
    name: ci
    url: https://ci.example.com/hooks/bitbucket
    events: [repo:refs_changed]
    sslVerificationRequired: true
    secretEnv: BB_WEBHOOK_SECRET
    credentialsUsername: bitbucket
    credentialsPasswordEnv: BB_WEBHOOK_PASSWORD
```

- `secretEnv` and `credentialsPasswordEnv` must look like environment variable
  names (`^[A-Za-z_][A-Za-z0-9_]*$`). A pasted credential almost never does, so
  the mistake the field invites — reading `secretEnv` as "the secret" — is
  refused at plan time, by the published JSON schema as you type and by `bb`
  when it validates the policy.
- The plan file records the name only. Neither `bb bulk plan` nor the plan on
  disk carries the value.
- `bb bulk apply` **refuses** when a named variable is unset, rather than
  creating a webhook without a secret. A webhook whose deliveries carry no
  signature is not a smaller version of one whose deliveries do, and finding
  that out from a receiver that has started rejecting everything is a bad way to
  find out. The recorded failure names the variable that was missing:

```console
$ bb bulk apply --from-plan plan.json
Error: bulk apply op-4f2c… completed with failures
$ bb bulk status op-4f2c… --json | grep BB_WEBHOOK_SECRET
"secretEnv names $BB_WEBHOOK_SECRET, which is not set in this environment; export it before applying the plan"
```

- The same plan applies against different environments without being edited,
  which is the other thing a name buys over a value.
- The apply status is printed and written to the status store on disk. It
  reports `secretConfigured` and `credentialsUsername` for the webhook it
  created, never the credentials themselves.

### Limitation: one secret per operation

A `secretEnv` belongs to an **operation**, and the selector applies every
operation to every repository it matches. So a plan can create several webhooks
each reading its own variable:

```yaml
operations:
  - type: repo.webhook.create
    name: ci
    url: https://ci.example.com/hooks/bitbucket
    secretEnv: BB_WEBHOOK_SECRET_CI
  - type: repo.webhook.create
    name: audit
    url: https://audit.example.com/hooks/bitbucket
    secretEnv: BB_WEBHOOK_SECRET_AUDIT
```

— but every repository the selector matches gets the **same** secret for a given
operation. There is no way to say "this variable for `service-a`, that one for
`service-b`" inside one plan.

If each repository needs its own secret, that is one plan per repository (or per
group of repositories sharing a secret), each applied with its own variable
exported:

```bash
for repo in service-a service-b; do
  BB_WEBHOOK_SECRET="$(vault read -field=value "secret/bitbucket/$repo")" \
    bb bulk apply --from-plan "plans/$repo.json"
done
```

This is a real limitation of the plan model rather than a temporary gap: the
selector exists precisely so that one operation describes many repositories, and
per-repository values would make a plan stop being the reviewable artifact it is
for. A plan whose effect differs per target cannot be read once and understood.

## Where the fields can be set

The same flags are registered wherever a webhook is configured, so the answer to
"can I set the shared secret here" does not depend on which command you reached
for:

| Command | create | update |
|---|---|---|
| `bb webhook` | yes | yes |
| `bb project webhook` | yes | yes |
| `bb repo settings workflow webhooks` | yes | no update subcommand |
| `bb bulk` (`repo.webhook.create`) | yes, by variable name | n/a |
