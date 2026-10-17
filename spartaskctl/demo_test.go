package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveDemoSecrets(t *testing.T) {
	in := []byte(`{"tenant":{"id":"demo-mobilya"},"users":[{"email":"a@example.com","password":"env:DEMO_PASSWORD"},{"password":"literal-password"}],
	"data_sources":[{"port":5432,"host":"env: DEMO_DB_HOST","password":"env:DEMO_PASSWORD"}]}`)
	values := map[string]string{"DEMO_PASSWORD": "secret-value-1", "DEMO_DB_HOST": "10.0.0.5"}
	out, err := resolveDemoSecrets(in, func(n string) string { return values[n] })
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Users []struct {
			Password string `json:"password"`
		} `json:"users"`
		DataSources []struct {
			Port     json.Number `json:"port"`
			Host     string      `json:"host"`
			Password string      `json:"password"`
		} `json:"data_sources"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Users[0].Password != "secret-value-1" || doc.Users[1].Password != "literal-password" {
		t.Fatalf("users = %+v", doc.Users)
	}
	ds := doc.DataSources[0]
	if ds.Port != "5432" || ds.Host != "10.0.0.5" || ds.Password != "secret-value-1" {
		t.Fatalf("data source = %+v", ds)
	}
	if strings.Contains(string(out), "env:") {
		t.Fatalf("unresolved reference left: %s", out)
	}
}

func TestResolveDemoSecretsReportsEveryMissingVariable(t *testing.T) {
	in := []byte(`{"a":"env:B_VAR","b":["env:A_VAR","env:B_VAR"]}`)
	_, err := resolveDemoSecrets(in, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "A_VAR, B_VAR") {
		t.Fatalf("err = %v", err)
	}
	if _, err := resolveDemoSecrets([]byte(`{"a":1} {"b":2}`), func(string) string { return "" }); err == nil {
		t.Fatal("two JSON values must be refused")
	}
	if _, err := resolveDemoSecrets([]byte(`{`), func(string) string { return "" }); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
}
