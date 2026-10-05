package main

import (
	"fmt"
	"strconv"
	"strings"
)

// envRule describes one setting of a template (.env.example, marketplace.env.example). The
// template is the single source of truth: its comment lines are the description shown to the
// operator and its "#@" line holds the machine-readable rules.
type envRule struct {
	Key         string
	Default     string
	Description []string // comment lines above the key, without "# "
	comments    []string // the same lines as written in the template (copied when the key is added)

	Required    bool   // empty → the stack cannot run (update/restart stop)
	Recommended bool   // empty → a feature stays off (warning)
	Generate    int    // > 0: random hex of this many bytes when empty
	InstallOnly bool   // generate only on the first installation (changing it later breaks data)
	If          string // KEY (non-empty) or KEY=value: the rule applies only then
	Stack       string // "marketplace": the rule applies only with compose.marketplace.yml
	Values      []string
	Min         int
}

// parseRules reads the settings of a template in file order.
func parseRules(template []byte) ([]envRule, error) {
	var rules []envRule
	var comments []string
	var pending envRule
	seen := map[string]bool{}
	for i, line := range parseEnv(template).lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			comments, pending = nil, envRule{}
		case strings.HasPrefix(trimmed, "#@"):
			if err := pending.annotate(strings.Fields(strings.TrimPrefix(trimmed, "#@"))); err != nil {
				return nil, fmt.Errorf("template line %d: %w", i+1, err)
			}
		case strings.HasPrefix(trimmed, "# ---"):
			comments = nil
		case strings.HasPrefix(trimmed, "#"):
			comments = append(comments, line)
		default:
			key := lineKey(line)
			if key == "" {
				return nil, fmt.Errorf("template line %d: not a KEY=value line", i+1)
			}
			if seen[key] {
				return nil, fmt.Errorf("template line %d: %s is defined twice", i+1, key)
			}
			seen[key] = true
			_, raw, _ := strings.Cut(line, "=")
			rule := pending
			rule.Key, rule.Default, rule.comments = key, unquote(strings.TrimSpace(raw)), comments
			for _, comment := range comments {
				rule.Description = append(rule.Description, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#")))
			}
			rules = append(rules, rule)
			comments, pending = nil, envRule{}
		}
	}
	return rules, nil
}

func (r *envRule) annotate(tokens []string) error {
	for _, token := range tokens {
		name, value, _ := strings.Cut(token, "=")
		switch name {
		case "required":
			r.Required = true
		case "recommended":
			r.Recommended = true
		case "install-only":
			r.InstallOnly = true
		case "generate":
			size, ok := strings.CutPrefix(value, "hex:")
			n, err := strconv.Atoi(size)
			if !ok || err != nil || n < 16 {
				return fmt.Errorf("generate must be hex:N with N >= 16, got %q", value)
			}
			r.Generate = n
		case "if":
			if value == "" {
				return fmt.Errorf("if needs a KEY or KEY=value")
			}
			r.If = value
		case "stack":
			if value != "marketplace" {
				return fmt.Errorf("unknown stack %q", value)
			}
			r.Stack = value
		case "values":
			r.Values = strings.Split(value, "|")
		case "min":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return fmt.Errorf("min must be a positive number, got %q", value)
			}
			r.Min = n
		default:
			return fmt.Errorf("unknown rule %q", token)
		}
	}
	if r.Required && r.Recommended {
		return fmt.Errorf("a setting is either required or recommended")
	}
	return nil
}

// envScope tells which conditional rules apply.
type envScope struct {
	env         *envFile // the .env the conditions refer to
	marketplace bool
}

func scopeOf(env *envFile) envScope {
	return envScope{env: env, marketplace: strings.Contains(env.Get("COMPOSE_FILE"), "compose.marketplace.yml")}
}

func (r envRule) applies(scope envScope) bool {
	if r.Stack == "marketplace" && !scope.marketplace {
		return false
	}
	if r.If == "" {
		return true
	}
	key, want, hasValue := strings.Cut(r.If, "=")
	got := scope.env.Get(key)
	if hasValue {
		return strings.EqualFold(got, want)
	}
	return got != ""
}

// sentenceEnd finds the first ". " that is not an abbreviation such as "ör." (e.g.).
func sentenceEnd(text string) int {
	from := 0
	for {
		i := strings.Index(text[from:], ". ")
		if i < 0 {
			return -1
		}
		end := from + i
		fields := strings.Fields(text[:end])
		if len(fields) == 0 {
			return -1
		}
		switch strings.ToLower(strings.Trim(fields[len(fields)-1], "(")) {
		case "ör", "örn", "vb", "e.g":
			from = end + 2
			continue
		}
		return end
	}
}

// summary is the first sentence of the description, shown in compact reports.
func (r envRule) summary() string {
	text := strings.Join(r.Description, " ")
	if end := sentenceEnd(text); end > 0 && end < 160 {
		return text[:end+1]
	}
	if len(r.Description) > 0 && len(text) > 160 {
		return r.Description[0]
	}
	return text
}
