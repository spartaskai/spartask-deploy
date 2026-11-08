package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

const hypervUsage = `usage:
  spartaskctl hyperv install [-cpus 2] [-memory-gb 4] [-disk-gb 64] [-subnet 172.30.230.0/24]
                             [-dns IP,IP] <spartaskctl install options>
  spartaskctl hyperv ssh [command]`

// runHyperV runs SparTask on Windows: a Hyper-V VM with Ubuntu, reachable through ports 80 and
// 443 of the host, where install.sh installs it like on any Linux server.
func runHyperV(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(hypervUsage)
	}
	if runtime.GOOS != "windows" {
		return errors.New("spartaskctl hyperv runs on Windows; on a Linux server use spartaskctl install")
	}
	paths, err := newHypervPaths()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		return hypervInstall(ctx, paths, args[1:])
	case "ssh":
		return hypervShell(ctx, paths, args[1:])
	}
	return errors.New(hypervUsage)
}

// hypervPaths are the files kept under %ProgramData%\SparTask.
type hypervPaths struct{ root string }

func newHypervPaths() (hypervPaths, error) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return hypervPaths{}, errors.New("%ProgramData% is not set")
	}
	return hypervPaths{root: filepath.Join(programData, hypervName)}, nil
}

func (p hypervPaths) sshDir() string     { return filepath.Join(p.root, "ssh") }
func (p hypervPaths) key() string        { return filepath.Join(p.sshDir(), "id_ed25519") }
func (p hypervPaths) knownHosts() string { return filepath.Join(p.sshDir(), "known_hosts") }
func (p hypervPaths) images() string     { return filepath.Join(p.root, "images") }
func (p hypervPaths) vmDir() string      { return filepath.Join(p.root, "vm") }
func (p hypervPaths) disk() string       { return filepath.Join(p.vmDir(), "spartask.vhdx") }
func (p hypervPaths) seed() string       { return filepath.Join(p.vmDir(), "seed.vhdx") }

// Intermediate files of a disk being made; they never belong to a VM.
func (p hypervPaths) rawDisk() string { return filepath.Join(p.vmDir(), "spartask-raw.vhd") }
func (p hypervPaths) newDisk() string { return filepath.Join(p.vmDir(), "spartask-new.vhdx") }
func (p hypervPaths) rawSeed() string { return filepath.Join(p.vmDir(), "seed.vhd") }
func (p hypervPaths) leftovers() []string {
	return []string{p.rawDisk(), p.newDisk(), p.rawSeed(), p.seed()}
}

func hypervInstall(ctx context.Context, paths hypervPaths, args []string) error {
	o, err := parseHypervArgs(args)
	if err != nil {
		return err
	}
	out, err := powershell(ctx, hostInfoScript())
	if err != nil {
		return err
	}
	info, err := parseHostInfo(out)
	if err != nil {
		return err
	}
	if !info.Admin {
		return errors.New("run this in PowerShell opened with \"Run as administrator\"")
	}
	if !info.HyperV {
		fmt.Println("Turning Hyper-V on...")
		if _, err := powershell(ctx, enableHyperVScript()); err != nil {
			return fmt.Errorf("turn Hyper-V on: %w (the processor's virtualization must be enabled; on a virtual server the provider must allow nested virtualization)", err)
		}
		fmt.Println("\nHyper-V works after a restart: restart Windows (Restart-Computer), then run the same command again.")
		return nil
	}
	for _, tool := range []string{"ssh.exe", "ssh-keygen.exe"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is missing: add the OpenSSH client with Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0", tool)
		}
	}

	subnet := o.subnet
	if existing := info.natPrefix(); existing != "" {
		if o.subnetSet && existing != o.subnet {
			return fmt.Errorf("the %s network already uses %s; run without -subnet", hypervName, existing)
		}
		subnet = existing
	}
	net, err := newHypervNetwork(subnet)
	if err != nil {
		return err
	}
	if conflicts := info.conflicts(net); len(conflicts) > 0 {
		return fmt.Errorf("this computer is not ready for SparTask:\n  - %s", strings.Join(conflicts, "\n  - "))
	}
	fmt.Printf("Network %s: this computer is %s, the VM %s; ports %s go to the VM\n", net.prefix, net.gateway, net.vm, joinInts(hypervPorts))
	if _, err := powershell(ctx, networkScript(net)); err != nil {
		return fmt.Errorf("set up the %s network: %w", hypervName, err)
	}
	if info.VM == "" {
		if err := createHypervVM(ctx, paths, o, net, info.DNS); err != nil {
			return err
		}
	} else {
		fmt.Printf("Using the existing VM %s (%s)\n", hypervName, info.VM)
	}
	if _, err := powershell(ctx, startVMScript()); err != nil {
		return fmt.Errorf("start the VM: %w", err)
	}

	vm := vmSSH{paths: paths, host: net.vm}
	fmt.Println("Waiting for the VM (its first start takes a few minutes)...")
	if err := vm.wait(ctx, 10*time.Minute); err != nil {
		return err
	}
	if err := vm.waitCloudInit(ctx); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	manage := fmt.Sprintf("%s hyperv ssh -- sudo %s/spartaskctl -dir %s", self, hypervInstallDir, hypervInstallDir)
	installed, err := vm.installed(ctx)
	if err != nil {
		return err
	}
	if installed {
		fmt.Printf("\nSparTask is already installed in the VM. Manage it with:\n  %s status\n", manage)
		fmt.Printf("If its first start was interrupted, continue with:\n  %s restart\n", manage)
		return nil
	}
	if err := vm.run(ctx, remoteInstallCommand(toolVersion, o.install)); err != nil {
		return fmt.Errorf("the installation in the VM stopped: %w\nAfter fixing the cause run the same command again", err)
	}
	fmt.Printf("\nSparTask runs in the Hyper-V VM %s (%s). Ports %s of this computer are forwarded to it:\n", hypervName, net.vm, joinInts(hypervPorts))
	fmt.Println("the DNS record of its address points to this computer's public IP address. The VM starts and stops with Windows.")
	fmt.Printf("Manage the installation:\n  %s status\n  %s backup\n", manage, manage)
	return nil
}

