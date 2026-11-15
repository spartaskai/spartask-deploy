package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// syncResult is what syncEnv changed and found in one settings file.
type syncResult struct {
	File        string
	Added       []envRule // settings of a newer template that the file did not have
	Generated   []envRule // empty secrets filled with random values
	Missing     []envRule // required and empty
	Recommended []envRule // recommended and empty
	Invalid     []string  // set but not acceptable
	Unknown     []string  // in the file, not in the template
	Notes       []string  // advice about specific settings (filled by advisors)
	Derived     bool      // generated compose selection changed
}

func (r *syncResult) changed() bool  { return len(r.Added) > 0 || len(r.Generated) > 0 || r.Derived }
func (r *syncResult) blocking() bool { return len(r.Missing) > 0 || len(r.Invalid) > 0 }

// syncEnv brings file up to date with the template rules: missing settings are added next to
// their neighbours with the template default, empty generatable secrets are generated
// (install-only ones only when install is true), and every rule is checked. Existing values are
// never changed. scope is evaluated after the additions; nil means file itself.
func syncEnv(name string, file *envFile, rules []envRule, scopeEnv *envFile, install bool) (*syncResult, error) {
	result := &syncResult{File: name}
	known := map[string]bool{}
	previous := ""
	for _, rule := range rules {
		known[rule.Key] = true
		if !file.has(rule.Key) {
			file.insertAfter(previous, append(append([]string{}, rule.comments...), rule.Key+"="+rule.Default))
			result.Added = append(result.Added, rule)
		}
		previous = rule.Key
	}
	if scopeEnv == nil {
		scopeEnv = file
	}
	scope := scopeOf(scopeEnv)
	for _, rule := range rules {
		if !rule.applies(scope) {
			continue
		}
		value := file.Get(rule.Key)
		if value == "" && rule.Generate > 0 && (install || !rule.InstallOnly) {
			generated, err := randomHex(rule.Generate)
			if err != nil {
				return nil, fmt.Errorf("generate %s: %w", rule.Key, err)
			}
			file.Set(rule.Key, generated)
			result.Generated = append(result.Generated, rule)
			value = generated
		}
		switch {
		case value == "" && rule.Required:
			result.Missing = append(result.Missing, rule)
		case value == "" && rule.Recommended:
			result.Recommended = append(result.Recommended, rule)
		case value != "" && len(rule.Values) > 0 && !containsFold(rule.Values, value):
			result.Invalid = append(result.Invalid, fmt.Sprintf("%s=%s: şu değerlerden biri olmalı: %s", rule.Key, value, strings.Join(rule.Values, ", ")))
		case value != "" && rule.Min > 0 && len(value) < rule.Min:
			result.Invalid = append(result.Invalid, fmt.Sprintf("%s: en az %d karakter olmalı (şu an %d)", rule.Key, rule.Min, len(value)))
		}
	}
	for _, key := range file.keys() {
		if !known[key] {
			result.Unknown = append(result.Unknown, key)
		}
	}
	return result, nil
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

// installedConfig is the settings of an installation, checked against the templates.
type installedConfig struct {
	env, mp             *envFile // mp is nil without the marketplace
	envResult, mpResult *syncResult
	meeting             *meetingPlan
}

// checkInstalledConfig loads .env (and marketplace.env with the marketplace) and syncs them in
// memory; nothing is written.
func checkInstalledConfig(s stack) (*installedConfig, error) {
	env, err := loadInstalledEnv(s)
	if err != nil {
		return nil, err
	}
	return checkConfigEnv(s, env)
}

// checkConfigEnv also accepts an in-memory setup draft. The wizard never writes partial input.
func checkConfigEnv(s stack, env *envFile) (*installedConfig, error) {
	rules, err := readRules(s.path(".env.example"))
	if err != nil {
		return nil, err
	}
	// An endpoint configured before managed deployment existed must remain external unless
	// its owner explicitly opts into local management. Never generate remote credentials.
	legacyExternal := !env.has("LIVEKIT_MANAGED") && env.Get("LIVEKIT_URL") != ""
	if legacyExternal {
		env.Set("LIVEKIT_MANAGED", "false")
	}
	cfg := &installedConfig{env: env}
	if cfg.envResult, err = syncEnv(".env", env, rules, nil, false); err != nil {
		return nil, err
	}
	if legacyExternal {
		cfg.envResult.Derived = true
		cfg.envResult.Notes = append(cfg.envResult.Notes, "Mevcut LIVEKIT_URL korundu: sahiplik ayarı olmayan eski bağlantı harici kabul edildi; yerel yönetime geçiş için config -meeting kullanın.")
	}
	validateFormats(env, cfg.envResult)
	cfg.planMeeting(s)
	if !scopeOf(env).marketplace {
		return cfg, nil
	}
	mpRules, err := readRules(s.path("marketplace.env.example"))
	if err != nil {
		return nil, err
	}
	mp, err := readEnvFile(s.path("marketplace.env"))
	if errors.Is(err, os.ErrNotExist) {
		cfg.envResult.Invalid = append(cfg.envResult.Invalid, "marketplace.env yok: marketplace COMPOSE_FILE içinde açık ama gizli ayarları bulunamadı")
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.mp = mp
	if cfg.mpResult, err = syncEnv("marketplace.env", mp, mpRules, env, false); err != nil {
		return nil, err
	}
	return cfg, nil
}

func readRules(path string) ([]envRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w (run spartaskctl in the extracted spartask-deploy directory)", path, err)
	}
	rules, err := parseRules(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rules, nil
}

// save writes the files that changed, keeping the previous version as <file>.bak (mode 600).
func (c *installedConfig) save(s stack) error {
	if err := c.saveMeeting(s); err != nil {
		return err
	}
	for _, item := range []struct {
		name   string
		file   *envFile
		result *syncResult
	}{{".env", c.env, c.envResult}, {"marketplace.env", c.mp, c.mpResult}} {
		if item.file == nil || item.result == nil || !item.result.changed() {
			continue
		}
		previous, err := os.ReadFile(s.path(item.name))
		if err != nil {
			return fmt.Errorf("read %s: %w", item.name, err)
		}
		if err := writeSecretFile(s.path(item.name+".bak"), previous); err != nil {
			return err
		}
		if err := item.file.Save(s.path(item.name)); err != nil {
			return err
		}
	}
	return nil
}

func (c *installedConfig) results() []*syncResult {
	if c.mpResult == nil {
		return []*syncResult{c.envResult}
	}
	return []*syncResult{c.envResult, c.mpResult}
}

func (c *installedConfig) blocking() bool {
	for _, result := range c.results() {
		if result.blocking() {
			return true
		}
	}
	return false
}

// runConfig is `spartaskctl config [-check] [-meeting]`.
func runConfig(ctx context.Context, s stack, args []string) error {
	return configWith(ctx, s, args, newPrompter(false), defaultAdvisorProbes(s))
}

func configWith(ctx context.Context, s stack, args []string, p *prompter, probes advisorProbes) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "only report; do not add or generate anything")
	meetingSetup := fs.Bool("meeting", false, "guide LiveKit setup (with -check: show only the meeting guide, without prompting or writing)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: spartaskctl config [-check] [-meeting]")
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	before := env.Bytes()
	if *meetingSetup && !*checkOnly {
		if err := configureMeeting(p, env); err != nil {
			return err
		}
	}
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, cfg.env.Bytes()) {
		cfg.envResult.Derived = true
	}
	advise(ctx, cfg, probes)
	if !*checkOnly {
		if err := cfg.save(s); err != nil {
			return err
		}
	}
	if *meetingSetup {
		// The guided view reports blockers and effective state, without dumping every newly
		// added advanced setting. The full report remains available through plain config.
		for _, result := range cfg.results() {
			compact := *result
			compact.Added = nil
			printReport(os.Stdout, []*syncResult{&compact}, false, *checkOnly)
		}
	} else {
		printReport(os.Stdout, cfg.results(), true, *checkOnly)
	}
	printMeetingGuide(os.Stdout, cfg, *checkOnly)
	if cfg.blocking() {
		return errors.New("zorunlu ayarlar eksik veya geçersiz; .env dosyasını düzenleyip `spartaskctl config` ile tekrar kontrol edin")
	}
	return nil
}

