package player

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/hezimy/PrismCast/internal/applog"
)

// PlaybackStatus represents the current playback state
type PlaybackStatus struct {
	State    string  `json:"state"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Duration float64 `json:"duration"`
	Position float64 `json:"position"`
	Volume   int     `json:"volume"`
	URI      string  `json:"uri"`
}

// PlayerEventHandler 播放器状态变化回调
type PlayerEventHandler func(event string)

type playbackBackend int

const (
	backendNone playbackBackend = iota
	backendMPV
	backendBrowser
	backendSystem
)

// MPVPlayer 通过命令行启动 mpv，并用 JSON IPC 实现遥控
type MPVPlayer struct {
	playerPath       string
	imageViewerFirst   bool
	initialVolume      int
	dlnaVolumeLast     int
	dlnaVolumeSynced   bool
	browserPID         uint32
	playbackBackend    playbackBackend
	browserPlayBaseURL string
	cmd                *exec.Cmd
	mu               sync.RWMutex
	loadMu           sync.Mutex
	status           PlaybackStatus
	started          bool
	done             chan struct{}
	ipc              *mpvIPCClient
	ipcDone          chan struct{}
	onEvent          PlayerEventHandler

	imgViewerProcs   []syscall.Handle
	videoPlayerProcs []videoProcInfo
	tempFileCache    map[string]string
	lastLoadURI      string
	lastLoadTime     time.Time
}

// NewMPVPlayer creates a new MPV player controller
func NewMPVPlayer(playerPath string, imageViewerFirst bool) *MPVPlayer {
	p := &MPVPlayer{
		playerPath:       playerPath,
		imageViewerFirst: imageViewerFirst,
		tempFileCache:    make(map[string]string),
		initialVolume:    100,
		dlnaVolumeLast:   100,
		status: PlaybackStatus{
			State:  "idle",
			Volume: 100,
		},
	}
	return p
}

// SetBrowserPlayBaseURL 设置 DLNA HTTP 基址，用于无 mpv 时打开 HLS 浏览器播放页
func (p *MPVPlayer) SetBrowserPlayBaseURL(baseURL string) {
	p.mu.Lock()
	p.browserPlayBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	p.mu.Unlock()
}

// SetImageViewerFirst 更新图片打开优先级（立即生效，不影响正在播放的媒体）
func (p *MPVPlayer) SetImageViewerFirst(first bool) {
	p.mu.Lock()
	p.imageViewerFirst = first
	p.mu.Unlock()
}

// SetPlayerPath 更新 mpv 可执行文件路径
func (p *MPVPlayer) SetPlayerPath(path string) {
	p.mu.Lock()
	p.playerPath = strings.TrimSpace(path)
	p.mu.Unlock()
}

// SetInitialVolume 设置界面显示的默认音量刻度（不写入 mpv/浏览器参数，音量由系统与快捷键步进控制）
func (p *MPVPlayer) SetInitialVolume(volume int) {
	volume = clampDLNAVolume(volume)
	p.mu.Lock()
	p.initialVolume = volume
	if !p.dlnaVolumeSynced {
		p.dlnaVolumeLast = volume
		p.status.Volume = volume
	}
	p.mu.Unlock()
}

func (p *MPVPlayer) setPlaybackBackend(b playbackBackend) {
	p.mu.Lock()
	p.playbackBackend = b
	p.mu.Unlock()
}

func (p *MPVPlayer) resetDLNAVolumeSync() {
	p.mu.Lock()
	p.dlnaVolumeSynced = false
	p.dlnaVolumeLast = p.initialVolume
	if p.dlnaVolumeLast <= 0 {
		p.dlnaVolumeLast = 100
	}
	p.mu.Unlock()
}

// SetEventHandler 注册播放器事件回调（stopped / error）
func (p *MPVPlayer) SetEventHandler(handler PlayerEventHandler) {
	p.mu.Lock()
	p.onEvent = handler
	p.mu.Unlock()
}

func (p *MPVPlayer) emitEvent(event string) {
	p.mu.RLock()
	handler := p.onEvent
	p.mu.RUnlock()
	if handler != nil {
		handler(event)
	}
}

// resolvePlayerPath 将相对路径解析为基于exe目录的绝对路径
func (p *MPVPlayer) resolvePlayerPath() string {
	if filepath.IsAbs(p.playerPath) {
		return p.playerPath
	}
	exePath, err := os.Executable()
	if err != nil {
		return p.playerPath
	}
	exeDir := filepath.Dir(exePath)
	absPath := filepath.Join(exeDir, p.playerPath)
	if _, err := os.Stat(absPath); err == nil {
		return absPath
	}
	return p.playerPath
}

// Start 不做任何事（mpv按需启动）
func (p *MPVPlayer) Start() error {
	return nil
}

// isImageURI 判断URI是否为图片类型
func isImageURI(uri string) bool {
	lower := strings.ToLower(uri)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico", ".heic", ".heif", ".avif", ".tiff", ".tif"}
	for _, ext := range imageExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	if strings.Contains(lower, "/image/") || strings.Contains(lower, "image?") ||
		strings.Contains(lower, "/photo/") || strings.Contains(lower, "jpeg") ||
		strings.Contains(lower, ".jpg?") || strings.Contains(lower, ".png?") {
		return true
	}
	return false
}

// getImageExt 从URI提取图片扩展名，默认.jpg
func getImageExt(uri string) string {
	lower := strings.ToLower(uri)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico", ".heic", ".heif", ".avif", ".tiff", ".tif"}
	for _, ext := range imageExts {
		if strings.HasSuffix(lower, ext) {
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	return ".jpg"
}

// isVideoURI 判断URI是否为视频类型（不含音频）
func isVideoURI(uri string) bool {
	lower := strings.ToLower(uri)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	videoExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".rmvb", ".ts", ".mts", ".m2ts", ".mpg", ".mpeg", ".3gp"}
	for _, ext := range videoExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	// 检查URL中的MIME hint
	if strings.Contains(lower, "/video/") || strings.Contains(lower, "video?") ||
		strings.Contains(lower, ".mp4?") || strings.Contains(lower, ".mkv?") ||
		strings.Contains(lower, "m3u8") || strings.Contains(lower, ".flv?") {
		return true
	}
	return false
}

// isAudioURI 判断URI是否为音频类型
func isAudioURI(uri string) bool {
	lower := strings.ToLower(uri)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	audioExts := []string{".mp3", ".wav", ".flac", ".aac", ".ogg", ".wma", ".m4a", ".ape", ".opus"}
	for _, ext := range audioExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	if strings.Contains(lower, "/audio/") || strings.Contains(lower, "audio?") || strings.Contains(lower, ".mp3?") {
		return true
	}
	return false
}

// isMediaURI 判断URI是否为视频或音频类型
func isMediaURI(uri string) bool {
	return isVideoURI(uri) || isAudioURI(uri)
}

// needsBrowserFallback 无 mpv 时，系统播放器/ShellExecute 无法正确处理的 URL
func needsBrowserFallback(uri string) bool {
	lower := strings.ToLower(uri)
	if strings.Contains(lower, "m3u8") || strings.Contains(lower, ".php") ||
		strings.Contains(lower, "/api/") || strings.Contains(lower, "play.php") {
		return true
	}
	if isHTTPURL(uri) && !isMediaURI(uri) && !isImageURI(uri) {
		return true
	}
	return false
}

// normalizeURI 解码 HTML 实体并清理 URI
func normalizeURI(uri string) string {
	uri = strings.TrimSpace(uri)
	return decodeHTMLEntities(uri)
}

// isHTTPURL 判断是否为HTTP/HTTPS URL
func isHTTPURL(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// ensureTempFile 确保URL对应的图片已下载到临时文件（带缓存，避免重复下载）
func (p *MPVPlayer) ensureTempFile(uri string) (string, error) {
	// 检查缓存
	if cached, ok := p.tempFileCache[uri]; ok {
		if _, err := os.Stat(cached); err == nil {
			applog.Verbosef("[Cache] 命中缓存: %s", cached)
			return cached, nil
		}
		delete(p.tempFileCache, uri)
	}

	ext := getImageExt(uri)
	applog.Verbosef("[Download] 下载HTTP图片: %s", uri)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(uri)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP状态码: %d", resp.StatusCode)
	}

	tempDir := filepath.Join(os.TempDir(), "PrismCast_images")
	os.MkdirAll(tempDir, 0755)

	tempFile := filepath.Join(tempDir, fmt.Sprintf("prismcast_%d%s", time.Now().UnixNano(), ext))
	f, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}

	applog.Verbosef("[Download] 已下载 %d 字节到 %s", written, tempFile)
	p.tempFileCache[uri] = tempFile
	return tempFile, nil
}

// openWithSystemViewer 使用系统默认图片查看器打开本地文件
func (p *MPVPlayer) openWithSystemViewer(filePath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("system viewer only supported on Windows")
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteEx := shell32.NewProc("ShellExecuteExW")

	type shellExecuteInfo struct {
		cbSize       uint32
		fMask        uint32
		hwnd         uintptr
		lpVerb       *uint16
		lpFile       *uint16
		lpParameters *uint16
		lpDirectory  *uint16
		nShow        int32
		hInstApp     uintptr
		lpIDList     uintptr
		lpClass      *uint16
		hkeyClass    uintptr
		dwHotKey     uint32
		hIcon        uintptr
		hProcess     syscall.Handle
	}

	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(filePath)

	info := shellExecuteInfo{
		cbSize:   uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:    0x00000040,
		lpVerb:   verb,
		lpFile:   file,
		nShow:    1,
		hProcess: 0,
	}

	ret, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return fmt.Errorf("ShellExecuteEx failed: %w", err)
	}

	if info.hProcess != 0 {
		p.mu.Lock()
		p.imgViewerProcs = append(p.imgViewerProcs, info.hProcess)
		p.mu.Unlock()
		applog.Verbosef("[Viewer] 系统查看器已启动 (句柄: %d) 文件: %s", info.hProcess, filePath)
	} else {
		applog.Verbosef("[Viewer] 系统查看器已启动 (无进程句柄) 文件: %s", filePath)
	}

	return nil
}

// closeImageViewer 关闭所有系统图片查看器进程
func (p *MPVPlayer) closeImageViewer() {
	p.mu.Lock()
	procs := p.imgViewerProcs
	p.imgViewerProcs = nil
	p.mu.Unlock()

	if len(procs) == 0 {
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	terminateProcess := kernel32.NewProc("TerminateProcess")
	closeHandle := kernel32.NewProc("CloseHandle")

	for _, proc := range procs {
		if proc != 0 {
			terminateProcess.Call(uintptr(proc), 1)
			closeHandle.Call(uintptr(proc))
		}
	}
	applog.Verbosef("[Viewer] 已关闭 %d 个系统查看器进程", len(procs))
}

// openWithSystemVideoPlayer 使用系统默认播放器打开 URI（支持本地文件和网络流）
func (p *MPVPlayer) openWithSystemVideoPlayer(uri string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("system video player only supported on Windows")
	}

	if needsBrowserFallback(uri) {
		return fmt.Errorf("URL 需要浏览器/mpv 处理: %s", uri)
	}

	isHTTP := isHTTPURL(uri)
	isVid := isVideoURI(uri)
	isAud := isAudioURI(uri)
	applog.Verbosef("[VideoPlayer] openWithSystemVideoPlayer: uri=%s isHTTP=%v isVideo=%v isAudio=%v", uri, isHTTP, isVid, isAud)

	if isHTTP && isVid {
		return p.launchVideoPlayerDirect(uri)
	}

	if isHTTP && isAud {
		return p.launchAudioPlayerDirect(uri)
	}

	return p.openWithSystemVideoPlayerFallback(uri)
}

// launchVideoPlayerDirect 直接用系统默认视频播放器的 exe 打开 HTTP URL
func (p *MPVPlayer) launchVideoPlayerDirect(uri string) error {
	return p.launchPlayerDirect(uri, ".mp4", "视频")
}

// launchAudioPlayerDirect 直接用系统默认音频播放器的 exe 打开 HTTP URL
func (p *MPVPlayer) launchAudioPlayerDirect(uri string) error {
	return p.launchPlayerDirect(uri, ".mp3", "音频")
}

// launchPlayerDirect 通用函数：直接用指定扩展名关联的播放器 exe 打开 HTTP URL
func (p *MPVPlayer) launchPlayerDirect(uri string, ext string, mediaType string) error {
	exePath := ""

	exePath = getExePathViaAssocQueryString(ext)
	if exePath == "" || !fileExists(exePath) {
		fullCmd := GetPlayerExePathForExt(ext)
		if fullCmd != "" {
			exePath = extractExePathFromCmd(fullCmd)
			if exePath == "" {
				fields := strings.Fields(fullCmd)
				if len(fields) > 0 {
					exePath = strings.Trim(fields[0], `"`)
				}
			}
			if exePath != "" && !fileExists(exePath) {
				exePath = ""
			}
		}
	}

	if exePath == "" || !fileExists(exePath) {
		return fmt.Errorf("无法获取系统%s播放器路径，且不能对HTTP URL使用ShellExecuteExW（会打开浏览器）", mediaType)
	}

	cmd := exec.Command(exePath, uri)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动%s播放器失败: %w", mediaType, err)
	}

	p.mu.Lock()
	p.videoPlayerProcs = append(p.videoPlayerProcs, videoProcInfo{pid: cmd.Process.Pid})
	p.mu.Unlock()

	p.setPlaybackBackend(backendSystem)
	applog.Mainf("[VideoPlayer] %s播放器已启动 (PID: %d)", mediaType, cmd.Process.Pid)
	return nil
}