// createHypervVM makes the VM's disks (Ubuntu's cloud image and the cloud-init seed) and the VM.
func createHypervVM(ctx context.Context, paths hypervPaths, o hypervOptions, net hypervNetwork, hostDNS []string) error {
	if _, err := os.Stat(paths.disk()); err == nil {
		return fmt.Errorf("%s exists but there is no %s VM: it may hold an installation's data; attach it to a VM or move it away, then run again", paths.disk(), hypervName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dns, err := vmDNS(o.dns, hostDNS, net)
	if err != nil {
		return err
	}
	for _, dir := range []string{paths.vmDir(), paths.images()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, leftover := range paths.leftovers() {
		if err := removeIfExists(leftover); err != nil {
			return err
		}
	}
	publicKey, err := ensureHypervKey(ctx, paths)
	if err != nil {
		return err
	}

	image, err := fetchImage(ctx, http.DefaultClient, ubuntuImageBase, ubuntuImageName, paths.images())
	if err != nil {
		return fmt.Errorf("download Ubuntu: %w", err)
	}
	fmt.Println("Making the VM disk...")
	size, err := writeRawDisk(image, paths.rawDisk())
	if err != nil {
		return fmt.Errorf("convert %s: %w", image, err)
	}
	diskSize := int64(o.vm.diskGB) << 30
	if diskSize < size {
		return fmt.Errorf("-disk-gb %d is smaller than Ubuntu's disk (%d MB)", o.vm.diskGB, size>>20)
	}
	if _, err := powershell(ctx, convertDiskScript(paths.rawDisk(), paths.newDisk())); err != nil {
		return err
	}
	if _, err := powershell(ctx, resizeDiskScript(paths.newDisk(), diskSize)); err != nil {
		return err
	}
	if err := os.Rename(paths.newDisk(), paths.disk()); err != nil {
		return err
	}

	id, err := randomHex(8)
	if err != nil {
		return err
	}
	files, err := cloudInit(publicKey, "spartask-"+id, net, dns)
	if err != nil {
		return err
	}
	if err := writeSeedDisk(files, paths.rawSeed()); err != nil {
		return err
	}
	if _, err := powershell(ctx, convertDiskScript(paths.rawSeed(), paths.seed())); err != nil {
		return err
	}

	// A new VM has a new host key.
	if err := removeIfExists(paths.knownHosts()); err != nil {
		return err
	}
	fmt.Printf("Creating the VM %s (%d CPUs, %d GB memory, %d GB disk)...\n", hypervName, o.vm.cpus, o.vm.memoryGB, o.vm.diskGB)
	if _, err := powershell(ctx, createVMScript(o.vm, paths.vmDir(), paths.disk(), paths.seed())); err != nil {
		return fmt.Errorf("create the VM: %w", err)
	}
	// The VM has its own disk now.
	return removeIfExists(image)
}

// writeRawDisk writes the qcow2 image as a fixed VHD at path and returns its disk size.
func writeRawDisk(image, path string) (int64, error) {
	src, err := os.Open(image)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	size, err := qcow2ToRaw(src, info.Size(), dst)
	if err == nil {
		size, err = writeFixedVHD(dst, size, time.Now())
	}
	return size, errors.Join(err, dst.Close())
}

// seedDiskSize is the size of the seed disk: the FAT volume at its start, zeros after it (Hyper-V
// disks are not that small).
const seedDiskSize = 8 << 20

// writeSeedDisk writes the cloud-init seed (a FAT volume labelled CIDATA, which cloud-init's
// NoCloud source reads) as a fixed VHD at path.
func writeSeedDisk(files map[string][]byte, path string) error {
	now := time.Now()
	img, err := fatImage("CIDATA", files, now)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(img, 0)
	if err == nil {
		_, err = writeFixedVHD(f, seedDiskSize, now)
	}
	return errors.Join(err, f.Close())
}

// ensureHypervKey returns the public half of the key the host signs in to the VM with, making
// the key first. Only administrators and SYSTEM may read it (ssh refuses keys others can read).
func ensureHypervKey(ctx context.Context, paths hypervPaths) (string, error) {
	if err := os.MkdirAll(paths.sshDir(), 0o700); err != nil {
		return "", err
	}
	// Administrators (S-1-5-32-544) and SYSTEM (S-1-5-18), nothing inherited.
	if out, err := exec.CommandContext(ctx, "icacls", paths.sshDir(), "/inheritance:r",
		"/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F").CombinedOutput(); err != nil {
		return "", fmt.Errorf("restrict %s: %w: %s", paths.sshDir(), err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(paths.key()); errors.Is(err, os.ErrNotExist) {
		out, err := exec.CommandContext(ctx, "ssh-keygen.exe", "-q", "-t", "ed25519", "-N", "", "-C", "spartask-hyperv", "-f", paths.key()).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("ssh-keygen: %w: %s", err, strings.TrimSpace(string(out)))
		}
	} else if err != nil {
		return "", err
	}
	public, err := os.ReadFile(paths.key() + ".pub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(public)), nil
}

// vmSSH signs in to the VM as hypervUser with the host's key.
type vmSSH struct {
	paths hypervPaths
	host  netip.Addr
}

func (s vmSSH) command(ctx context.Context, options []string, remote ...string) *exec.Cmd {
	args := []string{"-F", "none", "-i", s.paths.key(), "-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + s.paths.knownHosts(), "-o", "StrictHostKeyChecking=accept-new",
		"-o", "LogLevel=ERROR", "-o", "ServerAliveInterval=30"}
	args = append(append(args, options...), hypervUser+"@"+s.host.String())
	return exec.CommandContext(ctx, "ssh.exe", append(args, remote...)...)
}

// batch runs a command without a terminal and returns its exit code; an ssh failure is an error.
func (s vmSSH) batch(ctx context.Context, remote string) (int, error) {
	cmd := s.command(ctx, []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5"}, remote)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = os.Stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() != 255 { // 255: ssh itself failed
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 0, fmt.Errorf("ssh %s: %w: %s", s.host, err, strings.TrimSpace(stderr.String()))
	}
	return 0, nil
}

// wait waits until the VM accepts the host's key.
func (s vmSSH) wait(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := s.batch(ctx, "true")
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the VM does not answer on %s after %s (see its console in Hyper-V Manager): %w", s.host, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// waitCloudInit waits until cloud-init finished the VM's first-boot setup.
func (s vmSSH) waitCloudInit(ctx context.Context) error {
	code, err := s.batch(ctx, "cloud-init status --wait")
	if err != nil {
		return err
	}
	switch code {
	case 0:
		return nil
	case 2: // done with recoverable errors
		fmt.Println("cloud-init reported recoverable errors (see: sudo cloud-init status --long in the VM)")
		return nil
	}
	return fmt.Errorf("cloud-init failed in the VM (exit %d); see: spartaskctl hyperv ssh -- sudo cloud-init status --long", code)
}

// installed reports whether SparTask's configuration exists in the VM.
func (s vmSSH) installed(ctx context.Context) (bool, error) {
	code, err := s.batch(ctx, "sudo test -e "+shQuote(hypervInstallDir+"/.env"))
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// run runs a command on a terminal connected to this console (the installer asks questions).
func (s vmSSH) run(ctx context.Context, remote ...string) error {
	cmd := s.command(ctx, []string{"-t"}, remote...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// hypervShell opens a shell in the VM, or runs a command there.
func hypervShell(ctx context.Context, paths hypervPaths, args []string) error {
	out, err := powershell(ctx, natPrefixScript())
	if err != nil {
		return err
	}
	prefix := strings.TrimSpace(out)
	if prefix == "" {
		return fmt.Errorf("there is no %s network on this computer: run spartaskctl hyperv install", hypervName)
	}
	net, err := newHypervNetwork(prefix)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return vmSSH{paths: paths, host: net.vm}.run(ctx, args...)
}

// powershell runs a script with Windows PowerShell and returns its output. The script travels
// encoded, so its quotes reach PowerShell unchanged.
func powershell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", fmt.Errorf("powershell: %w", err)
	}
	return string(out), nil
}

// encodePowerShell is the -EncodedCommand form of a script: base64 of its UTF-16LE text.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		b[2*i], b[2*i+1] = byte(u), byte(u>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}
