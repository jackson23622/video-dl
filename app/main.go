// 视频下载器 · 本地版（macOS / Windows）
// 一个独立可执行文件：内置网页界面，调用 yt-dlp / ffmpeg 下载。
// 启动后自动用默认浏览器打开 http://127.0.0.1:8765
package main

import (
	"archive/zip"
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

const port = "8765"

var (
	home, _  = os.UserHomeDir()
	dataDir  = appDataDir(home)
	binDir   = filepath.Join(dataDir, "bin")
	cookieDir = filepath.Join(dataDir, "cookies")
	outDir   = filepath.Join(home, "Downloads")
	ytdlp    = ""
	denoPath = "" // YouTube 解析需要的 JavaScript 运行时（yt-dlp 2025.11 起必需）
	ffmpegDir = ""

	setupMu  sync.Mutex
	setupMsg = "正在准备…"
	ready    = false
	version  = ""

	lastSeen = time.Now()
	byeAt    time.Time
	seenMu   sync.Mutex

	tasks   = map[string]*Task{}
	tasksMu sync.Mutex
	sem     = make(chan struct{}, 3)
)

func touch() { seenMu.Lock(); lastSeen = time.Now(); seenMu.Unlock() }

// ---------------------------------------------------------------- 工具准备

func exists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func setSetup(msg string) { setupMu.Lock(); setupMsg = msg; setupMu.Unlock(); log.Println(msg) }

func download(urlStr, dst string) error {
	resp, err := http.Get(urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, dst)
}

func unzipOne(zipPath, name, dst string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if filepath.Base(f.Name) == name && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				rc.Close()
				return err
			}
			_, err = io.Copy(out, rc)
			rc.Close()
			out.Close()
			return err
		}
	}
	return fmt.Errorf("压缩包里没有 %s", name)
}

