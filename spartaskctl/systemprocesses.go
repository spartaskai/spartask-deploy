package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const systemProcessesUsage = `usage: spartaskctl system-processes sync   publish the system processes (employees, format
                                          profiles) to every company now`

// runSystemProcesses asks spartask-admin in the api container to bring every company's system
// processes up to date. The API already does this when it starts (new definitions only come with
// a release) and when a company is created; this is for running it without a restart, e.g. after
// a failed publication.
func runSystemProcesses(ctx context.Context, s stack, args []string) error {
	if len(args) != 1 || args[0] != "sync" {
		return errors.New(systemProcessesUsage)
	}
	err := s.compose(ctx, "exec", "-T", "api", "/app/bin/spartask-admin", "system-processes", "sync")
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "exit status 2") {
		return fmt.Errorf("this Spartask release cannot sync system processes; update to a release that includes it (%w)", err)
	}
	return err
}
