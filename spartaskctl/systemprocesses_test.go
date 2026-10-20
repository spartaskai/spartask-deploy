package main

import (
	"context"
	"strings"
	"testing"
)

func TestSystemProcessesRequiresSync(t *testing.T) {
	for _, args := range [][]string{nil, {"run"}, {"sync", "now"}} {
		err := runSystemProcesses(context.Background(), stack{dir: t.TempDir()}, args)
		if err == nil || !strings.Contains(err.Error(), "usage: spartaskctl system-processes sync") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
