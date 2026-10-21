---
search:
  boost: 0.3
---

# ADR-104: What passes for the person's own is marked as the agent's, or left to them

Through MCP bb acts with the person's token, so whatever an agent does there carries the person's name. An action that passes for the person's own judgement or words, such as an approval, or prose the model composed, is either marked as the agent's, or left for the person to give the final okay where the agent cannot reach it. Asking the person is neither: clients implement no approval mechanism a server can enforce (ADR-103), so the guards left to bb are not offering a tool, and leaving the call to the client. An action that passes for the person's own and cannot be marked is not offered.

An approval or a request for changes carries no text to mark, so no MCP tool approves, requests changes or publishes a review. A comment added through MCP is pending: it joins the person's review, which only they see, and a review they had already begun is added to, since Bitbucket keeps one per person per pull request. `prepare_pr_review` reads that review and answers with the pull request's URL, how many comments it holds and the status the agent suggests; it changes nothing and asks nothing. The person publishes the review in Bitbucket with Finish review, and approves or requests changes there. A view's Approve and Request changes do the same, and a comment sent from a view says where it went, because nothing a view reads shows a pending comment (ADR-101). The CLI keeps `bb pr review approve`, `set` and `complete` for the person at the terminal.

An approval made with the person's token looks to every other reviewer, and to the merge checks that count approvals, exactly like their own. A review left for the person costs them one step, where the comments already are.

Before adding an MCP tool, decide whether what it does would pass for the person's own. If it would, mark it as the agent's or leave it for the person to publish in Bitbucket; if neither can be done, do not add it. Post every comment through MCP as pending, replies and inline comments included. Count a review by its comments' state, since Bitbucket returns a pending reply inside the published thread it answers.

## Not chosen

- **Approve once the person confirms in the client**: bb cannot reliably ask. Clients implement no standard approval a server can enforce: an elicitation is answered however the client decides, by the person, by a rule or by the client itself, and bb cannot tell which. With asking unenforceable, an approval tool would be guarded only by the client's decision, and an approval given that way could be given by no one, under the person's name.
- **Refuse when the person already has a review in progress**: The review is theirs to add to, and the count tells them what it holds.
