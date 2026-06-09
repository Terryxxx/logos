# Logos diagnostic -- verifies the local server + database + agent CLI
# detection are all consistent.
#
# Usage (from anywhere):
#   pwsh -File scripts\diagnose.ps1
#
# or from the repo root:
#   .\scripts\diagnose.ps1
#
# Exits 0 when everything checks out, 1 when at least one check failed.

[CmdletBinding()]
param(
    # Override the data dir if you run with LOGOS_DATA_DIR set or you want
    # to inspect a different profile. Defaults to the OS-conventional location.
    [string]$DataDir = $(if ($env:LOGOS_DATA_DIR) { $env:LOGOS_DATA_DIR } else { Join-Path $env:APPDATA 'Logos' })
)

$ErrorActionPreference = 'Continue'
$ok = $true

function Section($title) {
    Write-Host ""
    Write-Host "=== $title ===" -ForegroundColor Cyan
}
function Pass($msg) { Write-Host "  [PASS] $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "  [WARN] $msg" -ForegroundColor Yellow }
function Fail($msg) { Write-Host "  [FAIL] $msg" -ForegroundColor Red; $script:ok = $false }

Write-Host "Logos diagnostic" -ForegroundColor Magenta
Write-Host "Data dir: $DataDir"

# ----------------------------------------------------------------------
Section "1. Server process and port"

$conn = Get-NetTCPConnection -LocalPort 7878 -State Listen -ErrorAction SilentlyContinue
if ($conn) {
    $proc = Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
    if ($proc) {
        Pass "Port 7878 is listened by PID $($proc.Id) ($($proc.ProcessName)) started $($proc.StartTime)"
    } else {
        Pass "Port 7878 is open (owning PID $($conn.OwningProcess) gone)"
    }
} else {
    Fail "Nothing listening on 127.0.0.1:7878 -- start the server: cd server && go run ./cmd/logos-server"
}

# ----------------------------------------------------------------------
Section "2. runtime.json"

$rtPath = Join-Path $DataDir 'runtime.json'
$runtime = $null
if (Test-Path $rtPath) {
    try {
        $runtime = Get-Content $rtPath -Raw | ConvertFrom-Json
        Pass "Found at $rtPath"
        Write-Host "         addr=$($runtime.addr)  port=$($runtime.port)  pid=$($runtime.pid)"
        Write-Host "         token=$($runtime.token.Substring(0,18))..."
    } catch {
        Fail "Could not parse runtime.json: $_"
    }
} else {
    Fail "$rtPath missing -- server has never started, or DataDir is wrong"
}

# ----------------------------------------------------------------------
Section "3. SQLite -- agent_runtime table"

$dbPath = Join-Path $DataDir 'logos.db'
if (Test-Path $dbPath) {
    Pass "Database file: $dbPath ($((Get-Item $dbPath).Length) bytes)"
    $sqlite = Get-Command sqlite3 -ErrorAction SilentlyContinue
    if ($sqlite) {
        Write-Host ""
        & sqlite3 -header -column $dbPath @"
SELECT
  substr(id,1,8)||'…' AS id,
  provider, status,
  CASE WHEN length(version)=0 THEN '--' ELSE version END AS version,
  CASE WHEN length(binary_path)=0 THEN '--' ELSE binary_path END AS binary_path
FROM agent_runtime;
"@
        Write-Host ""
        $rowCount = & sqlite3 $dbPath "SELECT COUNT(*) FROM agent_runtime;"
        if ([int]$rowCount -eq 0) {
            Fail "agent_runtime table is empty -- detection produced no rows (very unusual; check server startup logs)"
        }
    } else {
        Warn "sqlite3 CLI not installed. Install with: winget install SQLite.SQLite"
    }
} else {
    Fail "$dbPath missing"
}

# ----------------------------------------------------------------------
Section "4. HTTP -- GET /api/runtimes"

if ($runtime) {
    try {
        $headers = @{ Authorization = "Bearer $($runtime.token)" }
        $resp = Invoke-RestMethod -Uri "http://$($runtime.addr)/api/runtimes" -Headers $headers -TimeoutSec 5
        if ($resp.runtimes -and $resp.runtimes.Count -gt 0) {
            Pass "API returned $($resp.runtimes.Count) runtime(s)"
            $resp.runtimes | Format-Table provider, status, version, binary_path -AutoSize
            $onlineClaude = $resp.runtimes | Where-Object { $_.provider -eq 'claude' -and $_.status -eq 'online' }
            if (-not $onlineClaude) {
                Warn "No 'claude' runtime with status=online. UI will say 'no agent available'."
            }
        } else {
            Fail "API responded but returned 0 runtimes -- agent_runtime table likely empty (see section 3)"
        }
    } catch {
        Fail "API call failed: $_"
    }
} else {
    Warn "Skipped -- no runtime.json"
}

# ----------------------------------------------------------------------
Section "5. PATH -- can the CURRENT shell see claude?"

$claudeCmd = Get-Command claude.exe -ErrorAction SilentlyContinue
if ($claudeCmd) {
    Pass "claude.exe -> $($claudeCmd.Path)"
    $claudeDir = Split-Path $claudeCmd.Path
    $onPath = ($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -contains $claudeDir.TrimEnd('\')
    if ($onPath) {
        Pass "Its directory ($claudeDir) is on PATH"
    } else {
        Warn "$claudeDir is NOT on session PATH (claude.exe is reachable via 'Get-Command' anyway -- Go's exec.LookPath needs PATH)"
        Write-Host "    Fix for this shell:   `$env:Path = `"$claudeDir;`$env:Path`""
    }
    try {
        $version = & $claudeCmd.Path --version 2>&1
        Pass "claude --version -> $version"
    } catch {
        Fail "claude --version failed: $_"
    }
} else {
    Fail "claude.exe NOT found by Get-Command (Go server WILL fail to detect it)"
    $likely = Join-Path $env:USERPROFILE '.local\bin\claude.exe'
    if (Test-Path $likely) {
        Warn "But claude.exe exists at $likely -- your PATH is missing $($env:USERPROFILE)\.local\bin"
        Write-Host "    One-time fix:"
        Write-Host "      [Environment]::SetEnvironmentVariable('Path', '$($env:USERPROFILE)\.local\bin;' + [Environment]::GetEnvironmentVariable('Path','User'), 'User')"
        Write-Host "    Then OPEN A NEW TERMINAL and restart the server."
    }
}

# ----------------------------------------------------------------------
Section "6. Cross-check -- does the server PATH match this shell?"

if ($runtime -and $runtime.pid) {
    $serverProc = Get-Process -Id $runtime.pid -ErrorAction SilentlyContinue
    if ($serverProc) {
        # Server PATH is whatever it was started with -- can't read it from another shell.
        # But we can compare the SQLite binary_path to the local Get-Command path.
        $sqlite = Get-Command sqlite3 -ErrorAction SilentlyContinue
        if ($sqlite) {
            $dbBinary = (& sqlite3 $dbPath "SELECT binary_path FROM agent_runtime WHERE provider='claude';").Trim()
            if ($dbBinary -and $claudeCmd) {
                if ($dbBinary -ieq $claudeCmd.Path) {
                    Pass "Server and current shell agree: $dbBinary"
                } else {
                    Warn "Server detected claude at:  $dbBinary"
                    Warn "Current shell sees it at:   $($claudeCmd.Path)"
                    Warn "If status=online, this is fine. If status=offline, restart the server from THIS shell."
                }
            } elseif (-not $dbBinary) {
                Fail "Server's binary_path for claude is empty -- server did not see claude on PATH at startup. Restart server from a shell where 'claude --version' works."
            }
        }
    } else {
        Warn "PID $($runtime.pid) from runtime.json no longer exists. Restart the server."
    }
}

# ----------------------------------------------------------------------
Write-Host ""
if ($ok) {
    Write-Host "All critical checks passed." -ForegroundColor Green
    exit 0
} else {
    Write-Host "One or more checks failed -- fix the items above (top to bottom) and re-run." -ForegroundColor Red
    exit 1
}
