// Command spartaskctl installs and operates a Spartask server (Docker Compose stack).
//
//	spartaskctl install [flags]            first installation (asks for domain, version, TLS ...)
//	spartaskctl update <version>           backup, switch release, migrate, health check
//	spartaskctl restart                    apply .env changes
//	spartaskctl config [-check]            add new settings, generate secrets, report what is missing
//	spartaskctl status                     containers and schema migrations
//	spartaskctl logs [service]             follow logs
//	spartaskctl backup                     database dumps + storage + config into backups/
//	spartaskctl restore <backups/TS>       replace the data with a backup
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Set at release build time (-ldflags "-X main.toolVersion=... -X main.defaultVersion=...").
var (
	toolVersion    = "dev"
	defaultVersion = ""
)

const usage = `spartaskctl %s — Spartask server management

Usage: spartaskctl [-dir DIR] <command> [arguments]

Commands:
  install [-domain D] [-version V] [-tls files|internal] [-cert F -key F] [-cloudflare]
          [-registry-user U] [-with-marketplace] [-marketplace-url URL] [-y]
                                 first installation in DIR (default: the current directory)
  update <version> [-marketplace-version V] [-no-backup]
                                 backup, switch release, apply migrations, health check
  restart                        apply .env changes (docker compose up -d)
  config [-check]                compare .env with this package's template: add new settings,
                                 generate secrets, explain what you still have to fill in
  status                         version, containers, schema migrations
  logs [service]                 follow logs (api, worker, scheduler, migrate, caddy, db ...)
  backup                         write backups/<timestamp>/ and keep the newest BACKUP_KEEP
  restore <backups/TS> [-with-config] [-yes]
                                 replace databases and storage with a backup
  version                        print the spartaskctl version
`

func main() {
	global := flag.NewFlagSet("spartaskctl", flag.ContinueOnError)
	dir := global.String("dir", ".", "installation directory (compose.yml, .env)")
	global.Usage = func() { fmt.Fprintf(os.Stderr, usage, toolVersion) }
	if err := global.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if global.NArg() == 0 {
		global.Usage()
		os.Exit(2)
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, stack{dir: abs}, global.Arg(0), global.Args()[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, s stack, command string, args []string) error {
	switch command {
	case "install":
		return runInstall(ctx, s, args)
	case "update":
		return runUpdate(ctx, s, args)
	case "restart":
		return runRestart(ctx, s)
	case "config":
		return runConfig(ctx, s, args)
	case "status":
		return runStatus(ctx, s)
	case "logs":
		return s.compose(ctx, append([]string{"logs", "-f", "--tail", "200"}, args...)...)
	case "backup":
		return runBackup(ctx, s)
	case "restore":
		return runRestore(ctx, s, args)
	case "version":
		fmt.Println(toolVersion)
		return nil
	case "help", "-h", "--help":
		fmt.Printf(usage, toolVersion)
		return nil
	default:
		return fmt.Errorf("unknown command %q (see spartaskctl help)", command)
	}
}
