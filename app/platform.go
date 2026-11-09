package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// 各平台差异集中在这里

func isWin() bool { return runtime.GOOS == "windows" }

// exe 给程序名加上平台后缀
func exe(name string) string {
	if isWin() {
		return name + ".exe"
	}
	return name
}

func appDataDir(home string) string {
	if isWin() {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "VideoDL")
		}
		return filepath.Join(home, "AppData", "Local", "VideoDL")
	}
	return filepath.Join(home, "Library", "Application Support", "VideoDL")
}

// command 创建子进程；Windows 上不弹出黑色命令行窗口
func command(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	hideWindow(c)
	return c
}

func openURL(u string) {
	if isWin() {
		command("rundll32", "url.dll,FileProtocolHandler", u).Start()
		return
	}
	exec.Command("open", u).Start()
}

func revealFile(p string) {
	if isWin() {
		revealWin(p)
		return
	}
	exec.Command("open", "-R", p).Start()
}

func ytdlpURL() string {
	if isWin() {
		return "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	}
	return "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos"
}

// denoURL 返回 Deno（JavaScript 运行时）的下载地址
func denoURL() string {
	base := "https://github.com/denoland/deno/releases/latest/download/"
	switch {
	case isWin():
		return base + "deno-x86_64-pc-windows-msvc.zip"
	case runtime.GOARCH == "arm64":
		return base + "deno-aarch64-apple-darwin.zip"
	default:
		return base + "deno-x86_64-apple-darwin.zip"
	}
}

// ffmpegZips 返回 ffmpeg / ffprobe 的下载地址（Windows 是一个包里同时带两个）
func ffmpegZips() map[string]string {
	if isWin() {
		u := "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"
		return map[string]string{"ffmpeg": u, "ffprobe": u}
	}
	if runtime.GOARCH == "arm64" {
		return map[string]string{
			"ffmpeg":  "https://ffmpeg.martin-riedl.de/redirect/latest/macos/arm64/release/ffmpeg.zip",
			"ffprobe": "https://ffmpeg.martin-riedl.de/redirect/latest/macos/arm64/release/ffprobe.zip",
		}
	}
	return map[string]string{
		"ffmpeg":  "https://evermeet.cx/ffmpeg/getrelease/zip",
		"ffprobe": "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip",
	}
}
