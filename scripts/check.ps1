# Windows equivalent of `make check` without the race detector, which needs cgo
# and a C toolchain. Run from the repository root: ./scripts/check.ps1
$ErrorActionPreference = 'Stop'
function Step([string]$name, [scriptblock]$cmd) {
    Write-Host "==> $name"
    & $cmd
    if ($LASTEXITCODE -ne 0) { throw "$name failed (exit $LASTEXITCODE)" }
}
Step 'gofmt' {
    $files = gofmt -l .
    if ($files) { $files; $global:LASTEXITCODE = 1 }
}
Step 'go vet' { go vet ./... }
Step 'go test' { go test ./... }
Step 'integration' { go test -count=1 ./test/integration }
if ((go env CGO_ENABLED) -eq '1') {
    Step 'go test -race' { go test -race ./... }
} else {
    Write-Host '==> go test -race SKIPPED (CGO_ENABLED=0; install a C toolchain to enable)'
}
Step 'build' { go build -trimpath -o bin/ ./cmd/... }
Write-Host 'all checks passed'
