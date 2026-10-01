---
search:
  boost: 0.3
---

# ADR-088: Every Bitbucket release Atlassian supports is served, and none is dropped

bb serves every Bitbucket Data Center release Atlassian supports, and never drops one. A release past Atlassian's end of support stops being tested, not served. The window is docs/quality/bitbucket-releases.json: the oldest release served, and the releases the live suite is run against, ending with the one the harness runs. docs/site/reference/bitbucket-versions.md states the oldest to readers and catalogues each difference. A fix ships in bb's next release; no earlier bb release is patched.

bb is generated against the newest release (ADR-042). An older release behaves the same except where the versions page catalogues a difference, and each difference is handled in the call it affects and nowhere else. The call asks the instance's release through internal/compat, once per instance per process, and only when the request or the answer is one the release changes. Where bb can make the older release answer as the newest does, it does. Where it cannot, it refuses before sending, with kind unsupported (exit 14), naming the release that has the capability, and a dry run gives that refusal as its verdict.

A difference is found by running the live suite against the release: `RELEASE=<tag> task test:live`, or `task test:live:matrix` for the oldest and newest. Every release in the window passes it at least once, locally; CI runs the newest. A live test whose behaviour differs by release asserts each side, with the boundary stated in the test. `task quality:bitbucket-releases:verify` fails when internal/compat and the versions page disagree about a difference, and when the window does not end with the release the harness runs.

When a live test fails on an older release and passes on the newest, compare what each release stores before changing anything. A real difference gets a compat.Difference with the release it arrived in, a row in the versions page, and either an adaptation or a refusal in the call that differs. Never branch on the release outside the call that differs, never skip a test on a release, and never send a request an older release answers 200 and ignores. Do not generate a client per release, and do not remove a release from the versions page when Atlassian ends its support.

Bitbucket answers 2xx to a JSON property it does not know and ignores it, so an older release does not refuse what it cannot do: it does something else. A required build created to spare pull requests blocks them on a release that predates the setting. Only the release says which requests those are, and administrators upgrade on their own schedule, so the window a tool supports has to include what Atlassian still supports.

## Not chosen

- **A generated client per release**: Multiplies the generated code by the number of releases for differences that fit in a table, and still needs a release check to choose the client.
- **Infer the release from what an answer leaves out**: A request cannot be checked that way, and the differences that do harm are in requests. The release is read, and only when a call is one that differs.
- **Drop a release when Atlassian ends its support**: Nothing about the release changes on that date, and the administrators still running it lose bb for no reason. Testing it stops; serving it does not.
