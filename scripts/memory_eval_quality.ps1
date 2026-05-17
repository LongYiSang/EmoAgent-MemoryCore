param(
    [ValidateSet("brief", "full", "short", "all")]
    [string]$Mode = "brief",

    [string]$Suite = "retrieval",

    [string]$Root = "",

    [string]$Fixture = "",

    [string]$TempDir = ""
)

$ErrorActionPreference = "Stop"
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()

$repoRoot = Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")
Push-Location $repoRoot
try {
    if (-not $env:GOCACHE) {
        $env:GOCACHE = Join-Path $repoRoot ".gocache"
    }

    $args = @("run", "./cmd/memory-eval", "--mode", $Mode, "--suite", $Suite)
    if ($Root) {
        $args += @("--root", $Root)
    }
    if ($Fixture) {
        $args += @("--fixture", $Fixture)
    }
    if ($TempDir) {
        $args += @("--temp-dir", $TempDir)
    }

    & go @args
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
