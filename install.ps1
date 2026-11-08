# Installs SparTask on Windows Server (or Windows 10/11 Pro) with one command, in PowerShell
# opened with "Run as administrator":
#
#   & ([scriptblock]::Create((irm https://get.spartask.ai/windows))) -activation <code> -license-server <url>
#
# (get.spartask.ai/windows redirects to .../releases/latest/download/install.ps1 of this repository.)
# It downloads the Windows spartask-deploy bundle of the latest release
# ($env:SPARTASK_DEPLOY_VERSION = 'v1.2.0' pins one), verifies it against the release's
# SHA256SUMS, unpacks it into %ProgramData%\SparTask and runs `spartaskctl hyperv install` there
# with the given arguments: a Hyper-V VM with Ubuntu in which install.sh installs SparTask.
# Run it again after the restart that turning Hyper-V on needs.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$identity = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $identity.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw 'spartask: open PowerShell with "Run as administrator" and run the command again'
}
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
  throw "spartask: SparTask runs on x86_64 (amd64) computers; this one is $env:PROCESSOR_ARCHITECTURE"
}

$repo = 'https://github.com/spartaskai/spartask-deploy/releases'
if ($env:SPARTASK_DEPLOY_VERSION) {
  $base = "$repo/download/$env:SPARTASK_DEPLOY_VERSION"
} else {
  $base = "$repo/latest/download"
}
$bundle = 'spartask-deploy-windows-amd64.zip'
$root = Join-Path $env:ProgramData 'SparTask'
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "spartask: downloading $bundle"
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$bundle" -OutFile (Join-Path $tmp $bundle)
  Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')
  $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { ($_ -split '\s+')[1] -eq $bundle }
  if (-not $line) {
    throw "spartask: the release's SHA256SUMS lists no $bundle"
  }
  $hash = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $bundle)).Hash
  if ($hash -ne ($line -split '\s+')[0]) {
    throw "spartask: $bundle does not match the release's SHA256SUMS"
  }
  Expand-Archive -Path (Join-Path $tmp $bundle) -DestinationPath $root -Force
} finally {
  Remove-Item -Recurse -Force -LiteralPath $tmp
}
Write-Host "spartask: installing from $root\spartask-deploy"
& (Join-Path $root 'spartask-deploy\spartaskctl.exe') hyperv install @args