// openWithSystemVideoPlayerFallback 回退方案：使用 ShellExecuteExW open
func (p *MPVPlayer) openWithSystemVideoPlayerFallback(uri string) error {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteEx := shell32.NewProc("ShellExecuteExW")

	type shellExecuteInfo struct {
		cbSize       uint32
		fMask        uint32
		hwnd         uintptr
		lpVerb       *uint16
		lpFile       *uint16
		lpParameters *uint16
		lpDirectory  *uint16
		nShow        int32
		hInstApp     uintptr
		lpIDList     uintptr
		lpClass      *uint16
		hkeyClass    uintptr
		dwHotKey     uint32
		hIcon        uintptr
		hProcess     syscall.Handle
	}

	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(uri)

	info := shellExecuteInfo{
		cbSize:   uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:    0x00000040,
		lpVerb:   verb,
		lpFile:   file,
		nShow:    1,
		hProcess: 0,
	}

	ret, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return fmt.Errorf("ShellExecuteEx failed: %w", err)
	}
	applog.Verbosef("[VideoPlayer] ShellExecuteExW 已打开 (无句柄) URI: %s", uri)
	p.setPlaybackBackend(backendSystem)
	return nil
}

// extractExePathFromCmd 从注册表命令字符串中提取 exe 完整路径
func extractExePathFromCmd(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if strings.HasPrefix(cmd, `"`) {
		end := strings.Index(cmd[1:], `"`)
		if end >= 0 {
			return cmd[1 : end+1]
		}
	}
	parts := strings.Fields(cmd)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

type videoProcInfo struct {
	pid int
}

// getExePathViaAssocQueryString 使用 Windows AssocQueryStringW API 获取指定扩展名关联的可执行文件路径
func getExePathViaAssocQueryString(ext string) string {
	shlwapi := syscall.NewLazyDLL("shlwapi.dll")
	assocQueryString := shlwapi.NewProc("AssocQueryStringW")

	if assocQueryString.Find() != nil {
		shell32 := syscall.NewLazyDLL("shell32.dll")
		assocQueryString = shell32.NewProc("AssocQueryStringW")
		if assocQueryString.Find() != nil {
			return ""
		}
	}

	const ASSOCSTR_EXECUTABLE = 2
	const ASSOCF_NONE = 0
	const bufSize = 1024
	var buf [bufSize]uint16
	returnedBufLen := uint32(bufSize)

	extW, _ := syscall.UTF16PtrFromString(ext)
	ret, _, _ := assocQueryString.Call(
		ASSOCF_NONE,
		ASSOCSTR_EXECUTABLE,
		uintptr(unsafe.Pointer(extW)),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&returnedBufLen)))

	if ret != 0 {
		return ""
	}

	pathStr := syscall.UTF16ToString(buf[:])
	return pathStr
}

