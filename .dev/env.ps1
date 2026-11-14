# Windows 本地开发环境变量（可选）。
#
# 作用：把 Go / pnpm / npm 的下载缓存与临时目录放进仓库内的 .cache/ ，
# 避免某些受限环境（或 CI 容器、沙箱）无法写入 %LOCALAPPDATA%、%APPDATA%、%USERPROFILE%\.cache。
# 非 Windows 或默认环境无需使用本脚本。
#
# 用法（PowerShell，在仓库根目录执行）：
#   iex (Get-Content .dev\env.ps1 -Raw)
#
# 说明：本脚本只写入进程级环境变量，不修改系统设置；关闭终端即失效。

# 定位仓库根目录：优先脚本所在目录，其次向父级查找含 backend\go.mod 的目录
# 注意：兼容 Windows PowerShell 5.1，不使用 ?: 三元运算符
if ($PSScriptRoot) {
  $root = Split-Path -Parent $PSScriptRoot
} else {
  $root = (Get-Location).Path
}
while ($root -and -not (Test-Path (Join-Path $root 'backend\go.mod'))) {
  $parent = Split-Path -Parent $root
  if (-not $parent -or $parent -eq $root) { break }
  $root = $parent
}
if (-not $root) { throw 'cannot locate repository root (backend\go.mod not found)' }

$env:GOPATH = Join-Path $root '.cache\gopath'
$env:GOMODCACHE = Join-Path $env:GOPATH 'pkg\mod'
$env:GOCACHE = Join-Path $root '.cache\go-build'
$env:GOTMPDIR = Join-Path $root '.cache\tmp'
$env:PNPM_HOME = Join-Path $root '.cache\pnpm-home'
$env:npm_config_store_dir = Join-Path $root '.cache\pnpm-store'
$env:npm_config_cache_dir = Join-Path $root '.cache\pnpm-cache'
$env:npm_config_registry = 'https://registry.npmmirror.com'

foreach ($d in @(
  $env:GOMODCACHE, $env:GOCACHE, $env:GOTMPDIR, $env:PNPM_HOME,
  $env:npm_config_store_dir, $env:npm_config_cache_dir
)) {
  if (-not (Test-Path $d)) { New-Item -ItemType Directory -Force $d | Out-Null }
}

$env:PATH = "$env:PNPM_HOME;$env:PATH"

Write-Host "dev env ready: GOPATH=$env:GOPATH"
