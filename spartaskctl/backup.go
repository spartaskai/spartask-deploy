package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A backup is a directory backups/<UTC timestamp>/ with:
//
//	spartask.dump              pg_dump -Fc of the application database
//	storage.tar.gz             data/storage (tenant plugins and uploads)
//	config.tar.gz              .env, marketplace.env, certs/, .secrets/ (SECRET_KEY decrypts stored passwords)
//	marketplace.dump           pg_dump -Fc of the marketplace database (with the marketplace only)
//	marketplace-storage.tar.gz the marketplace license/storage volume (with the marketplace only)
const backupTimeFormat = "20060102T150405Z"

func runBackup(ctx context.Context, s stack) error {
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	dir, err := backup(ctx, s, env)
	if err != nil {
		return err
	}
	fmt.Println("Backup written to", dir)
	return nil
}

func backup(ctx context.Context, s stack, env *envFile) (string, error) {
	name := time.Now().UTC().Format(backupTimeFormat)
	dir := s.path("backups", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir) // never leave a partial backup that looks complete
		}
	}()
	dbUser, dbName := envOr(env, "DB_USER", "spartask"), envOr(env, "DB_NAME", "spartask")
	if err := dumpDatabase(ctx, s, "db", dbUser, dbName, filepath.Join(dir, "spartask.dump")); err != nil {
		return "", err
	}
	if err := tarDirectory(s.path("data", "storage"), filepath.Join(dir, "storage.tar.gz")); err != nil {
		return "", err
	}
	config := []string{".env", "marketplace.env", "certs", ".secrets"}
	if err := tarPaths(s.dir, config, filepath.Join(dir, "config.tar.gz")); err != nil {
		return "", err
	}
	marketplace, err := s.hasService(ctx, "marketplace-db")
	if err != nil {
		return "", err
	}
	if marketplace {
		if err := dumpDatabase(ctx, s, "marketplace-db", "marketplace", "marketplace", filepath.Join(dir, "marketplace.dump")); err != nil {
			return "", err
		}
		if err := streamToFile(filepath.Join(dir, "marketplace-storage.tar.gz"), func(w io.Writer) error {
			return s.composeStream(ctx, nil, w, "exec", "-T", "marketplace-backend", "tar", "-C", "/var/spartask", "-czf", "-", ".")
		}); err != nil {
			return "", err
		}
	}
	ok = true
	keep, err := strconv.Atoi(envOr(env, "BACKUP_KEEP", "14"))
	if err != nil || keep < 1 {
		return dir, fmt.Errorf("BACKUP_KEEP must be a positive number")
	}
	if err := pruneBackups(s.path("backups"), keep); err != nil {
		return dir, err
	}
	return dir, nil
}

func envOr(env *envFile, key, def string) string {
	if value := env.Get(key); value != "" {
		return value
	}
	return def
}

func dumpDatabase(ctx context.Context, s stack, service, user, database, target string) error {
	return streamToFile(target, func(w io.Writer) error {
		return s.composeStream(ctx, nil, w, "exec", "-T", service, "pg_dump", "-U", user, "-d", database, "-Fc")
	})
}

func streamToFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := write(f); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func tarDirectory(src, target string) error {
	return tarPaths(src, []string{"."}, target)
}

// tarPaths archives the given paths (relative to base, missing ones skipped) into a .tar.gz.
func tarPaths(base string, paths []string, target string) error {
	return streamToFile(target, func(w io.Writer) error {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		for _, rel := range paths {
			root := filepath.Join(base, rel)
			if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
				continue
			}
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() && !info.IsDir() {
					return nil // sockets, devices and links are not part of the data
				}
				name, err := filepath.Rel(base, path)
				if err != nil {
					return err
				}
				header, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return err
				}
				header.Name = filepath.ToSlash(name)
				if err := tw.WriteHeader(header); err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(tw, f)
				return err
			})
			if err != nil {
				return fmt.Errorf("archive %s: %w", rel, err)
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	})
}

