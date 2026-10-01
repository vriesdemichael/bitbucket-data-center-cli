---
search:
  boost: 0.3
---

# ADR-080: Bitbucket resolves CODEOWNERS, not bb

bb asks Bitbucket who owns a change: `GET /rest/ui/latest/projects/{key}/repos/{slug}/code-owners?sourceRefId=&targetRefId=[&sourceRepo=]`, the endpoint the "add code owners" button in the pull request UI calls. It returns users, already resolved out of whatever the file named. bb does not read `.bitbucket/CODEOWNERS`, match it against the diff, expand its groups or apply selection strategies. The syntax is Bitbucket's and so is its meaning, so bb and the web interface give the same answer.

Never parse CODEOWNERS. If a form seems unsupported, probe the endpoint against the live stack and pin what it answers; the file's meaning is not ours to decide. Pass sourceRepo whenever the source ref lives in another repository, or a fork pull request resolves against the wrong one. Filter the author from the result: Bitbucket rejects a pull request whose author is a reviewer, and the endpoint does not know who is opening it.

Two implementations of one file format drift apart, and the format is Bitbucket's to change. Reviewers that differ between the CLI and the browser are worse than reviewers that are missing: nobody checks a list they believe is already right.

## Not chosen

- **Use the endpoint, then add bb's extra forms on top**: Both implementations then run on every pull request, and the answer is their union -- so the CLI still assigns reviewers the button does not.
- **Avoid /rest/ui because it is not the documented public API**: It is the only server-side evaluation there is, it carries swagger annotations and a declared scope, and it is what the product's own UI depends on. The live suite pins its shape, so a change is a test failure here rather than a surprise in someone's pull request.
