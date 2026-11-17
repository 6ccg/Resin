param(
    [ValidatePattern('^[a-zA-Z0-9][a-zA-Z0-9._-]*$')]
    [string]$Name,
    [string]$Overlay,
    [switch]$SkipWeb
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$targetVars = @('GOOS', 'GOARCH', 'CGO_ENABLED')
$previous = @{}
foreach ($key in $targetVars) {
    $previous[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}

Push-Location $repo
try {
    $revision = (git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the Git revision' }
    if (!$Name) { $Name = $revision.Substring(0, 7) }
    $buildDir = Join-Path $repo ".local/build/$Name-linux-amd64"

    if (!$SkipWeb) {
        Push-Location 'webui'
        try {
            if (!(Test-Path -LiteralPath 'node_modules')) {
                npm ci
                if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
            }
            npm run build
            if ($LASTEXITCODE -ne 0) { throw 'Web UI build failed' }
        } finally {
            Pop-Location
        }
    }
    if (!(Test-Path -LiteralPath 'webui/dist/index.html')) { throw 'Build the Web UI first' }

    New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
    $bin = Join-Path $buildDir 'resin'
    $time = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    $info = 'github.com/Resinat/Resin/internal/buildinfo'
    $ldflags = "-s -w -X $info.Version=1.3.0-6ccg.$Name -X $info.GitCommit=$revision -X $info.BuildTime=$time"
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $options = @()
    if ($Overlay) { $options = @('-overlay', (Resolve-Path -LiteralPath $Overlay).Path) }
    go build @options -trimpath -tags 'with_quic with_wireguard with_grpc with_utls' -ldflags $ldflags -o $bin ./cmd/resin
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }

    $compressed = "$bin.gz"
    $inputStream = [IO.File]::OpenRead($bin)
    try {
        $outputStream = [IO.File]::Create($compressed)
        try {
            $gzip = [IO.Compression.GZipStream]::new($outputStream, [IO.Compression.CompressionLevel]::Optimal)
            try { $inputStream.CopyTo($gzip) } finally { $gzip.Dispose() }
        } finally {
            $outputStream.Dispose()
        }
    } finally {
        $inputStream.Dispose()
    }
    $sha256 = (Get-FileHash -LiteralPath $bin -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -LiteralPath (Join-Path $buildDir 'resin.sha256') -Value "$sha256  resin" -Encoding ascii
    Write-Output "revision=$revision sha256=$sha256"
    Write-Output $compressed
} finally {
    foreach ($key in $targetVars) {
        [Environment]::SetEnvironmentVariable($key, $previous[$key], 'Process')
    }
    Pop-Location
}
