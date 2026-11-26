# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- npm publishing uses trusted publishing (OIDC) only; the `NPM_TOKEN` secret is no longer
  read, and publishing no longer silently skips when it is missing.
- Go module caching uses `setup-go`'s built-in cache keyed on `provider/go.sum`; the
  duplicate `actions/cache` steps (and the "Restore cache failed" warning) are gone.

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

