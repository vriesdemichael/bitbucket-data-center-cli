---
search:
  boost: 0.3
---

# ADR-051: bb does not manage server-side hooks

bb has no commands that configure code running inside Bitbucket: no hook scripts, no plugin hook enablement, no hook settings, whatever a future API calls them. Webhooks are the supported way to make Bitbucket tell an external service that something happened, through `bb webhook` and `bb project webhook`. The hook endpoints stay reachable through `bb api` (ADR-053) for a one-off case. The API is not forbidden; it just gets no first-class commands that imply it is a good idea.

When a user asks for push-time enforcement, answer with the alternatives rather than the endpoint: a webhook to a service they control, a required build status, or a merge check. Each fails where the person who caused the failure can see it. Point someone who genuinely needs the hook endpoints, for a migration or an audit, at `bb api` rather than reopening the command surface. This is arguable and should stay arguable. A workflow that cannot be expressed as a webhook, a merge check or a CI job is a reason to revisit; a preference for driving it from a terminal is not.

A hook script is a shell script that runs in the Bitbucket server's process on every push to every repository it is bound to, deployed by upload rather than from version control. What runs is whatever was last pushed through an API call: no branch, no review, no history, and no straightforward answer to what changed and who changed it. When it breaks it breaks pushes for everyone, on the server, away from anyone who can see it. Pushing enforcement into the server makes it invisible to the people it applies to; the alternatives fail somewhere the author can act. Plugin hooks are better behaved, being versioned and installed as apps, and are excluded for a different reason: enabling one and setting its configuration is an administrative act performed once per repository or project, not a scripted workflow, and not something a coding agent should reach for while working on a change.

## Not chosen

- **Hook script commands, with the feature switched on in the test stack**: The feature is off on the Atlassian SDK stack the live suite uses. Turning it on for every test that runs there pays to keep a surface the project does not want, and answers the testing question while leaving the design question unasked.
- **Plugin hook commands without hook scripts**: Defensible, since plugin hooks are versioned and installed as apps. But enablement is still a one-off administrative act, and a surface that keeps the safer half of a pair invites the question of why the other half is missing every time someone reads the help.
