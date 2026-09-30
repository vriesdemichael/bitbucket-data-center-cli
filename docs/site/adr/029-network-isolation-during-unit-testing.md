---
search:
  boost: 0.3
---

# ADR-029: Network Isolation during Unit Testing

Implement a project-wide network isolation policy for unit tests by introducing a SafeTransport RoundTripper that blocks all non-local HTTP requests when BB_BLOCK_EXTERNAL_NETWORK=1 is set.

Ensure all unit tests remain isolated from the external network. Mocks must use httptest.Server or local loopback addresses (127.0.0.1, localhost, ::1). Any test attempting to reach an  external or unconfigured domain must fail immediately with a descriptive error.

Unintended network calls in tests lead to flaky behavior, slow suites, and build failures in isolated CI environments. Standardizing on local-only communication during testing improves stability and developer confidence.

## Not chosen

- **Depend on manual environment configuration**: Error-prone and fails to catch new tests that accidentally leak network calls.
- **Use a general mock library only**: Doesn't provide a project-wide safety net for accidental direct http.Client usage.
