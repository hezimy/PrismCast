//go:build windows

package player

import (
	"fmt"
	"log"
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procGetWindowThread  = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procAllowSetFGWin    = user32.NewProc("AllowSetForegroundWindow")
	procSendInput        = user32.NewProc("SendInput")
)

func captureBrowserProcess(hProcess syscall.Handle) uint32 {
	if hProcess == 0 {
		return 0
	}
	defer syscall.CloseHandle(hProcess)
	pid, _, _ := kernel32.NewProc("GetProcessId").Call(uintptr(hProcess))
	return uint32(pid)
}

const (
	inputKeyboard       = 1
	keyeventfKeyUp      = 0x0002
	vkControl           = 0x11
	vkUp               = 0x26
	vkDown             = 0x28
	asfwAny            = 0xFFFFFFFF
)

type keyboardInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type input struct {
	inputType uint32
	ki        keyboardInput
	padding   uint64
}

func adjustBrowserVolumeByKeypress(pid uint32, delta int) error {
	if pid == 0 || delta == 0 {
		return nil
	}
	up := delta > 0
	steps := delta
	if steps < 0 {
		steps = -steps
	}
	if steps > 100 {
		steps = 100
	}
	hwnd, err := findMainWindowForPID(pid)
	if err != nil {
		return err
	}
	for i := 0; i < steps; i++ {
		if err := sendCtrlArrow(hwnd, up); err != nil {
			return err
		}
	}
	return nil
}

func findMainWindowForPID(pid uint32) (syscall.Handle, error) {
	var found syscall.Handle
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lparam uintptr) uintptr {
		var winPID uint32
		procGetWindowThread.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&winPID)))
		if winPID != pid {
			return 1
		}
		visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
		if visible == 0 {
			return 1
		}
		found = hwnd
		return 0
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return 0, fmt.Errorf("未找到 PID %d 的可见窗口", pid)
	}
	return found, nil
}

func sendCtrlArrow(hwnd syscall.Handle, up bool) error {
	_, _, _ = procAllowSetFGWin.Call(asfwAny)
	if ok, _, _ := procSetForegroundWin.Call(uintptr(hwnd)); ok == 0 {
		log.Printf("[Volume] SetForegroundWindow 失败，仍尝试发送快捷键")
	}

	arrow := vkDown
	if up {
		arrow = vkUp
	}

	inputs := []input{
		{inputType: inputKeyboard, ki: keyboardInput{vk: vkControl}},
		{inputType: inputKeyboard, ki: keyboardInput{vk: uint16(arrow)}},
		{inputType: inputKeyboard, ki: keyboardInput{vk: uint16(arrow), flags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keyboardInput{vk: vkControl, flags: keyeventfKeyUp}},
	}
	ret, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(inputs[0])),
	)
	if ret != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput 失败: %w", err)
	}
	return nil
}

func terminateProcessByPID(pid uint32) {
	if pid == 0 {
		return
	}
	const processTerminate = 0x0001
	h, _, _ := kernel32.NewProc("OpenProcess").Call(processTerminate, 0, uintptr(pid))
	if h == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	kernel32.NewProc("TerminateProcess").Call(h, 1)
}
