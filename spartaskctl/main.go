// Command spartaskctl installs and operates a Spartask server (Docker Compose stack).
//
//	spartaskctl install [flags]            first installation (asks for domain, version, TLS ...)
//	spartaskctl install -activation CODE   a customer's installation activated on the on-prem page
//	spartaskctl update <version>           download, backup, switch release, migrate, health check
//	spartaskctl restart                    apply .env changes
//	spartaskctl config [-check]            add new settings, generate secrets, report what is missing
//	spartaskctl status                     containers and schema migrations
//	spartaskctl logs [service]             follow logs
//	spartaskctl backup                     database dumps + storage + config into backups/
//	spartaskctl restore <backups/TS>       replace the data with a backup
//	spartaskctl demo apply <file.json>     create or reset a demo company (users, roles, data sources)
//	spartaskctl hyperv install [flags]     Windows: a Hyper-V VM with Ubuntu, installed like a Linux server
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
          [-registry-user U] [-with-marketplace] [-marketplace-url URL] [-backup-job=false] [-y]
                                 first installation in DIR (default: the current directory)
  install -activation CODE -license-server URL [-admin-email E] [-admin-name N]
          [-tls acme|files] [-cert F -key F] [-company-id ID] [-version V] [-registry-user U] [-y]
                                 a customer's installation with the code from the on-prem page:
                                 its address, license and company come with the code; the
                                 certificate comes from Let's Encrypt (acme) or the company's
                                 files (an address reachable only in the company network or VPN)
                                 The server is checked first (nothing is changed when it is not
                                 ready); after a later failure run the same command again: it
                                 continues where it stopped
  update <version> [-marketplace-version V] [-no-backup]
                                 download the release, backup, switch, apply migrations, health check
  restart                        apply .env changes (docker compose up -d)
  config [-check]                compare .env with this package's template: add new settings,
                                 generate secrets, explain what you still have to fill in
  status                         version, containers, schema migrations
  logs [service]                 follow logs (api, worker, scheduler, migrate, caddy, db ...)
  backup                         write backups/<timestamp>/ and keep the newest BACKUP_KEEP
  restore <backups/TS> [-with-config] [-yes]
                                 replace databases and storage with a backup
  demo apply|check <file.json>   create a demo company or reset it to the file: users,
                                 roles, data sources ("env:NAME" values come from .env)
  system-processes sync          publish the system processes (employees, format profiles)
                                 to every company now; the API also does it when it starts
  license show                   this installation's key fingerprint and license state
  license install <file>         install the license the license service issued for it;
                                 without one no company can be created
  license renew                  renew the license with the license service now (the scheduler
                                 renews it 30 days before it expires)
  hyperv install [-cpus 2] [-memory-gb 4] [-disk-gb 64] [-subnet CIDR] [-dns IP,IP]
          <install options>      Windows Server (or Windows 10/11 Pro): runs SparTask in a Hyper-V
                                 VM with Ubuntu 24.04 behind a NAT network; ports 80 and 443 of
                                 this computer are forwarded to it, and the install options go to
                                 spartaskctl install inside it. Run again after a restart or a
                                 failure: it continues where it stopped
  hyperv ssh [-- command]        a shell in that VM, or a command there
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
	case "demo":
		return runDemo(ctx, s, args)
	case "system-processes":
		return runSystemProcesses(ctx, s, args)
	case "license":
		return runLicense(ctx, s, args)
	case "hyperv":
		return runHyperV(ctx, args)
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
