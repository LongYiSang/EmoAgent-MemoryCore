param(
    [string]$Suite = "retrieval",

    [string]$Fixture = "",

    [string]$Profiles = "sqlite_go,mirror_real_dense,mirror_real_graph,mirror_real_graph_rerank,mirror_real_rerank_no_graph",

    [string]$RunRoot = "",

    [string]$KeyFile = "",

    [int]$Port = 8765,

    [ValidateSet("off", "read_write", "read_only", "refresh")]
    [string]$EmbeddingCacheMode = "read_write",

    [switch]$ReuseExistingSidecar,

    [switch]$AllowSkipMissingProvider,

    [switch]$NoRerank,

    [ValidateSet("rule_only", "semantic_always", "semantic_on_low_confidence")]
    [string]$QueryAnalysisMode = "rule_only",

    [string]$QueryAnalysisKeyFile = "",

    [string]$QueryAnalysisModel = "qwen-plus",

    [int]$QueryAnalysisTimeoutMS = 15000,

    [int]$QueryAnalysisMaxTokens = 768
)

$ErrorActionPreference = "Stop"
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()

function Test-SidecarHealth {
    param([int]$HealthPort)
    try {
        $response = Invoke-WebRequest -Uri "http://127.0.0.1:$HealthPort/health" -UseBasicParsing -TimeoutSec 2
        return $response.StatusCode -ge 200 -and $response.StatusCode -lt 300
    }
    catch {
        return $false
    }
}

function Stop-ProcessTree {
    param([int]$ProcessId)
    $children = Get-CimInstance Win32_Process -Filter "ParentProcessId=$ProcessId" -ErrorAction SilentlyContinue
    foreach ($child in $children) {
        Stop-ProcessTree -ProcessId $child.ProcessId
    }
    Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
}

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$sidecarDir = Join-Path $repoRoot "sidecar"
if (-not $RunRoot) {
    $RunRoot = Join-Path $repoRoot ("artifacts\memory_eval\manual-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
}
if (-not [System.IO.Path]::IsPathRooted($RunRoot)) {
    $RunRoot = Join-Path $repoRoot $RunRoot
}
$RunRoot = [System.IO.Path]::GetFullPath($RunRoot)
$reportDir = Join-Path $RunRoot "reports"
$logDir = Join-Path $RunRoot "logs"
$tmpDir = Join-Path $RunRoot "tmp"
$mirrorDir = Join-Path $RunRoot "mirrors"
New-Item -ItemType Directory -Force -Path $reportDir, $logDir, $tmpDir, $mirrorDir | Out-Null

if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path $env:TEMP "memorycore-gocache-manual"
}
if (-not $env:UV_CACHE_DIR) {
    $env:UV_CACHE_DIR = Join-Path $env:TEMP "memorycore-uv-cache-manual"
}

$profileList = $Profiles.Split(",") | ForEach-Object { $_.Trim() } | Where-Object { $_ }
$usesMirrorProfile = ($profileList | Where-Object { $_ -like "mirror_real_*" }).Count -gt 0
$needsSidecar = $usesMirrorProfile
$queryAnalysisEnabled = $QueryAnalysisMode -ne "rule_only"
$needsRerank = ($profileList | Where-Object { $_ -like "*rerank*" }).Count -gt 0
if ($NoRerank -and $needsRerank) {
    throw "Profiles include rerank but -NoRerank was supplied. Remove rerank profiles or omit -NoRerank."
}
if ($queryAnalysisEnabled) {
    if (-not $usesMirrorProfile) {
        throw "Query analysis is enabled but no mirror_real_* profile was requested. sqlite_go remains a pure fallback profile."
    }
    if ($QueryAnalysisTimeoutMS -le 0) {
        throw "QueryAnalysisTimeoutMS must be > 0."
    }
    if ($QueryAnalysisMaxTokens -le 0) {
        throw "QueryAnalysisMaxTokens must be > 0."
    }
    if (-not $QueryAnalysisKeyFile) {
        $QueryAnalysisKeyFile = Join-Path $repoRoot "tmp\TMP_KEY_LLM"
    }
    if (-not (Test-Path -LiteralPath $QueryAnalysisKeyFile)) {
        throw "Query analysis is enabled but key file was not found: $QueryAnalysisKeyFile"
    }
    $env:MEMORYCORE_QUERY_ANALYSIS_API_KEY = (Get-Content -LiteralPath $QueryAnalysisKeyFile -Raw).Trim()
    $env:MEMORYCORE_QUERY_ANALYSIS_PROVIDER = "openai-compatible"
    $env:MEMORYCORE_QUERY_ANALYSIS_API_KEY_ENV = "MEMORYCORE_QUERY_ANALYSIS_API_KEY"
    $env:MEMORYCORE_QUERY_ANALYSIS_MODEL = $QueryAnalysisModel
    $env:MEMORYCORE_QUERY_ANALYSIS_TIMEOUT_SECONDS = [string][Math]::Max(1, [Math]::Ceiling($QueryAnalysisTimeoutMS / 1000.0))
    $env:MEMORYCORE_QUERY_ANALYSIS_MAX_TOKENS = [string]$QueryAnalysisMaxTokens
}

