# Bitbucket Versions

`bb` works with Bitbucket Data Center 9.2 and every release after it. A release
stays supported after Atlassian ends its support; it is only no longer tested.

The newest release, [[ bitbucket_version ]], is tested on every pull request.
Every other release in the window — [[ bitbucket_releases_tested ]] — is
checked by running that same suite against a real instance of it.

## Where an older release differs

`bb` is built against the newest release, and an older one behaves the same
except in the places below. In each, `bb` either answers the way the newest
release would, or refuses before sending anything, with the error kind
`unsupported` and exit code `14`, naming the release that can do it. A dry run
gives the same answer as its verdict: the run would fail, as `unsupported`.

| Capability | From | On an earlier release |
|---|---|---|
| A required build that spares pull requests or applies to the merge queue: `requiredForPullRequest: false` or `requiredForMergeQueue: true` in `bb build required create` or `update` | 10.2 | Refused. Bitbucket would store the check and ignore both fields, so it would block every pull request. A check reads back as applying to pull requests and not to a merge queue, which that release does not have. |
| A default reviewer condition naming reviewer groups: `reviewerGroups` in `bb reviewer condition create` or `update` | 9.5 | Refused. Bitbucket would store the condition without the groups. |
| A `no-creates` branch restriction: `--type no-creates` on `bb branch restriction` and `bb project branch-restriction` | 9.4 | `create` and `update` are refused. `list --type no-creates` answers with none, which is what that release holds. |
| A build status naming its repository: `repository` in what `bb --json build get <commit> --key <key> --repo <project>/<slug>` prints | 9.4 | `bb` names the repository the status was read through, as a later release does. |

## Checking a release yourself

The live suite runs against any release the stack can start:

```bash
task test:live RELEASE=9.4.24
```

It starts an instance of that release beside the one already running, and a
test whose behaviour differs by release checks the side that applies.

`task test:live:matrix` does the same for the oldest release and the newest,
which is where a difference shows, and `task test:live:matrix RELEASES=all`
for every release in the window. Either way it writes which of them passed to
`.tmp/`.
