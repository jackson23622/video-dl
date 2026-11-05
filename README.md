# 视频下载器

cobalt 风格的本地视频下载器，支持 **macOS** 和 **Windows**。粘贴链接，按 » 下载。

- 解析用 [yt-dlp](https://github.com/yt-dlp/yt-dlp)，支持 YouTube、哔哩哔哩、抖音、TikTok、X、Instagram、微博等 1800+ 站点
- 只在你自己的电脑上运行，没有广告，不追踪，不需要服务器
- 不依赖 Python，也不需要 Homebrew 之类的工具

## 一键安装

### macOS

打开「终端」，粘贴这一行并回车：

```bash
curl -fsSL https://raw.githubusercontent.com/jackson23622/video-dl/main/install.sh | bash
```

装好后会自动打开。以后从「应用程序」或聚焦搜索（⌘ + 空格，输入“视频下载器”）打开。

### Windows

打开「PowerShell」（开始菜单搜索 PowerShell），粘贴这一行并回车：

```powershell
irm https://raw.githubusercontent.com/jackson23622/video-dl/main/install.ps1 | iex
```

装好后会自动打开。以后从开始菜单或桌面上的「视频下载器」打开。

> 两条命令都可以重复运行，用来更新到最新版本。
> 用命令安装，是为了避开系统对“未签名程序”的拦截（macOS 的“无法验证开发者”、Windows 的 SmartScreen）。

## 使用

1. 打开「视频下载器」，默认浏览器会自动打开 http://127.0.0.1:8765 。
2. 粘贴视频链接（整段分享文案也行），按 » 开始下载。
   - 模式：**自动**（视频+音频）/ **音频** / **静音**
   - **画质**按钮：手动选这一次的分辨率
   - 播放列表和合集会弹出列表，可以挑着下，也可以全部下载
3. 文件直接存进「下载」文件夹，点“在访达中显示”（Windows 上是“打开所在文件夹”）就能找到。

**第一次打开**会自动下载 yt-dlp 和 ffmpeg（macOS 约 100MB，Windows 约 150MB），页面底部会显示进度，之后打开就是秒开。yt-dlp 每天自动检查一次更新。

**退出**：关掉页面后程序几秒内自动退出（有任务在下载时不会退出），也可以在「关于」页点“退出视频下载器”。程序没有窗口，也没有程序坞/任务栏图标。

## 设置

| 设置 | 说明 |
|---|---|
| 画质 | 最高 / 4K / 1440p / 1080p / 720p / 480p / 360p。选的画质不存在时，自动降到最接近的一档 |
| 视频编码 | **H.264**（默认，兼容性最好，QuickTime、iPhone、微信、剪映都能直接打开）/ AV1 / VP9 |
| 音频格式 | 原始 / MP3 / M4A / Opus / WAV，以及码率 |
| 文件名 | 简洁 / 美观 / 详细 |
| 浏览器登录信息 | 下载需要登录的内容（B站高画质、YouTube 人机验证、抖音等）时，借用本机浏览器里已登录的账号 |
| 登录 Cookies | 浏览器读不到时，手动粘贴 Netscape 格式的 cookies.txt |

页面不在浏览器里保存任何数据，设置只在本次打开期间有效。

**浏览器登录信息的注意事项**

- macOS + Chrome / Edge / Brave：第一次会弹出“钥匙串”授权，点“允许”。
- macOS + Safari：先在 系统设置 → 隐私与安全性 → 完全磁盘访问权限 里打开「视频下载器」。
- Windows：推荐 Firefox。新版 Chrome / Edge 对登录信息做了额外加密，经常读不到，这时用“登录 Cookies”手动粘贴（可用浏览器扩展 “Get cookies.txt LOCALLY” 导出）。

## 文件位置

|  | macOS | Windows |
|---|---|---|
| 程序 | `/Applications/视频下载器.app`（没有写入权限时装到 `~/Applications`） | `%LOCALAPPDATA%\VideoDL\VideoDL.exe` |
| yt-dlp、ffmpeg、cookies | `~/Library/Application Support/VideoDL/` | `%LOCALAPPDATA%\VideoDL\` |
| 下载的视频 | `~/Downloads` | `C:\Users\你的用户名\Downloads` |

## 卸载

- **macOS**：删掉「应用程序」里的「视频下载器」，再删掉 `~/Library/Application Support/VideoDL` 文件夹。
- **Windows**：删掉 `%LOCALAPPDATA%\VideoDL` 文件夹（在资源管理器地址栏粘贴这个路径就能打开），再删掉桌面和开始菜单里的「视频下载器」快捷方式。

## 常见问题

- **YouTube 4K 在 QuickTime 里放不了**：YouTube 的 4K 只有 VP9/AV1 编码。日常选 1080p + H.264 最稳。
- **提示需要登录**：在设置里选“浏览器登录信息”，并确认该浏览器里已经登录了对应网站。
- **平台改版导致失败**：yt-dlp 每天自动更新；退出后重新打开应用，会再检查一次（距上次检查超过 24 小时才会更新）。
- **海外网站连不上**：需要电脑本身能访问这些网站（开着系统代理即可）。
- **端口 8765 被占用**：关掉占用这个端口的程序后重新打开。

## 开发

```
web/index.html     网页界面（打包进程序）
app/               程序源码（Go，只用标准库）
  main.go          HTTP 服务、解析、下载任务
  platform.go      macOS / Windows 差异（路径、打开浏览器、下载工具的地址）
  build.sh         构建脚本
dist/              构建好的安装包：VideoDL-mac.zip、VideoDL-windows.exe
install.sh         macOS 一键安装脚本
install.ps1        Windows 一键安装脚本
```

构建（需要 Go 1.22+、Python3 + Pillow、zip；在 Linux 或 macOS 上都能交叉编译）：

```bash
cd app && ./build.sh
```

改完 `web/index.html` 后要重新构建，并提交 `dist/` 里的新文件，一键安装才会装到新版本。