func setupTools() {
	os.MkdirAll(binDir, 0o755)
	os.MkdirAll(cookieDir, 0o755)
	dirs := []string{binDir}
	if !isWin() {
		dirs = append(dirs, filepath.Join(home, "视频下载器", "bin")) // 复用 Mac 弹窗版已下载的工具
	}
	pick := func(name string) string {
		for _, d := range dirs {
			if p := filepath.Join(d, exe(name)); exists(p) {
				return p
			}
		}
		return ""
	}
	ytdlp = pick("yt-dlp")
	if ytdlp == "" {
		setSetup("首次运行：正在下载 yt-dlp（约 35MB）…")
		ytdlp = filepath.Join(binDir, exe("yt-dlp"))
		if err := download(ytdlpURL(), ytdlp); err != nil {
			setSetup("下载 yt-dlp 失败：" + err.Error() + "（请检查网络后重新打开应用）")
			return
		}
		os.Chmod(ytdlp, 0o755)
		os.WriteFile(filepath.Join(binDir, ".last_update"), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
	}
	ff, fp := pick("ffmpeg"), pick("ffprobe")
	if ff == "" || fp == "" || filepath.Dir(ff) != filepath.Dir(fp) {
		zips := ffmpegZips()
		got := map[string]string{} // 同一个包只下载一次
		for _, tool := range []string{"ffmpeg", "ffprobe"} {
			u := zips[tool]
			z, ok := got[u]
			if !ok {
				size := ""
				if isWin() {
					size = "，约 150MB"
				}
				setSetup("首次运行：正在下载 " + tool + "（用于合并高清音视频、转 MP3" + size + "）…")
				z = filepath.Join(binDir, tool+".zip")
				if err := download(u, z); err != nil {
					setSetup("下载 " + tool + " 失败：" + err.Error() + "（请检查网络后重新打开应用）")
					return
				}
				got[u] = z
			}
			if err := unzipOne(z, exe(tool), filepath.Join(binDir, exe(tool))); err != nil {
				setSetup("解压 " + tool + " 失败：" + err.Error())
				return
			}
		}
		for _, z := range got {
			os.Remove(z)
		}
		ffmpegDir = binDir
	} else {
		ffmpegDir = filepath.Dir(ff)
	}
	// Deno：yt-dlp 解 YouTube 签名必需，缺了会丢格式（多语言配音视频尤其明显）。失败不阻塞启动
	denoPath = pick("deno")
	if denoPath == "" {
		setSetup("正在下载 YouTube 解析组件 Deno（约 45MB）…")
		z := filepath.Join(binDir, "deno.zip")
		dst := filepath.Join(binDir, exe("deno"))
		if err := download(denoURL(), z); err != nil {
			log.Println("下载 deno 失败：", err)
		} else if err := unzipOne(z, exe("deno"), dst); err != nil {
			log.Println("解压 deno 失败：", err)
		} else {
			denoPath = dst
		}
		os.Remove(z)
	}
	if !isWin() {
		exec.Command("xattr", "-dr", "com.apple.quarantine", binDir).Run()
	}

	// 每天最多自更新一次
	stamp := filepath.Join(filepath.Dir(ytdlp), ".last_update")
	b, _ := os.ReadFile(stamp)
	last, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if time.Now().Unix()-last > 86400 {
		setSetup("正在检查 yt-dlp 更新…")
		if command(ytdlp, "-U").Run() == nil {
			os.WriteFile(stamp, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
		}
	}
	out, err := command(ytdlp, "--version").Output()
	if err != nil {
		setSetup("无法运行 yt-dlp：" + err.Error())
		return
	}
	setupMu.Lock()
	version = strings.TrimSpace(string(out))
	ready = true
	setupMsg = ""
	setupMu.Unlock()
	log.Println("ready, yt-dlp", version)
}

// ---------------------------------------------------------------- 通用

var urlRe = regexp.MustCompile(`https?://[^\s<>"'，。！？、）]+`)

func extractURL(s string) string {
	m := urlRe.FindString(strings.TrimSpace(s))
	return strings.TrimRight(m, ".,;)")
}

var browsers = map[string]bool{"chrome": true, "safari": true, "edge": true, "firefox": true, "brave": true, "chromium": true, "opera": true, "vivaldi": true}

func cookieFor(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	host := strings.ToLower(pu.Hostname())
	parts := strings.Split(host, ".")
	for i := 0; i < len(parts)-1; i++ {
		f := filepath.Join(cookieDir, strings.Join(parts[i:], ".")+".txt")
		if exists(f) {
			return f
		}
	}
	alias := map[string]string{"b23.tv": "bilibili.com", "youtu.be": "youtube.com", "v.douyin.com": "douyin.com", "xhslink.com": "xiaohongshu.com"}
	for k, v := range alias {
		if strings.HasSuffix(host, k) {
			if f := filepath.Join(cookieDir, v+".txt"); exists(f) {
				return f
			}
		}
	}
	if f := filepath.Join(cookieDir, "all.txt"); exists(f) {
		return f
	}
	return ""
}

func baseArgs(u, browser string) []string {
	a := []string{"--no-warnings", "--ffmpeg-location", ffmpegDir, "--socket-timeout", "20", "--retries", "3",
		"--concurrent-fragments", "4"}
	if denoPath != "" {
		a = append(a, "--js-runtimes", "deno:"+denoPath)
	}
	if browsers[browser] {
		a = append(a, "--cookies-from-browser", browser)
	} else if c := cookieFor(u); c != "" {
		a = append(a, "--cookies", c)
	}
	return a
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func friendlyError(msg string) string {
	msg = ansiRe.ReplaceAllString(msg, "")
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "ERROR") {
			msg = lines[i]
			break
		}
	}
	msg = strings.TrimPrefix(strings.TrimSpace(msg), "ERROR: ")
	low := strings.ToLower(msg)
	has := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(low, k) {
				return true
			}
		}
		return false
	}
	cut := func(s string, n int) string {
		r := []rune(s)
		if len(r) > n {
			return string(r[:n])
		}
		return s
	}
	switch {
	case has("unsupported url"):
		return "暂不支持该网站，或链接不是视频页面"
	case has("could not find", "cookies database", "keychain", "operation not permitted"):
		return "读取浏览器登录信息失败：请确认已在该浏览器登录；用 Safari 需给本应用开启“完全磁盘访问权限”。原始信息：" + cut(msg, 200)
	case has("timed out", "timeout", "unable to connect", "connection reset", "name or service not known", "nodename nor servname"):
		return "网络连接失败（海外网站可能需要开代理）：" + cut(msg, 200)
	case has("sign in to confirm", "login required", "requires login", "need to log in", "fresh cookies",
		"cookies are needed", "registered users", "confirm you're not a bot", "confirm your age"):
		return "该内容需要登录：到「设置 → 浏览器登录信息」选择你已登录的浏览器后重试。原始信息：" + cut(msg, 200)
	case has("private"):
		return "视频为私密或已删除"
	case has("not available in your country", "geo restrict"):
		return "地区限制：需要开代理"
	}
	if msg == "" {
		return "未知错误"
	}
	return cut(msg, 400)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) { writeJSON(w, code, map[string]string{"error": msg}) }