// closeVideoPlayer 关闭所有系统视频播放器进程
func (p *MPVPlayer) closeVideoPlayer() {
	p.mu.Lock()
	procs := p.videoPlayerProcs
	p.videoPlayerProcs = nil
	p.mu.Unlock()

	if len(procs) == 0 {
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	openProcess := kernel32.NewProc("OpenProcess")
	terminateProcess := kernel32.NewProc("TerminateProcess")
	closeHandle := kernel32.NewProc("CloseHandle")

	const PROCESS_TERMINATE = 0x0001
	closedCount := 0

	for _, proc := range procs {
		if proc.pid == 0 {
			continue
		}
		hProcess, _, _ := openProcess.Call(PROCESS_TERMINATE, 0, uintptr(proc.pid))
		if hProcess == 0 {
			continue
		}
		terminateProcess.Call(hProcess, 1)
		closeHandle.Call(hProcess)
		closedCount++
	}

	applog.Verbosef("[VideoPlayer] 已关闭 %d/%d 个系统视频播放器进程", closedCount, len(procs))
}

func (p *MPVPlayer) closeBrowser() {
	p.mu.Lock()
	pid := p.browserPID
	p.browserPID = 0
	p.mu.Unlock()
	if pid == 0 {
		return
	}
	applog.Verbosef("[Browser] 关闭浏览器进程 (PID: %d)", pid)
	terminateProcessByPID(pid)
}

// Load 直接启动mpv播放指定URI
func (p *MPVPlayer) Load(uri string, title string) error {
	uri = normalizeURI(uri)

	p.loadMu.Lock()
	defer p.loadMu.Unlock()

	p.lastLoadURI = uri
	p.lastLoadTime = time.Now()

	p.stopMPV()
	p.closeBrowser()
	p.closeVideoPlayer()
	p.closeImageViewer()
	p.setPlaybackBackend(backendNone)
	p.resetDLNAVolumeSync()

	return p.fallbackMPV(uri, title)
}

// fallbackMPV 当系统播放器不可用时回退到 mpv 播放器
func (p *MPVPlayer) fallbackMPV(uri string, title string) error {
	resolvedPath := p.resolvePlayerPath()
	p.playerPath = resolvedPath

	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		if isImageURI(uri) {
			return p.openImageWithFallback(uri, title)
		}

		if err := p.openWithSystemVideoPlayer(uri); err != nil {
			if err2 := p.openWithBrowserFallback(uri); err2 != nil {
				p.mu.Lock()
				p.started = false
				p.status.State = "error"
				p.status.Title = fmt.Sprintf("无法播放: %v", err)
				p.mu.Unlock()
				return fmt.Errorf("mpv不存在且系统默认打开失败: %w", err)
			}
		} else {
			p.setPlaybackBackend(backendSystem)
		}
		p.mu.Lock()
		p.started = true
		p.status.URI = uri
		p.status.Title = title
		p.status.State = "playing"
		p.mu.Unlock()
		return nil
	}

	return p.startMPV(uri, title)
}

