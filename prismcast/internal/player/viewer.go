//go:build windows

package player

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// readRegStringValue 从已打开的注册表键中读取字符串值
func readRegStringValue(hKey uintptr, name string, regQueryValue *syscall.LazyProc) string {
	nameW, _ := syscall.UTF16PtrFromString(name)
	var bufSize uint32
	var valType uint32

	ret, _, _ := regQueryValue.Call(
		hKey,
		uintptr(unsafe.Pointer(nameW)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)))
	if ret != 0 || bufSize == 0 {
		return ""
	}

	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = regQueryValue.Call(
		hKey,
		uintptr(unsafe.Pointer(nameW)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)))
	if ret != 0 {
		return ""
	}

	return syscall.UTF16ToString(buf)
}

// readDefaultViewerFromUserChoice 从 FileExts UserChoice 读取默认程序 exe 名
func readDefaultViewerFromUserChoice(ext string) string {
	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	pathW, _ := syscall.UTF16PtrFromString(
		`Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\` + ext + `\UserChoice`)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000001),
		uintptr(unsafe.Pointer(pathW)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return ""
	}
	defer regCloseKey.Call(hKey)

	progId := readRegStringValue(hKey, "ProgId", regQueryValue)
	if progId == "" {
		return ""
	}

	return readExeNameFromProgId(progId)
}

// readDefaultViewerFromClasses 从 HKEY_CLASSES_ROOT 的扩展名键读取默认程序
func readDefaultViewerFromClasses(ext string) string {
	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	pathW, _ := syscall.UTF16PtrFromString(ext)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000000),
		uintptr(unsafe.Pointer(pathW)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return ""
	}
	defer regCloseKey.Call(hKey)

	progId := readRegStringValue(hKey, "", regQueryValue)
	if progId == "" {
		return ""
	}

	return readExeNameFromProgId(progId)
}

// readExeNameFromProgId 从 ProgId 的 shell\open\command 提取 exe 文件名
func readExeNameFromProgId(progId string) string {
	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	pathW, _ := syscall.UTF16PtrFromString(`Software\Classes\` + progId + `\shell\open\command`)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000001),
		uintptr(unsafe.Pointer(pathW)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret == 0 {
		defer regCloseKey.Call(hKey)
		cmd := readRegStringValue(hKey, "", regQueryValue)
		return extractExeNameFromCmd(cmd)
	}

	pathW2, _ := syscall.UTF16PtrFromString(progId + `\shell\open\command`)
	ret2, _, _ := regOpenKey.Call(
		uintptr(0x80000000),
		uintptr(unsafe.Pointer(pathW2)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret2 == 0 {
		defer regCloseKey.Call(hKey)
		cmd := readRegStringValue(hKey, "", regQueryValue)
		return extractExeNameFromCmd(cmd)
	}

	return ""
}

// extractExeNameFromCmd 从命令行字符串中提取 exe 文件名
func extractExeNameFromCmd(cmd string) string {
	cmd = cmdTrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if cmd[0] == '"' {
		end := findQuoteEnd(cmd, 1)
		if end > 0 {
			return filepath.Base(cmd[1:end])
		}
	}
	fields := strings.Fields(cmd)
	if len(fields) > 0 {
		return filepath.Base(fields[0])
	}
	return ""
}

func cmdTrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// findQuoteEnd 查找从 start 开始的下一个未转义引号位置
func findQuoteEnd(s string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '"' {
			return i
		}
	}
	return -1
}