func humanSize(n float64) string {
	if n <= 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB"}
	for _, u := range units {
		if n < 1024 {
			return fmt.Sprintf("%.1f%s", n, u)
		}
		n /= 1024
	}
	return fmt.Sprintf("%.1fTB", n)
}

// ---------------------------------------------------------------- 解析

type M = map[string]any

func str(m M, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func num(m M, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}

func thumb(m M) string {
	if t := str(m, "thumbnail"); t != "" {
		return t
	}
	if th, ok := m["thumbnails"].([]any); ok && len(th) > 0 {
		if last, ok := th[len(th)-1].(M); ok {
			return str(last, "url")
		}
	}
	return ""
}

func simplifyFormats(info M) []M {
	fmts, _ := info["formats"].([]any)
	var list []M
	for _, f := range fmts {
		if m, ok := f.(M); ok {
			list = append(list, m)
		}
	}
	none := func(v string) bool { return v == "" || v == "none" }
	hasAudioOnly := false
	for _, f := range list {
		// 纯音频：没有视频轨，且音频编码不是 none（YouTube 配音音轨常是 HLS，编码显示为未知）
		if str(f, "vcodec") == "none" && str(f, "acodec") != "none" && str(f, "ext") != "mhtml" {
			hasAudioOnly = true
		}
	}
	best := map[int]M{}
	for _, f := range list {
		h := int(num(f, "height"))
		if none(str(f, "vcodec")) || h == 0 || strings.HasPrefix(str(f, "protocol"), "mhtml") {
			continue
		}
		cur, ok := best[h]
		better := func(a, b M) bool {
			if num(a, "tbr") != num(b, "tbr") {
				return num(a, "tbr") > num(b, "tbr")
			}
			return str(a, "ext") == "mp4" && str(b, "ext") != "mp4"
		}
		if !ok || better(f, cur) {
			best[h] = f
		}
	}
	var hs []int
	for h := range best {
		hs = append(hs, h)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(hs)))
	out := []M{}
	seen := map[string]bool{}
	for _, h := range hs {
		f := best[h]
		id := str(f, "format_id")
		sel := id
		if none(str(f, "acodec")) && hasAudioOnly {
			sel = id + "+bestaudio"
		}
		if seen[sel] {
			continue
		}
		seen[sel] = true
		label := fmt.Sprintf("%dp", h)
		if fps := num(f, "fps"); fps > 30 {
			label += strconv.Itoa(int(fps))
		}
		size := num(f, "filesize")
		if size == 0 {
			size = num(f, "filesize_approx")
		}
		out = append(out, M{"id": sel, "type": "video", "label": label, "height": h, "ext": "mp4",
			"codec": strings.Split(str(f, "vcodec"), ".")[0], "size": humanSize(size), "note": str(f, "format_note")})
	}
	if len(out) == 0 {
		for _, f := range list {
			if str(f, "vcodec") == "none" {
				continue
			}
			id := str(f, "format_id")
			if id == "" {
				id = "best"
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			lbl := str(f, "format_note")
			if r := str(f, "resolution"); lbl == "" && r != "" && r != "audio only" {
				lbl = r
			}
			if lbl == "" {
				lbl = "原画质"
			}
			ext := str(f, "ext")
			if ext == "" {
				ext = "mp4"
			}
			out = append(out, M{"id": id, "type": "video", "label": lbl, "ext": ext, "codec": "", "size": humanSize(num(f, "filesize")), "note": ""})
		}
		if len(out) == 0 {
			ext := str(info, "ext")
			if ext == "" {
				ext = "mp4"
			}
			out = append(out, M{"id": "best", "type": "video", "label": "最佳质量", "ext": ext, "codec": "", "size": "", "note": ""})
		}
	}
	out = append(out, M{"id": "bestaudio/best", "type": "audio", "label": "仅音频 MP3", "ext": "mp3", "codec": "mp3", "size": "", "note": "转码"})
	if hasAudioOnly {
		out = append(out, M{"id": "bestaudio[ext=m4a]/bestaudio", "type": "audio_raw", "label": "仅音频 原始", "ext": "m4a", "codec": "", "size": "", "note": "无损抽取"})
	}
	return out
}

// audioTracks 列出配音视频的各语言音轨（原声排第一）；只有一条时返回空
func audioTracks(info M) []M {
	fmts, _ := info["formats"].([]any)
	seen := map[string]M{}
	var order []string
	for _, x := range fmts {
		f, ok := x.(M)
		if !ok || str(f, "vcodec") != "none" || str(f, "language") == "" {
			continue
		}
		lang := str(f, "language")
		note := str(f, "format_note")
		orig := strings.Contains(strings.ToLower(note), "original") || num(f, "language_preference") >= 10
		name := note
		for _, cut := range []string{"Untested, ", ", low", ", medium", ", high", ", ultralow", " (default)"} {
			name = strings.ReplaceAll(name, cut, "")
		}
		name = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(name, "original", "原声"), "dubbed-auto", "AI 配音"))
		name = strings.TrimSpace(strings.ReplaceAll(name, "dubbed", "配音"))
		name = strings.Trim(name, " ,-")
		if name == "" {
			name = lang
		}
		if t, ok := seen[lang]; ok {
			if orig {
				t["original"] = true
			}
			continue
		}
		seen[lang] = M{"lang": lang, "name": name, "original": orig}
		order = append(order, lang)
	}
	if len(order) < 2 {
		return nil
	}
	out := []M{}
	for _, l := range order {
		if seen[l]["original"] == true {
			out = append([]M{seen[l]}, out...)
		} else {
			out = append(out, seen[l])
		}
	}
	return out
}

