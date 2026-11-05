# Builds the Zero2Dev UI and the Windows binary.
# Usage: powershell -ExecutionPolicy Bypass -File build.ps1
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$version = (Get-Content VERSION -Raw).Trim()

Push-Location web
try {
    if (Get-Command vp -ErrorAction SilentlyContinue) { vp build } else { npm run build }
    if ($LASTEXITCODE -ne 0) { throw 'web build failed' }
} finally { Pop-Location }

New-Item -ItemType Directory -Force -Path bin | Out-Null
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -ldflags "-s -w -X main.version=$version" -o bin/zero2dev.exe ./cmd/zero2dev
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
Write-Host "Built bin/zero2dev.exe $version"
