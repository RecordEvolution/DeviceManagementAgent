# connect-agent-to-wsl-docker.ps1
#
# Makes the WSL Docker Engine (installed by setup-docker-wsl.ps1) usable by the
# IronFlock device agent (reagent) running as a Windows service (SYSTEM):
#   - dockerd additionally listens on tcp://127.0.0.1:2376 (loopback only)
#   - mutual TLS: only clients holding the generated client certificate get in
#   - client certs land in C:\WSL-Setup\certs (SYSTEM, Administrators, WSL account only)
#   - plain-HTTP appliance registries are added to the WSL engine's /etc/docker/daemon.json
#     (from the agent's device.flock, plus $ExtraInsecureRegistries below)
#     (proxy and corporate CAs for the engine are set up by setup-docker-wsl.ps1)
#   - the cifs kernel module is loaded now and at every boot, so apps can use SMB volumes
#   - a Windows docker.exe CLI is placed in C:\WSL-Setup\bin and added to the machine PATH
#   - the Docker Compose plugin is installed to C:\Program Files\Docker\cli-plugins
#   - the agent service gets DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH and PATH in
#     its own service environment (not machine-wide) and is restarted when that changed
#
# Refuses to run while Docker Desktop is installed: the agent's apps live in Docker
# Desktop's engine, and switching the agent to the WSL engine would start them twice.
#
# Run from the same SYSTEM/admin shell after setup-docker-wsl.ps1 succeeded:
#   powershell -NoProfile -ExecutionPolicy Bypass -File C:\Windows\Temp\connect-agent-to-wsl-docker.ps1
# or paste the whole file. Safe to re-run: existing certificates are kept, and Docker and
# the agent are only restarted when their configuration actually changed.
#
# Re-run it after every 'reagent.exe service install': the install recreates the service
# and, with a proxy, overwrites its environment.
#
# Apps need agent 1.2.11 or newer on this engine: older agents pass Windows paths
# (C:\ProgramData\IronFlock\...) as the bind-mount sources of every app, which a Linux
# engine rejects. The script warns when the installed agent is older.

