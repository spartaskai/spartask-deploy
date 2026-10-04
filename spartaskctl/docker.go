package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// stack is an installation directory: compose files, .env and data.
type stack struct {
	dir string
}

func (s stack) path(parts ...string) string {
	return joinPath(s.dir, parts...)
}

func (s stack) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = s.dir
	return cmd
}

// compose runs `docker compose <args>` with the output shown to the operator.
func (s stack) compose(ctx context.Context, args ...string) error {
	cmd := s.command(ctx, append([]string{"compose"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// composeOutput runs `docker compose <args>` and returns its trimmed stdout.
func (s stack) composeOutput(ctx context.Context, args ...string) (string, error) {
	return s.output(ctx, append([]string{"compose"}, args...)...)
}

func (s stack) output(ctx context.Context, args ...string) (string, error) {
	cmd := s.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// composeStream runs `docker compose <args>` with stdin and stdout connected to the given streams
// (database dumps and restores).
func (s stack) composeStream(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := s.command(ctx, append([]string{"compose"}, args...)...)
	cmd.Stdin, cmd.Stdout = stdin, stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s stack) services(ctx context.Context) ([]string, error) {
	out, err := s.composeOutput(ctx, "config", "--services")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func (s stack) hasService(ctx context.Context, name string) (bool, error) {
	services, err := s.services(ctx)
	if err != nil {
		return false, err
	}
	for _, service := range services {
		if service == name {
			return true, nil
		}
	}
	return false, nil
}

// waitHealthy waits until the container of service reports a healthy status.
func (s stack) waitHealthy(ctx context.Context, service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		id, err := s.composeOutput(ctx, "ps", "-q", service)
		if err == nil && id != "" {
			status, inspectErr := s.output(ctx, "inspect", "-f", "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", id)
			if inspectErr == nil {
				last = status
				if status == "running healthy" {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			if last == "" {
				last = "not running"
			}
			return fmt.Errorf("%s is not healthy after %s (state: %s)", service, timeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func checkDocker(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker is not installed (Ubuntu: curl -fsSL https://get.docker.com | sh)")
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
	out, err := cmd.Output()
	if err != nil {
		return errors.New("the Docker Compose plugin is missing or Docker is not running (try: docker compose version)")
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	if major, _, _ := strings.Cut(version, "."); major == "1" {
		return fmt.Errorf("docker compose %s is too old; version 2 or later is required", version)
	}
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		return errors.New("cannot talk to the Docker daemon; run as root or as a member of the docker group")
	}
	return nil
}
