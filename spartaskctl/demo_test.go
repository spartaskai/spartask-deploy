package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDemoSecrets(t *testing.T) {
	in := []byte(`{"tenant":{"id":"demo-mobilya"},"users":[{"email":"a@example.com","password":"env:DEMO_PASSWORD"},{"password":"literal-password"}],
	"data_sources":[{"port":5432,"host":"env: DEMO_DB_HOST","password":"env:DEMO_PASSWORD"}]}`)
	values := map[string]string{"DEMO_PASSWORD": "secret-value-1", "DEMO_DB_HOST": "10.0.0.5"}
	out, generated, err := resolveDemoSecrets(in, func(n string) string { return values[n] }, noGenerate)
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
	if len(generated) != 0 {
		t.Fatalf("nothing should be generated when every variable is set: %v", generated)
	}
}

func TestResolveDemoSecretsReportsEveryMissingVariable(t *testing.T) {
	in := []byte(`{"a":"env:B_VAR","b":["env:A_VAR","env:B_VAR"],"users":[{"password":"env:C_VAR"}]}`)
	_, _, err := resolveDemoSecrets(in, func(string) string { return "" }, noGenerate)
	if err == nil || !strings.Contains(err.Error(), "A_VAR, B_VAR") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := resolveDemoSecrets([]byte(`{"a":1} {"b":2}`), func(string) string { return "" }, noGenerate); err == nil {
		t.Fatal("two JSON values must be refused")
	}
	if _, _, err := resolveDemoSecrets([]byte(`{`), func(string) string { return "" }, noGenerate); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
}

func noGenerate() (string, error) { return "", errors.New("generate must not be called") }

func TestResolveDemoSecretsGeneratesMissingUserPasswords(t *testing.T) {
	in := []byte(`{"users":[{"email":"a@example.com","password":"env:DEMO_ADMIN_PASSWORD"},
	{"email":"b@example.com","password":"env:DEMO_PASSWORD"},{"email":"c@example.com","password":"env:DEMO_PASSWORD"}],
	"data_sources":[{"host":"env:DEMO_DB_HOST","password":"env:DEMO_DB_PASSWORD"}]}`)
	values := map[string]string{"DEMO_DB_HOST": "10.0.0.5", "DEMO_DB_PASSWORD": "db-secret"}
	n := 0
	gen := func() (string, error) { n++; return fmt.Sprintf("generated-%d-password", n), nil }
	out, generated, err := resolveDemoSecrets(in, func(k string) string { return values[k] }, gen)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != 2 || generated["DEMO_ADMIN_PASSWORD"] == "" || generated["DEMO_PASSWORD"] == "" {
		t.Fatalf("generated = %v", generated)
	}
	var doc struct {
		Users []struct {
			Password string `json:"password"`
		} `json:"users"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Users[0].Password != generated["DEMO_ADMIN_PASSWORD"] ||
		doc.Users[1].Password != generated["DEMO_PASSWORD"] || doc.Users[2].Password != generated["DEMO_PASSWORD"] {
		t.Fatalf("users = %+v, generated = %v", doc.Users, generated)
	}
}

func TestResolveDemoSecretsNeverGeneratesOtherSecrets(t *testing.T) {
	for name, in := range map[string]string{
		"database password":         `{"users":[{"password":"env:P"}],"data_sources":[{"password":"env:DB"}]}`,
		"shared with a data source": `{"users":[{"password":"env:P"}],"data_sources":[{"password":"env:P"}]}`,
		"password outside users":    `{"tenant":{"password":"env:P"}}`,
		"nested users list":         `{"tenant":{"users":[{"password":"env:P"}]}}`,
	} {
		_, _, err := resolveDemoSecrets([]byte(in), func(string) string { return "" }, demoPassword)
		if err == nil {
			t.Errorf("%s: missing variable must be reported, not generated", name)
		}
	}
	if _, generated, err := resolveDemoSecrets([]byte(`{"users":[{"password":"env:P"}],"data_sources":[{"password":"env:DB"}]}`),
		func(k string) string { return map[string]string{"DB": "x"}[k] }, demoPassword); err != nil || len(generated["P"]) != 24 {
		t.Fatalf("generated = %v, err = %v", generated, err)
	}
}

func TestSaveDemoPasswords(t *testing.T) {
	dir := t.TempDir()
	original := "SECRET_KEY=keep\n# comment\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	s := stack{dir: dir}
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDemoPasswords(s, env, map[string]string{"DEMO_PASSWORD": "b", "DEMO_ADMIN_PASSWORD": "a"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".env"))
	saved, err := readEnvFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Get("SECRET_KEY") != "keep" || saved.Get("DEMO_ADMIN_PASSWORD") != "a" || saved.Get("DEMO_PASSWORD") != "b" {
		t.Fatalf(".env = %q", got)
	}
	if bak, _ := os.ReadFile(filepath.Join(dir, ".env.bak")); string(bak) != original {
		t.Fatalf(".env.bak = %q", bak)
	}
	if info, _ := os.Stat(filepath.Join(dir, ".env")); info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %v", info.Mode().Perm())
	}
}
