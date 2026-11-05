#!/bin/bash
# 视频下载器 · macOS 一键安装 / 更新
#   curl -fsSL https://raw.githubusercontent.com/jackson23622/video-dl/main/install.sh | bash
set -e
URL="https://raw.githubusercontent.com/jackson23622/video-dl/main/dist/VideoDL-mac.zip"
NAME="视频下载器.app"

if [ "$(uname)" != "Darwin" ]; then
  echo "这个脚本只支持 macOS。Windows 请在 PowerShell 里运行："
  echo "  irm https://raw.githubusercontent.com/jackson23622/video-dl/main/install.ps1 | iex"
  exit 1
fi

echo "━━━━━━━━━━  安装 视频下载器  ━━━━━━━━━━"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
echo "▸ 下载中 …"
curl -fL --progress-bar -o "$tmp/vdl.zip" "$URL"

# 正在运行的旧版本先退出
curl -s -m 2 http://127.0.0.1:8765/api/quit >/dev/null 2>&1 && sleep 1 || true

DEST="/Applications"
[ -w "$DEST" ] || { DEST="$HOME/Applications"; mkdir -p "$DEST"; }
rm -rf "$DEST/$NAME"
ditto -x -k "$tmp/vdl.zip" "$DEST"
xattr -dr com.apple.quarantine "$DEST/$NAME" 2>/dev/null || true

echo "✔ 已安装到 $DEST/$NAME"
echo "  以后从「应用程序」或聚焦搜索（⌘+空格，输入“视频下载器”）打开。"
open "$DEST/$NAME"
