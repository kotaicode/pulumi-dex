# Pulumi Provider for Dex

A Pulumi provider for managing Dex (https://dexidp.io/) resources via the Dex gRPC Admin API. This provider allows you to manage Dex OAuth2 clients and connectors (IdPs) as infrastructure-as-code.

## Features

- **OAuth2 Client Management**: Create, update, and delete Dex OAuth2 clients
- **Generic Connector Support**: Manage any Dex connector type (OIDC, LDAP, SAML, etc.)
- **OIDC Connector Support**: First-class support for OIDC connectors with typed configuration
- **Azure/Entra ID Integration**: 
  - `AzureOidcConnector` - Uses generic OIDC connector (type: `oidc`)
  - `AzureMicrosoftConnector` - Uses Dex's Microsoft-specific connector (type: `microsoft`)
- **AWS Cognito Integration**: `CognitoOidcConnector` for managing Cognito user pools as IdPs
- **GitLab Integration**: `GitLabConnector` for GitLab.com and self-hosted GitLab instances
- **GitHub Integration**: `GitHubConnector` for GitHub.com and GitHub Enterprise
- **Google Integration**: `GoogleConnector` for Google Workspace and Google accounts
- **Password login**: `LocalConnector` adds a login with passwords stored in Dex

## Installation

### Prerequisites

- [Pulumi CLI](https://www.pulumi.com/docs/get-started/install/) installed
- Go 1.24+ (for building the provider)
- Access to a Dex instance with gRPC API enabled

### Building the Provider

```bash
# Clone the repository
git clone https://github.com/kotaicode/pulumi-dex.git
cd pulumi-dex

# Build the provider binary and install it as a local plugin
make install
```

### Generating Language SDKs

After building the provider, generate SDKs for your preferred language:

```bash
# Generate TypeScript SDK
pulumi package gen-sdk bin/pulumi-resource-dex --language typescript --out sdk/typescript

# Generate Go SDK
pulumi package gen-sdk bin/pulumi-resource-dex --language go --out sdk/go

# Generate Python SDK (optional)
pulumi package gen-sdk bin/pulumi-resource-dex --language python --out sdk/python
```

## Configuration

The provider requires configuration to connect to your Dex gRPC API:

```typescript
import * as dex from "@kotaicode/pulumi-dex";
import * as fs from "fs";

// TLS is the default. caCert is needed when Dex's certificate comes from a
// private CA; clientCert and clientKey when Dex requires client certificates (mTLS).
const provider = new dex.Provider("dex", {
    host: "dex.internal:5557", // Dex gRPC host:port
    caCert: fs.readFileSync("certs/ca.crt", "utf-8"),
    clientCert: fs.readFileSync("certs/client.crt", "utf-8"),
    clientKey: fs.readFileSync("certs/client.key", "utf-8"),
});

// A local Dex whose gRPC API has no TLS, such as the docker-compose setup in this repository:
const localProvider = new dex.Provider("dex-local", {
    host: "localhost:5557",
    plaintext: true,
});
```

### Connection Security

| Configuration | Connection |
| --- | --- |
| no TLS options | TLS 1.2 or later; Dex's certificate and host name are verified against the system's root CAs |
| `caCert` | TLS; only this CA is trusted, instead of the system's root CAs |
| `clientCert` and `clientKey` | TLS with a client certificate (mTLS); one without the other is an error |
| `insecureSkipVerify: true` | TLS without certificate verification (development only); still encrypted |
| `plaintext: true` | unencrypted; only for a Dex without TLS on a trusted network, such as localhost or inside a cluster |

`plaintext: true` cannot be combined with `caCert`, `clientCert`, `clientKey` or `insecureSkipVerify`. The provider never falls back from TLS to plaintext. If it reaches a Dex without TLS, the error names `plaintext: true` as the fix.

The provider reads no environment variables. To take connection settings from the environment, read them in your program (for example `process.env.DEX_HOST`) or use Pulumi config (`pulumi config set dex:host ...`, `pulumi config set --secret dex:caCert -- "$(cat ca.crt)"`).

## Dex Compatibility

| Provider | Dex servers | Tested in CI against |
| --- | --- | --- |
| 0.12.x | v2.45.x, v2.46.x | v2.45.1, v2.46.1 (release images, pinned by digest) |

Every resource works on both lines unless noted here: `LocalUser.groups` needs Dex v2.46 (Dex v2.45 would drop the groups silently, so the provider refuses them there). When it connects, the provider asks Dex for its version (Dex v2.45 reports API version 3, v2.46 API version 4). A feature that needs a newer Dex is refused with a message naming the required version; if the version cannot be determined, such features are refused too, with the underlying error. For a Dex outside the supported lines, the provider prints a warning, and existing resources keep working. CI runs a lifecycle suite (create, refresh, preview without changes, update, delete) against a real Dex of each line; to run it locally against your own Dex, see `provider/pkg/provider/resources/live_dex_test.go`.

The provider talks to Dex through its gRPC API, using Dex's official Go bindings `github.com/dexidp/dex/api/v2`. Dex has not tagged these bindings since v2.4.0 (August 2025), although Dex v2.46.0 changed the API, so the provider pins them at the commit of the Dex v2.46.1 release (`75eb19238e10`, Go module version `v2.4.1-0.20261021142137-75eb19238e10`). When Dex publishes a tag (`api/v2.5.0` or later), the provider moves to it; Dependabot proposes that update.

## Usage Examples

### Managing an OAuth2 Client

```typescript
import * as dex from "@kotaicode/pulumi-dex";

const webClient = new dex.Client("webClient", {
    clientId: "my-web-app",
    name: "My Web App",
    redirectUris: ["https://app.example.com/callback"],
    // secret is optional - will be auto-generated if omitted
}, { provider });

export const clientSecret = webClient.secret; // Pulumi secret
```

### Azure/Entra ID Connector (Generic OIDC)

```typescript
const azureConnector = new dex.AzureOidcConnector("azure-tenant-a", {
    connectorId: "azure-tenant-a",
    name: "Azure AD (Tenant A)",
    tenantId: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    clientId: "your-azure-app-client-id",
    clientSecret: "your-azure-app-client-secret", // Pulumi secret
    redirectUri: "https://dex.example.com/callback",
    scopes: ["openid", "profile", "email", "offline_access"],
    userNameSource: "preferred_username", // or "upn" or "email"
}, { provider });
```

### Azure/Entra ID Connector (Microsoft-Specific)

```typescript
const azureMsConnector = new dex.AzureMicrosoftConnector("azure-ms", {
    connectorId: "azure-ms",
    name: "Azure AD (Microsoft Connector)",
    // "common", "organizations", "consumers", a tenant ID or a domain such as "contoso.onmicrosoft.com"
    tenant: "contoso.onmicrosoft.com",
    clientId: "your-azure-app-client-id",
    clientSecret: "your-azure-app-client-secret",
    redirectUri: "https://dex.example.com/callback",
    // Optional: only members of these groups may log in. Requires a specific tenant
    // (ID or domain); Dex ignores the list for "common", "organizations" and "consumers".
    allowedGroups: ["dex-admins", "dex-users"],
}, { provider });
```

`allowedGroups` replaces the removed `groups` string, which Dex could not read: a connector with it failed to initialize, so nobody could log in through it.

### AWS Cognito Connector

```typescript
const cognitoConnector = new dex.CognitoOidcConnector("cognito-eu", {
    connectorId: "cognito-eu",
    name: "Cognito (EU)",
    region: "eu-central-1",
    userPoolId: "eu-central-1_XXXXXXX",
    clientId: "your-cognito-app-client-id",
    clientSecret: "your-cognito-app-client-secret",
    redirectUri: "https://dex.example.com/callback",
    userNameSource: "email", // or "sub"
}, { provider });
```

### GitLab Connector

```typescript
const gitlabConnector = new dex.GitLabConnector("gitlab", {
    connectorId: "gitlab",
    name: "GitLab",
    clientId: "your-gitlab-client-id",
    clientSecret: "your-gitlab-client-secret",
    redirectUri: "https://dex.example.com/callback",
    baseURL: "https://gitlab.com", // Optional, defaults to https://gitlab.com
    groups: ["my-group"], // Optional: groups whitelist
    useLoginAsID: false, // Optional: use username as ID instead of internal ID
    getGroupsPermission: false, // Optional: include group permissions in groups claim
}, { provider });
```

### GitHub Connector

```typescript
const githubConnector = new dex.GitHubConnector("github", {
    connectorId: "github",
    name: "GitHub",
    clientId: "your-github-client-id",
    clientSecret: "your-github-client-secret",
    redirectUri: "https://dex.example.com/callback",
    orgs: [
        { name: "my-organization" },
        { 
            name: "my-organization-with-teams",
            teams: ["red-team", "blue-team"]
        }
    ],
    teamNameField: "slug", // Optional: "name", "slug", or "both" - default: "slug"
    useLoginAsID: false, // Optional: use username as ID
    // For GitHub Enterprise:
    // hostName: "git.example.com",
    // rootCA: "/etc/dex/ca.crt",
}, { provider });
```

### Google Connector

```typescript
const googleConnector = new dex.GoogleConnector("google", {
    connectorId: "google",
    name: "Google",
    clientId: "your-google-client-id",
    clientSecret: "your-google-client-secret",
    redirectUri: "https://dex.example.com/callback",
    promptType: "consent", // Optional: default is "consent"
    hostedDomains: ["example.com"], // Optional: domain whitelist for G Suite
    groups: ["admins@example.com"], // Optional: group whitelist for G Suite
    // For group fetching:
    // serviceAccountFilePath: "/path/to/googleAuth.json",
    // domainToAdminEmail: {
    //     "*": "super-user@example.com",
    //     "my-domain.com": "super-user@my-domain.com"
    // },
}, { provider });
```

### Password Login (LocalConnector)

```typescript
// Adds a login with the passwords stored in Dex's password database.
// Remove the resource to turn this password login off.
const passwordLogin = new dex.LocalConnector("passwords", {
    connectorId: "passwords", // "local" is Dex's built-in password login when enablePasswordDB is true
    name: "Email and password",
}, { provider });
```

### Password Users (LocalUser)

```typescript
// A user in Dex's password database. Dex needs a password login for it:
// enablePasswordDB in its configuration, or a LocalConnector.
const alice = new dex.LocalUser("alice", {
    email: "alice@example.com",       // identifies the user; cannot change
    userId: "8f3e6d1c-alice",          // subject of the user's tokens; cannot change
    username: "Alice",
    passwordHash: pulumi.secret(process.env.ALICE_PASSWORD_HASH!), // bcrypt, never the password
    groups: ["dex-users"],             // needs Dex v2.46 or later
}, { provider });
```

Create the bcrypt hash outside Pulumi, so the password itself never reaches the program or the state, for example with `htpasswd -nBC 12 "" | tr -d ':\n'` (prompts for the password) or `python3 -c 'import bcrypt, getpass; print(bcrypt.hashpw(getpass.getpass().encode(), bcrypt.gensalt(12)).decode())'`. Dex accepts `$2a$`, `$2b$` and `$2y$` hashes with cost 10 to 16.

- Dex does not return the hash, so the provider keeps the last one you supplied; a password changed directly in Dex is not detected. A new `passwordHash` changes the password.
- Dex cannot change a user's `email` or `userId`, remove its `username`, or remove all its `groups`; such changes fail in preview. To change the email or userId, add a new `LocalUser` and remove the old one.
- `pulumi import dex:resources:LocalUser <name> <email>` imports a user without its password hash. The program can omit `passwordHash` afterwards; updates then leave the password unchanged.
- Deleting a `LocalUser` removes only its password record. Sessions and tokens that Dex already issued stay valid until they expire.

### Generic Connector (OIDC)

```typescript
const genericOidcConnector = new dex.Connector("github-oidc", {
    connectorId: "github-oidc",
    type: "oidc",
    name: "GitHub OIDC",
    oidcConfig: {
        issuer: "https://token.actions.githubusercontent.com",
        clientId: "your-github-oidc-client-id",
        clientSecret: "your-secret",
        redirectUri: "https://dex.example.com/callback",
        scopes: ["openid", "email", "profile"],
    },
}, { provider });
```

### Generic Connector (Raw JSON)

```typescript
const githubConnector = new dex.Connector("github", {
    connectorId: "github",
    type: "github",
    name: "GitHub",
    rawConfig: JSON.stringify({
        clientID: "your-github-client-id",
        clientSecret: "your-github-client-secret",
        redirectURI: "https://dex.example.com/callback",
        orgs: ["kotaicode"],
    }),
}, { provider });
```

## Looking Up Dex Objects

Three functions read one object from Dex without managing it: `getClient({ id })`, `getConnector({ id })` (the generic shape: type, name and the configuration as raw JSON) and `getLocalUser({ email })`. They never write to Dex and never take over an object. If the object does not exist, they fail with an error naming the ID; other failures, such as a refused or lost connection, are reported as such.

```typescript
const web = dex.resources.getClientOutput({ id: "my-web-app" }, { provider });
export const webRedirects = web.redirectUris;
```

The client secret (`getClient`) and the connector configuration (`getConnector`) are returned as secrets. Use the `...Output` form (`getClientOutput`, `get_client_output`, `LookupClientOutput`) to keep them secret; the plain form (`getClient`, which returns a promise or value) hands back plain values, as Pulumi does for every function. In Pulumi YAML, `fn::invoke` marks the whole result secret. `getLocalUser` returns no password hash: Dex does not return it.

## Importing Existing Dex Objects

Creating a resource whose ID already exists in Dex fails with a conflict, even when the existing object has an identical configuration. The provider never takes over an object it did not create: an equal ID is no proof that this stack owns it, and taking it over would let this stack delete an object that another stack or another tool manages. The error names the command to run instead:

```bash
pulumi import <type> <name> <id>
# for example
pulumi import dex:resources:Client webClient my-web-app
pulumi import dex:resources:GitHubConnector github github
```

| Resource | Type token |
| --- | --- |
| Client | `dex:resources:Client` |
| Generic connector (any Dex connector type) | `dex:resources:Connector` |
| Azure/Entra ID (generic OIDC) | `dex:resources:AzureOidcConnector` |
| Azure/Entra ID (Microsoft connector) | `dex:resources:AzureMicrosoftConnector` |
| AWS Cognito | `dex:resources:CognitoOidcConnector` |
| GitHub | `dex:resources:GitHubConnector` |
| GitLab | `dex:resources:GitLabConnector` |
| Google | `dex:resources:GoogleConnector` |
| Password login | `dex:resources:LocalConnector` |
| Password user | `dex:resources:LocalUser` (import ID: the email) |

- Import only reads from Dex; it never creates or changes the object. Run `pulumi up` afterwards to apply your program's settings.
- A typed connector resource only imports a connector of the matching Dex type (for example `github` for `GitHubConnector`). Import a connector of any other type with `dex:resources:Connector`.
- A generic `dex:resources:Connector` of type `oidc` is imported as `oidcConfig` when every stored field fits it (fields without a typed input go into `oidcConfig.extra`). Otherwise, for example when a known field has another type, it is imported as `rawConfig` with the stored configuration unchanged. Connectors of every other type are imported as `rawConfig`.
- If a create times out or the connection drops, the object may or may not have been created. The error says so; check whether the ID exists in Dex and import it instead of running `pulumi up` again.
- `pulumi import` prints the imported credentials in the generated program; see [Security Considerations](#security-considerations).

## Resources

### `dex.Client`

Manages an OAuth2 client in Dex.

**Inputs:**
- `clientId` (string, required) - Unique identifier for the client
- `name` (string, required) - Display name
- `secret` (string, optional, secret) - Client secret (auto-generated if omitted)
- `redirectUris` (string[], required) - Allowed redirect URIs
- `trustedPeers` (string[], optional) - Trusted peer client IDs
- `public` (boolean, optional) - Public (non-confidential) client
- `logoUrl` (string, optional) - Logo image URL

**Note:** Dex's API cannot clear `logoUrl`, `trustedPeers`, `redirectUris` or `name`; it ignores empty values. An update that would remove one of them fails in preview instead of reporting a change that Dex never applies. To remove such a value, delete the client and create it again; this generates a new secret unless `secret` is set.

**Changes Dex cannot apply in place:**

| Change | What happens |
| --- | --- |
| `clientId` (Client) or `connectorId` (all connectors) | Replacement: the object with the new ID is created, then the old one is deleted. A replaced client gets a new secret unless `secret` is set. |
| `secret` set to a different value | Fails in preview. Dex cannot change a client's secret. To rotate it, create a client with a new client ID, move its users to the new client, then remove the old one. An omitted, generated secret stays unchanged. |
| `public` | Fails in preview, for the same reason and with the same remedy. |
| connector `type` and settings stored in a connector's config (for example `tenant`, `tenantId`, `region`, `userPoolId`, `hostName`, `baseURL`) | Updated in place: Dex replaces the connector's type and config. |

**Outputs:**
- `id` - Resource ID (same as clientId)
- `clientId` - The client ID
- `secret` - The client secret (Pulumi secret)
- `createdAt` - Creation timestamp

### `dex.Connector`

Manages a generic connector in Dex.

**Inputs:**
- `connectorId` (string, required) - Unique identifier
- `type` (string, required) - Connector type (e.g., "oidc", "ldap", "saml", "github")
- `name` (string, required) - Display name
- `oidcConfig` (OIDCConfig, optional) - OIDC configuration (use when type="oidc"). Its `clientSecret` and `extra` fields are secret, and the whole object is a secret output.
- `rawConfig` (string, optional, secret) - Raw JSON configuration (for non-OIDC connectors)

**Note:** Exactly one of `oidcConfig` or `rawConfig` must be provided; preview fails otherwise. `oidcConfig` is only valid with `type: "oidc"`. `oidcConfig.extra` cannot contain keys that a typed `oidcConfig` field sets, in any spelling or letter case (for example `clientID` or `redirectURI`): Dex reads config keys case-insensitively, so such a key would replace the typed value.

### `dex.AzureOidcConnector`

Manages an Azure AD/Entra ID connector using generic OIDC.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `tenantId` (string, required) - Azure tenant ID (UUID)
- `clientId` (string, required) - Azure app client ID
- `clientSecret` (string, required, secret) - Azure app client secret
- `redirectUri` (string, required)
- `scopes` (string[], optional) - Defaults to `["openid", "profile", "email", "offline_access"]`
- `userNameSource` (string, optional) - "preferred_username" (default), "upn", or "email"
- `extraOidc` (map, optional, secret) - Additional OIDC config fields. Keys that typed inputs set (`issuer`, `clientID`, `clientSecret`, `redirectURI`, `scopes`, `userNameKey`, in any letter case) are rejected

### `dex.AzureMicrosoftConnector`

Manages an Azure AD/Entra ID connector using Dex's Microsoft-specific connector.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `tenant` (string, required) - "common", "organizations", or tenant ID (UUID)
- `clientId` (string, required)
- `clientSecret` (string, required, secret)
- `redirectUri` (string, required)
- `groups` (string, optional) - Group claim name (requires admin consent)

### `dex.CognitoOidcConnector`

Manages an AWS Cognito user pool connector.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `region` (string, required) - AWS region (e.g., "eu-central-1")
- `userPoolId` (string, required) - Cognito user pool ID
- `clientId` (string, required) - Cognito app client ID
- `clientSecret` (string, required, secret) - Cognito app client secret
- `redirectUri` (string, required)
- `scopes` (string[], optional) - Defaults to `["openid", "email", "profile"]`
- `userNameSource` (string, optional) - "email" (default) or "sub"
- `extraOidc` (map, optional, secret) - Additional OIDC config fields. Keys that typed inputs set (`issuer`, `clientID`, `clientSecret`, `redirectURI`, `scopes`, `userNameKey`, in any letter case) are rejected

### `dex.GitLabConnector`

Manages a GitLab connector in Dex.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `clientId` (string, required) - GitLab application client ID
- `clientSecret` (string, required, secret) - GitLab application client secret
- `redirectUri` (string, required)
- `baseURL` (string, optional) - GitLab instance URL, defaults to `https://gitlab.com`
- `groups` (string[], optional) - Groups whitelist
- `useLoginAsID` (bool, optional) - Use username as ID instead of internal ID, default: `false`
- `getGroupsPermission` (bool, optional) - Include group permissions in groups claim, default: `false`

### `dex.GitHubConnector`

Manages a GitHub connector in Dex.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `clientId` (string, required) - GitHub OAuth app client ID
- `clientSecret` (string, required, secret) - GitHub OAuth app client secret
- `redirectUri` (string, required)
- `orgs` (GitHubOrg[], optional) - List of organizations and teams
  - `name` (string, required) - Organization name
  - `teams` (string[], optional) - Team names within the organization
- `loadAllGroups` (bool, optional) - Load all user orgs/teams, default: `false`
- `teamNameField` (string, optional) - "name", "slug", or "both", default: "slug"
- `useLoginAsID` (bool, optional) - Use username as ID, default: `false`
- `preferredEmailDomain` (string, optional) - Preferred email domain
- `hostName` (string, optional) - GitHub Enterprise hostname
- `rootCA` (string, optional) - Root CA certificate path for GitHub Enterprise

### `dex.GoogleConnector`

Manages a Google connector in Dex.

**Inputs:**
- `connectorId` (string, required)
- `name` (string, required)
- `clientId` (string, required) - Google OAuth client ID
- `clientSecret` (string, required, secret) - Google OAuth client secret
- `redirectUri` (string, required)
- `promptType` (string, optional) - OIDC prompt parameter, default: "consent"
- `hostedDomains` (string[], optional) - Domain whitelist for G Suite
- `groups` (string[], optional) - Group whitelist for G Suite
- `serviceAccountFilePath` (string, optional) - Service account JSON file path for group fetching
- `domainToAdminEmail` (map[string]string, optional) - Domain to admin email mapping for group fetching

### `dex.LocalConnector`

Adds a password login to Dex: a connector of type `local`, through which users log in with the passwords stored in Dex's password database.

**Inputs:**
- `connectorId` (string, required) - `local` is reserved for Dex's built-in password login when `enablePasswordDB: true`; creating it fails with an error that says so
- `name` (string, required)
- `enabled` (bool, optional, deprecated) - Has no effect in Dex; `false` fails in preview

**Note:** Dex offers password login through every connector of type `local`, even when `enablePasswordDB` is `false` in its configuration. There is no disabled state: to turn this password login off, remove the resource. Removing it does not delete password users. Users are managed separately, through Dex's static passwords or its gRPC API.

### `dex.LocalUser`

Manages a user in Dex's password database.

**Inputs:**
- `email` (string, required) - Login email; identifies the user and is stored in lower case. Cannot change
- `userId` (string, required) - Stable user ID, the subject of the user's tokens. Cannot change
- `username` (string, optional) - Display name. Can change, but cannot be removed once set
- `passwordHash` (string, secret) - bcrypt hash of the password. Required to create the user; optional after import
- `groups` (string[], optional) - Groups added to the user's tokens. Needs Dex v2.46; cannot be removed entirely once set

## Local Development and Testing

### Running Dex Locally with Docker Compose

See `docker-compose.yml` for a local Dex setup with gRPC API enabled.

```bash
# Start Dex
docker-compose up -d

# Dex gRPC will be available at localhost:5557
# Dex web UI will be available at http://localhost:5556
```

### Example Pulumi Program

See the `examples/` directory for complete example programs.

## Dex Configuration Requirements

Your Dex instance must have the gRPC API enabled. Add this to your Dex configuration:

```yaml
grpc:
  addr: 127.0.0.1:5557
  tlsCert: /etc/dex/grpc.crt
  tlsKey: /etc/dex/grpc.key
  tlsClientCA: /etc/dex/client.crt
  reflection: true

# Enable connector CRUD (required for connector management)
enablePasswordDB: false
```

And set the environment variable:
```bash
export DEX_API_CONNECTORS_CRUD=true
```

## Security Considerations

- **Secrets**: Client secrets, TLS keys and opaque configuration that may contain credentials (`rawConfig`, `oidcConfig`, `extraOidc`) are marked as Pulumi secrets and encrypted in state, even when supplied as plain values or read back by `pulumi refresh` / `pulumi import`.
- **Importing**: `pulumi import` prints a program that reproduces the imported inputs, and that program includes the client and connector credentials read from Dex. Pulumi marks them `secret`, but the values are still in plain text. Use `pulumi import --generate-code=false` to print no code, or `--out <file>` to write it to a file instead of the console, and treat that file as sensitive. State is protected either way.
- **mTLS**: Strongly recommended for production use. Configure TLS certificates properly
- **Network**: Ensure Dex gRPC API is only accessible from trusted networks

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Dex Version Compatibility

This provider has been tested with:
- **Dex v2.4.0+** (with `DEX_API_CONNECTORS_CRUD=true`)

The provider requires:
- Dex gRPC API enabled
- `DEX_API_CONNECTORS_CRUD=true` environment variable set on Dex (required for connector CRUD operations)

For older Dex versions, connector management may not be available. Client management should work with any Dex version that exposes the gRPC API.

## Development

### Prerequisites
- Go 1.24.1+
- Pulumi CLI
- Docker and Docker Compose (for local testing)

### Building

```bash
make build
```

### Running Tests

```bash
# Unit tests
make test

# Integration tests (requires Dex running)
make dex-up
make test  # Run tests with integration tag
make dex-down
```

### Code Quality

```bash
# Run linter
golangci-lint run

# Format code
go fmt ./...
```

## Contributing

Contributions are welcome! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

[License TBD - Add MIT or Apache 2.0]

## Support

- **GitHub Issues**: https://github.com/kotaicode/pulumi-dex/issues
- **Documentation**: https://github.com/kotaicode/pulumi-dex#readme

