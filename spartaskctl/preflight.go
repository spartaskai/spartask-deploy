package main

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The smallest server a pilot installation runs on (2 vCPU / 4 GB; a 4 GB server reports a little
// less memory than that) and the free space images and the first data need.
const (
	minMemoryBytes   = 3584 << 20 // 3.5 GiB
	minFreeDiskBytes = 10 << 30   // 10 GiB
	maxClockSkew     = 2 * time.Minute
)

// webPorts are the ports Caddy publishes on the server.
var webPorts = []int{80, 443}

// preflightProbes look at the server; tests replace them.
type preflightProbes struct {
	memory     func() (uint64, error)                                       // total memory in bytes
	freeDisk   func(path string) (uint64, error)                            // bytes available to root
	dockerRoot func(ctx context.Context) (string, error)                    // Docker's data directory
	listen     func(port int) error                                         // nil when the port is free
	fetch      func(ctx context.Context, url string) (time.Time, error)     // the server's Date header
	lookup     func(ctx context.Context, host string) ([]netip.Addr, error) // DNS (the installation's address)
	now        func() time.Time
}

func defaultPreflightProbes(s stack) preflightProbes {
	return preflightProbes{
		memory:   totalMemory,
		freeDisk: freeDiskSpace,
		dockerRoot: func(ctx context.Context) (string, error) {
			return s.output(ctx, "info", "-f", "{{.DockerRootDir}}")
		},
		listen: func(port int) error {
			l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
			if err != nil {
				return err
			}
			return l.Close()
		},
		fetch: fetchDate,
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		now: time.Now,
	}
}

// preflightTarget is an address the installation has to reach.
type preflightTarget struct {
	name, url string
}

// preflightTargets are the addresses this installation downloads from or talks to while it is
// installed. The marketplace is named by the activation and checked by its first use.
func preflightTargets(o installOptions) []preflightTarget {
	targets := []preflightTarget{
		{"GitHub Container Registry (SparTask images)", "https://ghcr.io/v2/"},
		{"Docker Hub (Caddy, PostgreSQL and NATS images)", "https://registry-1.docker.io/v2/"},
	}
	if o.activation != "" {
		targets = append(targets, preflightTarget{"SparTask license service", o.licenseServer + "/v1/keys"})
	}
	if o.tls == tlsACME {
		targets = append(targets, preflightTarget{"Let's Encrypt", "https://acme-v02.api.letsencrypt.org/directory"})
	}
	return targets
}

// preflightResult is one checked item: a failure says what to do about it.
type preflightResult struct {
	item, problem, fix string
}

// preflight checks the server before the installation changes anything, and reports every
// problem at once with what to do about it: the things that usually cost a customer hours (a busy
// port, a blocked address, a proxy replacing certificates, a wrong clock, a small disk).
func preflight(ctx context.Context, w io.Writer, s stack, targets []preflightTarget, probes preflightProbes) error {
	fmt.Fprintln(w, "Checking the server (nothing is changed)...")
	var results []preflightResult
	results = append(results, checkMemory(probes))
	results = append(results, checkDisk(ctx, s, probes)...)
	for _, port := range webPorts {
		results = append(results, checkPort(port, probes))
	}
	results = append(results, checkReachable(ctx, targets, probes)...)
	failed := 0
	for _, r := range results {
		if r.problem == "" {
			fmt.Fprintf(w, "  ok    %s\n", r.item)
			continue
		}
		failed++
		fmt.Fprintf(w, "  FAIL  %s: %s\n        %s\n", r.item, r.problem, r.fix)
	}
	if failed > 0 {
		return fmt.Errorf("the server is not ready (%d problem(s) above); nothing was changed, run the same command again after fixing them", failed)
	}
	return nil
}

func checkMemory(probes preflightProbes) preflightResult {
	item := "memory"
	total, err := probes.memory()
	switch {
	case err != nil:
		return preflightResult{item: item, problem: err.Error(), fix: "SparTask needs a Linux server with at least 4 GB of memory"}
	case total < minMemoryBytes:
		return preflightResult{item: item, problem: fmt.Sprintf("%.1f GB", gigabytes(total)),
			fix: "SparTask needs at least 4 GB of memory (2 vCPU / 4 GB is the pilot size)"}
	}
	return preflightResult{item: fmt.Sprintf("memory %.1f GB", gigabytes(total))}
}

func checkDisk(ctx context.Context, s stack, probes preflightProbes) []preflightResult {
	dirs := []string{s.dir}
	root, err := probes.dockerRoot(ctx)
	if err != nil {
		return []preflightResult{{item: "Docker data directory", problem: err.Error(), fix: "check that Docker is running: docker info"}}
	}
	if root != s.dir {
		dirs = append(dirs, root)
	}
	var results []preflightResult
	for _, dir := range dirs {
		item := "free disk space in " + dir
		free, err := probes.freeDisk(dir)
		switch {
		case err != nil:
			results = append(results, preflightResult{item: item, problem: err.Error(), fix: "check that the directory exists and is readable"})
		case free < minFreeDiskBytes:
			results = append(results, preflightResult{item: item, problem: fmt.Sprintf("%.1f GB", gigabytes(free)),
				fix: fmt.Sprintf("free or add space: images and the first data need at least %.0f GB", gigabytes(minFreeDiskBytes))})
		default:
			results = append(results, preflightResult{item: fmt.Sprintf("%s (%.0f GB)", item, gigabytes(free))})
		}
	}
	return results
}

func checkPort(port int, probes preflightProbes) preflightResult {
	item := fmt.Sprintf("port %d", port)
	err := probes.listen(port)
	switch {
	case err == nil:
		return preflightResult{item: item + " is free"}
	case errors.Is(err, syscall.EADDRINUSE):
		return preflightResult{item: item, problem: "another program uses it (a web server such as nginx, Apache or IIS?)",
			fix: fmt.Sprintf("stop that program or install SparTask on a separate server; `sudo ss -ltnp 'sport = :%d'` shows it", port)}
	default:
		return preflightResult{item: item, problem: err.Error(), fix: "run the installation as root (sudo)"}
	}
}

// checkReachable fetches every target once and compares the clock with the first answer's Date.
func checkReachable(ctx context.Context, targets []preflightTarget, probes preflightProbes) []preflightResult {
	var results []preflightResult
	var serverTime time.Time
	for _, t := range targets {
		date, err := probes.fetch(ctx, t.url)
		if err != nil {
			results = append(results, preflightResult{item: t.name, problem: err.Error(), fix: reachFix(t.url, err)})
			continue
		}
		if serverTime.IsZero() {
			serverTime = date
		}
		results = append(results, preflightResult{item: t.name + " is reachable"})
	}
	if serverTime.IsZero() {
		return results
	}
	skew := probes.now().Sub(serverTime)
	if skew < -maxClockSkew || skew > maxClockSkew {
		return append(results, preflightResult{item: "clock", problem: fmt.Sprintf("this server's clock is %s off", skew.Round(time.Second)),
			fix: "turn on time synchronization (Ubuntu: sudo timedatectl set-ntp true); licenses and certificates depend on it"})
	}
	return append(results, preflightResult{item: "clock is synchronized"})
}

// reachFix explains the usual causes of an unreachable address.
func reachFix(url string, err error) string {
	var unknownCA x509.UnknownAuthorityError
	if errors.As(err, &unknownCA) {
		return "a proxy on this network replaces certificates: add the company's root certificate to the server " +
			"(Ubuntu: /usr/local/share/ca-certificates + update-ca-certificates) and restart Docker"
	}
	host := strings.TrimPrefix(url, "https://")
	host, _, _ = strings.Cut(host, "/")
	return fmt.Sprintf("allow outbound HTTPS (port 443) to %s in the firewall or proxy; with a proxy set HTTPS_PROXY for this command and for Docker", host)
}

// fetchDate requests url and returns the Date header of any answer: an HTTP error status still
// proves the address is reachable.
func fetchDate(ctx context.Context, url string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		return time.Time{}, fmt.Errorf("read %s: %w", url, err)
	}
	date, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s answered without a valid Date header", url)
	}
	return date, nil
}

// totalMemory reads MemTotal from /proc/meminfo.
func totalMemory() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("cannot read the memory size: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("read MemTotal: %w", err)
			}
			return kb << 10, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return 0, errors.New("MemTotal not found in /proc/meminfo")
}

func gigabytes(bytes uint64) float64 {
	return float64(bytes) / (1 << 30)
}
