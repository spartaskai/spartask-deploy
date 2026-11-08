package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// Windows hosts run SparTask in a Hyper-V VM (Ubuntu cloud image) behind a NAT network of their
// own; this file builds what that setup needs as text: the network plan, the cloud-init seed
// and the PowerShell scripts that hyperv.go runs.

const (
	hypervName       = "SparTask"     // VM, internal switch, NAT network
	hypervFirewall   = "SparTask-Web" // inbound rule for the forwarded ports
	hypervUser       = "spartask"     // VM account the host signs in with
	hypervInstallDir = "/opt/spartask"
	deployReleases   = "https://github.com/spartaskai/spartask-deploy/releases"
)

// hypervPorts are forwarded from the host to the VM (Caddy: HTTP and HTTPS).
var hypervPorts = []int{80, 443}

// hypervAdapter is the host's adapter on the internal switch.
var hypervAdapter = "vEthernet (" + hypervName + ")"

// hypervNetwork is the VM's NAT network: the host is its gateway (.1), the VM has .10.
type hypervNetwork struct {
	prefix  netip.Prefix
	gateway netip.Addr
	vm      netip.Addr
}

func newHypervNetwork(cidr string) (hypervNetwork, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return hypervNetwork{}, fmt.Errorf("subnet %q is not an IPv4 network such as 172.30.230.0/24", cidr)
	}
	if prefix.Bits() < 16 || prefix.Bits() > 28 {
		return hypervNetwork{}, fmt.Errorf("subnet %s: use a prefix length between /16 and /28", cidr)
	}
	if prefix.Masked() != prefix {
		return hypervNetwork{}, fmt.Errorf("subnet %s is not a network address (did you mean %s?)", cidr, prefix.Masked())
	}
	base := prefix.Addr().As4()
	n := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	at := func(i uint32) netip.Addr {
		v := n + i
		return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	return hypervNetwork{prefix: prefix, gateway: at(1), vm: at(10)}, nil
}

// hostInfo is what hostInfoScript reports about the Windows host.
type hostInfo struct {
	Admin     bool   `json:"admin"`
	HyperV    bool   `json:"hyperV"` // the Hyper-V service runs
	VM        string `json:"vm"`     // state of the SparTask VM, empty without one
	Addresses []struct {
		IP     string `json:"ip"`
		Prefix int    `json:"prefix"`
		Alias  string `json:"alias"`
	} `json:"addresses"`
	NATs []struct {
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
	} `json:"nats"`
	Mappings []struct {
		NAT      string `json:"nat"`
		Protocol string `json:"protocol"`
		Port     int    `json:"port"`
	} `json:"mappings"`
	Listeners []struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		Process string `json:"process"`
	} `json:"listeners"`
	DNS []string `json:"dns"`
}

func parseHostInfo(out string) (hostInfo, error) {
	var info hostInfo
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &info); err != nil {
		return hostInfo{}, fmt.Errorf("read host information: %w", err)
	}
	return info, nil
}

// natPrefix is the subnet of an existing SparTask NAT network, empty without one.
func (h hostInfo) natPrefix() string {
	for _, n := range h.NATs {
		if n.Name == hypervName {
			return n.Prefix
		}
	}
	return ""
}

// conflicts lists what keeps the SparTask network and port forwarding from working on this host:
// addresses or other NAT networks overlapping the subnet, and other programs or NAT networks
// holding the forwarded ports.
func (h hostInfo) conflicts(net hypervNetwork) []string {
	var found []string
	for _, a := range h.Addresses {
		if a.Alias == hypervAdapter {
			continue
		}
		ip, err := netip.ParseAddr(a.IP)
		if err != nil {
			continue
		}
		if p := netip.PrefixFrom(ip, a.Prefix).Masked(); p.IsValid() && p.Overlaps(net.prefix) {
			found = append(found, fmt.Sprintf("the address %s/%d of %q overlaps %s (choose another network with -subnet)", a.IP, a.Prefix, a.Alias, net.prefix))
		}
	}
	for _, n := range h.NATs {
		if n.Name == hypervName {
			continue
		}
		if p, err := netip.ParsePrefix(n.Prefix); err == nil && p.Overlaps(net.prefix) {
			found = append(found, fmt.Sprintf("the NAT network %q (%s) overlaps %s (choose another network with -subnet)", n.Name, n.Prefix, net.prefix))
		}
	}
	for _, port := range hypervPorts {
		for _, l := range h.Listeners {
			if ip, err := netip.ParseAddr(l.Address); l.Port == port && (err != nil || !ip.IsLoopback()) {
				found = append(found, fmt.Sprintf("port %d is in use by %s on %s (IIS or another web server: stop it, SparTask serves this port)", port, l.Process, l.Address))
			}
		}
		for _, m := range h.Mappings {
			if m.NAT != hypervName && m.Port == port && strings.EqualFold(m.Protocol, "TCP") {
				found = append(found, fmt.Sprintf("port %d is already forwarded by the NAT network %q (Get-NetNatStaticMapping)", port, m.NAT))
			}
		}
	}
	return found
}