// stopMPV 停止mpv进程
func (p *MPVPlayer) stopMPV() {
	p.mu.RLock()
	cmd := p.cmd
	done := p.done
	p.mu.RUnlock()

	p.clearIPC()

	if cmd != nil && cmd.Process != nil {
		applog.Verbosef("[MPV] 停止mpv进程 (PID: %d)", cmd.Process.Pid)
		cmd.Process.Kill()
		if done != nil {
			<-done
		}
	}
	p.mu.Lock()
	p.started = false
	p.status.State = "stopped"
	p.status.URI = ""
	p.status.Position = 0
	p.status.Duration = 0
	p.cmd = nil
	p.mu.Unlock()
}

func (p *MPVPlayer) Play() error {
	p.mu.RLock()
	ipc := p.ipc
	p.mu.RUnlock()
	if ipc != nil {
		if err := ipc.play(); err != nil {
			applog.Mainf("[MPV-IPC] Play 失败: %v", err)
			return err
		}
		p.mu.Lock()
		p.status.State = "playing"
		p.mu.Unlock()
		return nil
	}
	return nil
}

func (p *MPVPlayer) Pause() error {
	p.mu.RLock()
	ipc := p.ipc
	p.mu.RUnlock()
	if ipc != nil {
		if err := ipc.pause(); err != nil {
			applog.Mainf("[MPV-IPC] Pause 失败: %v", err)
			return err
		}
		p.mu.Lock()
		p.status.State = "paused"
		p.mu.Unlock()
		return nil
	}
	return nil
}

