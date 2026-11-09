package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func loadInstalledEnv(s stack) (*envFile, error) {
	env, err := readEnvFile(s.path(".env"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no .env in %s: run spartaskctl install first", s.dir)
		}
		return nil, err
	}
	return env, nil
}

// runUpdate switches the stack to another release: the new images are downloaded first (the
// running release keeps serving), then a backup, migrations, restart and health check. When the
// new release does not come up, .env is switched back; a database the new release's migrations
// already changed is brought back with the pre-update backup.
func runUpdate(ctx context.Context, s stack, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	noBackup := fs.Bool("no-backup", false, "skip the pre-update backup")
	marketplaceVersion := fs.String("marketplace-version", "", "also switch the marketplace to this release")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: spartaskctl update <version> [-marketplace-version X] [-no-backup]")
	}
	version := strings.TrimPrefix(fs.Arg(0), "v")
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q, e.g. 1.2.0", version)
	}
	mpVersion := strings.TrimPrefix(*marketplaceVersion, "v")
	if mpVersion != "" && !versionPattern.MatchString(mpVersion) {
		return fmt.Errorf("invalid marketplace version %q", mpVersion)
	}
	// New settings of this package are added and safe secrets generated; missing required
	// settings stop the update before anything else.
	env, err := prepareConfig(s)
	if err != nil {
		return err
	}
	previous, previousMP := env.Get("SPARTASK_VERSION"), env.Get("MARKETPLACE_VERSION")
	target := map[string]string{"SPARTASK_VERSION": version}
	if mpVersion != "" {
		target["MARKETPLACE_VERSION"] = mpVersion
	}
	fmt.Printf("Downloading %s (the running release keeps serving)...\n", version)
	if err := s.composeWith(ctx, target, "pull"); err != nil {
		return fmt.Errorf("%w\nNothing was changed; %s keeps running", err, previous)
	}
	restore := "There is no pre-update backup (-no-backup)."
	if !*noBackup {
		fmt.Println("Backup before the update...")
		dir, err := backup(ctx, s, env)
		if err != nil {
			return fmt.Errorf("pre-update backup failed (use -no-backup to skip): %w", err)
		}
		restore = "If the previous release does not start because the new release's migrations changed the database, bring back the pre-update backup\n" +
			"(data written since then is lost; decide before restoring) and start again:\n  spartaskctl restore " + dir + "\n  spartaskctl restart"
	}
	for key, value := range target {
		env.Set(key, value)
	}
	if err := env.Save(s.path(".env")); err != nil {
		return err
	}
	revert := func(cause error) error {
		env.Set("SPARTASK_VERSION", previous)
		env.Set("MARKETPLACE_VERSION", previousMP)
		if saveErr := env.Save(s.path(".env")); saveErr != nil {
			return errors.Join(cause, saveErr)
		}
		return fmt.Errorf("%w\n.env is back on %s. Logs: docker compose logs --tail 200 migrate api\n"+
			"Start the previous release: spartaskctl restart\n%s", cause, previous, restore)
	}
	if err := s.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return revert(err)
	}
	if err := s.waitHealthy(ctx, "api", 3*time.Minute); err != nil {
		return revert(err)
	}
	fmt.Printf("SparTask %s -> %s is running.\n", previous, version)
	return nil
}

func runRestart(ctx context.Context, s stack) error {
	if _, err := prepareConfig(s); err != nil {
		return err
	}
	if err := s.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	return s.waitHealthy(ctx, "api", 3*time.Minute)
}

func runStatus(ctx context.Context, s stack) error {
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	fmt.Printf("SparTask %s on %s", env.Get("SPARTASK_VERSION"), env.Get("SPARTASK_DOMAIN"))
	if v := env.Get("MARKETPLACE_VERSION"); v != "" && strings.Contains(env.Get("COMPOSE_FILE"), "marketplace") {
		fmt.Printf(", marketplace %s on %s", v, env.Get("MARKETPLACE_DOMAIN"))
	}
	fmt.Println()
	if err := s.compose(ctx, "ps"); err != nil {
		return err
	}
	fmt.Println("\nSchema migrations:")
	return s.compose(ctx, "run", "--rm", "--no-deps", "migrate", "-status")
}

// reorderFlags moves flags in front of positional arguments so `update 1.2.0 -no-backup` works.
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			// value of a "-flag value" pair (boolean flags here never take a separate value)
			if !strings.Contains(arg, "=") && arg != "-no-backup" && arg != "--no-backup" && arg != "-yes" && arg != "--yes" && arg != "-with-config" && arg != "--with-config" && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}
