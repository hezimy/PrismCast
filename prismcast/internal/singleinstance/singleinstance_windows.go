//go:build windows

package singleinstance

import (
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mutexName   = `Global\PrismCast_Instance_v1`
	eventName   = `Global\PrismCast_Activate_v1`
	lockAddr    = "127.0.0.1:38765"
	windowTitle = "PrismCast"
)

var (
	mutexHandle windows.Handle
	eventHandle windows.Handle
	tcpListener net.Listener

	activateMu sync.Mutex
	onActivate func()

	listenerOnce sync.Once
)

const (
	eventModifyState = 0x0002
	waitObject0      = 0x00000000
	swRestore        = 9
	swShow           = 5
)

// TryAcquire 尝试成为唯一实例。已有时返回 alreadyRunning=true。
func TryAcquire() (alreadyRunning bool, err error) {
	if running, err := probeExistingMutex(); err != nil {
		log.Printf("[SingleInstance] OpenMutex 探测失败: %v", err)
	} else if running {
		log.Println("[SingleInstance] 检测到已有实例 (OpenMutex)")
		return true, nil
	}

	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}

	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
			log.Println("[SingleInstance] 检测到已有实例 (CreateMutex)")
			return true, nil
		}
		return false, fmt.Errorf("CreateMutex: %w", err)
	}
	mutexHandle = handle

	ln, err := net.Listen("tcp", lockAddr)
	if err != nil {
		_ = windows.CloseHandle(mutexHandle)
		mutexHandle = 0
		log.Printf("[SingleInstance] 检测到已有实例 (TCP %s): %v", lockAddr, err)
		return true, nil
	}
	tcpListener = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			fireActivate()
		}
	}()

	log.Printf("[SingleInstance] 单实例锁已获取 (pid=%d)", windows.GetCurrentProcessId())
	return false, nil
}

func probeExistingMutex() (bool, error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND {
			return false, nil
		}
		return false, err
	}
	_ = windows.CloseHandle(handle)
	return true, nil
}

// Release 释放单实例锁（重启前必须调用）
func Release() {
	if tcpListener != nil {
		_ = tcpListener.Close()
		tcpListener = nil
	}
	if mutexHandle != 0 {
		_ = windows.CloseHandle(mutexHandle)
		mutexHandle = 0
	}
	if eventHandle != 0 {
		_ = windows.CloseHandle(eventHandle)
		eventHandle = 0
	}
}

func SetActivateHandler(fn func()) {
	activateMu.Lock()
	onActivate = fn
	activateMu.Unlock()
}

func fireActivate() {
	activateMu.Lock()
	fn := onActivate
	activateMu.Unlock()
	if fn != nil {
		fn()
	} else {
		fallbackShowWindow()
	}
}

// StartActivationListener 主实例监听二次启动的激活信号
func StartActivationListener() {
	listenerOnce.Do(func() {
		name, err := windows.UTF16PtrFromString(eventName)
		if err != nil {
			log.Printf("[SingleInstance] 创建激活事件失败: %v", err)
			return
		}

		handle, err := windows.CreateEvent(nil, 1, 0, name)
		if err != nil {
			if err == windows.ERROR_ALREADY_EXISTS {
				handle, err = windows.OpenEvent(eventModifyState, false, name)
			}
		}
		if err != nil || handle == 0 {
			log.Printf("[SingleInstance] CreateEvent 失败: %v", err)
			return
		}
		eventHandle = handle

		go func() {
			for {
				ret, err := windows.WaitForSingleObject(eventHandle, windows.INFINITE)
				if err != nil || ret != waitObject0 {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				_ = windows.ResetEvent(eventHandle)
				fireActivate()
			}
		}()
	})
}

// RequestActivate 二次启动时通知已在运行的实例显示窗口
func RequestActivate() (activated bool, err error) {
	// 优先走 TCP，主实例 Accept 后会 fireActivate
	conn, err := net.DialTimeout("tcp", lockAddr, 2*time.Second)
	if err == nil {
		_, _ = conn.Write([]byte("activate"))
		_ = conn.Close()
		time.Sleep(200 * time.Millisecond)
		return true, nil
	}

	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return fallbackShowWindow(), nil
	}

	var handle windows.Handle
	for i := 0; i < 30; i++ {
		handle, err = windows.OpenEvent(eventModifyState, false, name)
		if handle != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if handle != 0 {
		_ = windows.SetEvent(handle)
		_ = windows.CloseHandle(handle)
		time.Sleep(200 * time.Millisecond)
		return true, nil
	}

	return fallbackShowWindow(), nil
}

func fallbackShowWindow() bool {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")
	setForeground := user32.NewProc("SetForegroundWindow")

	title, err := windows.UTF16PtrFromString(windowTitle)
	if err != nil {
		return false
	}
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return false
	}
	showWindow.Call(hwnd, swRestore)
	showWindow.Call(hwnd, swShow)
	setForeground.Call(hwnd)
	return true
}