& {
$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'
try { Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force } catch { }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ---- settings ---------------------------------------------------------------
# Extra plain-HTTP registries (host:port) for the WSL engine, e.g. when the agent is not
# installed yet. Registries from the agent's device.flock are added automatically.
$ExtraInsecureRegistries = @()
# Restart the agent service when its environment changed (device offline for ~30 s).
$RestartAgent = $true
# Continue although Docker Desktop is installed. Only when you are sure its engine no
# longer runs the agent's apps - otherwise they run twice.
$AllowDockerDesktop = $false
# -----------------------------------------------------------------------------

$Helper      = 'C:\WSL-Setup\run-as-wsl-user.ps1'
$CertDir     = 'C:\WSL-Setup\certs'
$BinDir      = 'C:\WSL-Setup\bin'
$PluginDir   = Join-Path $env:ProgramFiles 'Docker\cli-plugins'
$ServiceName = 'reagent'
$DockerHost  = 'tcp://127.0.0.1:2376'
$MinAgentVersion = [version]'1.2.11'   # first agent that translates mount paths for this engine

if ((Test-Path (Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe')) -and -not $AllowDockerDesktop) {
    throw 'Docker Desktop is installed. Uninstall it first: the agent''s apps run in its engine and would be started a second time in the WSL engine (duplicate apps, port conflicts).'
}
if (-not (Test-Path $Helper)) { throw 'C:\WSL-Setup\run-as-wsl-user.ps1 not found - run setup-docker-wsl.ps1 first' }

# Same normalization as the agent: no scheme, no path, lower case.
function Get-RegistryHost([string]$Entry) {
    $e = $Entry.Trim() -replace '^https?://', ''
    return ($e -split '/')[0].ToLower()
}

# Value of a '-name value' / '-name=value' argument on the service command line.
function Get-ServiceArg([string]$CommandLine, [string]$Name) {
    if ($CommandLine -match ('(?:^|\s)--?' + $Name + '(?:\s+|=)(?:"([^"]+)"|(\S+))')) {
        if ($Matches[1]) { return $Matches[1] }
        return $Matches[2]
    }
    return $null
}

Write-Host "`n== 1/6  Agent service and registries" -ForegroundColor Cyan
$svc = Get-CimInstance Win32_Service -Filter "Name='$ServiceName'"
$AgentExe   = $null
$FlockPath  = $null
$ServiceEnv = @()
if ($svc -and ($svc.PathName -match '^\s*"([^"]+)"' -or $svc.PathName -match '^\s*(\S+)')) {
    $AgentExe = $Matches[1]
    Write-Host "   agent service: $AgentExe ($($svc.State))"
    $FlockPath = Get-ServiceArg $svc.PathName 'config'
    if (-not $FlockPath) { $FlockPath = Join-Path (Split-Path $AgentExe) 'device.flock' }
    $agentVersion = $null
    try { $agentVersion = [version]("$(& $AgentExe -version)".Trim()) } catch { }
    if ($agentVersion -and $agentVersion -lt $MinAgentVersion) {
        Write-Warning "agent $agentVersion is older than ${MinAgentVersion}: apps will not start on this engine until the agent is updated"
    } elseif ($agentVersion) {
        Write-Host "   agent version $agentVersion"
    }
    $ServiceEnv = @((Get-ItemProperty "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName" -Name Environment -ErrorAction SilentlyContinue).Environment | Where-Object { $_ })
} else {
    Write-Host '   agent service not installed yet'
}

$candidates = @($ExtraInsecureRegistries)
if ($FlockPath -and (Test-Path $FlockPath)) {
    try {
        $flock  = Get-Content $FlockPath -Raw | ConvertFrom-Json
        # a JSON-array string on backend-issued flocks, a bare host:port/ on appliance ones
        $listed = "$($flock.'insecure-registries')".Trim()
        if ($listed) {
            try { $candidates += @($listed | ConvertFrom-Json) } catch { $candidates += $listed }
        }
        # an explicit port marks the appliance's plain-HTTP registry (same rule as the agent)
        $registryHost = Get-RegistryHost "$($flock.docker_registry_url)"
        if ($registryHost -match ':\d+$') { $candidates += $registryHost }
    } catch {
        Write-Warning "Could not read $FlockPath - its registries are skipped"
    }
}
$registries = New-Object System.Collections.Generic.List[string]
foreach ($c in $candidates) {
    $h = Get-RegistryHost "$c"
    if ($h -and -not $registries.Contains($h)) { $registries.Add($h) }
}
if ($registries.Count) {
    Write-Host "   insecure registries: $($registries -join ', ')"
} else {
    Write-Host '   no plain-HTTP registry needed (cloud / HTTPS registry)'
}

# The downloads below go through the proxy 'reagent.exe service install' put on the
# service (the engine's own proxy comes from setup-docker-wsl.ps1). Never printed: the
# value may hold credentials.
$web = @{ UseBasicParsing = $true }
$webProxy = $null
foreach ($entry in $ServiceEnv) {
    $name, $value = $entry -split '=', 2
    if ($value -and ($name -eq 'HTTPS_PROXY' -or ($name -eq 'HTTP_PROXY' -and -not $webProxy))) { $webProxy = $value }
}
if ($webProxy) {
    if ($webProxy -notmatch '^\w+://') { $webProxy = "http://$webProxy" }
    $web.Proxy = $webProxy
    Write-Host '   downloads use the agent service''s proxy'
}

Write-Host "`n== 2/6  WSL engine: certificates, TLS listener, registries, SMB" -ForegroundColor Cyan
$daemonB64 = ''
if ($registries.Count) {
    $lines = @((& $Helper -Bash @'
echo "@@BEGIN@@"
if [ -s /etc/docker/daemon.json ]; then base64 -w 76 /etc/docker/daemon.json; fi
echo "@@END@@"
'@ | Out-String) -split "\r?\n" | ForEach-Object { $_.Trim() })
    $begin = [array]::IndexOf($lines, '@@BEGIN@@')
    $end   = [array]::IndexOf($lines, '@@END@@')
    if ($begin -lt 0 -or $end -le $begin) { throw 'Could not read /etc/docker/daemon.json from WSL' }
    # base64 wraps at 76 characters, so the log capture can't truncate it; join the lines again
    $currentB64  = if ($end -gt $begin + 1) { ($lines[($begin + 1)..($end - 1)] -join '').Trim() } else { '' }
    $currentJson = ''
    if ($currentB64) { $currentJson = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($currentB64)).TrimStart([char]0xFEFF) }
    try {
        $cfg = if ($currentJson.Trim()) { $currentJson | ConvertFrom-Json } else { New-Object PSObject }
        $existing = @($cfg.'insecure-registries' | Where-Object { $_ })
        $missing  = @($registries | Where-Object { $existing -notcontains $_ })
        if ($missing.Count) {
            $cfg | Add-Member -NotePropertyName 'insecure-registries' -NotePropertyValue ([string[]]($existing + $missing)) -Force
            $daemonB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(($cfg | ConvertTo-Json -Depth 20)))
            Write-Host "   adding to daemon.json: $($missing -join ', ')"
        } else {
            Write-Host '   daemon.json already lists them'
        }
    } catch {
        Write-Warning '/etc/docker/daemon.json in WSL is not valid JSON - fix it by hand; insecure registries not added'
    }
}

$bash = @'
set -eu
CHANGED=0
# put FILE BASE64: write FILE when its content differs; an empty BASE64 leaves FILE alone
put() {
  [ -n "$2" ] || return 0
  mkdir -p "$(dirname "$1")"
  echo "$2" | base64 -d > "$1.new"
  if cmp -s "$1.new" "$1"; then rm -f "$1.new"; else mv "$1.new" "$1"; echo "updated $1"; CHANGED=1; fi
}

T=/etc/docker/tls
W=/mnt/c/WSL-Setup/certs
mkdir -p "$T" && cd "$T"
# Keep the certificates only while the whole set exists. An interrupted earlier run or a
# deleted C:\WSL-Setup\certs leaves a partial set that would never work and never heal.
if [ -f ca.pem ] && [ -f server-cert.pem ] && [ -f server-key.pem ] && [ -f "$W/ca.pem" ] && [ -f "$W/cert.pem" ] && [ -f "$W/key.pem" ]; then
  echo "certificates already exist, keeping them"
else
  rm -f ca.pem ca-key.pem ca.srl server-cert.pem server-key.pem
  openssl genrsa -out ca-key.pem 4096 2>/dev/null
  openssl req -new -x509 -days 3650 -sha256 -key ca-key.pem -subj "/CN=wsl-docker-ca" -out ca.pem
  openssl genrsa -out server-key.pem 4096 2>/dev/null
  openssl req -new -key server-key.pem -subj "/CN=localhost" -out server.csr
  printf 'subjectAltName=IP:127.0.0.1,DNS:localhost\nextendedKeyUsage=serverAuth\n' > server.ext
  openssl x509 -req -days 3650 -sha256 -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out server-cert.pem -extfile server.ext
  openssl genrsa -out key.pem 4096 2>/dev/null
  openssl req -new -key key.pem -subj "/CN=device-agent" -out client.csr
  printf 'extendedKeyUsage=clientAuth\n' > client.ext
  openssl x509 -req -days 3650 -sha256 -in client.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out cert.pem -extfile client.ext
  mkdir -p "$W"
  cp ca.pem cert.pem key.pem "$W/"
  rm -f key.pem cert.pem ./*.csr ./*.ext
  chmod 0400 ca-key.pem server-key.pem
  echo "certificates created"
  # dockerd must load the new server certificate even when override.conf is unchanged
  CHANGED=1
fi

# insecure registries, merged on the Windows side
put /etc/docker/daemon.json '@@DAEMON_JSON_B64@@'

# SMB volumes: the WSL kernel ships cifs as a module - load it now and at every boot
echo cifs > /etc/modules-load.d/cifs.conf
if grep -qw cifs /proc/filesystems || modprobe cifs 2>/dev/null; then
  echo "cifs: available (SMB volumes can be mounted)"
else
  echo "WARNING: cifs kernel module not available - SMB volumes will not mount (try 'wsl --update')"
fi

# keep the distro's own ExecStart and add the TLS listener to it
O=/etc/systemd/system/docker.service.d/override.conf
BASE=$(systemctl cat docker | grep -m1 '^ExecStart=/' | cut -d= -f2-)
[ -n "$BASE" ] || { echo "ERROR: could not read ExecStart of docker.service - override not written"; exit 1; }
mkdir -p "$(dirname "$O")"
cat > "$O.new" <<EOF
[Service]
ExecStart=
ExecStart=$BASE -H tcp://127.0.0.1:2376 --tlsverify --tlscacert=$T/ca.pem --tlscert=$T/server-cert.pem --tlskey=$T/server-key.pem
EOF
if cmp -s "$O.new" "$O"; then
  rm -f "$O.new"
else
  mv "$O.new" "$O"
  echo "updated $O"
  CHANGED=1
fi

# only bounce docker when something changed - a restart restarts every running app
if [ "$CHANGED" = 1 ] || ! systemctl is-active --quiet docker; then
  systemctl daemon-reload
  systemctl restart docker
  sleep 2
  echo "docker restarted: $(systemctl is-active docker)"
else
  echo "docker configuration unchanged - not restarted"
fi
ss -lnt | grep ':2376' || echo "WARNING: nothing listening on 2376 inside WSL"
'@
$bash = $bash.Replace('@@DAEMON_JSON_B64@@', $daemonB64)
$out = & $Helper -Bash $bash
$out | ForEach-Object { Write-Host "   $_" }
if (-not (Test-Path (Join-Path $CertDir 'key.pem'))) { throw "Client certificates missing in $CertDir - see output above" }

Write-Host "`n== 3/6  Windows docker.exe CLI" -ForegroundColor Cyan
$docker = Join-Path $BinDir 'docker.exe'
if (Test-Path $docker) {
    Write-Host '   already present'
} else {
    try {
        $base = 'https://download.docker.com/win/static/stable/x86_64/'
        $zip  = (Invoke-WebRequest $base @web).Links.href |
                Where-Object { $_ -match '^docker-\d+\.\d+\.\d+\.zip$' } |
                Sort-Object { [version]($_ -replace '^docker-|\.zip$', '') } |
                Select-Object -Last 1
        $tmp = Join-Path $env:TEMP 'docker-cli'
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Path $tmp, $BinDir -Force | Out-Null
        Invoke-WebRequest ($base + $zip) -OutFile "$tmp\docker.zip" @web
        Expand-Archive "$tmp\docker.zip" -DestinationPath $tmp
        Copy-Item "$tmp\docker\docker.exe" $docker
        Remove-Item $tmp -Recurse -Force
        Write-Host "   $zip -> $docker"
    } catch {
        Write-Warning "Could not download the Windows Docker CLI: $($_.Exception.Message)"
    }
}

# The agent runs 'docker' / 'docker compose' by name. Edit the raw value so %SystemRoot%-style
# entries stay unexpanded (REG_EXPAND_SZ).
$envKey  = 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
$rawPath = (Get-Item $envKey).GetValue('Path', '', 'DoNotExpandEnvironmentNames')
if (@($rawPath -split ';' | ForEach-Object { $_.TrimEnd('\') }) -notcontains $BinDir) {
    Set-ItemProperty -Path $envKey -Name Path -Value ($rawPath.TrimEnd(';') + ';' + $BinDir) -Type ExpandString
    Write-Host "   added $BinDir to the machine PATH"
} else {
    Write-Host "   $BinDir already on the machine PATH"
}
if (@($env:Path -split ';') -notcontains $BinDir) { $env:Path = "$env:Path;$BinDir" }

# The agent logs in with 'docker login' as SYSTEM. A credsStore left behind by Docker Desktop
# names a helper that is gone once Desktop is uninstalled, and every login would fail.
$sysDockerCfg = Join-Path $env:SystemRoot 'System32\config\systemprofile\.docker\config.json'
if (Test-Path $sysDockerCfg) {
    try {
        $dockerCfg = Get-Content $sysDockerCfg -Raw | ConvertFrom-Json
        $store = "$($dockerCfg.credsStore)"
        if ($store -and -not (Get-Command "docker-credential-$store" -ErrorAction SilentlyContinue)) {
            Copy-Item $sysDockerCfg "$sysDockerCfg.bak" -Force
            $dockerCfg.PSObject.Properties.Remove('credsStore')
            [IO.File]::WriteAllText($sysDockerCfg, ($dockerCfg | ConvertTo-Json -Depth 20), (New-Object Text.UTF8Encoding $false))
            Write-Host "   removed credsStore '$store' from SYSTEM's docker config (helper not installed, backup config.json.bak)"
        }
    } catch {
        Write-Warning "Could not check SYSTEM's docker config ($sysDockerCfg): $($_.Exception.Message)"
    }
}

Write-Host "`n== 4/6  Docker Compose plugin" -ForegroundColor Cyan
$compose = Join-Path $PluginDir 'docker-compose.exe'
if (Test-Path $compose) {
    Write-Host '   already present'
} else {
    try {
        $url = 'https://github.com/docker/compose/releases/latest/download/docker-compose-windows-x86_64.exe'
        $tmp = Join-Path $env:TEMP 'docker-compose'
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Path $tmp, $PluginDir -Force | Out-Null
        Invoke-WebRequest $url -OutFile "$tmp\docker-compose.exe" @web
        Invoke-WebRequest "$url.sha256" -OutFile "$tmp\docker-compose.exe.sha256" @web
        $expected = ((Get-Content "$tmp\docker-compose.exe.sha256" -Raw).Trim() -split '\s+')[0]
        $actual   = (Get-FileHash "$tmp\docker-compose.exe" -Algorithm SHA256).Hash
        if ($expected -ne $actual) { throw "checksum mismatch (expected $expected, got $actual)" }
        Copy-Item "$tmp\docker-compose.exe" $compose
        Remove-Item $tmp -Recurse -Force
        Write-Host "   docker-compose.exe -> $compose"
    } catch {
        Write-Warning "Could not install the Docker Compose plugin: $($_.Exception.Message)"
    }
}

Write-Host "`n== 5/6  Test from Windows" -ForegroundColor Cyan
Start-Sleep -Seconds 3
if (-not (Test-NetConnection 127.0.0.1 -Port 2376 -WarningAction SilentlyContinue).TcpTestSucceeded) {
    Write-Warning '127.0.0.1:2376 does not answer on the Windows side. WSL localhost forwarding is not active for this VM.'
    Write-Warning 'Fallback: networkingMode=mirrored in C:\Users\wsldocker\.wslconfig (ask before changing).'
    Write-Warning 'The agent service environment was NOT changed.'
    return
}
Write-Host '   port 2376 reachable'
$env:DOCKER_HOST       = $DockerHost
$env:DOCKER_TLS_VERIFY = '1'
$env:DOCKER_CERT_PATH  = $CertDir
if (Test-Path $docker) {
    & $docker version --format '   client {{.Client.Version}}  ->  engine {{.Server.Version}} ({{.Server.Os}})'
    # Windows PowerShell turns redirected native stderr into a terminating error under 'Stop'
    $ErrorActionPreference = 'Continue'
    $composeVersion = & $docker compose version --short 2>$null
    $composeExit    = $LASTEXITCODE
    $ErrorActionPreference = 'Stop'
    if ($composeExit -eq 0 -and $composeVersion) {
        Write-Host "   compose $composeVersion"
    } else {
        Write-Warning 'docker compose is not available - compose apps will not run'
    }
}

Write-Host "`n== 6/6  Agent service environment" -ForegroundColor Cyan
if (-not $svc) {
    Write-Host '   agent service not installed yet - re-run this script after "reagent.exe service install"'
    return
}
$svcKey = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"
$wanted = [ordered]@{
    DOCKER_HOST       = $DockerHost
    DOCKER_TLS_VERIFY = '1'
    DOCKER_CERT_PATH  = $CertDir
    # services keep the PATH they had at boot; this hands the agent the one with docker.exe now
    PATH              = [Environment]::GetEnvironmentVariable('Path', 'Machine')
}
# keep everything else, e.g. the proxy variables 'reagent.exe service install' wrote
$kept    = @($ServiceEnv | Where-Object { $wanted.Keys -notcontains ($_ -split '=', 2)[0] })
$updated = @($kept) + @($wanted.Keys | ForEach-Object { "$_=$($wanted[$_])" })
$envChanged = -not (($ServiceEnv -join "`n") -ceq ($updated -join "`n"))
if ($envChanged) {
    Set-ItemProperty $svcKey -Name Environment -Value ([string[]]$updated) -Type MultiString
    Write-Host "   set DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH and PATH on the '$ServiceName' service"
    if ($kept.Count) { Write-Host "   kept: $(($kept | ForEach-Object { ($_ -split '=', 2)[0] }) -join ', ')" }
} else {
    Write-Host '   already configured'
}

# The agent loads its client certificate at start, so new certificates need a restart too.
$certsCreated = @($out | ForEach-Object { "$_".Trim() }) -contains 'certificates created'
$restart = ($envChanged -or $certsCreated) -and $svc.State -eq 'Running'
if (($envChanged -or $certsCreated) -and $svc.State -ne 'Running') {
    Write-Host "   service is $($svc.State) - the changes apply on its next start"
}

Write-Host "`nDone." -ForegroundColor Green
Write-Host "   engine:  $DockerHost (mutual TLS, client certs in $CertDir)"
Write-Host "   CLI:     $docker"
Write-Host "   compose: $compose"

if ($restart -and -not $RestartAgent) {
    Write-Host "`nRestart the agent to apply the changes (e.g. from the IronFlock platform)." -ForegroundColor Yellow
} elseif ($restart) {
    # The restart runs as a one-shot SYSTEM task, outside this shell. Started from the
    # IronFlock device terminal, this shell belongs to the agent's kill-on-close job and dies
    # with the agent - a stop issued from here would never be followed by the start.
    # reagent.exe's own stop waits for the agent to drain; Restart-Service gives up early.
    $taskName = 'IronFlock-Agent-Restart'
    $exe      = $AgentExe.Replace("'", "''")
    $command  = "& '$exe' service stop; & '$exe' service start; Unregister-ScheduledTask -TaskName '$taskName' -Confirm:`$false"
    $encoded  = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
    $action   = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -NonInteractive -EncodedCommand $encoded"
    $principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 10) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
    Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings -Force | Out-Null
    Write-Host "`nRestarting the agent to apply the changes - the device is offline for up to a minute." -ForegroundColor Yellow
    Write-Host 'Run from the IronFlock device terminal, that terminal disconnects now: reconnect after a minute.' -ForegroundColor Yellow
    Start-ScheduledTask -TaskName $taskName
}
}
