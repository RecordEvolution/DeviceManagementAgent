# setup-docker-wsl.ps1
#
# Installs Docker Engine inside WSL2 (Ubuntu 24.04) so that it starts at boot
# without anyone logging in. Built to run from a SYSTEM shell (remote agent),
# where wsl.exe itself refuses to run: all WSL steps are executed as a local
# service account through Scheduled Tasks, and their output is shown here.
#
# Corporate networks:
#   - proxy: set $Proxy below, or install the IronFlock agent with its proxy first - the
#     agent service's HTTP_PROXY/HTTPS_PROXY/NO_PROXY are then used for the downloads,
#     apt and Docker itself
#   - certificates: Windows' trusted root certificates are imported into Ubuntu, so
#     corporate CAs (TLS-inspecting proxies, appliance registries) work for apt, curl and
#     image pulls. Re-run after the Windows certificate store or the proxy changed.
#
# Refuses to run while Docker Desktop is installed: this would be a second engine next to it.
#
# Run (64-bit PowerShell, as SYSTEM or admin):
#   powershell -NoProfile -ExecutionPolicy Bypass -File C:\Windows\Temp\setup-docker-wsl.ps1
# or paste the whole file into the shell. Then run connect-agent-to-wsl-docker.ps1.
#
# Safe to re-run: finished steps are skipped, and WSL and Docker are only restarted when
# their configuration changed. The service account password is rotated and both tasks
# are updated with it. Logs: C:\WSL-Setup\*.log

