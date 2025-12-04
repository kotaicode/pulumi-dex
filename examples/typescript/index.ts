import * as pulumi from "@pulumi/pulumi";
// Note: After generating the SDK, this import will work:
// import * as dex from "@kotaicode/pulumi-dex";

// For now, this is a template that shows how to use the provider
// Once SDKs are generated, uncomment and adjust the imports

/*
// Configure the Dex provider
const dexProvider = new dex.Provider("dex", {
    host: "localhost:5557", // Dex gRPC endpoint
    // For development with insecure Dex:
    insecureSkipVerify: true,
    // For production with mTLS:
    // caCert: fs.readFileSync("certs/ca.crt", "utf-8"),
    // clientCert: fs.readFileSync("certs/client.crt", "utf-8"),
    // clientKey: fs.readFileSync("certs/client.key", "utf-8"),
});

// Example 1: Create an OAuth2 client
const webClient = new dex.Client("webClient", {
    clientId: "my-web-app",
    name: "My Web App",
    redirectUris: ["http://localhost:3000/callback"],
    // secret is optional - will be auto-generated
}, { provider: dexProvider });

// Example 2: Create an Azure/Entra ID connector using generic OIDC
const azureConnector = new dex.AzureOidcConnector("azure-tenant", {
    connectorId: "azure-tenant",
    name: "Azure AD",
    tenantId: "your-tenant-id-here", // Replace with your Azure tenant ID
    clientId: "your-azure-app-client-id",
    clientSecret: "your-azure-app-client-secret",
    redirectUri: "http://localhost:5556/dex/callback",
    userNameSource: "preferred_username",
}, { provider: dexProvider });

// Example 3: Create an AWS Cognito connector
const cognitoConnector = new dex.CognitoOidcConnector("cognito", {
    connectorId: "cognito",
    name: "AWS Cognito",
    region: "us-east-1",
    userPoolId: "us-east-1_XXXXXXXXX", // Replace with your Cognito pool ID
    clientId: "your-cognito-client-id",
    clientSecret: "your-cognito-client-secret",
    redirectUri: "http://localhost:5556/dex/callback",
    userNameSource: "email",
}, { provider: dexProvider });

// Example 4: Create a generic OIDC connector
const genericOidcConnector = new dex.Connector("generic-oidc", {
    connectorId: "generic-oidc",
    type: "oidc",
    name: "Generic OIDC Provider",
    oidcConfig: {
        issuer: "https://example.com",
        clientId: "your-client-id",
        clientSecret: "your-client-secret",
        redirectUri: "http://localhost:5556/dex/callback",
        scopes: ["openid", "email", "profile"],
    },
}, { provider: dexProvider });

// Export outputs
export const webClientId = webClient.clientId;
export const webClientSecret = webClient.secret; // This is a Pulumi secret
export const azureConnectorId = azureConnector.id;
export const cognitoConnectorId = cognitoConnector.id;
*/

// Placeholder export until SDK is generated
export const placeholder = "Replace this with actual provider usage after generating SDKs";

