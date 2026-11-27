# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.12.0] - 2026-11-27

### Added
- New `LocalUser` resource (`dex:resources:LocalUser`) for users in Dex's password database. It takes
  a bcrypt hash, never a password; the hash is a secret and is never generated. `groups` needs Dex
  v2.46 and is refused on v2.45, which would drop it silently. Dex cannot change a user's email or
  userId, remove its username or remove all its groups; those changes fail in preview. Import works
  without the password hash, and later updates leave the password unchanged. Deleting a user does not
  end its sessions or revoke issued tokens.
- Lookup functions `getClient`, `getConnector` and `getLocalUser` read one object from Dex without
  managing or adopting it. A missing object is an error naming the ID; network and permission errors
  are reported as such. The client secret and the connector configuration are returned as secrets.
- Compatibility testing against real Dex servers: CI runs a lifecycle suite (create, refresh, preview
  without changes, update, delete) for `Client`, `GitHubConnector`, the generic `Connector` and
  `LocalUser`, plus the lookup functions and the version check, against Dex v2.45.1 and v2.46.1. The
  README lists the supported Dex lines.
- Server version checks: the provider asks Dex for its version once per connection and refuses a
  feature that needs a newer Dex with a message naming the required version. A Dex whose version
  cannot be determined, or that is outside the supported lines, gets a warning; existing resources
  keep working.

### Changed
- The provider uses Dex's API bindings from the Dex v2.46.1 release commit (`75eb19238e10`) instead of
  the `api/v2` v2.4.0 tag, which predates the API changes in Dex v2.46.0. Dex has not tagged the
  bindings since; the provider moves to a tag once one exists. Behaviour against Dex servers is
  unchanged.
- `docker-compose.yml` runs Dex v2.46.1, pinned by digest, instead of the floating `latest-alpine`
  image.

### Fixed
- After `pulumi import` into a stack whose Dex provider had not been used yet, the next `pulumi up`
  planned to replace every imported resource. The import stored the provider settings as strings (for
  example `plaintext: "true"`), which counted as a change of the provider configuration; the
  replacement then failed without deleting anything. Such a stack now updates normally. A real change
  of a provider setting still replaces.
- Importing an ID that does not exist in Dex succeeded and added an empty resource to the stack. It
  now fails with a "not found in Dex" error naming the ID. `pulumi refresh` still removes resources
  whose objects were deleted in Dex.
- After `pulumi refresh`, every preview of a `Client` whose secret Dex generated proposed removing the
  `secret` input. Refresh now keeps a generated secret in the state only. A stack refreshed with an
  earlier version shows this change once more; one `pulumi up` settles it and leaves the secret in Dex
  unchanged.

## [0.11.0] - 2026-11-26

**Security: a `LocalConnector` with `enabled: false` has password login enabled.** Dex offers password
login through every connector of type `local`, and the provider never sent `enabled` to Dex. A stack
that declares a `LocalConnector` with `enabled: false` therefore lets users log in with their Dex
passwords through that connector, even when `enablePasswordDB` is `false` in Dex's configuration.
Check your programs for `enabled: false` on a `LocalConnector`:

- To turn that password login off, remove the `LocalConnector` resource from the program and run
  `pulumi up`. This deletes the connector in Dex; password users are kept.
- To keep password login, remove `enabled: false` from the resource.

From this version on, `enabled: false` fails at preview with this instruction.

**Before upgrading: if your Dex gRPC API is not TLS-enabled, set `plaintext: true` in the provider
configuration.** The provider now connects with TLS by default. A configuration without TLS options
that reached a Dex without TLS, such as the docker-compose setup in this repository, fails with an
error that names `plaintext: true` as the fix:

```bash
pulumi config set --type bool dex:plaintext true   # default provider
```

or `plaintext: true` in an explicit `dex.Provider`. Use it only for a Dex on your own machine or
inside a cluster.

### Breaking
- **The connection to Dex is encrypted by default.** Without `caCert`, `clientCert`, `clientKey` or
  `insecureSkipVerify`, the provider connected without TLS, so client secrets and connector
  credentials crossed the network unencrypted. It now uses TLS 1.2 or later and verifies Dex's
  certificate and host name against the system's root CAs. The new `plaintext: true` option connects
  without TLS; combined with any TLS option it is an error naming the conflicting options. There is
  no automatic fallback to plaintext. `caCert` still replaces the system's root CAs, and
  `insecureSkipVerify: true` still means TLS without certificate verification, never plaintext.
  Documentation that recommended `insecureSkipVerify: true` for the local Dex without TLS was wrong
  (that combination never connected) and now uses `plaintext: true`.
- **`AzureMicrosoftConnector.groups` is replaced by `allowedGroups`.** `groups` was a string described
  as a group claim name, but Dex reads its `groups` setting as a list of groups whose members may log
  in. Dex v2.46.1 cannot read the string, so a connector with `groups` failed to initialize and login
  through it failed. The new `allowedGroups` list is written to Dex's `groups`. A program that still
  sets `groups` fails at preview with a migration message; refresh of existing stacks keeps working.
  `allowedGroups` must not be empty (Dex would treat that as no restriction) and needs a specific
  tenant ID or domain, because Dex ignores it for `common`, `organizations` and `consumers`.
  To migrate, replace `groups: "..."` with `allowedGroups: ["group-a", ...]`, or remove it.