& {
$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'
try { Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force } catch { }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ---- settings ---------------------------------------------------------------
$User    = 'wsldocker'                 # local service account that owns the distro
$Dir     = 'C:\WSL-Setup'              # helper scripts + logs (admins only)
$VmDir   = 'C:\WSL\Ubuntu-24.04'       # virtual disk of the distro
$ImgUrl  = 'https://releases.ubuntu.com/24.04.5/ubuntu-24.04.5-wsl-amd64.wsl'
$ImgSha  = 'bb415d824822c4b878125729af451a5d18fb13d1cf5cbed9a7393ad64ac6039e'
$MinWsl  = [version]'2.5.0'
$Proxy   = ''                          # e.g. 'http://proxy.example.com:8080'; empty = the agent's proxy, if any
$NoProxy = ''                          # with $Proxy: hosts that bypass it, e.g. 'localhost,127.0.0.1,.corp.example.com'
$AllowDockerDesktop = $false           # continue although Docker Desktop is installed
$Helper  = Join-Path $Dir 'run-as-wsl-user.ps1'
$WslExe  = Join-Path $env:ProgramFiles 'WSL\wsl.exe'
# -----------------------------------------------------------------------------

function Step($t) { Write-Host "`n== $t" -ForegroundColor Cyan }
function Show($lines) { $lines | ForEach-Object { Write-Host "   $_" } }

if (-not [Environment]::Is64BitProcess) {
    throw 'This is 32-bit PowerShell. Start C:\Windows\SysNative\WindowsPowerShell\v1.0\powershell.exe and run the script there.'
}
if ((Test-Path (Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe')) -and -not $AllowDockerDesktop) {
    throw 'Docker Desktop is installed. Uninstall it first: this would be a second Docker engine next to it, and the agent''s apps run in Docker Desktop''s.'
}

# Proxy: $Proxy above, else what 'reagent.exe service install' put on the agent service.
# Values may hold credentials - they are never printed.
$proxyVars = [ordered]@{}
if ($Proxy) {
    $proxyVars['HTTP_PROXY']  = $Proxy
    $proxyVars['HTTPS_PROXY'] = $Proxy
    if ($NoProxy) { $proxyVars['NO_PROXY'] = $NoProxy }
    $proxySource = 'the $Proxy setting'
} else {
    $agentEnv = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Services\reagent' -Name Environment -ErrorAction SilentlyContinue).Environment
    foreach ($entry in @($agentEnv | Where-Object { $_ })) {
        $name, $value = $entry -split '=', 2
        if ($value -and $name -in 'HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY') { $proxyVars[$name.ToUpper()] = $value }
    }
    $proxySource = 'the agent service'
}
function Get-ProxyUrl([string]$Value) {
    if ($Value -and $Value -notmatch '^\w+://') { return "http://$Value" }
    return $Value
}
$httpProxy  = Get-ProxyUrl $(if ($proxyVars['HTTP_PROXY'])  { $proxyVars['HTTP_PROXY'] }  else { $proxyVars['HTTPS_PROXY'] })
$httpsProxy = Get-ProxyUrl $(if ($proxyVars['HTTPS_PROXY']) { $proxyVars['HTTPS_PROXY'] } else { $proxyVars['HTTP_PROXY'] })
$web = @{ UseBasicParsing = $true }
if ($httpsProxy) { $web.Proxy = $httpsProxy }

# HTTP status of a HEAD request (through the proxy, if any); 0 when nothing answered. Only a
# 2xx/3xx proves the site itself answered: 407 and 403 usually come from the proxy.
function Get-UrlStatus([string]$Url) {
    try { return [int](Invoke-WebRequest $Url -Method Head -TimeoutSec 20 @web).StatusCode }
    catch { if ($_.Exception.Response) { return [int]$_.Exception.Response.StatusCode } return 0 }
}

# An appliance registry sits on the LAN, which the proxy cannot reach. Docker gets it on
# NO_PROXY in case the agent's list doesn't cover it - but only when it really answers
# directly, so a cloud registry on a proxied site keeps using the proxy.
$directRegistry = $null
if ($httpsProxy) {
    $agentSvc = Get-CimInstance Win32_Service -Filter "Name='reagent'"
    if ($agentSvc -and $agentSvc.PathName -match '(?:^|\s)--?config(?:\s+|=)(?:"([^"]+)"|(\S+))') {
        $flockPath = if ($Matches[1]) { $Matches[1] } else { $Matches[2] }
        $registry  = $null
        try { $registry = (((Get-Content $flockPath -Raw | ConvertFrom-Json).docker_registry_url -replace '^https?://', '') -split '/')[0].ToLower() } catch { }
        if ($registry) {
            $scheme = if ($registry -match ':\d+$') { 'http' } else { 'https' }
            $status = 0
            $saved  = [Net.WebRequest]::DefaultWebProxy
            [Net.WebRequest]::DefaultWebProxy = New-Object Net.WebProxy
            try { $status = [int](Invoke-WebRequest "${scheme}://$registry/v2/" -Method Head -TimeoutSec 10 -UseBasicParsing).StatusCode }
            catch { if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode } }
            finally { [Net.WebRequest]::DefaultWebProxy = $saved }
            # without a proxy in between, any HTTP answer comes from the registry itself
            if ($status -gt 0) {
                $directRegistry = $registry -replace ':\d+$', ''
                $proxyVars['NO_PROXY'] = (@($proxyVars['NO_PROXY'], $directRegistry) | Where-Object { $_ }) -join ','
            }
        }
    }
}

# -----------------------------------------------------------------------------
Step '1/8  Network'
if ($httpsProxy) {
    Write-Host "   proxy from ${proxySource}: $($proxyVars.Keys -join ', ') (values not shown)"
    if ($directRegistry) { Write-Host "   registry $directRegistry answers directly - Docker reaches it without the proxy" }
} else {
    Write-Host '   no proxy (set $Proxy at the top if this site needs one)'
}
$required = @('http://archive.ubuntu.com/ubuntu/', 'http://security.ubuntu.com/ubuntu/', 'https://download.docker.com/linux/ubuntu/gpg')
if (-not (Test-Path (Join-Path $VmDir 'ext4.vhdx'))) { $required += $ImgUrl }
$blocked = @(foreach ($u in $required) {
    $s = Get-UrlStatus $u
    if ($s -lt 200 -or $s -ge 400) { "$u ($(if ($s) { "HTTP $s" } else { 'no answer' }))" }
})
if ($blocked.Count) {
    $blocked | ForEach-Object { Write-Warning "Not reachable: $_" }
    Write-Warning 'HTTP 407: the proxy wants credentials. HTTP 403: blocked by the proxy. No answer: no route - behind a proxy?'
    Write-Warning 'Set $Proxy at the top of this script, or install the agent with its proxy first.'
    return
}
# Docker Hub answers 401 to an anonymous request - that is the expected "reachable"
$hub = Get-UrlStatus 'https://registry-1.docker.io/v2/'
if ($hub -ne 401 -and ($hub -lt 200 -or $hub -ge 400)) {
    Write-Warning 'Docker Hub is not reachable: fine for apps from your own registry, but Docker Hub images will not pull.'
}
Write-Host '   ok'

# -----------------------------------------------------------------------------
Step '2/8  Windows features'
$reboot = $false
foreach ($f in 'VirtualMachinePlatform', 'Microsoft-Windows-Subsystem-Linux') {
    if ((Get-WindowsOptionalFeature -Online -FeatureName $f).State -ne 'Enabled') {
        Write-Host "   enabling $f"
        Enable-WindowsOptionalFeature -Online -FeatureName $f -All -NoRestart | Out-Null
        $reboot = $true
    }
}
if ($reboot) {
    Write-Warning 'Reboot required: run Restart-Computer, reconnect, then run this script again.'
    return
}
Write-Host '   ok'

# -----------------------------------------------------------------------------
Step '3/8  WSL version'
New-Item -ItemType Directory -Path $Dir, $VmDir -Force | Out-Null
$ver = $null
if (Test-Path $WslExe) { try { $ver = [version](Get-Item $WslExe).VersionInfo.ProductVersion } catch { } }
if (-not $ver -or $ver -lt $MinWsl) {
    Write-Host "   installed: $(if ($ver) { $ver } else { 'none / inbox' }) -> installing latest WSL release (x64 MSI)"
    $rel   = Invoke-RestMethod 'https://api.github.com/repos/microsoft/WSL/releases/latest' @web
    $asset = $rel.assets | Where-Object { $_.name -like '*x64.msi' } | Select-Object -First 1
    if (-not $asset) { throw 'No x64 MSI found in the latest WSL release' }
    $msi = Join-Path $Dir 'wsl.msi'
    Invoke-WebRequest $asset.browser_download_url -OutFile $msi @web
    $proc = Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart" -Wait -PassThru
    Remove-Item $msi -ErrorAction SilentlyContinue
    if ($proc.ExitCode -eq 3010) {
        Write-Warning 'WSL updated, reboot required: run Restart-Computer, reconnect, then run this script again.'
        return
    }
    if ($proc.ExitCode -ne 0) { throw "WSL MSI install failed, exit code $($proc.ExitCode)" }
    $ver = [version](Get-Item $WslExe).VersionInfo.ProductVersion
}
Write-Host "   WSL $ver"

# -----------------------------------------------------------------------------
Step '4/8  Service account, folders, runner task'
$chars = [char[]]'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_.!'
$bytes = New-Object byte[] 24
[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
$Plain  = (-join ($bytes | ForEach-Object { $chars[$_ % $chars.Length] })) + 'Aa1!'
$Secure = ConvertTo-SecureString $Plain -AsPlainText -Force

if (Get-LocalUser -Name $User -ErrorAction SilentlyContinue) {
    Set-LocalUser -Name $User -Password $Secure -PasswordNeverExpires $true
} else {
    New-LocalUser -Name $User -Password $Secure -PasswordNeverExpires -AccountNeverExpires `
        -Description 'Owner of the WSL/Docker distro' | Out-Null
}
try { Add-LocalGroupMember -SID 'S-1-5-32-544' -Member $User -ErrorAction Stop } catch { }  # Administrators, any UI language

# hide the account from the logon screen
$ul = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon\SpecialAccounts\UserList'
if (-not (Test-Path $ul)) { New-Item -Path $ul -Force | Out-Null }
New-ItemProperty -Path $ul -Name $User -Value 0 -PropertyType DWord -Force | Out-Null

# Only SYSTEM, Administrators and the service account may touch the helper folder or the
# distro's virtual disk. C:\ would otherwise hand "Authenticated Users: Modify" down to
# C:\WSL, letting any local user read or alter the disk while the VM is stopped.
foreach ($d in $Dir, (Split-Path $VmDir)) {
    icacls $d /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' "${User}:(OI)(CI)F" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "icacls failed on $d (exit $LASTEXITCODE)" }
}

Set-Content -Path (Join-Path $Dir 'runner.ps1') -Encoding ASCII -Value @'
& 'C:\WSL-Setup\cmd.ps1' *>&1 | Out-File -FilePath 'C:\WSL-Setup\out.log' -Encoding utf8
'@

Set-Content -Path $Helper -Encoding ASCII -Value @'
# Runs a command as the WSL owner account (via the WSL-Runner task) and prints its output.
# Usage from a SYSTEM/admin shell:
#   Set-ExecutionPolicy -Scope Process Bypass -Force
#   & C:\WSL-Setup\run-as-wsl-user.ps1 -Bash 'docker ps -a'        # bash in Ubuntu-24.04 as root
#   & C:\WSL-Setup\run-as-wsl-user.ps1 -PowerShell 'wsl -l -v'     # PowerShell as the WSL owner
param([string]$Bash, [string]$PowerShell, [int]$TimeoutMin = 30)
$dir  = 'C:\WSL-Setup'
$utf8 = New-Object System.Text.UTF8Encoding $false
if ($Bash) {
    [IO.File]::WriteAllText("$dir\cmd.sh", ($Bash -replace "`r`n", "`n") + "`n", $utf8)
    $PowerShell = 'wsl -d Ubuntu-24.04 -u root -- bash /mnt/c/WSL-Setup/cmd.sh'
}
if (-not $PowerShell) { throw 'Use -Bash or -PowerShell' }
[IO.File]::WriteAllText("$dir\cmd.ps1", "`$env:WSL_UTF8 = '1'`r`n$PowerShell`r`n", $utf8)
Remove-Item "$dir\out.log" -ErrorAction SilentlyContinue
$t0 = (Get-Date).AddSeconds(-2)
Start-ScheduledTask -TaskName 'WSL-Runner'
$deadline = (Get-Date).AddMinutes($TimeoutMin)
do {
    Start-Sleep -Seconds 2
    $state = (Get-ScheduledTask -TaskName 'WSL-Runner').State
    $last  = (Get-ScheduledTaskInfo -TaskName 'WSL-Runner').LastRunTime
} while ((($last -lt $t0) -or ($state -eq 'Running')) -and ((Get-Date) -lt $deadline))
if ($state -eq 'Running') { Write-Warning "Still running after $TimeoutMin min" }
if (Test-Path "$dir\out.log") { Get-Content "$dir\out.log" -Encoding UTF8 }
'@

$runAct = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File $Dir\runner.ps1"
$runSet = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Hours 2) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName 'WSL-Runner' -Action $runAct -Settings $runSet `
    -User "$env:COMPUTERNAME\$User" -Password $Plain -RunLevel Highest -Force | Out-Null
# The boot task needs the rotated password now, not only in step 8: a re-run that fails in
# between would otherwise leave it with a stale password and Docker without a start at boot.
if (Get-ScheduledTask -TaskName 'WSL-Docker-KeepAlive' -ErrorAction SilentlyContinue) {
    Set-ScheduledTask -TaskName 'WSL-Docker-KeepAlive' -User "$env:COMPUTERNAME\$User" -Password $Plain | Out-Null
}
Write-Host "   account '$User' ready, runner task registered"

# -----------------------------------------------------------------------------
Step '5/8  Ubuntu 24.04'
if (Test-Path (Join-Path $VmDir 'ext4.vhdx')) {
    Write-Host '   already imported, skipping'
} else {
    $img = Join-Path $Dir 'ubuntu-24.04.wsl'
    if (-not (Test-Path $img) -or (Get-FileHash $img -Algorithm SHA256).Hash -ne $ImgSha) {
        Write-Host '   downloading image (a few hundred MB)...'
        Invoke-WebRequest $ImgUrl -OutFile $img @web
        if ((Get-FileHash $img -Algorithm SHA256).Hash -ne $ImgSha) { throw 'SHA256 mismatch for the Ubuntu image' }
    }
    $out = & $Helper -PowerShell @'
wsl --install --from-file 'C:\WSL-Setup\ubuntu-24.04.wsl' --name Ubuntu-24.04 --location 'C:\WSL\Ubuntu-24.04' --no-launch
if (-not ((wsl -l -q) -match 'Ubuntu-24.04')) {
    wsl --import Ubuntu-24.04 'C:\WSL\Ubuntu-24.04' 'C:\WSL-Setup\ubuntu-24.04.wsl' --version 2
}
wsl -l -v
'@
    $out | Set-Content (Join-Path $Dir 'step5.log')
    Show $out
    if (-not (Test-Path (Join-Path $VmDir 'ext4.vhdx'))) { throw 'Import failed - see output above' }
    Remove-Item $img -ErrorAction SilentlyContinue
}

# -----------------------------------------------------------------------------
Step '6/8  WSL settings: systemd, no Windows interop, idle timeout'
# Interop off: the VM runs as an elevated administrator, so a Windows program started from
# Linux - e.g. by an app that escaped its container - would run with those rights. Nothing
# here needs it: Windows starts wsl.exe, never the other way round.
# Automount options: the agent bind-mounts the apps' folders from C:\ProgramData (as
# /mnt/c/...). 'metadata' lets containers chown/chmod there like on Linux; umask 000 opens
# what Windows created to every container user, as Docker Desktop's mounts do.
$out = @(& $Helper -Bash @'
cat > /etc/wsl.conf.new <<'EOF'
[boot]
systemd=true

[automount]
options = "metadata,umask=000"

[interop]
enabled=false
appendWindowsPath=false
EOF
if cmp -s /etc/wsl.conf.new /etc/wsl.conf; then rm -f /etc/wsl.conf.new; echo "wsl.conf unchanged"; else mv /etc/wsl.conf.new /etc/wsl.conf; echo "wsl.conf updated"; fi
echo "pid1=$(ps -p 1 -o comm=)"
'@ | ForEach-Object { "$_".Trim() })
Show $out
$cfgOut = @(& $Helper -PowerShell @'
$cfg = Join-Path $env:USERPROFILE '.wslconfig'
$text = if (Test-Path $cfg) { [IO.File]::ReadAllText($cfg) } else { '' }
if ($text -match '(?m)^\s*vmIdleTimeout\s*=\s*-1\s*$') {
    'wslconfig unchanged'
} else {
    if ($text -match '(?m)^\s*vmIdleTimeout\s*=.*$') { $text = $text -replace '(?m)^\s*vmIdleTimeout\s*=.*$', 'vmIdleTimeout=-1' }
    elseif ($text -match '(?m)^\s*\[wsl2\]\s*$') { $text = $text -replace '(?m)^\s*\[wsl2\]\s*$', "[wsl2]`r`nvmIdleTimeout=-1" }
    else { $text = $text.TrimEnd() + "`r`n[wsl2]`r`nvmIdleTimeout=-1`r`n" }
    [IO.File]::WriteAllText($cfg, $text.TrimStart())
    'wslconfig updated'
}
'@ | ForEach-Object { "$_".Trim() })
Show $cfgOut
# a restart applies both files, but also stops every running container - only when needed
if (($out -contains 'wsl.conf updated') -or ($cfgOut -contains 'wslconfig updated') -or -not ($out -contains 'pid1=systemd')) {
    $out = & $Helper -PowerShell @'
wsl --shutdown
Start-Sleep -Seconds 8
wsl -d Ubuntu-24.04 -u root -- ps -p 1 -o comm=
'@
    $out | Set-Content (Join-Path $Dir 'step6.log')
    Show $out
    if (-not ($out -match '^\s*systemd\s*$')) { throw 'systemd is not PID 1 - see output above' }
} else {
    Write-Host '   already configured - WSL not restarted'
}

# -----------------------------------------------------------------------------
Step '7/8  Certificates, proxy, Docker Engine (first install takes a few minutes)'
# Windows' trusted roots for Ubuntu: covers the corporate CA of a TLS-inspecting proxy and of
# an appliance registry, the way Docker Desktop would trust them through Windows.
$trustDir = Join-Path $Dir 'trust'
Remove-Item $trustDir -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $trustDir | Out-Null
$utf8  = New-Object Text.UTF8Encoding $false
$now   = Get-Date
$roots = @(Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.NotAfter -gt $now })
foreach ($c in $roots) {
    $pem = [Convert]::ToBase64String($c.RawData, [Base64FormattingOptions]::InsertLineBreaks) -replace "`r`n", "`n"
    [IO.File]::WriteAllText((Join-Path $trustDir "$($c.Thumbprint).crt"), "-----BEGIN CERTIFICATE-----`n$pem`n-----END CERTIFICATE-----`n", $utf8)
}
Write-Host "   $($roots.Count) Windows root certificates exported for Ubuntu"

$dockerProxyB64 = ''
$aptProxyB64    = ''
$curlProxyB64   = ''
if ($httpProxy -or $httpsProxy) {
    # systemd Environment= values: backslash and double quote escaped, % doubled
    $envLines = $proxyVars.Keys | ForEach-Object {
        $v = $proxyVars[$_].Replace('\', '\\').Replace('"', '\"').Replace('%', '%%')
        "Environment=`"$_=$v`""
    }
    $dockerProxyB64 = [Convert]::ToBase64String($utf8.GetBytes("[Service]`n" + ($envLines -join "`n") + "`n"))
    $aptProxyB64    = [Convert]::ToBase64String($utf8.GetBytes("Acquire::http::Proxy `"$httpProxy`";`nAcquire::https::Proxy `"$httpsProxy`";`n"))
    $curlProxyB64   = [Convert]::ToBase64String($utf8.GetBytes($httpsProxy))
}

$bash = @'
set -eu
export DEBIAN_FRONTEND=noninteractive
CHANGED=0
# put FILE BASE64: write FILE (root-only) when its content differs; an empty BASE64 leaves
# FILE alone. PUT_CHANGED tells the caller whether it wrote.
put() {
  PUT_CHANGED=0
  [ -n "$2" ] || return 0
  mkdir -p "$(dirname "$1")"
  echo "$2" | base64 -d > "$1.new"
  chmod 0600 "$1.new"
  if cmp -s "$1.new" "$1"; then rm -f "$1.new"; else mv "$1.new" "$1"; echo "updated $1"; PUT_CHANGED=1; fi
}
systemctl is-system-running --wait >/dev/null 2>&1 || true

# Windows' root certificates, replaced on every run; dockerd reads the store only at start
before=$(sha256sum /etc/ssl/certs/ca-certificates.crt | cut -d' ' -f1)
W=/usr/local/share/ca-certificates/windows-root
rm -rf "$W"
mkdir -p "$W"
cp /mnt/c/WSL-Setup/trust/*.crt "$W/" 2>/dev/null || echo "WARNING: no Windows root certificates to import"
update-ca-certificates 2>/dev/null | grep -E 'added|removed' || true
after=$(sha256sum /etc/ssl/certs/ca-certificates.crt | cut -d' ' -f1)
[ "$before" = "$after" ] || CHANGED=1

# proxy for apt, curl and dockerd
put /etc/apt/apt.conf.d/95proxy '@@APT_PROXY_B64@@'
put /etc/systemd/system/docker.service.d/http-proxy.conf '@@DOCKER_PROXY_B64@@'
[ "$PUT_CHANGED" = 0 ] || CHANGED=1
P=$(echo '@@CURL_PROXY_B64@@' | base64 -d)
[ -z "$P" ] || export https_proxy="$P"

if dpkg -s docker-ce >/dev/null 2>&1; then
  echo "Docker Engine already installed"
else
  APT="apt-get -y -o DPkg::Lock::Timeout=300"
  $APT update
  $APT install ca-certificates curl
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  $APT update
  $APT install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  CHANGED=1
fi
mkdir -p /etc/docker
[ -f /etc/docker/daemon.json ] || echo '{ "log-driver": "local" }' > /etc/docker/daemon.json
systemctl enable containerd docker >/dev/null 2>&1

# a restart stops every running container - only when something changed
if [ "$CHANGED" = 1 ] || ! systemctl is-active --quiet docker; then
  systemctl daemon-reload
  systemctl restart docker
  echo "docker restarted"
else
  echo "docker configuration unchanged - not restarted"
fi
echo "Engine $(docker info --format '{{.ServerVersion}}' 2>/dev/null)"
if docker run --rm hello-world >/dev/null 2>&1; then
  echo "hello-world: ok"
else
  echo "WARNING: hello-world could not be pulled - the engine works, but Docker Hub images will not pull"
fi
'@
$bash = $bash.Replace('@@APT_PROXY_B64@@', $aptProxyB64).Replace('@@DOCKER_PROXY_B64@@', $dockerProxyB64).Replace('@@CURL_PROXY_B64@@', $curlProxyB64)
$out = & $Helper -TimeoutMin 45 -Bash $bash
$out | Set-Content (Join-Path $Dir 'step7.log')
Show ($out | Select-Object -Last 25)
if (-not ($out -match '^\s*Engine \S+')) { throw 'Docker Engine is not running - full log in C:\WSL-Setup\step7.log' }

# -----------------------------------------------------------------------------
Step '8/8  Keep-alive at boot + power settings'
$kaAct = New-ScheduledTaskAction -Execute $WslExe -Argument '-d Ubuntu-24.04 -u root --exec /bin/sleep infinity'
$kaTrg = @(
    New-ScheduledTaskTrigger -AtStartup
    New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 5) -RepetitionDuration (New-TimeSpan -Days 3650)
)
$kaSet = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew `
    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
Register-ScheduledTask -TaskName 'WSL-Docker-KeepAlive' -Action $kaAct -Trigger $kaTrg -Settings $kaSet `
    -User "$env:COMPUTERNAME\$User" -Password $Plain -RunLevel Highest -Force | Out-Null
Start-ScheduledTask -TaskName 'WSL-Docker-KeepAlive'
powercfg /change standby-timeout-ac 0
powercfg /change hibernate-timeout-ac 0
Start-Sleep -Seconds 5
Show (& $Helper -Bash 'systemctl is-active docker; docker version --format "Engine {{.Server.Version}}"')
Write-Host "   keep-alive task: $((Get-ScheduledTask -TaskName 'WSL-Docker-KeepAlive').State)"

Write-Host "`nDone. Docker Engine runs in WSL (Ubuntu-24.04) as '$User' and starts at boot." -ForegroundColor Green
Write-Host 'Next: connect-agent-to-wsl-docker.ps1 connects the IronFlock agent to it.'
Write-Host 'Run Docker commands from this shell with:'
Write-Host '   Set-ExecutionPolicy -Scope Process Bypass -Force'
Write-Host "   & $Helper -Bash 'docker ps -a'"
}