// vmDNS returns the resolvers the VM uses: the given list, otherwise the host's own resolvers
// that the VM can reach (not loopback or link-local, not inside the VM network).
func vmDNS(explicit string, host []string, net hypervNetwork) ([]string, error) {
	candidates := host
	if explicit != "" {
		candidates = strings.Split(explicit, ",")
	}
	var dns []string
	for _, c := range candidates {
		ip, err := netip.ParseAddr(strings.TrimSpace(c))
		if err != nil || !ip.Is4() {
			if explicit != "" {
				return nil, fmt.Errorf("-dns %q: %q is not an IPv4 address", explicit, c)
			}
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || net.prefix.Contains(ip) {
			if explicit != "" {
				return nil, fmt.Errorf("-dns %q: the VM cannot reach %s", explicit, ip)
			}
			continue
		}
		if !slices.Contains(dns, ip.String()) {
			dns = append(dns, ip.String())
		}
	}
	if len(dns) == 0 {
		return nil, errors.New("this computer has no DNS server the VM can use (e.g. it answers DNS itself on 127.0.0.1): give the VM resolvers with -dns, e.g. -dns 10.0.0.2,10.0.0.3")
	}
	return dns, nil
}

// cloudInit returns the NoCloud seed files: the account the host signs in with (its SSH key only,
// passwordless sudo), the VM's identity and its static address on the NAT network.
func cloudInit(publicKey, instanceID string, net hypervNetwork, dns []string) (map[string][]byte, error) {
	publicKey = strings.TrimSpace(publicKey)
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") || strings.ContainsAny(publicKey, "\r\n") {
		return nil, errors.New("the SSH public key is not a single ssh-ed25519 line")
	}
	q := strconv.Quote // Go's escapes are valid in a YAML double-quoted scalar
	userData := "#cloud-config\n" +
		"hostname: spartask\n" +
		"users:\n" +
		"  - name: " + hypervUser + "\n" +
		"    gecos: SparTask\n" +
		"    groups: [sudo]\n" +
		"    shell: /bin/bash\n" +
		"    sudo: " + q("ALL=(ALL) NOPASSWD:ALL") + "\n" +
		"    lock_passwd: true\n" +
		"    ssh_authorized_keys:\n" +
		"      - " + q(publicKey) + "\n" +
		"ssh_pwauth: false\n" +
		"disable_root: true\n"
	metaData := "instance-id: " + q(instanceID) + "\n" +
		"local-hostname: spartask\n"
	bits := strconv.Itoa(net.prefix.Bits())
	networkConfig := "version: 2\n" +
		"ethernets:\n" +
		"  eth0:\n" +
		"    match:\n" +
		"      driver: hv_netvsc\n" +
		"    set-name: eth0\n" +
		"    dhcp4: false\n" +
		"    addresses: [" + net.vm.String() + "/" + bits + "]\n" +
		"    routes:\n" +
		"      - to: default\n" +
		"        via: " + net.gateway.String() + "\n" +
		"    nameservers:\n" +
		"      addresses: [" + strings.Join(dns, ", ") + "]\n"
	return map[string][]byte{
		"user-data":      []byte(userData),
		"meta-data":      []byte(metaData),
		"network-config": []byte(networkConfig),
	}, nil
}

// psQuote is s as a PowerShell single-quoted string.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// shQuote is s as a POSIX shell single-quoted word.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psScript wraps a script so that any error stops it with the error's message on stderr.
func psScript(body string) string {
	return "$ErrorActionPreference = 'Stop'\n" +
		"$ProgressPreference = 'SilentlyContinue'\n" +
		"[Console]::OutputEncoding = [Text.Encoding]::UTF8\n" +
		"try {\n" + body + "\n} catch {\n" +
		"  [Console]::Error.WriteLine($_.Exception.Message)\n" +
		"  exit 1\n}\n"
}