func (p *MPVPlayer) Stop() error {
	p.loadMu.Lock()
	defer p.loadMu.Unlock()
	p.stopMPV()
	p.closeBrowser()
	p.closeImageViewer()
	p.closeVideoPlayer()
	p.setPlaybackBackend(backendNone)
	return nil
}

func (p *MPVPlayer) Seek(position float64) error {
	p.mu.RLock()
	ipc := p.ipc
	dur := p.status.Duration
	p.mu.RUnlock()
	if ipc != nil {
		if dur <= 0 && position > 0 {
			// 媒体尚未加载完成，忽略过早的 Seek
			return nil
		}
		if err := ipc.seek(position); err != nil {
			applog.Mainf("[MPV-IPC] Seek 失败: %v", err)
			return err
		}
		p.mu.Lock()
		p.status.Position = position
		p.mu.Unlock()
	}
	return nil
}

func (p *MPVPlayer) SetVolume(volume int) error {
	return p.adjustVolumeFromDLNA(volume, "local")
}

func (p *MPVPlayer) AdjustVolumeFromDLNA(volume int, source string) error {
	return p.adjustVolumeFromDLNA(volume, source)
}

func (p *MPVPlayer) adjustVolumeFromDLNA(desired int, source string) error {
	desired = clampDLNAVolume(desired)

	p.mu.Lock()
	if !p.dlnaVolumeSynced {
		p.dlnaVolumeLast = desired
		p.dlnaVolumeSynced = true
		p.status.Volume = desired
		p.mu.Unlock()
		applog.Verbosef("[Volume] 首次同步(%s): %d（不改动播放器，由系统默认音量播放）", source, desired)
		return nil
	}

	delta := desired - p.dlnaVolumeLast
	p.dlnaVolumeLast = desired
	p.status.Volume = desired
	backend := p.playbackBackend
	ipc := p.ipc
	browserPID := p.browserPID
	p.mu.Unlock()

	if delta == 0 {
		return nil
	}

	applog.Verbosef("[Volume] 调节(%s): %d -> %d (Δ%d)", source, desired-delta, desired, delta)

	switch backend {
	case backendMPV:
		if ipc == nil {
			return nil
		}
		if err := adjustMPVVolumeByKeypress(ipc, delta); err != nil {
			applog.Verbosef("[Volume] MPV 快捷键调节失败: %v", err)
			return err
		}
	case backendBrowser:
		if err := adjustBrowserVolumeByKeypress(browserPID, delta); err != nil {
			applog.Verbosef("[Volume] 浏览器 Ctrl+方向键调节失败: %v", err)
			return err
		}
	default:
		// 系统关联播放器：无法注入快捷键，仅记录手机端刻度
	}
	return nil
}

