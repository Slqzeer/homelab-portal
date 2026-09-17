[CmdletBinding()]
param(
    [string]$ReportPath = (Join-Path $PSScriptRoot '../reports/acceptance.json')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$exitCode = 1
$previousReport = $env:ACCEPT_REPORT
$repoRoot = Split-Path -Parent $PSScriptRoot

try {
    $fullReportPath = [System.IO.Path]::GetFullPath($ReportPath)
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $fullReportPath)) | Out-Null
    # A failed launch must never leave a previous successful report in place.
    $pending = @{ schema = 1; passed = $false; liveAttempted = $false; results = @(@{ case = 'launcher'; passed = $false }) }
    [System.IO.File]::WriteAllText($fullReportPath, ($pending | ConvertTo-Json -Depth 4))
    $env:ACCEPT_REPORT = $fullReportPath
    Push-Location $repoRoot
    try {
        # The test validates ALL configuration before any cluster/HTTP request.
        # Go/kubectl/HTTP diagnostics are deliberately not copied to CI artifacts.
        & go test -tags=e2e ./tests/e2e -run '^TestPortalAcceptance$' -count=1 -timeout=18m 2>&1 | Out-Null
        $exitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
    $report = [System.IO.File]::ReadAllText($fullReportPath) | ConvertFrom-Json
    if ($exitCode -ne 0 -or -not $report.passed -or -not $report.liveAttempted) {
        $exitCode = 1
        Write-Host 'Cluster acceptance failed. Retain the non-secret acceptance report; inspect failed case names.'
    }
    else {
        Write-Host 'Cluster acceptance passed. Retain the non-secret acceptance report.'
    }
}
catch {
    # Exception messages may contain external command output or caller input.
    Write-Host 'Cluster acceptance could not complete. No successful acceptance is recorded.'
    $exitCode = 1
}
finally {
    $env:ACCEPT_REPORT = $previousReport
}
exit $exitCode
