package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const demoUsage = `usage: spartaskctl demo apply <file.json>   create the demo company or reset it to the file
       spartaskctl demo check <file.json>   validate the file, write nothing`

// runDemo hands a demo company specification to spartask-admin inside the api container. Values
// written as "env:NAME" (passwords, database connection) are read here, from the environment of
// spartaskctl or else from .env, so secrets never sit in the specification file.
func runDemo(ctx context.Context, s stack, args []string) error {
	if len(args) != 2 || (args[0] != "apply" && args[0] != "check") {
		return errors.New(demoUsage)
	}
	action, file := args[0], args[1]
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	resolved, err := resolveDemoSecrets(data, func(name string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return env.Get(name)
	})
	if err != nil {
		return err
	}
	err = s.composeStream(ctx, bytes.NewReader(resolved), os.Stdout,
		"exec", "-T", "api", "/app/bin/spartask-admin", "demo", action, "-f", "-")
	if err != nil && strings.Contains(err.Error(), "spartask-admin") && strings.Contains(strings.ToLower(err.Error()), "no such file") {
		return fmt.Errorf("this Spartask release has no spartask-admin; update to a release that includes it (%w)", err)
	}
	return err
}

// resolveDemoSecrets replaces every JSON string value "env:NAME" by lookup(NAME). Every missing
// variable is reported at once. Numbers keep their exact text.
func resolveDemoSecrets(data []byte, lookup func(string) string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("demo file is not valid JSON: %w", err)
	}
	if dec.More() {
		return nil, errors.New("demo file holds more than one JSON value")
	}
	missing := map[string]bool{}
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, item := range t {
				t[k] = walk(item)
			}
		case []any:
			for i, item := range t {
				t[i] = walk(item)
			}
		case string:
			name, ok := strings.CutPrefix(t, "env:")
			if !ok {
				return t
			}
			name = strings.TrimSpace(name)
			value := lookup(name)
			if value == "" {
				missing[name] = true
			}
			return value
		}
		return v
	}
	doc = walk(doc)
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("set %s in .env or in the environment (e.g. sudo %s=... ./spartaskctl demo ...)", strings.Join(names, ", "), names[0])
	}
	return json.Marshal(doc)
}
