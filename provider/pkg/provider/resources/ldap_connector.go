package resources

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// ============================================================================
// LdapConnector - LDAP connector (type: "ldap")
// ============================================================================

// LdapUserSearch configures how Dex finds the user who logs in.
type LdapUserSearch struct {
	BaseDn                string   `pulumi:"baseDn"`
	Filter                *string  `pulumi:"filter,optional"`
	UsernameAttributes    []string `pulumi:"usernameAttributes"`
	Scope                 *string  `pulumi:"scope,optional"`
	IdAttr                *string  `pulumi:"idAttr,optional"`
	EmailAttr             *string  `pulumi:"emailAttr,optional"`
	NameAttr              *string  `pulumi:"nameAttr,optional"`
	PreferredUsernameAttr *string  `pulumi:"preferredUsernameAttr,optional"`
	EmailSuffix           *string  `pulumi:"emailSuffix,optional"`
}

// LdapUserMatcher matches a user to a group by an attribute of each.
type LdapUserMatcher struct {
	UserAttr           string  `pulumi:"userAttr"`
	GroupAttr          string  `pulumi:"groupAttr"`
	RecursionGroupAttr *string `pulumi:"recursionGroupAttr,optional"`
}

// LdapGroupSearch configures how Dex finds the groups of a user.
type LdapGroupSearch struct {
	BaseDn       string            `pulumi:"baseDn"`
	Filter       *string           `pulumi:"filter,optional"`
	Scope        *string           `pulumi:"scope,optional"`
	UserMatchers []LdapUserMatcher `pulumi:"userMatchers"`
	NameAttr     string            `pulumi:"nameAttr"`
}

