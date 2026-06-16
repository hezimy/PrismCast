//go:build windows

package player

import (
	"log"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// VideoPlayerInfo 视频播放器信息
type VideoPlayerInfo struct {
	Name      string `json:"name"`
	ExeName   string `json:"exeName"`
	Supported bool   `json:"supported"`
}

// 支持网络流的视频播放器列表（exe 文件名 → 显示名称）
var supportedVideoPlayers = map[string]string{
	"mpv.exe":                    "mpv",
	"vlc.exe":                    "VLC media player",
	"potplayermini64.exe":        "Daum PotPlayer Mini (64bit)",
	"potplayermini.exe":          "Daum PotPlayer Mini",
	"potplayer64.exe":            "Daum PotPlayer (64bit)",
	"potplayer.exe":              "Daum PotPlayer",
	"zmplayer.exe":               "搜狐影音",
	"qqplayer.exe":               "腾讯视频",
	"qvodplayer.exe":             "快播",
	"暴风影音.exe":                "暴风影音（旧版）",
	"stormplayer.exe":            "暴风影音",
	"xmp.exe":                    "迅雷看看",
	"realplay.exe":               "RealPlayer",
	"kmplyer.exe":                "KMPlayer",
	"kmplayer.exe":               "KMPlayer",
	"mplayerc64.exe":              "MPC-HC (64bit)",
	"mplayerc.exe":               "MPC-HC",
	"mplayer2.exe":               "MPlayer2",
	"smplayer.exe":               "SMPlayer",
	"bomi.exe":                   "Bomi",
	"celluloid.exe":              "Celluloid",
	"gnome-mpv.exe":              "GNOME MPV",
	"media player classic.exe":  "Media Player Classic",
	"wmplayer.exe":               "Windows Media Player",
	"movies & tv.exe":           "Windows 电影和电视",
}

// DetectDefaultVideoPlayer 检测系统默认视频播放器
func DetectDefaultVideoPlayer() VideoPlayerInfo {
	exeName, displayName := getSystemDefaultVideoPlayer()
	lowerExe := strings.ToLower(exeName)
	_, isSupported := supportedVideoPlayers[lowerExe]

	info := VideoPlayerInfo{
		Name:      displayName,
		ExeName:   exeName,
		Supported: isSupported,
	}

	log.Printf("[VideoPlayer] 检测到默认视频播放器: %s (%s), 支持网络流: %v", info.Name, info.ExeName, info.Supported)
	return info
}

// getSystemDefaultVideoPlayer 通过 Windows 注册表获取系统默认视频播放器
func getSystemDefaultVideoPlayer() (exeName string, displayName string) {
	exeName = readDefaultViewerFromUserChoice(".mp4")
	if exeName == "" {
		exeName = readDefaultViewerFromUserChoice(".avi")
	}
	if exeName == "" {
		exeName = readDefaultViewerFromUserChoice(".mkv")
	}
	if exeName == "" {
		exeName = readDefaultViewerFromClasses(".mp4")
	}

	if exeName == "" {
		return "unknown", "未知播放器"
	}

	lowerExe := strings.ToLower(exeName)
	if name, ok := supportedVideoPlayers[lowerExe]; ok {
		return exeName, name
	}

	return exeName, strings.TrimSuffix(exeName, filepath.Ext(exeName))
}

// GetVideoPlayerExePath 获取系统默认视频播放器的完整可执行文件路径（.mp4 关联）
func GetVideoPlayerExePath() string {
	return GetPlayerExePathForExt(".mp4")
}

// GetPlayerExePathForExt 获取指定扩展名关联播放器的完整可执行文件路径
func GetPlayerExePathForExt(ext string) string {
	exeName := readDefaultViewerFromUserChoice(ext)
	if exeName == "" {
		exeName = readDefaultViewerFromClasses(ext)
	}
	if exeName == "" {
		return ""
	}

	// 从注册表命令中提取完整的 exe 路径
	fullPath := readFullCommandForExt(ext)
	if fullPath != "" {
		log.Printf("[VideoPlayer] 获取到 %s 播放器完整路径: %s", ext, fullPath)
		return fullPath
	}

	return ""
}

// readFullCommandForExt 读取文件扩展名关联程序的完整命令行（含exe路径）
func readFullCommandForExt(ext string) string {
	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	pathW, _ := syscall.UTF16PtrFromString(
		`Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\` + ext + `\UserChoice`)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000001), uintptr(unsafe.Pointer(pathW)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		pathW2, _ := syscall.UTF16PtrFromString(ext)
		ret2, _, _ := regOpenKey.Call(
			uintptr(0x80000000), uintptr(unsafe.Pointer(pathW2)),
			0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
		if ret2 != 0 {
			return ""
		}
		defer regCloseKey.Call(hKey)
		progId := readRegStringValue(hKey, "", regQueryValue)
		return readFullCommandForProgId(progId)
	}
	defer regCloseKey.Call(hKey)

	progId := readRegStringValue(hKey, "ProgId", regQueryValue)
	if progId == "" {
		return ""
	}
	return readFullCommandForProgId(progId)
}

// readFullCommandForProgId 从 ProgId 读取 shell\open\command 的完整值
func readFullCommandForProgId(progId string) string {
	if progId == "" {
		return ""
	}

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	roots := []struct {
		root uintptr
		path string
	}{
		{0x80000001, `Software\Classes\` + progId + `\shell\open\command`},
		{0x80000000, progId + `\shell\open\command`},
	}

	for _, r := range roots {
		pathW, _ := syscall.UTF16PtrFromString(r.path)
		var hKey uintptr
		ret, _, _ := regOpenKey.Call(r.root, uintptr(unsafe.Pointer(pathW)), 0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
		if ret != 0 {
			continue
		}
		defer regCloseKey.Call(hKey)
		return readRegStringValue(hKey, "", regQueryValue)
	}
	return ""
}
