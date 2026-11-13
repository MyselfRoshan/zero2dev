# Builds the Zero2Dev UI and the Windows binary.
# Usage: powershell -ExecutionPolicy Bypass -File build.ps1
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$version = (Get-Content VERSION -Raw).Trim()

Push-Location web
try {
    pnpm install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'pnpm install failed' }
    if (Get-Command vp -ErrorAction SilentlyContinue) { vp build } else { pnpm run build }
    if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
} finally { Pop-Location }

New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -ldflags "-s -w -H windowsgui -X main.version=$version" -o dist/zero2dev.exe ./cmd/zero2dev
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
Write-Host "Built dist/zero2dev.exe $version"