- **Changing a client's `secret` or `public` fails at preview.** Dex's `UpdateClient` cannot change
  either. A new explicit `secret` was silently ignored, so a rotation never happened; a new `public`
  value was recorded in state although Dex kept the old one, and every refresh brought the difference
  back. The preview now explains that Dex cannot change them and how to rotate: create a client with
  a new client ID, move its users to it, then remove the old client. An omitted, generated secret
  stays unchanged and shows no change. A `public` value changed directly in Dex also stops the
  preview after a refresh, instead of proposing an update that would not apply.
- **Changing `clientId` or `connectorId` replaces the object.** Before, the preview proposed an
  update that then failed with "cannot be changed". Now Pulumi creates the object with the new ID and
  deletes the old one; the IDs differ, so creating first cannot collide. A replaced client without an
  explicit `secret` gets a newly generated one. The schema marks `clientId` and every `connectorId` as
  `replaceOnChanges`, so the generated SDKs request the replacement as well.
- **Extension keys can no longer override typed connector settings.** `extraOidc`
  (`AzureOidcConnector`, `CognitoOidcConnector`) and `oidcConfig.extra` (generic `Connector`) were
  merged over the typed fields, so a key such as `clientID`, `issuer` or `redirectURI` silently
  replaced the typed value in Dex. Dex reads config keys case-insensitively, so every spelling
  collides. Such keys now fail at preview with an error that names the keys but not their values.
  To migrate, move the value to the typed input (for example `clientId`, or `tenantId` for the Azure
  issuer) and remove the key from the extension map.
- **The generic `Connector`'s input rule is checked at preview.** Exactly one of `oidcConfig` and
  `rawConfig`, `oidcConfig` only with `type: "oidc"`, and valid JSON in `rawConfig` were only checked
  when `pulumi up` applied the change. Unknown values in a preview are not judged.
- **`LocalConnector.enabled: false` fails at preview, and `enabled` is deprecated.** A local connector
  in Dex always offers password login (see the security note above), so the only way to turn it off
  is to remove the resource. Refresh reports `enabled: true` for every existing local connector, which
  is what Dex does with it.

### Fixed
- **Release binaries are built with Go 1.26.11 instead of 1.26.6.** The `go.mod` files named only
  Go 1.26.6, so CI and releases built with it. `govulncheck` reports 12 standard library
  vulnerabilities in 1.26.6 that the provider's code can reach, among them HTTP/2 server crashes and
  memory exhaustion in `net/http` and missing distrust of compromised intermediate certificates in
  `crypto/x509`. A `toolchain go1.26.11` line now sets the build version; the minimum Go version for
  Go SDK users stays 1.26.6.
- Connection failures caused by TLS are reported with their cause and fix instead of only "timed
  out": a Dex without TLS (set `plaintext: true`), a certificate the system does not trust (set
  `caCert`), a certificate that does not cover the host name, and a Dex that rejects the client
  certificate or requires one (set `clientCert` and `clientKey`). Errors never include certificate
  or key contents.
- A preview whose provider configuration depends on other resources no longer fails or connects to
  Dex with guessed settings. An unknown `host` failed with "host is required", and an unknown
  `caCert` was treated as absent, so the provider connected with the wrong settings. The provider now
  connects only once all configuration values are known.
- **A login restriction set in Dex for an `AzureMicrosoftConnector` is no longer removed silently.**
  Refresh ignored a `groups` list stored in Dex, and the next update of the connector, for example a
  name change, wrote its config without it, so every user of the tenant could log in again. Refresh
  now reads the list into `allowedGroups`, and a program without it shows the removal in the preview.
- `AzureMicrosoftConnector.tenant` accepts every form Dex documents: `common`, `organizations`,
  `consumers`, a tenant ID or a tenant domain such as `contoso.onmicrosoft.com`. Before, only
  `common`, `organizations` and tenant IDs passed validation.
- Settings stored in a connector's config are updated in place instead of failing the update with
  "cannot be changed (would require replace)": `AzureOidcConnector.tenantId`,
  `AzureMicrosoftConnector.tenant`, `CognitoOidcConnector.region` and `userPoolId`,
  `GitHubConnector.hostName` and `GitLabConnector.baseURL`. Dex replaces a connector's whole config,
  as it already did for a generic `Connector`'s `type`.
- Creating a `LocalConnector` with the ID `local` while Dex runs with `enablePasswordDB: true` fails
  with an error that explains that `local` is Dex's built-in password login, defined in its
  configuration and read-only for the API, instead of Dex's bare "static connectors: read-only".
  Creating any other client or connector that Dex's configuration file defines gets a similar error.
