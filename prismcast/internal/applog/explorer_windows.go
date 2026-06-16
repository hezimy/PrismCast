//go:build windows

package applog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32         = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

const swShowNormal = 1

// openWindowsExplorer 在资源管理器中打开目录；若日志文件已存在则选中该文件
func openWindowsExplorer(dir string) error {
	dir = filepath.Clean(dir)
	logFile := filepath.Join(dir, "prismcast.log")

	if _, err := os.Stat(logFile); err == nil {
		cmd := exec.Command("explorer.exe", "/select,"+logFile)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}

	if err := shellExecuteOpen(dir); err == nil {
		return nil
	}

	cmd := exec.Command("explorer.exe", dir)
	return cmd.Start()
}

func shellExecuteOpen(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ret, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		swShowNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW: %v (code %d)", callErr, ret)
	}
	return nil
}
