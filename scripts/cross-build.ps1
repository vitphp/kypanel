$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

# 1) 同步 i.sh
Copy-Item -Force scripts/install.sh bin/i.sh

# 2) 交叉编译 Linux amd64（不带 webui/dist 内嵌，直接走 panel-web.tar.gz）
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$ldflags = "-s -w -X kypanel/internal/version.Version=0.30 -X kypanel/internal/version.Commit=dev -X kypanel/internal/version.Date=2026-08-27T16:30:00"
& go build -ldflags $ldflags -o bin/kypanel_amd64 ./cmd/panel
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# 3) 校验 ELF magic
$b = [System.IO.File]::ReadAllBytes('bin/kypanel_amd64')
$magic = -join ($b[0..3] | ForEach-Object { [char]$_ })
$md5 = (Get-FileHash -Path 'bin/kypanel_amd64' -Algorithm MD5).Hash
Write-Output "size=$($b.Length)"
Write-Output "magic=$magic"
Write-Output "md5=$md5"
$magicBytes = $b[0..3]
$elfCheck = ($magicBytes[0] -eq 0x7F) -and ($magicBytes[1] -eq 0x45) -and ($magicBytes[2] -eq 0x4C) -and ($magicBytes[3] -eq 0x46)
Write-Output "elf=$elfCheck"

# 4) 重新打前端包
tar -czf bin/panel-web.tar.gz -C webui/dist .

# 5) 复制 IP 库
Copy-Item -Force data/ip2region.xdb bin/ip2region.xdb

Write-Output "==== final bin/ ===="
Get-ChildItem bin | Format-Table Name, Length, LastWriteTime
