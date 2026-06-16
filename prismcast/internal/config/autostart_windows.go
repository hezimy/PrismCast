//go:build windows

package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// SetAutoStart 设置或取消 Windows 开机自启动
func SetAutoStart(enable bool, exePath string) error {
	if enable {
		return setAutoStartRegistry(exePath)
	}
	return removeAutoStartRegistry()
}

// IsAutoStartEnabled 检查当前是否已设置开机自启动
func IsAutoStartEnabled() (bool, error) {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	const appName = "PrismCast"

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regQueryValue := advapi32.NewProc("RegQueryValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_READ = 0x20019

	pathW, _ := syscall.UTF16PtrFromString(keyPath)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000001),
		uintptr(unsafe.Pointer(pathW)),
		0, KEY_READ, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return false, fmt.Errorf("无法打开注册表键")
	}
	defer regCloseKey.Call(hKey)

	nameW, _ := syscall.UTF16PtrFromString(appName)
	var dataType uint32
	var bufLen uint32 = 512
	buf := make([]uint16, 256)

	ret, _, _ = regQueryValue.Call(
		hKey,
		uintptr(unsafe.Pointer(nameW)),
		0,
		uintptr(unsafe.Pointer(&dataType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)))

	if ret != 0 || bufLen == 0 {
		return false, nil
	}

	n := bufLen / 2
	if n > 0 && buf[n-1] == 0 {
		n--
	}
	value := syscall.UTF16ToString(buf[:n])
	return value != "", nil
}

// setAutoStartRegistry 在注册表中添加启动项
func setAutoStartRegistry(exePath string) error {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	const appName = "PrismCast"

	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("获取可执行文件路径失败: %w", err)
		}
	}

	value := fmt.Sprintf(`"%s"`, filepath.Clean(exePath))

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regCreateKey := advapi32.NewProc("RegCreateKeyExW")
	regSetValue := advapi32.NewProc("RegSetValueExW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_WRITE = 0x20006

	pathW, _ := syscall.UTF16PtrFromString(keyPath)
	var hKey uintptr
	var disp uintptr
	ret, _, _ := regCreateKey.Call(
		uintptr(0x80000001),
		uintptr(unsafe.Pointer(pathW)),
		0,
		0,
		0,
		KEY_WRITE,
		0,
		uintptr(unsafe.Pointer(&hKey)),
		uintptr(unsafe.Pointer(&disp)))
	if ret != 0 {
		return fmt.Errorf("无法创建/打开注册表键 (error=%d)", ret)
	}
	defer regCloseKey.Call(hKey)

	nameW, _ := syscall.UTF16PtrFromString(appName)
	valueW, _ := syscall.UTF16PtrFromString(value)

	ret, _, _ = regSetValue.Call(
		hKey,
		uintptr(unsafe.Pointer(nameW)),
		0,
		1,
		uintptr(unsafe.Pointer(valueW)),
		uintptr((len(value)+1)*2))

	if ret != 0 {
		return fmt.Errorf("写入注册表值失败 (error=%d)", ret)
	}

	log.Printf("[AutoStart] 已启用开机自启动: %s", value)
	return nil
}

// removeAutoStartRegistry 从注册表中移除启动项
func removeAutoStartRegistry() error {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	const appName = "PrismCast"

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	regOpenKey := advapi32.NewProc("RegOpenKeyExW")
	regDeleteValue := advapi32.NewProc("RegDeleteValueW")
	regCloseKey := advapi32.NewProc("RegCloseKey")

	const KEY_WRITE = 0x20006

	pathW, _ := syscall.UTF16PtrFromString(keyPath)
	var hKey uintptr
	ret, _, _ := regOpenKey.Call(
		uintptr(0x80000001),
		uintptr(unsafe.Pointer(pathW)),
		0, KEY_WRITE, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return fmt.Errorf("无法打开注册表键 (error=%d)", ret)
	}
	defer regCloseKey.Call(hKey)

	nameW, _ := syscall.UTF16PtrFromString(appName)
	ret, _, _ = regDeleteValue.Call(hKey, uintptr(unsafe.Pointer(nameW)))
	if ret != 0 && ret != 2 {
		return fmt.Errorf("删除注册表值失败 (error=%d)", ret)
	}

	log.Println("[AutoStart] 已禁用开机自启动")
	return nil
}