// LdapConnectorArgs defines inputs for LdapConnector.
type LdapConnectorArgs struct {
	ConnectorId        string           `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name               string           `pulumi:"name"`
	Host               string           `pulumi:"host"`
	InsecureNoSsl      *bool            `pulumi:"insecureNoSsl,optional"`
	InsecureSkipVerify *bool            `pulumi:"insecureSkipVerify,optional"`
	StartTls           *bool            `pulumi:"startTls,optional"`
	RootCa             *string          `pulumi:"rootCa,optional"`
	RootCaPem          *string          `pulumi:"rootCaPem,optional"`
	ClientCert         *string          `pulumi:"clientCert,optional"`
	ClientKey          *string          `pulumi:"clientKey,optional"`
	BindDn             *string          `pulumi:"bindDn,optional"`
	BindPassword       *string          `pulumi:"bindPassword,optional" provider:"secret"`
	UsernamePrompt     *string          `pulumi:"usernamePrompt,optional"`
	UserSearch         LdapUserSearch   `pulumi:"userSearch"`
	GroupSearch        *LdapGroupSearch `pulumi:"groupSearch,optional"`
}

// LdapConnectorState defines outputs for LdapConnector.
type LdapConnectorState struct {
	LdapConnectorArgs
}

// LdapConnector manages an LDAP connector in Dex.
type LdapConnector struct{}

// Annotate provides schema metadata.
func (c *LdapConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages an LDAP connector in Dex (type: ldap). Users log in with their directory username and "+
		"password; Dex looks the user up, checks the password with a bind as that user, and reads the user's groups. "+
		"Dex checks this configuration only when someone logs in, so test a login after every change. "+
		"Settings this resource does not model, such as kerberos, make refresh and import fail; "+
		"manage such a connector with dex.Connector and rawConfig.")
}

// Annotate provides schema metadata for LdapConnectorArgs.
func (c *LdapConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the LDAP connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.Host, "Host and port of the LDAP server, e.g. 'ldap.example.com:636'. Without a port, Dex uses "+
		"389 when insecureNoSsl is true and 636 otherwise, also with startTls; startTls therefore requires a port.")
	a.Describe(&c.InsecureNoSsl, "If true, connects without TLS. Passwords then cross the network in plain text; "+
		"only for an LDAP server on a trusted network.")
	a.Describe(&c.InsecureSkipVerify, "If true, does not verify the LDAP server's certificate. Not recommended for production.")
	a.Describe(&c.StartTls, "If true, connects without TLS and then upgrades the connection with StartTLS. "+
		"Otherwise Dex connects with TLS (LDAPS). Requires a port in host.")
	a.Describe(&c.RootCa, "Path, on the Dex host, of a PEM file with the CA certificates that sign the LDAP server's "+
		"certificate. Use either rootCa or rootCaPem.")
	a.Describe(&c.RootCaPem, "PEM-encoded CA certificates that sign the LDAP server's certificate, sent inline as "+
		"Dex's rootCAData. Use either rootCa or rootCaPem.")
	a.Describe(&c.ClientCert, "Path, on the Dex host, of a client certificate for the LDAP server. Requires clientKey.")
	a.Describe(&c.ClientKey, "Path, on the Dex host, of the private key for clientCert. Requires clientCert.")
	a.Describe(&c.BindDn, "DN of the account that Dex binds as to search for users and groups. Requires "+
		"bindPassword. Without bindDn and bindPassword, Dex searches anonymously.")
	a.Describe(&c.BindPassword, "Password of bindDn (Dex's bindPW). Dex uses it as written.")
	a.Describe(&c.UsernamePrompt, "Label of the username field on Dex's login page, e.g. 'Email Address'. Defaults to 'Username'.")
	a.Describe(&c.UserSearch, "How Dex finds the user who logs in.")
	a.Describe(&c.GroupSearch, "How Dex finds the user's groups. Without it, tokens carry no groups.")
}

// Annotate provides schema metadata for LdapUserSearch.
func (c *LdapUserSearch) Annotate(a infer.Annotator) {
	a.Describe(&c.BaseDn, "DN to search users under, e.g. 'ou=people,dc=example,dc=org'.")
	a.Describe(&c.Filter, "LDAP filter that users must match, e.g. '(objectClass=person)'.")
	a.Describe(&c.UsernameAttributes, "Attributes that the username entered at login is matched against, e.g. ['uid']. "+
		"With several, a user matches through any of them; that requires Dex v2.46 or later. "+
		"A username that matches more than one entry cannot log in.")
	a.Describe(&c.Scope, "Search scope: 'sub' (the whole subtree, Dex's default) or 'one' (one level).")
	a.Describe(&c.IdAttr, "Attribute that becomes the user ID in Dex, e.g. 'uid' (Dex's default). Changing it "+
		"changes the identity of every user in the applications that use Dex.")
	a.Describe(&c.EmailAttr, "Attribute that becomes the email claim. Defaults to 'mail'.")
	a.Describe(&c.NameAttr, "Attribute that becomes the name claim, e.g. 'cn'.")
	a.Describe(&c.PreferredUsernameAttr, "Attribute that becomes the preferred_username claim.")
	a.Describe(&c.EmailSuffix, "If set, the email claim is the idAttr value, '@' and this suffix, e.g. 'example.org'.")
}

// Annotate provides schema metadata for LdapGroupSearch.
func (c *LdapGroupSearch) Annotate(a infer.Annotator) {
	a.Describe(&c.BaseDn, "DN to search groups under, e.g. 'ou=groups,dc=example,dc=org'.")
	a.Describe(&c.Filter, "LDAP filter that groups must match, e.g. '(objectClass=groupOfNames)'.")
	a.Describe(&c.Scope, "Search scope: 'sub' (the whole subtree, Dex's default) or 'one' (one level).")
	a.Describe(&c.UserMatchers, "How a group is matched to the user. A group belongs to the user if it matches any entry.")
	a.Describe(&c.NameAttr, "Attribute of the group that becomes the group name in the groups claim, e.g. 'cn'.")
}

// Annotate provides schema metadata for LdapUserMatcher.
func (c *LdapUserMatcher) Annotate(a infer.Annotator) {
	a.Describe(&c.UserAttr, "Attribute of the user whose value the group must hold, e.g. 'DN' or 'uid'.")
	a.Describe(&c.GroupAttr, "Attribute of the group that holds the user's value, e.g. 'member' or 'memberUid'.")
	a.Describe(&c.RecursionGroupAttr, "If set, groups whose attribute of this name holds the DN of one of the "+
		"user's groups are the user's groups too, at any depth, e.g. 'member'.")
}

// Annotate provides schema metadata for LdapConnectorState.
func (c *LdapConnectorState) Annotate(a infer.Annotator) {
	// LdapConnectorState embeds LdapConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *LdapConnector) WireDependencies(f infer.FieldSelector, _ *LdapConnectorArgs, state *LdapConnectorState) {
	f.OutputField(&state.BindPassword).AlwaysSecret()
}

// Check enforces at preview the rules that Dex itself checks only at login.
func (c *LdapConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[LdapConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[LdapConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[LdapConnectorArgs]{Failures: failures}, err
	}
	failures = append(failures, checkLdapInputs(req.NewInputs)...)
	return infer.CheckResponse[LdapConnectorArgs]{Inputs: args, Failures: failures}, nil
}

// checkLdapInputs judges only known values and names properties, never values.
func checkLdapInputs(inputs property.Map) []p.CheckFailure {
	known := func(m property.Map, key string) (property.Value, bool) {
		v, ok := m.GetOk(key)
		return v, ok && !v.IsComputed() && !v.IsNull()
	}
	isTrue := func(key string) bool {
		v, ok := known(inputs, key)
		return ok && v.IsBool() && v.AsBool()
	}
	// given reports whether a string is set (true), omitted or empty (false), or
	// still unknown (ok false).
	given := func(key string) (set, ok bool) {
		v, present := inputs.GetOk(key)
		if !present || v.IsNull() {
			return false, true
		}
		if v.IsComputed() || !v.IsString() {
			return false, false
		}
		return v.AsString() != "", true
	}
	var failures []p.CheckFailure
	fail := func(property, reason string) {
		failures = append(failures, p.CheckFailure{Property: property, Reason: reason})
	}

	host, hostKnown := known(inputs, "host")
	if hostKnown && host.IsString() && host.AsString() == "" {
		fail("host", "host is required: the LDAP server's host and port, e.g. ldap.example.com:636")
	}
	if us, ok := known(inputs, "userSearch"); ok && us.IsMap() {
		if v, ok := known(us.AsMap(), "baseDn"); ok && v.IsString() && v.AsString() == "" {
			fail("userSearch.baseDn", "userSearch.baseDn is required: the DN to search users under")
		}
		if v, ok := known(us.AsMap(), "usernameAttributes"); ok && v.IsArray() && v.AsArray().Len() == 0 {
			fail("userSearch.usernameAttributes", "userSearch.usernameAttributes needs at least one attribute, e.g. uid")
		}
	}

	noSsl := isTrue("insecureNoSsl")
	if noSsl && isTrue("startTls") {
		fail("startTls", "startTls and insecureNoSsl exclude each other: startTls upgrades the connection to TLS, "+
			"insecureNoSsl keeps it without TLS")
	}
	if noSsl && isTrue("insecureSkipVerify") {
		fail("insecureSkipVerify", "insecureSkipVerify has no effect with insecureNoSsl, which connects without TLS")
	}
	if isTrue("startTls") && hostKnown && host.IsString() && host.AsString() != "" {
		if _, _, err := net.SplitHostPort(host.AsString()); err != nil {
			fail("host", "startTls requires a port in host: without one, Dex connects to port 636, the port for "+
				"LDAP over TLS, and StartTLS fails there; add the port of the server's StartTLS listener, usually 389")
		}
	}

	rootCa, rootCaOk := given("rootCa")
	rootCaPem, rootCaPemOk := given("rootCaPem")
	if rootCaOk && rootCaPemOk && rootCa && rootCaPem {
		fail("rootCaPem", "set either rootCa (a file on the Dex host) or rootCaPem (inline), not both")
	}
	if rootCaPemOk && rootCaPem && !x509.NewCertPool().AppendCertsFromPEM([]byte(inputs.Get("rootCaPem").AsString())) {
		fail("rootCaPem", "rootCaPem contains no PEM-encoded certificate")
	}

	bothOrNeither := func(a, b, reason string) {
		setA, okA := given(a)
		setB, okB := given(b)
		if okA && okB && setA != setB {
			missing := a
			if setA {
				missing = b
			}
			fail(missing, reason)
		}
	}
	bothOrNeither("clientCert", "clientKey", "clientCert and clientKey must be set together")
	bothOrNeither("bindDn", "bindPassword", "bindDn and bindPassword must be set together; "+
		"leave both out to search anonymously")
	return failures
}

func (c *LdapConnector) controller() connectorController[LdapConnectorArgs, LdapConnectorState] {
	return connectorController[LdapConnectorArgs, LdapConnectorState]{
		kind:    "ldap-connector",
		token:   tokenLdapConnector,
		dexType: "ldap",
		id:      func(a LdapConnectorArgs) string { return a.ConnectorId },
		name:    func(a LdapConnectorArgs) string { return a.Name },
		state:   func(a LdapConnectorArgs) LdapConnectorState { return LdapConnectorState{LdapConnectorArgs: a} },
		args:    func(s LdapConnectorState) LdapConnectorArgs { return s.LdapConnectorArgs },
		encode:  encodeLdapConfig,
		decode:  decodeLdapConfig,
		requires: func(cfg provider.DexConfig, args LdapConnectorArgs) error {
			if len(args.UserSearch.UsernameAttributes) > 1 {
				return cfg.Require(provider.LdapUsernameAttributes)
			}
			return nil
		},
	}
}

// Keys of Dex's LDAP config that LdapConnector models, by object.
var (
	ldapConfigKeys = []string{"host", "insecureNoSSL", "insecureSkipVerify", "startTLS", "rootCA", "rootCAData",
		"clientCert", "clientKey", "bindDN", "bindPW", "usernamePrompt", "userSearch", "groupSearch"}
	ldapUserSearchKeys = []string{"baseDN", "filter", "username", "scope", "idAttr", "emailAttr", "nameAttr",
		"preferredUsernameAttr", "emailSuffix"}
	ldapGroupSearchKeys = []string{"baseDN", "filter", "scope", "userMatchers", "nameAttr"}
	ldapUserMatcherKeys = []string{"userAttr", "groupAttr", "recursionGroupAttr"}
)

// encodeLdapConfig writes the fields that are set, under Dex's keys. One
// username attribute is written as a string, which both Dex lines read;
// several as a list, which needs Dex v2.46.
func encodeLdapConfig(args LdapConnectorArgs) ([]byte, error) {
	config := map[string]any{"host": args.Host}
	setStr := func(m map[string]any, key string, v *string) {
		if v != nil {
			m[key] = *v
		}
	}
	setBool := func(key string, v *bool) {
		if v != nil {
			config[key] = *v
		}
	}
	setBool("insecureNoSSL", args.InsecureNoSsl)
	setBool("insecureSkipVerify", args.InsecureSkipVerify)
	setBool("startTLS", args.StartTls)
	setStr(config, "rootCA", args.RootCa)
	if args.RootCaPem != nil {
		config["rootCAData"] = []byte(*args.RootCaPem) // base64 in JSON, as Dex reads it
	}
	setStr(config, "clientCert", args.ClientCert)
	setStr(config, "clientKey", args.ClientKey)
	setStr(config, "bindDN", args.BindDn)
	setStr(config, "bindPW", args.BindPassword)
	setStr(config, "usernamePrompt", args.UsernamePrompt)

	us := args.UserSearch
	userSearch := map[string]any{"baseDN": us.BaseDn}
	if len(us.UsernameAttributes) == 1 {
		userSearch["username"] = us.UsernameAttributes[0]
	} else {
		userSearch["username"] = us.UsernameAttributes
	}
	setStr(userSearch, "filter", us.Filter)
	setStr(userSearch, "scope", us.Scope)
	setStr(userSearch, "idAttr", us.IdAttr)
	setStr(userSearch, "emailAttr", us.EmailAttr)
	setStr(userSearch, "nameAttr", us.NameAttr)
	setStr(userSearch, "preferredUsernameAttr", us.PreferredUsernameAttr)
	setStr(userSearch, "emailSuffix", us.EmailSuffix)
	config["userSearch"] = userSearch

	if gs := args.GroupSearch; gs != nil {
		matchers := make([]map[string]any, 0, len(gs.UserMatchers))
		for _, m := range gs.UserMatchers {
			matcher := map[string]any{"userAttr": m.UserAttr, "groupAttr": m.GroupAttr}
			setStr(matcher, "recursionGroupAttr", m.RecursionGroupAttr)
			matchers = append(matchers, matcher)
		}
		groupSearch := map[string]any{"baseDN": gs.BaseDn, "userMatchers": matchers, "nameAttr": gs.NameAttr}
		setStr(groupSearch, "filter", gs.Filter)
		setStr(groupSearch, "scope", gs.Scope)
		config["groupSearch"] = groupSearch
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal LDAP config: %w", err)
	}
	return configBytes, nil
}

// decodeLdapConfig rebuilds the inputs and fails on any setting that the
// resource does not model: dropping it would remove it from Dex on the next
// update. The error names the settings, never their values.
func decodeLdapConfig(found *api.Connector, _ LdapConnectorArgs) (LdapConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return LdapConnectorArgs{}, err
	}
	unknown := conf.unknownKeys(ldapConfigKeys...)
	args := LdapConnectorArgs{
		ConnectorId:        found.Id,
		Name:               found.Name,
		Host:               conf.str("host"),
		InsecureNoSsl:      conf.boolPtr("insecureNoSSL"),
		InsecureSkipVerify: conf.boolPtr("insecureSkipVerify"),
		StartTls:           conf.boolPtr("startTLS"),
		RootCa:             conf.optStr("rootCA"),
		ClientCert:         conf.optStr("clientCert"),
		ClientKey:          conf.optStr("clientKey"),
		BindDn:             conf.optStr("bindDN"),
		BindPassword:       conf.optStr("bindPW"),
		UsernamePrompt:     conf.optStr("usernamePrompt"),
	}
	if data := conf.optStr("rootCAData"); data != nil {
		pem, err := base64.StdEncoding.DecodeString(*data)
		if err != nil {
			return LdapConnectorArgs{}, errors.New("config.rootCAData is not base64-encoded data")
		}
		s := string(pem)
		args.RootCaPem = &s
	}
	if us := conf.object("userSearch"); us != nil {
		unknown = append(unknown, us.unknownKeys(ldapUserSearchKeys...)...)
		args.UserSearch = LdapUserSearch{
			BaseDn:                us.str("baseDN"),
			Filter:                us.optStr("filter"),
			Scope:                 us.optStr("scope"),
			IdAttr:                us.optStr("idAttr"),
			EmailAttr:             us.optStr("emailAttr"),
			NameAttr:              us.optStr("nameAttr"),
			PreferredUsernameAttr: us.optStr("preferredUsernameAttr"),
			EmailSuffix:           us.optStr("emailSuffix"),
		}
		// Dex v2.46 reads a string or a list; v2.45 only a string.
		if s, ok := us.values["username"].(string); ok {
			args.UserSearch.UsernameAttributes = []string{s}
		} else {
			args.UserSearch.UsernameAttributes = us.strList("username")
		}
	}
	if gs := conf.object("groupSearch"); gs != nil {
		unknown = append(unknown, gs.unknownKeys(ldapGroupSearchKeys...)...)
		args.GroupSearch = &LdapGroupSearch{
			BaseDn:   gs.str("baseDN"),
			Filter:   gs.optStr("filter"),
			Scope:    gs.optStr("scope"),
			NameAttr: gs.str("nameAttr"),
		}
		for _, m := range gs.objects("userMatchers") {
			unknown = append(unknown, m.unknownKeys(ldapUserMatcherKeys...)...)
			args.GroupSearch.UserMatchers = append(args.GroupSearch.UserMatchers, LdapUserMatcher{
				UserAttr:           m.str("userAttr"),
				GroupAttr:          m.str("groupAttr"),
				RecursionGroupAttr: m.optStr("recursionGroupAttr"),
			})
		}
	}
	if err := conf.Err(); err != nil {
		return LdapConnectorArgs{}, err
	}
	if len(unknown) > 0 {
		return LdapConnectorArgs{}, fmt.Errorf("it has settings that %s does not model: %s. Updating the connector "+
			"through this resource would remove them from Dex; manage it with %s and rawConfig instead, "+
			"or remove these settings in Dex", tokenLdapConnector, strings.Join(unknown, ", "), tokenConnector)
	}
	return args, nil
}

// Create creates a new LDAP connector.
func (c *LdapConnector) Create(ctx context.Context, req infer.CreateRequest[LdapConnectorArgs]) (infer.CreateResponse[LdapConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing LDAP connector.
func (c *LdapConnector) Read(ctx context.Context, req infer.ReadRequest[LdapConnectorArgs, LdapConnectorState]) (infer.ReadResponse[LdapConnectorArgs, LdapConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing LDAP connector.
func (c *LdapConnector) Update(ctx context.Context, req infer.UpdateRequest[LdapConnectorArgs, LdapConnectorState]) (infer.UpdateResponse[LdapConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes an LDAP connector.
func (c *LdapConnector) Delete(ctx context.Context, req infer.DeleteRequest[LdapConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