// untar extracts a .tar.gz into dst, refusing entries that would escape it.
func untar(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", archive, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", archive, err)
		}
		target := filepath.Join(dst, filepath.FromSlash(header.Name))
		if rel, err := filepath.Rel(dst, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path in %s: %s", archive, header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0o777|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

// pruneBackups keeps the newest keep backup directories (names sort chronologically).
func pruneBackups(root string, keep int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	var names []string
	for _, e := range entries {
		if _, err := time.Parse(backupTimeFormat, e.Name()); e.IsDir() && err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.RemoveAll(filepath.Join(root, names[0])); err != nil {
			return fmt.Errorf("remove old backup %s: %w", names[0], err)
		}
		names = names[1:]
	}
	return nil
}

// runRestore replaces the databases and storage with a backup. Configuration (.env with
// SECRET_KEY) is restored only with -with-config, e.g. on a fresh server.
func runRestore(ctx context.Context, s stack, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	withConfig := fs.Bool("with-config", false, "also restore .env, marketplace.env, certs and .secrets from the backup")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: spartaskctl restore <backups/TIMESTAMP> [-with-config] [-yes]")
	}
	dir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "spartask.dump")); err != nil {
		return fmt.Errorf("%s is not a spartaskctl backup (spartask.dump missing)", dir)
	}
	if *withConfig {
		if err := untar(filepath.Join(dir, "config.tar.gz"), s.dir); err != nil {
			return err
		}
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	if !*yes {
		ok, err := newPrompter(false).confirm(fmt.Sprintf("Replace ALL data of %s with the backup %s?", env.Get("SPARTASK_DOMAIN"), filepath.Base(dir)), false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("restore cancelled")
		}
	}
	marketplace, err := s.hasService(ctx, "marketplace-db")
	if err != nil {
		return err
	}
	apps := []string{"caddy", "api", "worker", "scheduler"}
	if marketplace {
		apps = append(apps, "marketplace-web", "marketplace-backend")
	}
	if err := s.compose(ctx, append([]string{"stop"}, apps...)...); err != nil {
		return err
	}
	databases := []string{"db"}
	if marketplace {
		databases = append(databases, "marketplace-db")
	}
	if err := s.compose(ctx, append([]string{"up", "-d", "--wait"}, databases...)...); err != nil {
		return err
	}
	dbUser, dbName := envOr(env, "DB_USER", "spartask"), envOr(env, "DB_NAME", "spartask")
	if err := restoreDatabase(ctx, s, "db", dbUser, dbName, filepath.Join(dir, "spartask.dump")); err != nil {
		return err
	}
	storage := s.path("data", "storage")
	if err := os.Rename(storage, storage+".before-restore-"+time.Now().UTC().Format(backupTimeFormat)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("move current storage aside: %w", err)
	}
	if err := os.MkdirAll(storage, 0o755); err != nil {
		return err
	}
	if err := untar(filepath.Join(dir, "storage.tar.gz"), storage); err != nil {
		return err
	}
	if marketplace {
		if _, err := os.Stat(filepath.Join(dir, "marketplace.dump")); err == nil {
			if err := restoreDatabase(ctx, s, "marketplace-db", "marketplace", "marketplace", filepath.Join(dir, "marketplace.dump")); err != nil {
				return err
			}
			if err := restoreMarketplaceStorage(ctx, s, filepath.Join(dir, "marketplace-storage.tar.gz")); err != nil {
				return err
			}
		}
	}
	fmt.Println("Data restored; starting the stack (pending migrations run first)...")
	return runRestart(ctx, s)
}

func restoreDatabase(ctx context.Context, s stack, service, user, database, dump string) error {
	drop := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, database)
	create := fmt.Sprintf(`CREATE DATABASE "%s" OWNER "%s"`, database, user)
	for _, sql := range []string{drop, create} {
		if err := s.composeStream(ctx, nil, io.Discard, "exec", "-T", service, "psql", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", "postgres", "-c", sql); err != nil {
			return err
		}
	}
	f, err := os.Open(dump)
	if err != nil {
		return fmt.Errorf("open %s: %w", dump, err)
	}
	defer f.Close()
	return s.composeStream(ctx, f, os.Stdout, "exec", "-T", service, "pg_restore", "--exit-on-error", "--no-owner", "-U", user, "-d", database)
}

func restoreMarketplaceStorage(ctx context.Context, s stack, archive string) error {
	f, err := os.Open(archive)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}
	defer f.Close()
	// The backend container is stopped; a one-off container with the same volume extracts the files.
	return s.composeStream(ctx, f, os.Stdout, "run", "--rm", "--no-deps", "-T", "--entrypoint", "tar", "marketplace-backend", "-C", "/var/spartask", "-xzf", "-")
}

func joinPath(base string, parts ...string) string {
	return filepath.Join(append([]string{base}, parts...)...)
}
