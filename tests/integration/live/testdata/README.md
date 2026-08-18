# Live suite fixtures

`live-suite-gpg-public-key.asc` is a throwaway OpenPGP public key, generated once
for `TestLiveGPGKeyLifecycle`. Bitbucket parses the key, so a fabricated block
will not do, and generating one per run would need `gpg` on the machine running
the suite.

The matching private key was never kept. Nothing signs with it and nothing trusts
it; it exists so there is a well-formed key to add and remove.