- `pulumi import` with the default provider failed when the stack config set `plaintext`,
  `insecureSkipVerify` or `timeoutSeconds` (for example with `pulumi config set --type bool`):
  "Field 'plaintext' on 'provider.DexConfig' must be a 'bool'; got 'string' instead". The import path
  passes these values as strings; the provider now accepts `"true"`, `"false"` and whole numbers
  given as strings.
- The README listed `DEX_*` environment variables that the provider never read. It now says that the
  provider reads no environment variables and shows how to pass settings through the program or
  Pulumi config.

### Changed
- New `Security` workflow: `govulncheck` (pinned to v1.10.0) scans the provider and the Go SDK on
  every pull request and weekly. A finding, or a scan that cannot run, fails the check.
- All GitHub Actions in the workflows are pinned to a commit SHA with the version as a comment;
  Dependabot keeps the pins up to date. The golangci-lint version in CI is pinned to v2.15.0.
- `SECURITY.md` asks for reports through GitHub's private vulnerability reporting, names the latest
  minor version as supported, and no longer promises response times or mentions an email address.
- `make generate-schema` and `make generate-sdks` fail when any required step fails. Before, a
  failed code generation or TypeScript export fix only printed a warning and still reported
  success; a failed Go generation even deleted the Go SDK, and a failed schema generation left an
  empty `schema.json`. `make generate-sdks` now always regenerates `schema.json` from the freshly
  built provider first, so SDKs are never generated from a stale schema.
- The Pulumi CLI used for code generation is pinned in `.pulumi.version` (3.278.0) for CI, the
  release workflow and the Makefile, which warns when the local CLI differs. CI fails when the
  committed `schema.json` or `sdk/` differ from freshly generated output.

## [0.10.0] - 2026-11-26

**Before upgrading, check whether more than one stack manages the same Dex object.** Earlier
versions silently took over an existing client or connector when a second stack created one with
the same ID, so several stacks can hold the same object. Destroying any of them deletes the object
in Dex for all of them. In each stack, list the Dex objects it manages:

```bash
pulumi stack export | jq -r '.deployment.resources[] | select(.type | startswith("dex:resources:")) | (if .type == "dex:resources:Client" then "client " else "connector " end) + .id'
```

If a line appears in more than one stack, keep the resource in one stack only. In each of the
others, run `pulumi state delete <urn>` (`pulumi stack --show-urns` lists the URNs). This removes
the resource from that stack's state without deleting it in Dex.

### Breaking
- **Creating a resource whose ID already exists in Dex now fails instead of taking over the
  object.** `Client` and the generic `Connector` silently adopted an existing client or connector
  with the same ID, even one created by another stack or tool, and copied its configuration,
  including the client secret, into state. Destroying that stack then deleted the other owner's
  object. The typed connectors already failed, but without guidance. Every resource now returns a
  conflict that names the exact command to take the object over explicitly, for example
  `pulumi import dex:resources:Client webClient my-web-app`. The existing object is left untouched.
  This applies whether Dex reports the existing ID with its `already_exists` response flag (Dex
  v2.46.1) or with a gRPC `AlreadyExists` status.
- **Typed connector resources only read connectors of their own Dex type.** Importing or refreshing,
  for example, a `GoogleConnector` whose connector in Dex has type `github` now fails. Before, a later
  update would have rewritten the connector as a Google connector. Use `dex:resources:Connector` for
  connectors of other types.
- A create that times out, is canceled or loses its connection now reports that the outcome is
  unknown and how to check and import the object. Creates are never retried automatically.

Upgrade steps, after the check at the top of this section:
1. Resources already in a stack's state keep working after the upgrade: refresh, update and delete
   behave as before.
2. If `pulumi up` now fails with "already exists", either run the `pulumi import` command from the
   error and then `pulumi up` again, or choose a different ID. Programs that relied on recreating
   their stack against an existing Dex need this step once.
3. If refresh now reports that a connector "has type X, not Y", import it with the resource for its
   actual type, or with `dex:resources:Connector`.

### Fixed
- **Refresh no longer drops connectors whose config in Dex cannot be read.** Dex only checks that a
  connector config is valid JSON, so it can store a config that is not a JSON object or a known
  field with the wrong type. `AzureOidcConnector`, `AzureMicrosoftConnector`,
  `CognitoOidcConnector`, `GitHubConnector`, `GitLabConnector` and `GoogleConnector` reported such
  a connector as deleted, so `pulumi refresh` removed it from state although it still existed.
  Others read the field as empty and wrote that into state. Refresh now fails with an error naming
  the connector, the path and the expected type (for example
  `config.orgs[0].name is a JSON number, expected a string`) and keeps the resource and its
  previous values in state. The error never includes config values.
- The generic `Connector` with `type: oidc` falls back to `rawConfig` when the stored config cannot
  be represented as `oidcConfig`, instead of silently dropping a known field with the wrong type.
  The stored config is kept verbatim and stays secret.

### Changed
- GitHub release notes now start with the release's section from this changelog, above the
  installation instructions and the generated list of pull requests. A release whose tag has no
  changelog section stops before anything is published.

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

