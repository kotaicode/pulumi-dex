# Security Policy

## Supported Versions

Security fixes are released for the latest minor version of the provider (for example, while 0.10.x
is the newest release line, fixes go into the next 0.10.x or a later release). Older release lines
do not receive fixes; upgrade to the latest release to get them.

## Reporting a Vulnerability

Please report vulnerabilities privately through GitHub's private vulnerability reporting. Do not open
a public issue, pull request or discussion for a suspected vulnerability.

1. Open the [Security tab](https://github.com/kotaicode/pulumi-dex/security) of this repository.
2. Choose **Report a vulnerability**.
3. Describe the problem, the affected versions, how to reproduce it and its impact. Do not include
   real credentials, tokens or client secrets; use placeholder values.

The report is visible only to you and the maintainers. We review reports on a best-effort basis and
coordinate fixes and disclosure through the advisory.

## Disclosure

Once a fix is released, we publish a GitHub security advisory and list the fix in `CHANGELOG.md`.
Reporters are credited in the advisory unless they prefer otherwise.

## Automated Checks

Every pull request and a weekly scheduled run check the provider and the Go SDK with
[`govulncheck`](https://go.dev/security/vuln/), which reports known vulnerabilities in code the
provider actually calls, including the Go standard library. Dependabot keeps Go modules and the
pinned GitHub Actions up to date.

## Using the Provider Securely

- **Keep the connection to the Dex gRPC API encrypted.** The provider connects with TLS and verifies
  Dex's certificate by default. Use mTLS (`clientCert` and `clientKey`) where Dex supports it. Use
  `plaintext: true` only for a Dex on your own machine or inside a cluster, and avoid
  `insecureSkipVerify`, which disables certificate checks.
- **Restrict network access** to Dex's gRPC API; anyone who can reach it can manage clients and
  connectors.
- **Keep credentials secret.** Pass client secrets, connector secrets and TLS keys as Pulumi
  secrets; the provider marks them secret in state.
- **Stay on the latest release** to receive security fixes.
