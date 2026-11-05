# 视频下载器 · Windows 一键安装 / 更新
#   irm https://raw.githubusercontent.com/jackson23622/video-dl/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'   # 关掉进度条，下载快很多

$url = 'https://raw.githubusercontent.com/jackson23622/video-dl/main/dist/VideoDL-windows.exe'
$dir = Join-Path $env:LOCALAPPDATA 'VideoDL'
$exe = Join-Path $dir 'VideoDL.exe'

Write-Host '━━━━━━━━━━  安装 视频下载器  ━━━━━━━━━━'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

# 正在运行的旧版本先退出
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 'http://127.0.0.1:8765/api/quit' | Out-Null; Start-Sleep -Seconds 1 } catch {}

Write-Host '▸ 下载中 …'
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exe
Unblock-File -Path $exe

# 开始菜单 + 桌面快捷方式
$ws = New-Object -ComObject WScript.Shell
foreach ($folder in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('Desktop'))) {
  $lnk = $ws.CreateShortcut((Join-Path $folder '视频下载器.lnk'))
  $lnk.TargetPath = $exe
  $lnk.WorkingDirectory = $dir
  $lnk.Description = '视频下载器'
  $lnk.Save()
}

Write-Host "✔ 已安装到 $exe"
Write-Host '  以后从开始菜单或桌面上的「视频下载器」打开。'
Start-Process -FilePath $exe
