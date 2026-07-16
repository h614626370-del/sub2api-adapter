[CmdletBinding()]
param(
    [ValidatePattern('^[a-z0-9]+(?:[._-][a-z0-9]+)*/[a-z0-9]+(?:[._-][a-z0-9]+)*$')]
    [string]$Repository = '614626370/sub2api-adapter',

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

$dockerCommand = 'docker'
$dockerPrefix = @()
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    if (-not (Get-Command wsl -ErrorAction SilentlyContinue)) {
        throw '未找到 docker 或 WSL。请先安装 Docker 并完成 docker login。'
    }
    $dockerCommand = 'wsl'
    $dockerPrefix = @('-d', 'Ubuntu', '--', 'docker')
}

function Invoke-Docker {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)
    & $script:dockerCommand @script:dockerPrefix @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker 命令失败：docker $($Arguments -join ' ')"
    }
}

$versionTag = "${Repository}:${Version}"
$latestTag = "${Repository}:latest"
$buildTime = (Get-Date).ToUniversalTime().ToString('o')
$commit = (git rev-parse --short=12 HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) {
    $commit = 'unknown'
}

Invoke-Docker @('build', '--build-arg', "VERSION=$Version", '--build-arg', "COMMIT=$commit", '--build-arg', "BUILD_TIME=$buildTime", '--tag', $versionTag, '--tag', $latestTag, '.')
Invoke-Docker @('push', $versionTag)
Invoke-Docker @('push', $latestTag)

Write-Host "已发布 $versionTag 和 $latestTag（commit $commit）"
