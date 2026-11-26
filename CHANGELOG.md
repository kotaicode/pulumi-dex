# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.9.1] - 2026-11-26

### Fixed
- **`pulumi refresh` no longer creates spurious changes for OIDC connectors.** After a refresh, the
  next preview proposed updates for connectors that had not changed:
  - A generic `Connector` with `type: oidc` and `rawConfig` came back as `oidcConfig`. Refresh now
    keeps `rawConfig`, and reformatting the JSON alone (whitespace or key order) is not a change.
  - `AzureOidcConnector` and `CognitoOidcConnector` lost `extraOidc` on refresh. They also reported
    `userNameSource` (`preferred_username` or `email`) when the program had omitted it. Both are now
    read back the way they were written.

  Changes made directly in Dex are still reported. Credentials stay secret through these conversions.
  `pulumi import` is unchanged: an imported `oidc` connector is still represented as `oidcConfig`.
- **`pulumi refresh` no longer creates spurious changes for `Client` resources.** A client without
  `public` came back as `public: false`, and one with `logoUrl: ""` came back without `logoUrl`.
  The next preview then proposed an update although nothing had changed. Refresh now reports these
  values the way the program set them. Changes made directly in Dex, such as making a client public
  or adding a trusted peer, are still reported.

## [0.9.0] - 2026-11-26

### Changed
- **Updating a `Client` to remove `logoUrl`, `trustedPeers`, `redirectUris` or `name` now fails.**
  Dex's `UpdateClient` API ignores empty values, so the update reported success and Pulumi recorded the
  field as removed while Dex kept the old value. For `trustedPeers`, Dex kept trusting peers that
  appeared to have been removed. Preview and update now stop with an error naming the field. To remove
  the value, delete the client and create it again; this generates a new secret unless `secret` is set.
  Stacks that already recorded such a removal hold the wrong state: run `pulumi refresh` to see the
  values Dex still has.
- npm publishing uses trusted publishing (OIDC) only; the `NPM_TOKEN` secret is no longer
  read, and publishing no longer silently skips when it is missing.
- Go module caching uses `setup-go`'s built-in cache keyed on `provider/go.sum`; the
  duplicate `actions/cache` steps (and the "Restore cache failed" warning) are gone.

### Security
- **Opaque connector configuration is now a Pulumi secret.** `Connector.rawConfig`,
  `OIDCConfig.extra` and the `extraOidc` maps of `AzureOidcConnector` and `CognitoOidcConnector`
  can carry credentials, such as bind passwords or client assertions, but were stored in plaintext
  in state and printed in preview and update output when given as plain values. They are now
  marked secret in the schema and SDKs.
- **Credential outputs are secret regardless of how they were supplied.** The provider now marks
  every `clientSecret`, `Client.secret`, `rawConfig`, `extraOidc` and the generic connector's
  whole `oidcConfig` output as secret itself. Before, it relied on the SDK having wrapped the input,
  so values read back by `pulumi import` were stored in plaintext. Because the framework cannot mark
  nested fields, `oidcConfig` is secret as a whole, and diffs show it as `[secret]`.
- **Upgrading existing stacks:** run `pulumi up` once with the new provider and SDK. Connectors that
  use the newly secret fields show a one-time `[secret] => [secret]` update, which resends the
  unchanged configuration to Dex. `pulumi refresh` alone encrypts outputs but leaves earlier plaintext
  inputs in state. Neither step removes plaintext from earlier checkpoints in your backend's history,
  exported state files or CI logs. If those may have been exposed, rotate the affected credentials.
- **`pulumi import` stores credentials as secrets.** Imported credentials were stored in plaintext
  in the resource's inputs, because only outputs were protected. The provider now marks Read inputs secret
  wherever the schema declares a secret property, including nested `oidcConfig.clientSecret` and
  `oidcConfig.extra`. The program that `pulumi import` prints still contains the values, because it must
  reproduce them: use `--generate-code=false`, or `--out <file>` and treat that file as sensitive.
- **Credentials are removed from Dex error messages.** Dex includes raw storage-backend errors in its
  messages. If a backend echoed the object it rejected, the client secret or connector configuration
  would have been printed with the Pulumi error. The provider now replaces the credential values sent
  in that request with `[secret]`. Connector configuration is opaque, so this applies to every
  config string of 8 or more characters. The status code and the rest of the message are unchanged.

