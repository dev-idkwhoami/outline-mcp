param([string]$Version = "1.0.0")
$ErrorActionPreference = "Stop"
Push-Location (Join-Path $PSScriptRoot "..")
$oldCGO = $env:CGO_ENABLED
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
try {
    New-Item -ItemType Directory -Force bin | Out-Null
    $env:CGO_ENABLED = "0"
    $env:GOARCH = "amd64"
    foreach ($target in @("linux", "windows")) {
        $env:GOOS = $target
        $suffix = if ($target -eq "windows") { ".exe" } else { "" }
        go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$Version" -o "bin/outline-mcp-$target-amd64$suffix" ./cmd/outline-mcp
        if ($LASTEXITCODE -ne 0) { throw "Build failed for $target" }
    }
    # Run the helper on the host, not the final cross-compilation target.
    $env:GOOS = $null
    $env:GOARCH = $null
    go run ./scripts/checksums bin/outline-mcp-linux-amd64 bin/outline-mcp-windows-amd64.exe |
        Set-Content -Encoding ascii bin/SHA256SUMS
    if ($LASTEXITCODE -ne 0) { throw "Checksum generation failed" }
} finally {
    $env:CGO_ENABLED = $oldCGO
    $env:GOOS = $oldOS
    $env:GOARCH = $oldArch
    Pop-Location
}
