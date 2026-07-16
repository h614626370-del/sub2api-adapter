[CmdletBinding()]
param(
    [ValidatePattern('^[a-z0-9]+(?:[._-][a-z0-9]+)*/[a-z0-9]+(?:[._-][a-z0-9]+)*$')]
    [string]$Repository = '614626370/sub2api-adapter',

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw '未找到 docker 命令。请先安装 Docker 并完成 docker login。'
}

$versionTag = "${Repository}:${Version}"
$latestTag = "${Repository}:latest"
$buildTime = (Get-Date).ToUniversalTime().ToString('o')

docker build `
    --build-arg "VERSION=$Version" `
    --build-arg 'COMMIT=release' `
    --build-arg "BUILD_TIME=$buildTime" `
    --tag $versionTag `
    --tag $latestTag `
    .

if ($LASTEXITCODE -ne 0) { throw 'Docker 镜像构建失败。' }

docker push $versionTag
if ($LASTEXITCODE -ne 0) { throw "推送 $versionTag 失败。" }

docker push $latestTag
if ($LASTEXITCODE -ne 0) { throw "推送 $latestTag 失败。" }

Write-Host "已发布 $versionTag 和 $latestTag"
