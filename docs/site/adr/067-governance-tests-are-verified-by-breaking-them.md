---
search:
  boost: 0.3
---

# ADR-067: Governance tests are verified by breaking them

A governance test asserts an invariant over everything of one kind the repository holds, rather than a behaviour of one function or command, so that one added later is held to it without anyone remembering to: every command is classified, every MCP tool has a scope rule, no record names something that was removed. They are what keeps documentation and registries in step with the code they describe. Three rules govern them. Break a guard before trusting it, and write the sabotage as a test where it can be expressed as one. Prefer making a contradiction unrepresentable over testing for it: when two declarations answer the same question, derive one from the other; when they answer different questions, assert both directions. And keep the set listed here, because it follows no naming convention and cannot be found by grep. The set:

- TestAllRunnableCommandsDeclareArgsPolicy: a command with a positional placeholder declares an Args policy.
- TestAllCommandsExhaustivelyClassifiedForDryRun: every command is in exactly one dry-run category.
- TestCommandVerbsAgreeWithTheirDryRunClassification: a command's name and its category do not contradict.
- TestVerbClassificationExemptionsNameRealCommands: an exemption names a real command and gives a reason.
- TestEveryStatefulProfileDeclaresItsTier: every stateful dry-run profile declares one of the three tiers, which DeclaredDryRunTier reports.
- TestEveryLocalChangeIsCreateUpdateOrDelete: every command classified as changing this machine has the action create, update or delete, and says what it would change.
- TestEveryCommandSaysWhatDryRunDoesForIt: every visible runnable command's help has a Dry run section with its own line.
- TestEveryLeafCommandUnderDryRunAnswersInPreview: every leaf command under --dry-run --json writes one document, in which any verdict is a preview that exits 0.
- TestClassifyUsageErrorMatchesCobrasRealMessages: the usage-error markers match what Cobra emits.
- TestAMissingArgumentIsNamed: a missing positional argument is named in the error.
- TestUnknownSubcommandFailsInEveryGroup: every command group refuses an unknown subcommand with a validation error naming it, exiting 2.
- TestEveryShallowAliasNamesACommandThatExists: every command whose help calls it an alias names its canonical command, and that command exists.
- TestEveryLeafCommandUnderJSONWritesExactlyOneEnvelope: every leaf command under --json writes exactly one envelope, carrying meta.bbVersion and one of data or error.
- TestEveryLeafCommandUnderYAMLWritesTheJSONDocument: every leaf command writes under --yaml the document it writes under --json.
- TestEveryExemptionIsAReachableLeaf: every command exempt from the JSON contract exists and is runnable.
- TestEveryJSONExemptionIsARealCommandWithAReason: every command exempt from the JSON contract exists and gives a reason.
- TestTheExemptionListStaysSmallEnoughToReview: the JSON exemptions keep ai skill show and api, cover every command Cobra supplies, and hold at most five of bb's own.
- TestEveryCommandIsModelled: every runnable command publishes the schema of its result, or says why it has none.
- TestExemptionsNameRealCommandsAndDoNotOverlapDeclarations: every command exempt from declaring a result is runnable, gives a reason, declares none, and is in one exemption list only.
- TestEveryDeclaredResultNamesARealCommand: every declared result names a runnable command.
- TestEveryDeclarationResolves: every declared result derives a schema.
- TestEveryPublishedFieldIsCamelCase: every property a declared result publishes is camelCase.
- TestEveryRunnableCommandAnswersDescribe: every runnable command takes --describe.
- TestEveryDescriptionIsAValidSchema: every output schema --describe gives for a runnable command compiles.
- TestAnEchoedFlagPublishesTheValuesItAccepts: a payload field that echoes an enum flag publishes the flag's values.
- TestEveryAdvertisedEnumIsEnforced: every flag that lists its values accepts each one and refuses any other, naming them.
- TestNoFlagEnumeratesValuesWithoutEnforcingThem: no flag spells out a value set in its help without enforcing it, outside a recorded exemption.
- TestEveryCompletionSlotIsDeclared: every argument and every flag that takes a value declares what it accepts.
- TestEveryDeclaredArgumentCanBeCompleted: a command that declares an argument has something completing it.
- TestEveryPermissionArgumentCompletesTheSetItsCommandEnforces: every permission argument completes the permission set its command enforces.
- TestEveryWebhookEventFlagDefaultsToAnEventCompletionOffers: every --event flag defaults to events that completion offers.
- TestEveryVocabularyValueCarriesADescription: every value in a completion vocabulary has a description.
- TestEverySourceCompletesNothingWhenTheContextCannotBeResolved: every completion source answers within ten seconds when the context cannot be resolved, and, unless it reads this machine, offers candidates exactly when its values are compiled in.
- TestEveryCommandHasAnExample: every command shows an example.
- TestEveryExampleIsAnInvocationBBAccepts: bb accepts every example a command shows.
- TestEveryDeprecatedFlagIsStillTakenAndSaysSo: a deprecated flag is still taken, hidden from help, and warns on stderr.
- TestEveryRegisteredEntryIsWellFormed: every deprecation entry has a name, a reason and advice, and a version its removal release can be worked out from.
- TestACappedListingSaysSoIsEnforced: a command that takes --limit reports reaching it, in JSON and in text.
- TestOnlyPagingCommandsAdvertisePagingFlags: --limit and --all are registered only on a command that reads them, every paging option that is read is registered, and no command inherits them.
- TestNoServiceOptionIsCalledLimit: no service option is called Limit or PageSize; a cap is MaxResults, and paging belongs to openapi.PageThrough.
- TestTheServiceLayerDoesNotImportTheCLI: no file the service layer ships imports internal/cli.
- TestNoFailureIsWrappedAsTransient: a transport failure keeps its classification through the service that wraps it.
- TestTheStatusMappingIsOnlyTestedWhereItLives: no package outside internal/openapi tests the status-to-kind mapping.
- TestKindsCoversEveryDeclaredKind: every error kind errors.go declares is listed once by Kinds() and maps to an exit code.
- TestTheRegistryShowsTheKindBBDecides: every row of the committed error registry shows the kind bb decides for its status and exception.
- TestOnlyTheSharedHelperDecidesInteractivity: only the shared helper asks whether a stream is a terminal.
- TestEveryUseOfStandardInputIsAccountedFor: every place standard input is used is recorded.
- TestEveryDestructiveCommandCanBeConfirmed: every command named delete, remove, revoke or clear takes --yes.
- TestEveryDestructiveCommandRefusesWhenNobodyConfirmed: every command named delete, remove, revoke or clear refuses with a validation error when run without --yes and nobody to ask.
- TestPolicyLoadingNeverCreatesTheSystemConfigDirectory: reading policy does not create its directory.
- TestReadOnlyCoversEveryCommandThatChangesBitbucket: read_only refuses every command classified as changing Bitbucket, and no other.
- TestDiagnoseAgreesWithTheLoader: bb doctor resolves a configuration to the values the loader does.
- TestConfigurationPageSaysWhichFileReadsWhichKey: the configuration page names the keys each file's loader reads.
- TestSystemPolicyPageListsEveryPolicyKey: the system policy page lists every policy key.
- TestEnvironmentPageNamesEveryVariableBBReads: the environment page names every variable bb reads, and only those.
- TestMachineModePageStatesEachKindsExitCode: the machine-mode page gives every error kind its real exit code.
- TestTroubleshootingPageStatesEachKindsExitCode: the troubleshooting page gives every error kind its real exit code.
- TestRepositoryDocumentationIsValid: every Markdown file under docs and skills, and README.md, AGENTS.md and CONTRIBUTING.md, passes docs-lint.
- TestTheNavigationListsEveryPage: mkdocs.yml lists every command reference page once, and no page the reference does not have.
- TestNoPageGivesTwoThingsTheSameID: no command reference page gives one id to two things.
- TestNoPageHoldsWhatTheDocsBuildReadsAsAMacro: no command reference page holds what the docs build reads as a macro.
- TestTheCommittedWindowHoldsTogether: the committed release window, the release the stack provisions, the differences internal/compat declares and the Bitbucket versions page agree.
- TestCommittedSkillHasNoUnrenderedPlaceholders: no shipped skill's committed SKILL.md holds a template marker.
- TestEveryMCPToolIsAccountedFor: every MCP tool maps to a command or is recorded as MCP-only.
- TestEveryMappedCLICommandExists: no mapping names a command that was removed.
- TestAllSpecsHaveNonEmptyNames: every MCP tool has a name.
- TestAllSpecsHaveUniqueNames: no two MCP tools share a name.
- TestAllSpecsHaveNonEmptyDescriptions: every MCP tool has a description.
- TestAllSpecsHaveRegistrars: every MCP tool has a registrar.
- TestEveryToolHasAScopeRule: no MCP tool escapes workspace scoping.
- TestEveryResourceTemplateNamesItsProjectAndRepository: no MCP resource escapes workspace scoping.
- TestEveryPromptTakesItsProjectAndRepository: no MCP prompt escapes workspace scoping.
- TestTheServerListsEveryTemplateAndPrompt: a read-only server lists every resource template, with a name, title and description, and every prompt, with a title and description.
- TestToolsThatAskAreTheOnesThatDecideAMerge: the tools that ask the person are exactly the ones that decide a merge, and create_tag.
- TestReadOnlyToolsDoNotAsk: a tool annotated read-only asks nothing.
- TestLiveMCPToolsThatAskChangeNothingUnlessAccepted: every MCP tool that asks is refused with -32021 to a client that cannot ask and stopped when the person declines, and Bitbucket reads back unchanged.
- TestEveryToolDeclaresItsHintsAndTitle: every MCP tool states all four hints and a title.
- TestNoToolIsOpenWorld: no MCP tool is annotated open-world.
- TestEveryLimitedToolSaysWhenItStopped: an MCP tool that takes a limit returns a required limit_reached.
- TestLiveMCPEveryToolReturnsAClientCompatibleResult: every MCP tool is called, and its result is a JSON object with a text fallback.
- TestViewScriptsBuildNoHTMLFromStrings: no MCP view script parses a string as HTML.
- TestViewScriptsDeclareEachNameOnce: no top-level name is declared twice across the MCP view scripts.
- TestTheRepositorysRecordsLoad: every record parses as one, and no two share a number.
- TestADRDoesNotNameToolsThatDoNotExist: a record does not name a removed tool.
- TestADRDoesNotNameFlagsThatDoNotExist: a record does not name a removed flag.
- TestEveryADRMentionHasARecord: nothing in the repository names a record that does not exist.
- TestNoRecordNamesABitbucketVersion: no record restates a Bitbucket release.
- TestGovernanceTestsNamedInThisRecordExist: this list names only tests that exist.
- TestEveryHookRunnableGateRunsOnBothSides: every gate a git hook can run runs locally and in CI.
- TestNoGateIsDefinedAndNeverRun: a task named like a check is reachable from something that runs it.
- TestAmbientGitConfigGuardIsInstalledWhereTestsShellOutToGit: a package running git installs the guard.
- TestTheSealIsInstalledWhereTestsLoadTheConfiguration: a package whose tests load the configuration seals its process.
- TestNoFixtureIsNamedFromTheClock: no test builds a fixture name from time.Now().
- TestTheRepositoryHasNoUnclassifiedMocks: every mocked server under internal is classified.
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