func (p *MPVPlayer) SetMute(mute bool) error {
	p.mu.RLock()
	ipc := p.ipc
	p.mu.RUnlock()
	if ipc != nil {
		if err := ipc.setMute(mute); err != nil {
			applog.Mainf("[MPV-IPC] SetMute 失败: %v", err)
			return err
		}
	}
	return nil
}

func (p *MPVPlayer) Status() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status.State
}

func (p *MPVPlayer) GetStatus() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"state":    p.status.State,
		"title":    p.status.Title,
		"artist":   p.status.Artist,
		"duration": p.status.Duration,
		"position": p.status.Position,
		"volume":   p.status.Volume,
		"uri":      p.status.URI,
	}
}

// Cleanup 清理所有进程和临时文件
func (p *MPVPlayer) Cleanup() {
	p.loadMu.Lock()
	defer p.loadMu.Unlock()
	p.stopMPV()
	p.closeImageViewer()
	p.closeVideoPlayer()

	// 清理所有缓存的临时文件
	for url, path := range p.tempFileCache {
		os.Remove(path)
		delete(p.tempFileCache, url)
	}
	applog.Verbosef("[Cleanup] 已清理所有临时缓存")
}

// openImageWithFallback 打开图片：按用户设置决定优先顺序，都失败时浏览器保底
func (p *MPVPlayer) openImageWithFallback(uri string, title string) error {
	var filePath string
	if isHTTPURL(uri) {
		tempFile, err := p.ensureTempFile(uri)
		if err != nil {
			return p.openWithBrowserFallback(uri)
		}
		filePath = tempFile
	} else {
		filePath = uri
	}

	if p.imageViewerFirst {
		if err := p.openWithSystemViewer(filePath); err != nil {
			resolvedPath := p.resolvePlayerPath()
			if _, err2 := os.Stat(resolvedPath); err2 == nil {
				if err3 := p.startMPV(uri, title); err3 != nil {
					return p.openWithBrowserFallback(uri)
				}
				return nil
			}
			return p.openWithBrowserFallback(uri)
		}
		return nil
	}

	resolvedPath := p.resolvePlayerPath()
	if _, err := os.Stat(resolvedPath); err == nil {
		if err2 := p.startMPV(uri, title); err2 != nil {
			if err3 := p.openWithSystemViewer(filePath); err3 != nil {
				return p.openWithBrowserFallback(uri)
			}
			return nil
		}
		return nil
	}
	if err := p.openWithSystemViewer(filePath); err != nil {
		return p.openWithBrowserFallback(uri)
	}
	return nil
}

