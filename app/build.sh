#!/bin/bash
# 构建所有平台：dist/VideoDL-mac.zip（视频下载器.app，含 Apple 芯片与 Intel）和 dist/VideoDL-windows.exe
# 需要：Go 1.22+、Python3 + Pillow（生成 Mac 图标）、zip
set -e
cd "$(dirname "$0")"
cp ../web/index.html index.html          # 页面打包进程序
DIST=../dist; mkdir -p "$DIST"
LD="-s -w"

# ---- Windows ----
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$LD -H=windowsgui" -o "$DIST/VideoDL-windows.exe" .

# ---- macOS .app ----
APP="build/视频下载器.app"
rm -rf build && mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$LD" -o "$APP/Contents/MacOS/vdl-arm64" .
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$LD" -o "$APP/Contents/MacOS/vdl-amd64" .
cat > "$APP/Contents/MacOS/VideoDL" <<'SH'
#!/bin/sh
D="$(cd "$(dirname "$0")" && pwd)"
if [ "$(uname -m)" = "arm64" ]; then exec "$D/vdl-arm64"; else exec "$D/vdl-amd64"; fi
SH
chmod +x "$APP/Contents/MacOS/"*
cat > "$APP/Contents/Info.plist" <<'PL'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>视频下载器</string>
  <key>CFBundleDisplayName</key><string>视频下载器</string>
  <key>CFBundleIdentifier</key><string>io.github.jackson23622.videodl</string>
  <key>CFBundleExecutable</key><string>VideoDL</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PL
python3 make_icon.py "$APP/Contents/Resources/AppIcon.icns"
rm -f "$DIST/VideoDL-mac.zip"
(cd build && zip -qry "../$DIST/VideoDL-mac.zip" "视频下载器.app")
rm -rf build index.html
ls -la "$DIST"