// prepareConfig runs before update and restart: new settings are added and safe secrets
// generated; missing required settings stop the command before anything changes.
func prepareConfig(s stack) (*envFile, error) {
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		return nil, err
	}
	if cfg.blocking() {
		printReport(os.Stdout, cfg.results(), false, true)
		return nil, errors.New("zorunlu ayarlar eksik veya geçersiz; .env dosyasını düzenleyin (ayrıntı: spartaskctl config -check)")
	}
	if err := cfg.save(s); err != nil {
		return nil, err
	}
	for _, result := range cfg.results() {
		if result.changed() || cfg.meeting != nil {
			printReport(os.Stdout, cfg.results(), false, false)
			break
		}
	}
	return cfg.env, nil
}

// printReport explains the state in plain language. Secret values are never printed.
func printReport(w io.Writer, results []*syncResult, detailed, dryRun bool) {
	for _, r := range results {
		fmt.Fprintf(w, "\n== %s ==\n", r.File)
		addedVerb, generatedVerb := "Eklendi", "Otomatik üretildi"
		if dryRun {
			addedVerb, generatedVerb = "Eklenecek", "Otomatik üretilecek"
		}
		section(w, fmt.Sprintf("%s — yeni sürümle gelen ayarlar (varsayılan değerle)", addedVerb), r.Added, false)
		section(w, fmt.Sprintf("%s — rastgele değer yazıldı, bir şey yapmanız gerekmez", generatedVerb), r.Generated, false)
		section(w, "ZORUNLU — doldurulmadan sistem çalışmaz", r.Missing, true)
		if detailed {
			section(w, "ÖNERİLEN — boş kalırsa ilgili özellik kapalı kalır", r.Recommended, true)
		} else if len(r.Recommended) > 0 {
			keys := make([]string, 0, len(r.Recommended))
			for _, rule := range r.Recommended {
				keys = append(keys, rule.Key)
			}
			fmt.Fprintf(w, "\nBoş önerilen ayarlar: %s (açıklama: spartaskctl config -check)\n", strings.Join(keys, ", "))
		}
		if len(r.Invalid) > 0 {
			fmt.Fprintln(w, "\nGEÇERSİZ değerler:")
			for _, message := range r.Invalid {
				fmt.Fprintf(w, "  - %s\n", message)
			}
		}
		if len(r.Notes) > 0 {
			fmt.Fprintln(w, "\nÖneriler:")
			for _, note := range r.Notes {
				fmt.Fprintf(w, "  - %s\n", strings.ReplaceAll(note, "\n", "\n    "))
			}
		}
		if detailed && len(r.Unknown) > 0 {
			sort.Strings(r.Unknown)
			fmt.Fprintf(w, "\nŞablonda olmayan ayarlar (eski sürümden kalmış ya da elle eklenmiş olabilir): %s\n", strings.Join(r.Unknown, ", "))
		}
		if !r.changed() && !r.blocking() && len(r.Recommended) == 0 && len(r.Notes) == 0 {
			fmt.Fprintln(w, "Her şey tamam.")
		}
	}
	if detailed {
		fmt.Fprintln(w, "\nDeğerleri doldurduktan sonra uygulamak için: spartaskctl restart")
	}
}

func section(w io.Writer, title string, rules []envRule, describe bool) {
	if len(rules) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s (%d):\n", title, len(rules))
	for _, rule := range rules {
		if !describe {
			fmt.Fprintf(w, "  %-32s %s\n", rule.Key, rule.summary())
			continue
		}
		fmt.Fprintf(w, "  %s\n", rule.Key)
		for _, line := range rule.Description {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
}
