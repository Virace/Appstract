[CmdletBinding()]
param(
    [string]$ProjectRoot = "",
    [string]$OutputDir = "build",
    [string[]]$TargetOS = @("windows"),
    [string[]]$TargetArch = @("amd64"),
    [switch]$Clean
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
} else {
    $ProjectRoot = (Resolve-Path $ProjectRoot).Path
}

$outputPath = Join-Path $ProjectRoot $OutputDir
if ($Clean -and (Test-Path $outputPath)) {
    Remove-Item -Recurse -Force $outputPath
}
New-Item -ItemType Directory -Force $outputPath | Out-Null

Push-Location $ProjectRoot
try {
    $mainPkg = "./cmd/appstract"
    $commit = if ($env:GITHUB_SHA) { $env:GITHUB_SHA.Substring(0, [Math]::Min(8, $env:GITHUB_SHA.Length)) } else { "local" }
    $buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    foreach ($os in $TargetOS) {
        foreach ($arch in $TargetArch) {
            $ext = if ($os -eq "windows") { ".exe" } else { "" }
            $name = "appstract-{0}-{1}{2}" -f $os, $arch, $ext
            $dst = Join-Path $outputPath $name

            Write-Host "Building $name ..."
            $env:GOOS = $os
            $env:GOARCH = $arch
            $env:CGO_ENABLED = "0"

            go build -trimpath -ldflags "-s -w -X main.commit=$commit -X main.buildTime=$buildTime" -o $dst $mainPkg
            if ($LASTEXITCODE -ne 0) {
                throw "go build failed for $os/$arch"
            }
        }
    }

    Write-Host "Build outputs are in: $outputPath"
} finally {
    Pop-Location
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
