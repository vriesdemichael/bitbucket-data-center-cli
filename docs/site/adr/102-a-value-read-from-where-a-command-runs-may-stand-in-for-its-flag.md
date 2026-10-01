---
search:
  boost: 0.3
---

# ADR-102: A value read from where a command runs may stand in for its flag

A required value a command was not given may be inferred from where it is run. The inference is made where the value is asked for, in `prompt.Missing`, and never by a flag that asks for it. A person is shown the inferred value as the answer on offer and may change it. With nobody there it stands in for the flag only where it leaves no choice to make. `bb pr create` takes the repository's default branch as the target, the checked-out branch as the source when the repository itself came from the checkout, and the subject of the branch's commit as the title when it holds exactly one. What would be a choice is still refused by name, as ADR-073 asks: a title for a branch of several commits, a source branch for a repository named with `--repo`. What was inferred without asking is said on stderr, or under machine output by the document the command writes. No inference lets a destructive command act with nobody asked: `--yes` does not apply to a repository taken from the git remote (ADR-073).

Infer through `Missing.Infer`. Do not add a flag that turns inference on, and do not fill a value in before `FillMissing` is called. Mark an inference `Unattended` only when somebody else standing in the same place could not reasonably want another value. Give it a `Source`, so the notice can say where it came from. Never add an inference for the target of a destructive command.

A default is a value nobody chose, which is why ADR-073 refuses one. The checked-out branch and the repository's default branch are not that: they are read from where the person put themselves, as `--repo` already is from the git remote. Refusing them would make `bb pr create` fail with nobody to ask while it held every answer.

## Not chosen

- **A --fill flag, as gh has**: gh needs one because it prompts by default. Here the prompt path exists, and the flag would be a second way of asking for what the prompt would have offered.
- **Infer only for a person, and refuse as before when nobody is there**: Leaves a pipeline or an agent naming three flags whose values are in the checkout and at the server, which is the round trip the refusal was meant to make unnecessary.
- **Infer a title from the branch name, or the newest commit, when there are several**: That is a choice, and the pull request it names is created, not previewed.
