---
search:
  boost: 0.3
---

# ADR-067: Governance tests are verified by breaking them

A governance test asserts an invariant about the codebase rather than a behaviour: every command is classified, every MCP tool has a scope rule, no record names something that was removed. They are what keeps documentation and registries in step with the code they describe. Three rules govern them. Break a guard before trusting it, and write the sabotage as a test where it can be expressed as one. Prefer making a contradiction unrepresentable over testing for it: when two declarations answer the same question, derive one from the other; when they answer different questions, assert both directions. And keep the set listed here, because it follows no naming convention and cannot be found by grep. The set:

- TestAllRunnableCommandsDeclareArgsPolicy: a command with a positional placeholder declares an Args policy.
- TestAllCommandsExhaustivelyClassifiedForDryRun: every command is in exactly one dry-run category.
- TestCommandVerbsAgreeWithTheirDryRunClassification: a command's name and its category do not contradict.
- TestVerbClassificationExemptionsNameRealCommands: an exemption names a real command and gives a reason.
- TestClassifyUsageErrorMatchesCobrasRealMessages: the usage-error markers match what Cobra emits.
- TestAMissingArgumentIsNamed: a missing positional argument is named in the error.
- TestEveryCommandIsModelled: every runnable command publishes the schema of its result, or says why it has none.
- TestAnEchoedFlagPublishesTheValuesItAccepts: a payload field that echoes an enum flag publishes the flag's values.
- TestEveryCompletionSlotIsDeclared: every argument and every flag that takes a value declares what it accepts.
- TestEveryDeclaredArgumentCanBeCompleted: a command that declares an argument has something completing it.
- TestEveryCommandHasAnExample: every command shows an example.
- TestEveryExampleIsAnInvocationBBAccepts: bb accepts every example a command shows.
- TestEveryDeprecatedFlagIsStillTakenAndSaysSo: a deprecated flag is still taken, hidden from help, and warns on stderr.
- TestACappedListingSaysSoIsEnforced: a command that takes --limit reports reaching it, in JSON and in text.
- TestNoServiceOptionIsCalledLimit: no service option is called Limit or PageSize; a cap is MaxResults, and paging belongs to openapi.PageThrough.
- TestNoFailureIsWrappedAsTransient: a transport failure keeps its classification through the service that wraps it.
- TestTheStatusMappingIsOnlyTestedWhereItLives: no package outside internal/openapi tests the status-to-kind mapping.
- TestOnlyTheSharedHelperDecidesInteractivity: only the shared helper asks whether a stream is a terminal.
- TestEveryUseOfStandardInputIsAccountedFor: every place standard input is used is recorded.
- TestPolicyLoadingNeverCreatesTheSystemConfigDirectory: reading policy does not create its directory.
- TestConfigurationPageSaysWhichFileReadsWhichKey: the configuration page names the keys each file's loader reads.
- TestSystemPolicyPageListsEveryPolicyKey: the system policy page lists every policy key.
- TestEnvironmentPageNamesEveryVariableBBReads: the environment page names every variable bb reads, and only those.
- TestMachineModePageStatesEachKindsExitCode: the machine-mode page gives every error kind its real exit code.
- TestTroubleshootingPageStatesEachKindsExitCode: the troubleshooting page gives every error kind its real exit code.
- TestEveryMCPToolIsAccountedFor: every MCP tool maps to a command or is recorded as MCP-only.
- TestEveryMappedCLICommandExists: no mapping names a command that was removed.
- TestEveryToolHasAScopeRule: no MCP tool escapes workspace scoping.
- TestEveryResourceTemplateNamesItsProjectAndRepository: no MCP resource escapes workspace scoping.
- TestEveryPromptTakesItsProjectAndRepository: no MCP prompt escapes workspace scoping.
- TestToolsThatAskAreTheOnesThatDecideAMerge: the tools that ask the person are exactly the ones that decide a merge, and create_tag.
- TestReadOnlyToolsDoNotAsk: a tool annotated read-only asks nothing.
- TestEveryToolDeclaresItsHintsAndTitle: every MCP tool states all four hints and a title.
- TestEveryLimitedToolSaysWhenItStopped: an MCP tool that takes a limit returns a required limit_reached.
- TestLiveMCPEveryToolReturnsAClientCompatibleResult: every MCP tool is called, and its result is a JSON object with a text fallback.
- TestADRDoesNotNameToolsThatDoNotExist: a record does not name a removed tool.
- TestADRDoesNotNameFlagsThatDoNotExist: a record does not name a removed flag.
- TestEveryADRMentionHasARecord: nothing in the repository names a record that does not exist.
- TestRecordsInForceDoNotNameABitbucketVersion: a record in force does not restate a Bitbucket release.
- TestGovernanceTestsNamedInThisRecordExist: this list names only tests that exist.
- TestEveryHookRunnableGateRunsOnBothSides: every gate a git hook can run runs locally and in CI.
- TestNoGateIsDefinedAndNeverRun: a task named like a check is reachable from something that runs it.
- TestAmbientGitConfigGuardIsInstalledWhereTestsShellOutToGit: a package running git installs the guard.
- TestTheSealIsInstalledWhereTestsLoadTheConfiguration: a package whose tests load the configuration seals its process.
- TestNoFixtureIsNamedFromTheClock: no test builds a fixture name from time.Now().
- TestBitbucketImageIsProposedButNotAutoMerged: the product image is proposed but held from auto-merge.
- TestDependabotProposesUpdatesAgainstNext: every Dependabot update targets next, and auto-merge leaves one opened elsewhere to a person.
- TestEveryActionIsPinnedToACommit: every workflow action is pinned to a release's commit, with the release named beside it.
- TestEveryReleasedPlatformHasItsSBOMsAttested: every platform the release builds has its archives attested with their SBOMs.

Break a governance test before adding it, and before trusting one you did not write. Record what the invariant is, what breaks it, and that you saw it fail. Add the test to the list above in the same change; a guard nobody can find is one nobody maintains. A guard that scans the tree also fails when it finds too little to scan, so a detector that has stopped matching cannot report perfect compliance. Do not write a test that compares a value to something derived from it. Check first whether the assertion is already true by construction; if it is, the code is where the guarantee lives. Do not describe a check as CI-safe: every check can run anywhere, and what keeps a gate out of the git hooks is the cost of booting Bitbucket, not the ability to.

A guard that has stopped guarding still runs, still passes and still occupies the slot. A tautological one reads correctly, because its only fault is that both sides of the comparison come from the same place, and that is invisible until the sabotage is run. Deriving beats checking wherever it is available, because a value derived from another cannot disagree with it.

## Not chosen

- **Centralise the governance tests in one package**: Each sits beside the thing it guards, where someone changing that thing will trip over it. Enumerating the set is what this record does instead.
- **Keep the list in AGENTS.md only**: Nothing verifies AGENTS.md. A list is only worth having if something checks it.
- **Trust code review to catch a tautological guard**: A tautological guard reads exactly like a real one; only running the sabotage tells them apart.
