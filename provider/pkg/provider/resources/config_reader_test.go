package resources

import (
	"strings"
	"testing"
)

func TestConnectorConfig_absentAndNullFieldsAreEmpty(t *testing.T) {
	conf, err := parseConnectorConfig([]byte(`{"s":null,"b":null,"l":null,"m":null,"o":null,"empty":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if conf.str("s") != "" || conf.str("missing") != "" || conf.strPtr("empty") != nil || conf.boolPtr("b") != nil ||
		conf.strList("l") != nil || conf.strMap("m") != nil || conf.objects("o") != nil {
		t.Error("absent or null fields must read as empty")
	}
	if err := conf.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConnectorConfig_reportsFirstProblemByPath(t *testing.T) {
	conf, err := parseConnectorConfig([]byte(`{"orgs":[{"name":"a","teams":["x",7]}],"flag":"yes","secret":"s3cr3t-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, org := range conf.objects("orgs") {
		_ = org.str("name")
		_ = org.strList("teams")
	}
	_ = conf.boolPtr("flag") // second problem: must not replace the first
	got := conf.Err()
	if got == nil || got.Error() != "config.orgs[0].teams[1] is a JSON number, expected a string" {
		t.Errorf("Err() = %v", got)
	}
}

func TestConnectorConfig_errorsNeverContainValues(t *testing.T) {
	const secret = "s3cr3t-value"
	for _, raw := range []string{
		`{"clientSecret":"` + secret,
		`["` + secret + `"]`,
		`"` + secret + `"`,
	} {
		_, err := parseConnectorConfig([]byte(raw))
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("parse(%d bytes): err = %v", len(raw), err)
		}
	}
	conf, err := parseConnectorConfig([]byte(`{"clientID":{"nested":"` + secret + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = conf.str("clientID")
	if err := conf.Err(); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("field error = %v", err)
	}
}