// hostInfoScript prints the hostInfo JSON.
func hostInfoScript() string {
	ports := joinInts(hypervPorts)
	return psScript(`$hv = [bool](Get-Command Get-VM -ErrorAction SilentlyContinue) -and ((Get-Service vmms -ErrorAction SilentlyContinue).Status -eq 'Running')
$vm = ''
if ($hv) {
  $v = Get-VM -Name ` + psQuote(hypervName) + ` -ErrorAction SilentlyContinue
  if ($v) { $vm = "$($v.State)" }
}
@{
  admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
  hyperV = $hv
  vm = $vm
  addresses = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | ForEach-Object { @{ ip = $_.IPAddress; prefix = [int]$_.PrefixLength; alias = $_.InterfaceAlias } })
  nats = @(Get-NetNat -ErrorAction SilentlyContinue | ForEach-Object { @{ name = $_.Name; prefix = $_.InternalIPInterfaceAddressPrefix } })
  mappings = @(Get-NetNatStaticMapping -ErrorAction SilentlyContinue | ForEach-Object { @{ nat = $_.NatName; protocol = "$($_.Protocol)"; port = [int]$_.ExternalPort } })
  listeners = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -in ` + ports + ` } | ForEach-Object { @{ address = $_.LocalAddress; port = [int]$_.LocalPort; process = "$((Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue).ProcessName)" } })
  dns = @(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric | ForEach-Object { (Get-DnsClientServerAddress -InterfaceIndex $_.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses } | Select-Object -Unique)
} | ConvertTo-Json -Compress -Depth 4`)
}

// enableHyperVScript turns the Hyper-V role (Windows Server) or feature (Windows 10/11) on; it
// takes effect after a restart.
func enableHyperVScript() string {
	return psScript(`if ((Get-CimInstance Win32_OperatingSystem).ProductType -eq 1) {
  Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All -NoRestart | Out-Null
} else {
  Install-WindowsFeature -Name Hyper-V -IncludeManagementTools | Out-Null
}`)
}

// networkScript creates what is missing of the internal switch, the host's gateway address on it,
// the NAT network, the port forwarding to the VM and the firewall rule for it.
func networkScript(net hypervNetwork) string {
	name, alias := psQuote(hypervName), psQuote(hypervAdapter)
	gateway, vm := psQuote(net.gateway.String()), psQuote(net.vm.String())
	return psScript(`if (-not (Get-VMSwitch -Name ` + name + ` -ErrorAction SilentlyContinue)) {
  New-VMSwitch -Name ` + name + ` -SwitchType Internal | Out-Null
}
if (-not (Get-NetIPAddress -InterfaceAlias ` + alias + ` -IPAddress ` + gateway + ` -ErrorAction SilentlyContinue)) {
  New-NetIPAddress -InterfaceAlias ` + alias + ` -IPAddress ` + gateway + ` -PrefixLength ` + strconv.Itoa(net.prefix.Bits()) + ` | Out-Null
}
if (-not (Get-NetNat -Name ` + name + ` -ErrorAction SilentlyContinue)) {
  New-NetNat -Name ` + name + ` -InternalIPInterfaceAddressPrefix ` + psQuote(net.prefix.String()) + ` | Out-Null
}
foreach ($port in ` + joinInts(hypervPorts) + `) {
  if (-not (Get-NetNatStaticMapping -NatName ` + name + ` -ErrorAction SilentlyContinue | Where-Object { $_.ExternalPort -eq $port -and "$($_.Protocol)" -eq 'TCP' })) {
    Add-NetNatStaticMapping -NatName ` + name + ` -Protocol TCP -ExternalIPAddress '0.0.0.0' -ExternalPort $port -InternalIPAddress ` + vm + ` -InternalPort $port | Out-Null
  }
}
if (-not (Get-NetFirewallRule -Name ` + psQuote(hypervFirewall) + ` -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name ` + psQuote(hypervFirewall) + ` -DisplayName 'SparTask (HTTP, HTTPS)' -Direction Inbound -Protocol TCP -LocalPort ` + joinInts(hypervPorts) + ` -Action Allow | Out-Null
}`)
}

// convertDiskScript turns a fixed VHD into a dynamic VHDX (generation 2 VMs use VHDX only) and
// removes the VHD.
func convertDiskScript(vhd, vhdx string) string {
	return psScript(`Convert-VHD -Path ` + psQuote(vhd) + ` -DestinationPath ` + psQuote(vhdx) + ` -VHDType Dynamic
Remove-Item -LiteralPath ` + psQuote(vhd))
}