func runJSON(args []string) (M, error) {
	cmd := command(ytdlp, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New(friendlyError(stderr.String()))
	}
	var m M
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, errors.New("解析结果异常：" + friendlyError(stderr.String()))
	}
	return m, nil
}

func parseURL(u, browser string, depth int) (M, error) {
	info, err := runJSON(append(baseArgs(u, browser), "-J", "--flat-playlist", u))
	if err != nil {
		return nil, err
	}
	t := str(info, "_type")
	if (t == "playlist" || t == "multi_video") && info["entries"] != nil {
		raw, _ := info["entries"].([]any)
		var entries []M
		for _, e := range raw {
			if m, ok := e.(M); ok {
				entries = append(entries, m)
			}
		}
		if len(entries) == 1 && depth < 2 {
			nu := str(entries[0], "url")
			if nu == "" {
				nu = str(entries[0], "webpage_url")
			}
			if nu != "" && nu != u {
				return parseURL(nu, browser, depth+1)
			}
		}
		title := str(info, "title")
		if title == "" {
			title = "播放列表"
		}
		list := []M{}
		for i, e := range entries {
			if i >= 500 {
				break
			}
			et := str(e, "title")
			if et == "" {
				et = fmt.Sprintf("第%d个", i+1)
			}
			eu := str(e, "url")
			if eu == "" {
				eu = str(e, "webpage_url")
			}
			list = append(list, M{"title": et, "url": eu, "duration": e["duration"], "thumbnail": thumb(e)})
		}
		return M{"playlist": true, "title": title, "uploader": str(info, "uploader"), "platform": str(info, "extractor_key"),
			"count": len(entries), "entries": list}, nil
	}
	if info["formats"] == nil && str(info, "url") == "" {
		if info, err = runJSON(append(baseArgs(u, browser), "-J", "--no-playlist", u)); err != nil {
			return nil, err
		}
	}
	title := str(info, "title")
	if title == "" {
		title = "未命名"
	}
	up := str(info, "uploader")
	if up == "" {
		up = str(info, "channel")
	}
	wu := str(info, "webpage_url")
	if wu == "" {
		wu = u
	}
	return M{"playlist": false, "id": info["id"], "title": title, "uploader": up, "duration": info["duration"],
		"thumbnail": thumb(info), "platform": str(info, "extractor_key"), "webpage_url": wu,
		"view_count": info["view_count"], "upload_date": info["upload_date"], "formats": simplifyFormats(info),
		"audio_tracks": audioTracks(info)}, nil
}

