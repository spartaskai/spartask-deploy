package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// envFile is a dotenv file that keeps its comments, blank lines and key order when values change.
type envFile struct {
	lines []string
}

func parseEnv(data []byte) *envFile {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return &envFile{}
	}
	return &envFile{lines: strings.Split(text, "\n")}
}

func readEnvFile(path string) (*envFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseEnv(data), nil
}

// lineKey returns the key of a KEY=value line, or "" for comments and blank lines.
func lineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	key, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}

// Get returns the value of the last assignment of key (dotenv semantics), unquoted.
func (e *envFile) Get(key string) string {
	value := ""
	for _, line := range e.lines {
		if lineKey(line) != key {
			continue
		}
		_, raw, _ := strings.Cut(line, "=")
		value = unquote(strings.TrimSpace(raw))
	}
	return value
}

func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// Set replaces every assignment of key or appends KEY=value at the end.
func (e *envFile) Set(key, value string) {
	if strings.ContainsAny(value, "\n\r") {
		panic("env values must be single-line: " + key)
	}
	found := false
	for i, line := range e.lines {
		if lineKey(line) == key {
			e.lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		e.lines = append(e.lines, key+"="+value)
	}
}

// has reports whether key is assigned (even to an empty value).
func (e *envFile) has(key string) bool {
	for _, line := range e.lines {
		if lineKey(line) == key {
			return true
		}
	}
	return false
}

// keys lists the assigned keys in file order, each once.
func (e *envFile) keys() []string {
	var keys []string
	seen := map[string]bool{}
	for _, line := range e.lines {
		if key := lineKey(line); key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// insertAfter inserts block after the last assignment of key, or at the end of the file when key
// is empty or not assigned.
func (e *envFile) insertAfter(key string, block []string) {
	at := -1
	if key != "" {
		for i, line := range e.lines {
			if lineKey(line) == key {
				at = i
			}
		}
	}
	if at < 0 {
		if n := len(e.lines); n > 0 && strings.TrimSpace(e.lines[n-1]) != "" {
			e.lines = append(e.lines, "")
		}
		e.lines = append(e.lines, block...)
		return
	}
	lines := make([]string, 0, len(e.lines)+len(block))
	lines = append(lines, e.lines[:at+1]...)
	lines = append(lines, block...)
	e.lines = append(lines, e.lines[at+1:]...)
}

func (e *envFile) Bytes() []byte {
	var buf bytes.Buffer
	for _, line := range e.lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// writeSecretFile writes data with mode 0600 through a temporary file, so a crash never leaves a
// half-written secret file behind.
func writeSecretFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (e *envFile) Save(path string) error {
	return writeSecretFile(path, e.Bytes())
}

// keysOf lists the keys assigned in a dotenv text (used to check templates in tests).
func keysOf(data []byte) []string {
	var keys []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if key := lineKey(scanner.Text()); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