$sidecarProcess = $null
Push-Location $repoRoot
try {
    if ($needsSidecar) {
        if (-not $ReuseExistingSidecar -and (Test-SidecarHealth -HealthPort $Port)) {
            throw "Sidecar already responds on port $Port. Use -ReuseExistingSidecar or choose another -Port."
        }

        if (-not $env:DASHSCOPE_API_KEY) {
            if (-not $KeyFile) {
                $KeyFile = Join-Path $repoRoot "tmp\TMP_KEY"
            }
            if (-not (Test-Path -LiteralPath $KeyFile)) {
                throw "DASHSCOPE_API_KEY is not set and key file was not found: $KeyFile"
            }
            $env:DASHSCOPE_API_KEY = (Get-Content -LiteralPath $KeyFile -Raw).Trim()
        }

        if ($needsRerank) {
            $env:MEMORYCORE_RERANK_PROVIDER = "dashscope-vl"
        }
        else {
            $env:MEMORYCORE_RERANK_PROVIDER = "none"
        }

        if (-not $ReuseExistingSidecar) {
            $stdoutLog = Join-Path $logDir "sidecar.stdout.log"
            $stderrLog = Join-Path $logDir "sidecar.stderr.log"
            $escapedSidecarDir = $sidecarDir.Replace("'", "''")
            $sidecarCommand = "& { Set-Location -LiteralPath '$escapedSidecarDir'; & uv run python -m memorycore_sidecar.server --adapter trivium --host 127.0.0.1 --port $Port }"
            $sidecarProcess = Start-Process -FilePath "powershell.exe" `
                -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $sidecarCommand) `
                -PassThru `
                -WindowStyle Hidden `
                -RedirectStandardOutput $stdoutLog `
                -RedirectStandardError $stderrLog

            $ready = $false
            for ($i = 0; $i -lt 60; $i++) {
                if (Test-SidecarHealth -HealthPort $Port) {
                    $ready = $true
                    break
                }
                if ($sidecarProcess.HasExited) {
                    break
                }
                Start-Sleep -Seconds 1
            }
            if (-not $ready) {
                $stderrText = ""
                if (Test-Path -LiteralPath $stderrLog) {
                    $stderrText = Get-Content -LiteralPath $stderrLog -Raw
                }
                throw "Sidecar did not become healthy on port $Port. Log: $stderrLog`n$stderrText"
            }
        }
    }

    $args = @(
        "run", "./cmd/memory-eval",
        "--mode", "matrix",
        "--suite", $Suite,
        "--profiles", ($profileList -join ","),
        "--temp-dir", $tmpDir,
        "--report-dir", $reportDir,
        "--mirror-artifact-dir", $mirrorDir,
        "--embedding-cache-mode", $EmbeddingCacheMode,
        "--reuse-mirror", "auto",
        "--quality-no-stub",
        "--strict-capabilities"
    )
    if ($Fixture) {
        $args += @("--fixture", $Fixture)
    }
    if ($needsSidecar) {
        $args += @("--sidecar-url", "http://127.0.0.1:$Port")
    }
    if ($queryAnalysisEnabled) {
        $args += @("--query-analysis-mode", $QueryAnalysisMode, "--query-analysis-timeout-ms", $QueryAnalysisTimeoutMS)
    }
    if ($AllowSkipMissingProvider) {
        $args += "--allow-skip-missing-provider"
    }

    & go @args
    $exitCode = $LASTEXITCODE

    Write-Host ""
    Write-Host "Run root: $RunRoot"
    Write-Host "Summary report: $(Join-Path $reportDir 'report.md')"
    Write-Host "Detail report:  $(Join-Path $reportDir 'detail.md')"
    Write-Host "JSON report:    $(Join-Path $reportDir 'report.json')"
    exit $exitCode
}
finally {
    if ($sidecarProcess -ne $null -and -not $sidecarProcess.HasExited) {
        Stop-ProcessTree -ProcessId $sidecarProcess.Id
    }
    Pop-Location
}