// ---------------------------------------------------------------- 下载任务

type Task struct {
	mu         sync.Mutex
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Phase      string  `json:"phase"`
	Progress   float64 `json:"progress"`
	Speed      string  `json:"speed"`
	Eta        any     `json:"eta"`
	Downloaded string  `json:"downloaded"`
	Total      string  `json:"total"`
	File       string  `json:"file,omitempty"`
	Size       string  `json:"size,omitempty"`
	Error      string  `json:"error,omitempty"`
	Title      string  `json:"title"`
	path       string
}

type DLReq struct {
	URL          string `json:"url"`
	Mode         string `json:"mode"`
	Quality      string `json:"quality"`
	Codec        string `json:"codec"`
	AudioFormat  string `json:"audio_format"`
	AudioBitrate string `json:"audio_bitrate"`
	Filename     string `json:"filename"`
	FormatID     string `json:"format_id"`
	Title        string `json:"title"`
	Browser      string `json:"browser"`
	AudioLang    string `json:"audio_lang"` // 配音视频：指定音轨语言，空 = 原声
}

var langRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

func langFilter(l string) string {
	if l == "" || !langRe.MatchString(l) {
		return ""
	}
	return "[language=" + l + "]"
}

var nameTpl = map[string]string{
	"basic":  "%(title).80B.%(ext)s",
	"pretty": "%(title).70B%(uploader,channel& - {}|)s%(height& {}p|)s.%(ext)s",
	"nerdy":  "%(title).60B [%(id)s]%(height& {}p|)s%(vcodec& {}|)s.%(ext)s",
}
var codecFilter = map[string]string{"h264": "[vcodec^=avc]", "av1": "[vcodec^=av01]", "vp9": "[vcodec~='^(vp9|vp09)']"}

func buildFormat(r DLReq) (string, string) {
	h := ""
	if q, err := strconv.Atoi(r.Quality); err == nil && r.Quality != "max" {
		h = fmt.Sprintf("[height<=%d]", q)
	}
	cf := codecFilter[r.Codec]
	container := "mp4"
	if r.Codec == "vp9" {
		container = "webm/mkv"
	}
	L := langFilter(r.AudioLang)
	if r.FormatID != "" {
		id := r.FormatID
		if v, ok := strings.CutSuffix(id, "+bestaudio"); ok && L != "" {
			id = v + "+ba" + L + "/" + v + "+ba"
		}
		return id + "/bv*" + h + "+ba/b" + h + "/b", container
	}
	if r.Mode == "mute" {
		return "bv*" + h + cf + "/bv*" + h + "/bv*/b", container
	}
	// 音频不按编码筛选：yt-dlp 默认按语言优先级排序，会选中原声；合并时统一转成 AAC
	f := ""
	if L != "" {
		f = "bv*" + h + cf + "+ba" + L + "/bv*" + h + "+ba" + L + "/"
	}
	return f + "bv*" + h + cf + "+ba/bv*" + h + "+ba/b" + h + "/bv*+ba/b", container
}