func isHLStreamURI(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.Contains(lower, "m3u8") || strings.Contains(lower, "/hls/")
}

// openWithBrowserFallback 使用浏览器打开URI作为最终保底
func (p *MPVPlayer) openWithBrowserFallback(uri string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("browser fallback only supported on Windows")
	}

	openURI := uri
	if isHLStreamURI(uri) {
		p.mu.RLock()
		base := p.browserPlayBaseURL
		p.mu.RUnlock()
		if base != "" {
			openURI = fmt.Sprintf("%s/browser-play?uri=%s", base, url.QueryEscape(uri))
			applog.Mainf("[MPV] HLS 浏览器播放页: %s", openURI)
		}
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteEx := shell32.NewProc("ShellExecuteExW")

	type shellExecuteInfo struct {
		cbSize       uint32
		fMask        uint32
		hwnd         uintptr
		lpVerb       *uint16
		lpFile       *uint16
		lpParameters *uint16
		lpDirectory  *uint16
		nShow        int32
		hInstApp     uintptr
		lpIDList     uintptr
		lpClass      *uint16
		hkeyClass    uintptr
		dwHotKey     uint32
		hIcon        uintptr
		hProcess     syscall.Handle
	}

	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(openURI)

	info := shellExecuteInfo{
		cbSize:   uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:    0x00000040,
		lpVerb:   verb,
		lpFile:   file,
		nShow:    1,
		hProcess: 0,
	}

	ret, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return fmt.Errorf("ShellExecuteExW browser fallback failed: %w", err)
	}
	if info.hProcess != 0 {
		if pid := captureBrowserProcess(info.hProcess); pid != 0 {
			p.mu.Lock()
			p.browserPID = pid
			p.mu.Unlock()
			applog.Verbosef("[Browser] 已记录浏览器 PID: %d", pid)
		}
	}
	applog.Mainf("[MPV] 浏览器保底已打开 URI: %s", openURI)
	p.setPlaybackBackend(backendBrowser)
	return nil
}

