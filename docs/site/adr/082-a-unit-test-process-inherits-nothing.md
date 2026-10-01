---
search:
  boost: 0.3
---

# ADR-082: A unit test process inherits nothing

A package whose tests run the CLI or load the configuration seals its process in TestMain, with `testsupport.SealedMain`, or `testsupport.SealAmbientEnvironment` when its TestMain does more. The seal empties the credentials, host, repository context, configuration paths and logging settings, disables the stored config, sets the retry count to zero, and blocks the network beyond the machine (ADR-029). It empties rather than unsets, because .env fills in only the names the environment does not already carry. A test then says what it wants by passing it -- through the Dependencies seam a command is built with, through a flag, or through the config.Overrides a root command is constructed with -- and never by publishing it to the process. The last of those is how a credential travels, because a password is never a flag value (ADR-047) and the environment is shared. t.Setenv is reserved for tests whose subject is the environment, and those accept that they run alone, because the call disqualifies a test from t.Parallel and every helper that reaches it taints its callers. `TestTheSealIsInstalledWhereTestsLoadTheConfiguration` fails a package whose tests load the configuration without the seal.

Do not open a test with t.Setenv("SOMETHING", "") to clear what the process might be carrying; the seal has already cleared it. Do not set BITBUCKET_URL, BITBUCKET_TOKEN, BITBUCKET_PROJECT_KEY or BITBUCKET_REPO_SLUG to give a command its configuration -- pass them, and add a field to the package's test setup if there is nowhere to put them. A new package under internal/cli/cmd gets the sealed TestMain when it is created. When a value can only reach the code through the environment, that is a missing seam in the code and not a reason to publish it.

Unsealed, the machine decides what a test sees. config.LoadWithOverrides loads .env itself, walking up from the working directory, and the stored config holds whatever the developer is logged into, so `go test ./...` runs different inputs on a configured machine than on a clean one, and a green run on one is no evidence about the other. Clearing the inheritance test by test costs the parallelism, since t.Setenv rules it out. Sealing the process once answers both, and a test that cannot pass what it needs has found a seam worth adding.

## Not chosen

- **Keep t.Setenv and accept a sequential suite**: Slower on every commit and every pull request, and it leaves the inheritance in place, which is what makes a green run on one machine no evidence about another.
- **Set the environment once in TestMain and leave the values there**: Kept, but only for values that never vary: the seal writes empties, a retry count and the network block, and the live suite writes its URL and credentials, because nothing changes them afterwards, so no test can observe another's. It does not extend to a repository context, which differs per test and is exactly what two parallel tests would take from each other.
- **Run each package's tests in their own process**: Go already does that. The isolation problem is between tests inside one binary, which a second binary does not solve.
