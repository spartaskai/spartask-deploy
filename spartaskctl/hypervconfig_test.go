package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestHypervNetwork(t *testing.T) {
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if net.gateway.String() != "172.30.230.1" || net.vm.String() != "172.30.230.10" {
		t.Fatalf("gateway %s, vm %s", net.gateway, net.vm)
	}
	if net, err := newHypervNetwork("10.20.0.0/16"); err != nil || net.vm.String() != "10.20.0.10" {
		t.Fatalf("/16: %v %v", net.vm, err)
	}
	for _, bad := range []string{"172.30.230.5/24", "172.30.230.0/29", "10.0.0.0/8", "fd00::/64", "subnet", ""} {
		if _, err := newHypervNetwork(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// hostJSON is hostInfoScript's output as Windows PowerShell 5.1 writes it.
const hostJSON = `{"admin":true,"hyperV":true,"vm":"Running",
"addresses":[{"ip":"172.30.230.1","prefix":24,"alias":"vEthernet (SparTask)"},{"ip":"172.30.0.7","prefix":16,"alias":"Ethernet"},{"ip":"127.0.0.1","prefix":8,"alias":"Loopback Pseudo-Interface 1"}],
"nats":[{"name":"SparTask","prefix":"172.30.230.0/24"},{"name":"WSL","prefix":"172.30.0.0/16"}],
"mappings":[{"nat":"SparTask","protocol":"TCP","port":80},{"nat":"Docker","protocol":"TCP","port":443}],
"listeners":[{"address":"::","port":80,"process":"System"},{"address":"127.0.0.1","port":443,"process":"agent"}],
"dns":["127.0.0.1","10.0.0.2"]}`

func TestHostInfoConflicts(t *testing.T) {
	info, err := parseHostInfo(hostJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Admin || !info.HyperV || info.VM != "Running" || info.natPrefix() != "172.30.230.0/24" {
		t.Fatalf("parsed %+v", info)
	}
	net, err := newHypervNetwork(info.natPrefix())
	if err != nil {
		t.Fatal(err)
	}
	got := info.conflicts(net)
	want := []string{`"Ethernet" overlaps`, `"WSL" (172.30.0.0/16) overlaps`, "port 80 is in use by System on ::", `port 443 is already forwarded by the NAT network "Docker"`}
	if len(got) != len(want) {
		t.Fatalf("conflicts:\n%s", strings.Join(got, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("conflict %d = %q, want %q in it", i, got[i], w)
		}
	}

	other, err := newHypervNetwork("192.168.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	info.Listeners, info.Mappings = nil, nil
	if got := info.conflicts(other); len(got) != 0 {
		t.Fatalf("a free network conflicts: %v", got)
	}
	if _, err := parseHostInfo("WARNING: not json"); err == nil {
		t.Fatal("broken output accepted")
	}
}

func TestVMDNS(t *testing.T) {
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	got, err := vmDNS("", []string{"127.0.0.1", "169.254.1.1", "10.0.0.2", "172.30.230.1", "10.0.0.2", "fe80::1", "10.0.0.3"}, net)
	if err != nil || !reflect.DeepEqual(got, []string{"10.0.0.2", "10.0.0.3"}) {
		t.Fatalf("host resolvers: %v %v", got, err)
	}
	if got, err := vmDNS("10.1.1.1, 10.1.1.2", []string{"10.0.0.2"}, net); err != nil || !reflect.DeepEqual(got, []string{"10.1.1.1", "10.1.1.2"}) {
		t.Fatalf("-dns: %v %v", got, err)
	}
	for _, bad := range []string{"10.1.1.1,dns", "127.0.0.1", "172.30.230.1"} {
		if _, err := vmDNS(bad, nil, net); err == nil {
			t.Errorf("-dns %q accepted", bad)
		}
	}
	if _, err := vmDNS("", []string{"127.0.0.1"}, net); err == nil || !strings.Contains(err.Error(), "-dns") {
		t.Fatalf("no usable resolver: %v", err)
	}
}

func TestCloudInit(t *testing.T) {
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample spartask-hyperv"
	files, err := cloudInit(key+"\n", "spartask-0123abcd", net, []string{"10.0.0.2", "10.0.0.3"})
	if err != nil {
		t.Fatal(err)
	}
	user := string(files["user-data"])
	for _, want := range []string{"#cloud-config\n", "  - name: spartask\n", `sudo: "ALL=(ALL) NOPASSWD:ALL"`, `      - "` + key + `"`, "lock_passwd: true", "ssh_pwauth: false"} {
		if !strings.Contains(user, want) {
			t.Errorf("user-data has no %q:\n%s", want, user)
		}
	}
	if meta := string(files["meta-data"]); meta != "instance-id: \"spartask-0123abcd\"\nlocal-hostname: spartask\n" {
		t.Errorf("meta-data:\n%s", meta)
	}
	network := string(files["network-config"])
	for _, want := range []string{"driver: hv_netvsc", "addresses: [172.30.230.10/24]", "via: 172.30.230.1", "addresses: [10.0.0.2, 10.0.0.3]"} {
		if !strings.Contains(network, want) {
			t.Errorf("network-config has no %q:\n%s", want, network)
		}
	}
	for _, bad := range []string{"ssh-rsa AAAA", key + "\nssh-ed25519 AAAA other", ""} {
		if _, err := cloudInit(bad, "id", net, []string{"10.0.0.2"}); err == nil {
			t.Errorf("key %q accepted", bad)
		}
	}
}

func TestParseHypervArgs(t *testing.T) {
	o, err := parseHypervArgs([]string{"-activation", "ABC-123", "-cpus=4", "--memory-gb", "8", "-license-server", "https://accounts.example.test", "-subnet", "10.9.0.0/24", "-dns=10.0.0.2", "-y"})
	if err != nil {
		t.Fatal(err)
	}
	if o.vm != (hypervVM{cpus: 4, memoryGB: 8, diskGB: 64}) || o.subnet != "10.9.0.0/24" || !o.subnetSet || o.dns != "10.0.0.2" {
		t.Fatalf("options %+v", o)
	}
	if want := []string{"-activation", "ABC-123", "-license-server", "https://accounts.example.test", "-y"}; !reflect.DeepEqual(o.install, want) {
		t.Fatalf("install args %q", o.install)
	}
	o, err = parseHypervArgs([]string{"-activation", "K", "-tls", "files", "-cert", `C:\certs\firma.pem`, "-key=C:\\certs\\firma.key"})
	if err != nil || o.certFile != `C:\certs\firma.pem` || o.keyFile != `C:\certs\firma.key` {
		t.Fatalf("certificate files: %+v %v", o, err)
	}
	if want := []string{"-activation", "K", "-tls", "files", "-cert", hypervCertPath, "-key", hypervKeyPath}; !reflect.DeepEqual(o.vmInstallArgs(), want) {
		t.Fatalf("VM install args %q", o.vmInstallArgs())
	}
	o, err = parseHypervArgs(nil)
	if err != nil || o.subnet != defaultHypervSubnet || o.subnetSet || o.vm != (hypervVM{cpus: 2, memoryGB: 4, diskGB: 64}) {
		t.Fatalf("defaults %+v %v", o, err)
	}
	for _, bad := range [][]string{{"-cpus"}, {"-cpus", "two"}, {"-cpus", "0"}, {"-disk-gb", "8"}, {"-memory-gb=1"}} {
		if _, err := parseHypervArgs(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRemoteInstallCommand(t *testing.T) {
	got := remoteInstallCommand("v1.2.0", []string{"-activation", "AB'C", "-admin-name", "Ayşe Yılmaz"})
	want := `curl -fsSL 'https://github.com/spartaskai/spartask-deploy/releases/download/v1.2.0/install.sh' | sudo SPARTASK_DEPLOY_VERSION='v1.2.0' sh -s -- '-activation' 'AB'\''C' '-admin-name' 'Ayşe Yılmaz'`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := remoteInstallCommand("dev", nil); got != `curl -fsSL 'https://github.com/spartaskai/spartask-deploy/releases/latest/download/install.sh' | sudo sh -s --` {
		t.Fatalf("development build: %s", got)
	}
}

func TestPowerShellScripts(t *testing.T) {
	if got := psQuote(`C:\It's`); got != `'C:\It''s'` {
		t.Fatalf("psQuote: %s", got)
	}
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	network := networkScript(net)
	for _, want := range []string{"New-VMSwitch -Name 'SparTask' -SwitchType Internal", "-InterfaceAlias 'vEthernet (SparTask)' -IPAddress '172.30.230.1' -PrefixLength 24",
		"-InternalIPInterfaceAddressPrefix '172.30.230.0/24'", "foreach ($port in 80,443)", "-InternalIPAddress '172.30.230.10' -InternalPort $port", "-LocalPort 80,443 -Action Allow"} {
		if !strings.Contains(network, want) {
			t.Errorf("network script has no %q", want)
		}
	}
	vm := createVMScript(hypervVM{cpus: 2, memoryGB: 4, diskGB: 64}, `C:\It's\vm`, `C:\It's\vm\spartask.vhdx`, `C:\It's\vm\seed.vhdx`)
	for _, want := range []string{"-Generation 2 -MemoryStartupBytes 4294967296 -VHDPath 'C:\\It''s\\vm\\spartask.vhdx' -SwitchName 'SparTask' -Path 'C:\\It''s\\vm'",
		"-ProcessorCount 2 -StaticMemory", "-SecureBootTemplate MicrosoftUEFICertificateAuthority", "Add-VMHardDiskDrive -VMName 'SparTask' -Path 'C:\\It''s\\vm\\seed.vhdx'"} {
		if !strings.Contains(vm, want) {
			t.Errorf("VM script has no %q", want)
		}
	}
	if s := resizeDiskScript(`C:\vm\new.vhdx`, 64<<30); !strings.Contains(s, "Resize-VHD -Path 'C:\\vm\\new.vhdx' -SizeBytes 68719476736") {
		t.Errorf("resize script: %s", s)
	}
	for _, s := range []string{hostInfoScript(), enableHyperVScript(), network, vm, startVMScript(), natPrefixScript(), convertDiskScript("a.vhd", "a.vhdx")} {
		if !strings.HasPrefix(s, "$ErrorActionPreference = 'Stop'") || !strings.Contains(s, "exit 1") {
			t.Errorf("script without error handling:\n%s", s)
		}
	}
}

func TestEncodePowerShell(t *testing.T) {
	script := "Write-Output 'Ayşe \"x\"'\n"
	b, err := base64.StdEncoding.DecodeString(encodePowerShell(script))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Fatalf("decoded %q", got)
	}
}

func TestWriteSeedDisk(t *testing.T) {
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	files, err := cloudInit("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample k", "spartask-1", net, []string{"10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.vhd")
	if err := writeSeedDisk(files, path); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk) != seedDiskSize+vhdFooterSize || !bytes.HasPrefix(disk[seedDiskSize:], []byte("conectix")) {
		t.Fatalf("seed disk of %d bytes, footer %q", len(disk), disk[seedDiskSize:seedDiskSize+8])
	}
	label, got := readFATRoot(t, disk[:seedDiskSize])
	if label != "CIDATA" || !reflect.DeepEqual(got, files) {
		t.Fatalf("label %q, files %v", label, got)
	}
	if err := writeSeedDisk(files, path); err == nil {
		t.Fatal("an existing seed disk was overwritten")
	}
}

func TestWriteRawDisk(t *testing.T) {
	const bits = 12
	cs := uint64(1) << bits
	size := 3 * cs
	disk := bytes.Repeat([]byte{7}, int(size))
	img := buildQcow2(t, bits, size, disk, []qcow2Cluster{clusterPlain, clusterCompressed, clusterPlain})
	dir := t.TempDir()
	image := filepath.Join(dir, "ubuntu.img")
	if err := os.WriteFile(image, img, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "raw.vhd")
	got, err := writeRawDisk(image, path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1<<20 || len(raw) != 1<<20+vhdFooterSize || !bytes.Equal(raw[:size], disk) || !bytes.HasPrefix(raw[got:], []byte("conectix")) {
		t.Fatalf("disk size %d, file %d bytes", got, len(raw))
	}
}

// TestPowerShellSyntax parses the scripts with PowerShell where it is installed (GitHub's
// runners have it).
func TestPowerShellSyntax(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	net, err := newHypervNetwork("172.30.230.0/24")
	if err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"host":    hostInfoScript(),
		"hyperv":  enableHyperVScript(),
		"network": networkScript(net),
		"convert": convertDiskScript(`C:\vm\a.vhd`, `C:\vm\a.vhdx`),
		"resize":  resizeDiskScript(`C:\vm\a.vhdx`, 64<<30),
		"vm":      createVMScript(hypervVM{cpus: 2, memoryGB: 4, diskGB: 64}, `C:\vm`, `C:\vm\d.vhdx`, `C:\vm\s.vhdx`),
		"start":   startVMScript(),
		"nat":     natPrefixScript(),
	}
	const check = `$e = $null; [void][System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(), [ref]$null, [ref]$e); if ($e) { $e | ForEach-Object { "$($_.Extent.StartLineNumber): $($_.Message)" }; exit 1 }`
	for name, script := range scripts {
		cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", check)
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s script: %v\n%s\n%s", name, err, out, script)
		}
	}
}