// resizeDiskScript grows a VHDX to size bytes; Ubuntu grows its root partition into the space
// on first boot.
func resizeDiskScript(vhdx string, size int64) string {
	return psScript(`Resize-VHD -Path ` + psQuote(vhdx) + ` -SizeBytes ` + strconv.FormatInt(size, 10))
}

// hypervVM is the VM's hardware.
type hypervVM struct {
	cpus     int
	memoryGB int
	diskGB   int
}

// createVMScript creates the generation 2 VM booting disk, with the cloud-init seed as a second
// disk: Secure Boot with the template for Linux, fixed memory, no automatic checkpoints (a
// database runs inside), started with the host and shut down with it.
func createVMScript(vm hypervVM, dir, disk, seed string) string {
	name := psQuote(hypervName)
	return psScript(`New-VM -Name ` + name + ` -Generation 2 -MemoryStartupBytes ` + strconv.FormatInt(int64(vm.memoryGB)<<30, 10) +
		` -VHDPath ` + psQuote(disk) + ` -SwitchName ` + name + ` -Path ` + psQuote(dir) + ` | Out-Null
Set-VM -Name ` + name + ` -ProcessorCount ` + strconv.Itoa(vm.cpus) + ` -StaticMemory -AutomaticStartAction Start -AutomaticStopAction ShutDown -AutomaticCheckpointsEnabled $false
Set-VMFirmware -VMName ` + name + ` -SecureBootTemplate MicrosoftUEFICertificateAuthority -FirstBootDevice (Get-VMHardDiskDrive -VMName ` + name + ` | Select-Object -First 1)
Add-VMHardDiskDrive -VMName ` + name + ` -Path ` + psQuote(seed))
}

// startVMScript starts (or resumes) the VM unless it runs.
func startVMScript() string {
	name := psQuote(hypervName)
	return psScript(`if ((Get-VM -Name ` + name + `).State -ne 'Running') { Start-VM -Name ` + name + ` }`)
}

// natPrefixScript prints the subnet of the SparTask NAT network, nothing without one.
func natPrefixScript() string {
	return psScript(`$n = Get-NetNat -Name ` + psQuote(hypervName) + ` -ErrorAction SilentlyContinue
if ($n) { $n.InternalIPInterfaceAddressPrefix }`)
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// remoteInstallCommand runs install.sh of this spartaskctl's release (the latest release for a
// development build) inside the VM with the operator's install options.
func remoteInstallCommand(version string, args []string) string {
	base, env := deployReleases+"/latest/download", ""
	if version != "dev" {
		base, env = deployReleases+"/download/"+version, " SPARTASK_DEPLOY_VERSION="+shQuote(version)
	}
	var b strings.Builder
	b.WriteString("curl -fsSL " + shQuote(base+"/install.sh") + " | sudo" + env + " sh -s --")
	for _, a := range args {
		b.WriteString(" " + shQuote(a))
	}
	return b.String()
}

// hypervOptions are the options of `hyperv install`; the rest of its arguments go to
// `spartaskctl install` inside the VM.
type hypervOptions struct {
	vm        hypervVM
	subnet    string
	subnetSet bool
	dns       string
	install   []string
}

const defaultHypervSubnet = "172.30.230.0/24"

// parseHypervArgs takes the hyperv options out of args (-name value or -name=value, also with
// "--") and keeps everything else, in order, for the installer.
func parseHypervArgs(args []string) (hypervOptions, error) {
	o := hypervOptions{vm: hypervVM{cpus: 2, memoryGB: 4, diskGB: 64}, subnet: defaultHypervSubnet}
	ints := map[string]*int{"cpus": &o.vm.cpus, "memory-gb": &o.vm.memoryGB, "disk-gb": &o.vm.diskGB}
	strs := map[string]*string{"subnet": &o.subnet, "dns": &o.dns}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		_, isInt := ints[name]
		_, isStr := strs[name]
		if !strings.HasPrefix(arg, "-") || (!isInt && !isStr) {
			o.install = append(o.install, arg)
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return o, fmt.Errorf("flag -%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if isInt {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return o, fmt.Errorf("-%s %q: a positive whole number is required", name, value)
			}
			*ints[name] = n
			continue
		}
		*strs[name] = value
		if name == "subnet" {
			o.subnetSet = true
		}
	}
	if o.vm.diskGB < 16 {
		return o, fmt.Errorf("-disk-gb %d: SparTask needs at least 16 GB", o.vm.diskGB)
	}
	if o.vm.memoryGB < 2 {
		return o, fmt.Errorf("-memory-gb %d: SparTask needs at least 2 GB", o.vm.memoryGB)
	}
	return o, nil
}
