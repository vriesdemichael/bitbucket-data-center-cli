package testsupport

// RefusedURL is an address that refuses every connection.
//
// It is how a test asks for a transport fault. A client cannot be made to lose
// a connection by a server that is answering, and no live Bitbucket can be
// asked to drop one on cue, so this is the only honest way to check that a
// failure below the API is reported rather than swallowed.
//
// The address is real and refused rather than unroutable: connecting fails
// immediately instead of waiting for a DNS lookup or a timeout, which is what
// makes this usable in a unit test.
//
// Port 1, because no listener is ever handed it. A port a test takes from the
// system and gives back is free for the next listener that asks for any port,
// and with tests running in parallel that listener is a server in another
// test: the request meant to fail arrives there, and fails that test instead.
// The system hands out ports from its dynamic range, which starts far above the
// well-known ports, and nothing runs the service port 1 is assigned to.
const RefusedURL = "http://127.0.0.1:1"