var safeAudio = map[string]bool{"best": true, "mp3": true, "m4a": true, "opus": true, "wav": true}

func runTask(t *Task, r DLReq) {
	sem <- struct{}{}
	defer func() { <-sem }()
	set := func(f func()) { t.mu.Lock(); f(); t.mu.Unlock() }
	set(func() { t.Phase = "开始下载" })

	tpl := nameTpl[r.Filename]
	if tpl == "" {
		tpl = nameTpl["basic"]
	}
	// 每个任务先下到独立临时目录，完成后再移进「下载」，避免同名文件互相覆盖
	work := filepath.Join(dataDir, "tmp", t.ID)
	os.MkdirAll(work, 0o755)
	defer os.RemoveAll(work)
	args := append(baseArgs(r.URL, r.Browser),
		"-o", filepath.Join(work, tpl), "--no-playlist", "--newline", "--windows-filenames",
		"--progress", "--progress-template", "download:[vdl]%(progress)j",
		"--print", "after_move:[file]%(filepath)s", "--no-simulate")
	if r.Mode == "audio" {
		af := r.AudioFormat
		if !safeAudio[af] {
			af = "mp3"
		}
		args = append(args, "-f", "ba"+langFilter(r.AudioLang)+"/bestaudio/best", "-x", "--audio-format", af)
		if af != "best" {
			br := r.AudioBitrate
			if _, err := strconv.Atoi(br); err != nil {
				br = "192"
			}
			args = append(args, "--audio-quality", br+"K")
		}
	} else {
		f, container := buildFormat(r)
		args = append(args, "-f", f, "--merge-output-format", container)
		if container == "mp4" {
			args = append(args, "--postprocessor-args", "Merger:-c:v copy -c:a aac -b:a 192k")
		}
	}
	args = append(args, r.URL)

	cmd := command(ytdlp, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	var errBuf strings.Builder
	var errMu sync.Mutex
	var final string
	handle := func(line string) {
		if i := strings.Index(line, "[vdl]"); i >= 0 {
			var p M
			if json.Unmarshal([]byte(line[i+5:]), &p) == nil {
				set(func() {
					if str(p, "status") == "finished" {
						t.Phase = "处理中（合并/转码）"
						return
					}
					done, total := num(p, "downloaded_bytes"), num(p, "total_bytes")
					if total == 0 {
						total = num(p, "total_bytes_estimate")
					}
					t.Phase = "下载中"
					if total > 0 {
						t.Progress = float64(int(done/total*1000)) / 10
					}
					if s := num(p, "speed"); s > 0 {
						t.Speed = humanSize(s) + "/s"
					}
					t.Eta = p["eta"]
					t.Downloaded, t.Total = humanSize(done), humanSize(total)
				})
			}
			return
		}
		if strings.HasPrefix(line, "[file]") {
			final = strings.TrimPrefix(line, "[file]")
			return
		}
		errMu.Lock()
		errBuf.WriteString(line + "\n")
		errMu.Unlock()
	}
	if err := cmd.Start(); err != nil {
		set(func() { t.Status, t.Phase, t.Error = "error", "失败", err.Error() })
		return
	}
	var wg sync.WaitGroup
	for _, rd := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(rd io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(rd)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			for sc.Scan() {
				handle(sc.Text())
			}
		}(rd)
	}
	wg.Wait()
	err := cmd.Wait()
	if err != nil || final == "" || !exists(final) {
		msg := friendlyError(errBuf.String())
		if err == nil {
			msg = "下载完成但没找到文件：" + msg
		}
		set(func() { t.Status, t.Phase, t.Error = "error", "失败", msg })
		return
	}
	if r.Mode == "mute" {
		set(func() { t.Phase = "去除音轨" })
		ext := filepath.Ext(final)
		muted := strings.TrimSuffix(final, ext) + " (无声)" + ext
		if e := command(filepath.Join(ffmpegDir, exe("ffmpeg")), "-loglevel", "error", "-y", "-i", final, "-map", "0:v", "-c", "copy", muted).Run(); e == nil {
			os.Remove(final)
			final = muted
		}
	}
	os.MkdirAll(outDir, 0o755)
	ext := filepath.Ext(final)
	stem := strings.TrimSuffix(filepath.Base(final), ext)
	dest := filepath.Join(outDir, stem+ext)
	for i := 2; exists(dest); i++ {
		dest = filepath.Join(outDir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	if err := moveFile(final, dest); err != nil {
		set(func() { t.Status, t.Phase, t.Error = "error", "失败", "保存到下载文件夹失败："+err.Error() })
		return
	}
	final = dest
	st, _ := os.Stat(final)
	set(func() {
		t.Status, t.Phase, t.Progress = "done", "完成", 100
		t.File, t.path = filepath.Base(final), final
		if st != nil {
			t.Size = humanSize(float64(st.Size()))
		}
	})
}

func moveFile(src, dst string) error {
	if os.Rename(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---------------------------------------------------------------- HTTP

var domainRe = regexp.MustCompile(`^(all|[a-z0-9.-]{3,80})$`)

func newID() string {
	const c = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = c[rand.Intn(len(c))]
	}
	return string(b)
}

func main() {
	logf, _ := os.OpenFile(filepath.Join(os.TempDir(), "videodl.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logf != nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logf))
	}
	base := "http://127.0.0.1:" + port
	// 已在运行：直接打开页面
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond); err == nil {
		c.Close()
		openURL(base)
		time.Sleep(500 * time.Millisecond)
		return
	}
	go setupTools()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		touch()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML)
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		touch()
		setupMu.Lock()
		defer setupMu.Unlock()
		var ck []string
		ents, _ := os.ReadDir(cookieDir)
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".txt") {
				ck = append(ck, e.Name())
			}
		}
		writeJSON(w, 200, M{"need_token": false, "yt_dlp": version, "local": true, "native": true, "platform": runtime.GOOS,
			"out_dir": outDir, "ready": ready, "setup": setupMsg, "cookies": ck})
	})
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { touch(); writeJSON(w, 200, M{"ok": true}) })
	mux.HandleFunc("/api/bye", func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		byeAt = time.Now()
		seenMu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, M{"ok": true})
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	})
	notReady := func(w http.ResponseWriter) bool {
		setupMu.Lock()
		defer setupMu.Unlock()
		if !ready {
			fail(w, 503, "组件还在准备中："+setupMsg)
			return true
		}
		return false
	}
	mux.HandleFunc("/api/parse", func(w http.ResponseWriter, r *http.Request) {
		touch()
		if notReady(w) {
			return
		}
		var req struct{ URL, Browser string }
		json.NewDecoder(r.Body).Decode(&req)
		u := extractURL(req.URL)
		if u == "" {
			fail(w, 400, "没有找到有效链接")
			return
		}
		d, err := parseURL(u, req.Browser, 0)
		if err != nil {
			fail(w, 422, err.Error())
			return
		}
		d["source_url"] = u
		writeJSON(w, 200, d)
	})
	mux.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
		touch()
		if notReady(w) {
			return
		}
		req := DLReq{Mode: "auto", Quality: "1080", Codec: "h264", AudioFormat: "mp3", AudioBitrate: "192", Filename: "basic"}
		json.NewDecoder(r.Body).Decode(&req)
		req.URL = extractURL(req.URL)
		if req.URL == "" {
			fail(w, 400, "没有找到有效链接")
			return
		}
		t := &Task{ID: newID(), Status: "running", Phase: "排队中", Title: req.Title}
		tasksMu.Lock()
		tasks[t.ID] = t
		tasksMu.Unlock()
		go runTask(t, req)
		writeJSON(w, 200, M{"task_id": t.ID})
	})
	getTask := func(id string) *Task { tasksMu.Lock(); defer tasksMu.Unlock(); return tasks[id] }
	mux.HandleFunc("/api/task/", func(w http.ResponseWriter, r *http.Request) {
		touch()
		t := getTask(strings.TrimPrefix(r.URL.Path, "/api/task/"))
		if t == nil {
			fail(w, 404, "任务不存在")
			return
		}
		t.mu.Lock()
		b, _ := json.Marshal(t)
		t.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("/api/file/", func(w http.ResponseWriter, r *http.Request) {
		t := getTask(strings.TrimPrefix(r.URL.Path, "/api/file/"))
		if t == nil || t.path == "" {
			fail(w, 404, "文件不存在")
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(t.File))
		http.ServeFile(w, r, t.path)
	})
	mux.HandleFunc("/api/reveal/", func(w http.ResponseWriter, r *http.Request) {
		touch()
		t := getTask(strings.TrimPrefix(r.URL.Path, "/api/reveal/"))
		if t == nil || t.path == "" {
			fail(w, 404, "文件不存在")
			return
		}
		revealFile(t.path)
		writeJSON(w, 200, M{"ok": true})
	})
	mux.HandleFunc("/api/cookies", func(w http.ResponseWriter, r *http.Request) {
		touch()
		switch r.Method {
		case "GET":
			ds := []string{}
			ents, _ := os.ReadDir(cookieDir)
			for _, e := range ents {
				if strings.HasSuffix(e.Name(), ".txt") {
					ds = append(ds, strings.TrimSuffix(e.Name(), ".txt"))
				}
			}
			writeJSON(w, 200, M{"domains": ds})
		case "PUT":
			var req struct{ Domain, Text string }
			json.NewDecoder(r.Body).Decode(&req)
			d := strings.ToLower(strings.TrimSpace(req.Domain))
			if !domainRe.MatchString(d) {
				fail(w, 400, "域名格式不对")
				return
			}
			if !strings.Contains(req.Text, "\t") {
				fail(w, 400, "不是 Netscape 格式的 cookies.txt（应为制表符分隔）")
				return
			}
			txt := req.Text
			if !strings.HasPrefix(txt, "# ") {
				txt = "# Netscape HTTP Cookie File\n" + txt
			}
			os.WriteFile(filepath.Join(cookieDir, d+".txt"), []byte(txt), 0o600)
			writeJSON(w, 200, M{"ok": true})
		default:
			fail(w, 405, "method not allowed")
		}
	})
	mux.HandleFunc("/api/cookies/", func(w http.ResponseWriter, r *http.Request) {
		touch()
		d, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/cookies/"))
		if r.Method != "DELETE" || !domainRe.MatchString(d) {
			fail(w, 400, "请求不正确")
			return
		}
		os.Remove(filepath.Join(cookieDir, d+".txt"))
		writeJSON(w, 200, M{"ok": true})
	})

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		log.Fatal(err)
	}
	// 自动退出：页面关闭后几秒（刷新页面不算），或 3 分钟没有任何心跳；有任务在下载时不退出
	go func() {
		for range time.Tick(2 * time.Second) {
			seenMu.Lock()
			idle, bye, seen := time.Since(lastSeen), byeAt, lastSeen
			seenMu.Unlock()
			running := false
			tasksMu.Lock()
			for _, t := range tasks {
				t.mu.Lock()
				if t.Status == "running" {
					running = true
				}
				t.mu.Unlock()
			}
			tasksMu.Unlock()
			if running {
				continue
			}
			closed := !bye.IsZero() && seen.Before(bye) && time.Since(bye) > 8*time.Second
			if closed || idle > 3*time.Minute {
				log.Println("page closed / idle, exit")
				os.Exit(0)
			}
		}
	}()
	if os.Getenv("VDL_NO_OPEN") != "1" {
		go openURL(base)
	}
	log.Println("listening", base)
	log.Fatal(http.Serve(ln, mux))
}