### Fixed
- **Updates no longer report success for objects deleted outside Pulumi.** Dex answers an
  update of a missing client or connector with a `not_found` flag rather than a gRPC error,
  and every resource ignored it: `pulumi up` showed the update as successful and wrote the
  desired values to state although nothing existed in Dex. Such updates now fail with a
  "not found in Dex" error that points to `pulumi refresh`. A failed update does not recreate
  the object; run `pulumi refresh` (which removes it from state) and `pulumi up` again to
  recreate it.
- **`pulumi refresh` detects `Client` resources deleted outside Pulumi.** Dex reports a missing
  client from `GetClient` as an `Unknown` error, which refresh treated as a failure, so the
  refresh could not complete. Absence is now confirmed via `ListClients` and the client is
  removed from state. Authorization, timeout and other errors, or a failed confirmation,
  still fail the refresh instead of dropping the resource.

## [0.8.1] - 2026-11-25

### Fixed
- **npm package now ships compiled JavaScript.** Published versions up to and including 0.8.0
  contained only TypeScript sources and no `main`, so `require("@kotaicode/pulumi-dex")` failed
  and ts-node refused to compile it from `node_modules`. The release now publishes the
  compiled `bin/` output, and both CI and the release smoke-test loading the package.
- **Plugin version always matches the release tag.** The release built the provider (and
  generated the schema) with the Makefile's default `VERSION`, not the tag. npm versions
  0.2.0–0.7.2 therefore declared plugin version `0.1.0`, which was never released, and
  `pulumi install` failed with a 404. Fixed for 0.7.4+ by manual bumps; now enforced: every
  build step receives the tag version and a release check fails if the schema or any SDK
  disagrees with the tag.
- Docs (including the page the Pulumi Registry pulls in) told users to run
  `pulumi plugin install resource dex v0.1.0`, a version that was never released. They now
  use a version placeholder, and local-dev instructions use `make install`.
- **Go SDK can be installed again.** `sdk/go.mod` sits in `sdk/` but declared the module as
  `.../sdk/go`, so `go get` failed and the SDK's own `internal` import did not resolve. The
  module is now `github.com/kotaicode/pulumi-dex/sdk` (import path unchanged:
  `.../sdk/go/dex`), and each release also pushes the `sdk/vX.Y.Z` tag that Go needs.
- Cross-platform release binaries were built with an empty provider version (`$(VERSION)`
  was evaluated as a shell command substitution).

### Changed
- GitHub Actions bumped to current majors (checkout v7, setup-go v7, cache v6,
  upload-artifact v7, download-artifact v8, setup-node v7, setup-python v7,
  action-gh-release v3, codecov v7, pulumi/actions v7). Publishing runs on Node 24.
- Go version in CI and release follows `provider/go.mod` instead of a hardcoded value.
- Generated TypeScript SDK builds with TypeScript ^6.0 and @types/node ^24.
- Dependabot now watches `/provider` and `/sdk` for Go modules (it previously watched the
  repository root, where there is no `go.mod`); the no-op npm entry was removed.

### Added
- `dex.GitLabConnector` resource for GitLab.com and self-hosted GitLab instances
- `dex.GitHubConnector` resource for GitHub.com and GitHub Enterprise
- `dex.GoogleConnector` resource for Google Workspace and Google accounts
- `dex.LocalConnector` resource for local/builtin authentication
- GitHub Actions CI workflow for build, test, and lint
- Preview mode support (FR6.1) - simulate Dex calls without side effects during `pulumi preview`
- Improved error messages (FR6.2) - human-friendly error wrapping with context
- golangci-lint configuration for code quality
- Integration test infrastructure with Docker Compose

### Changed
- Error messages now include operation, resource type, and resource ID for better debugging

## [0.1.0] - 2025-01-XX

### Added
- Initial release of Pulumi provider for Dex
- `dex.Client` resource for managing OAuth2 clients
- `dex.Connector` resource for generic connector management
- `dex.AzureOidcConnector` resource for Azure AD/Entra ID (OIDC)
- `dex.AzureMicrosoftConnector` resource for Azure AD/Entra ID (Microsoft-specific)
- `dex.CognitoOidcConnector` resource for AWS Cognito user pools
- Provider configuration with TLS/mTLS support
- Secret generation for clients
- Local development environment with Docker Compose
- TypeScript and Go SDK generation
- Example Pulumi programs

