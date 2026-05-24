# Staging fiscal automatizado (local / CI) — NO sustituye E2E SUNAT/PSE real en VPS.
# Uso: .\scripts\fiscal-staging\run-automated.ps1 [-SkipFacturador]

param(
    [switch]$SkipFacturador
)

$ErrorActionPreference = "Stop"
$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$repoRoot = Split-Path $root -Parent
$reportDir = Join-Path $root "docs"
$reportFile = Join-Path $reportDir "STAGING-RESULTS-AUTOMATED.log"
$timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

function Write-Result($name, $status, $detail) {
    $line = "[$timestamp] $status | $name | $detail"
    Write-Host $line
    Add-Content -Path $reportFile -Value $line
}

"" | Set-Content $reportFile
Write-Result "START" "INFO" "Staging automatizado backend_go + facturador_lycet"

Push-Location $root
try {
    Write-Result "go build" "RUN" "./..."
    go build ./...
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Write-Result "go build" "PASS" "OK"

    $goTests = @(
        "./pkg/fiscalqueue/...",
        "./pkg/fiscaldedup/...",
        "./internal/billing/service/..."
    )
    foreach ($pkg in $goTests) {
        Write-Result "go test $pkg" "RUN" ""
        go test $pkg -count=1
        if ($LASTEXITCODE -ne 0) { throw "go test $pkg failed" }
        Write-Result "go test $pkg" "PASS" "OK"
    }

    Write-Result "go test ConcurrentEnqueue100Tenants" "RUN" ""
    go test ./pkg/fiscalqueue/ -run ConcurrentEnqueue100Tenants -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw "ConcurrentEnqueue100Tenants failed" }
    Write-Result "go test ConcurrentEnqueue100Tenants" "PASS" "100 tenants cola fiscal"
}
finally {
    Pop-Location
}

if (-not $SkipFacturador) {
    $factRoot = Join-Path $repoRoot "facturador_lycet"
    if (Test-Path $factRoot) {
        Push-Location $factRoot
        try {
            Write-Result "php bin/console list app:fiscal" "RUN" ""
            php bin/console list app:fiscal 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) {
                Write-Result "php bin/console list app:fiscal" "SKIP" "PHP/vendor no disponible en este host"
            } else {
                Write-Result "php bin/console list app:fiscal" "PASS" "comandos fiscales registrados"
            }

            if (Test-Path "vendor/bin/phpunit") {
                Write-Result "phpunit fiscal" "RUN" ""
                php vendor/bin/phpunit --filter Fiscal 2>&1 | Tee-Object -Variable phpOut | Out-Null
                if ($LASTEXITCODE -ne 0) {
                    Write-Result "phpunit fiscal" "WARN" "sin tests o fallos: revisar salida"
                } else {
                    Write-Result "phpunit fiscal" "PASS" "OK"
                }
            } else {
                Write-Result "phpunit fiscal" "SKIP" "vendor no instalado"
            }
        }
        finally {
            Pop-Location
        }
    } else {
        Write-Result "facturador_lycet" "SKIP" "directorio no encontrado"
    }
}

Write-Result "END" "INFO" "Ver STAGING-RESULTS.md para E2E manual SUNAT/PSE"
Write-Host "`nLog: $reportFile"