// mpvExtraArgsForURI 按 URL 来源生成 mpv HTTP 参数（避免错误 Referer 导致 302/CDN 鉴权失败）
func mpvExtraArgsForURI(uri string) []string {
	return mpvStreamArgsForURI(uri)
}

// startMPV 启动mpv播放
func (p *MPVPlayer) startMPV(uri string, title string) error {
	resolvedPath := p.resolvePlayerPath()
	pipePath := newIPCPath()

	p.mu.Lock()
	ipcDone := make(chan struct{})
	p.ipcDone = ipcDone
	p.ipc = nil
	p.mu.Unlock()

	args := []string{
		uri,
		"--force-window=immediate",
		"--keep-open=always",
		"--no-terminal",
		"--loop-file=no",
		"--loop-playlist=no",
		fmt.Sprintf("--input-ipc-server=%s", pipePath),
	}
	args = append(args, mpvExtraArgsForURI(uri)...)
	if title != "" {
		args = append(args, fmt.Sprintf("--title=%s", title))
	}

	p.cmd = exec.Command(resolvedPath, args...)
	p.cmd.Dir = filepath.Dir(resolvedPath)
	if runtime.GOOS == "windows" {
		p.cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: 0x08000000,
		}
	}

	stderrPipe, err := p.cmd.StderrPipe()
	if err != nil {
		applog.Mainf("[MPV] WARNING: 无法获取stderr pipe: %v", err)
	}

	if err := p.cmd.Start(); err != nil {
		p.clearIPC()
		return fmt.Errorf("mpv启动失败: %w", err)
	}
	if p.cmd.Process == nil {
		p.clearIPC()
		return fmt.Errorf("mpv进程创建失败")
	}
	applog.Mainf("[MPV] mpv进程已创建 (PID: %d, IPC: %s)", p.cmd.Process.Pid, pipePath)
	p.setPlaybackBackend(backendMPV)

	p.mu.Lock()
	p.ipc = newMPVIPCClient(pipePath)
	p.mu.Unlock()

	if stderrPipe != nil {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stderrPipe.Read(buf)
				if n > 0 {
					applog.Verbosef("[MPV-STDERR] %s", string(buf[:n]))
				}
				if err != nil {
					return
				}
			}
		}()
	}

	p.mu.Lock()
	p.started = true
	p.status.URI = uri
	p.status.Title = title
	p.status.State = "playing"
	p.status.Position = 0
	p.status.Duration = 0
	p.done = make(chan struct{})
	doneCh := p.done
	p.mu.Unlock()

	go p.connectIPCAfterStart()

	go func() {
		err := p.cmd.Wait()
		p.clearIPC()
		p.mu.Lock()
		p.started = false
		exitState := "stopped"
		if err != nil {
			exitState = "error"
		}
		p.status.State = exitState
		p.status.URI = ""
		p.status.Position = 0
		p.status.Duration = 0
		p.mu.Unlock()
		close(doneCh)
		if err != nil {
			applog.Mainf("[MPV] 进程异常退出: %v", err)
			p.emitEvent("error")
		} else {
			applog.Mainf("[MPV] 进程正常退出")
			p.emitEvent("stopped")
		}
	}()

	return nil
}
