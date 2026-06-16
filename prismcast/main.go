package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"fyne.io/systray"

	"github.com/hezimy/PrismCast/internal/applog"
	"github.com/hezimy/PrismCast/internal/config"
	"github.com/hezimy/PrismCast/internal/dlna"
	"github.com/hezimy/PrismCast/internal/player"
	"github.com/hezimy/PrismCast/internal/singleinstance"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var iconBytes []byte

// App struct
type App struct {
	ctx           context.Context
	config        *config.Config
	dlnaSrv       *dlna.Server
	player        *player.MPVPlayer
	windowVisible bool
	windowMu      sync.RWMutex
	windowOpMu    sync.Mutex
	castEnabled   bool
	castMu        sync.RWMutex
	castToggling  bool
	mShowHide     *systray.MenuItem
	mCastToggle   *systray.MenuItem
	systrayOps    chan systrayOp
	closing       atomic.Bool
}

func NewApp() *App {
	return &App{
		windowVisible: false,
		castEnabled:   true,
		systrayOps:    make(chan systrayOp, 8),
	}
}

// startup 在WebView2就绪后被调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	if a.config == nil {
		a.config = config.Default()
	}

	a.player = player.NewMPVPlayer(a.config.PlayerPath, a.config.ImageViewerFirst)
	a.player.SetInitialVolume(a.config.Volume)
	applog.SetLevel(a.config.LogLevel)
	a.player.SetEventHandler(func(event string) {
		if a.dlnaSrv != nil {
			a.dlnaSrv.NotifyPlaybackEnded(event)
		}
	})

	a.dlnaSrv = dlna.NewServer(a.config.DeviceName, a.config.DeviceUUID, a.player)
	go func() {
		if err := a.dlnaSrv.Start(); err != nil {
			log.Printf("Failed to start DLNA server: %v", err)
			return
		}
		a.player.SetBrowserPlayBaseURL(a.dlnaSrv.GetLocalBrowserURL())
		// 网络就绪后再补发一轮 SSDP
		time.Sleep(2 * time.Second)
		a.dlnaSrv.RefreshDiscovery()
	}()
	a.castEnabled = true

	runtime.WindowHide(a.ctx)
	a.windowVisible = false
	go a.ensureWindowHidden()

	go a.runTray()

	singleinstance.SetActivateHandler(func() {
		log.Println("[SingleInstance] 收到二次启动，显示主窗口")
		go a.ShowWindow()
	})
	singleinstance.StartActivationListener()

	log.Println("PrismCast started successfully")
}

// ensureWindowHidden 通过Windows API兜底确保窗口隐藏
func (a *App) ensureWindowHidden() {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")
	const swHide = 0

	title, _ := syscall.UTF16PtrFromString("PrismCast")

	for i := 0; i < 5; i++ {
		time.Sleep(300 * time.Millisecond)

		a.windowMu.RLock()
		visible := a.windowVisible
		a.windowMu.RUnlock()
		if visible {
			return
		}

		hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd != 0 {
			showWindow.Call(hwnd, swHide)
			log.Printf("[Startup] 窗口已通过Windows API隐藏 (尝试第%d次)", i+1)
			return
		}
	}
	log.Println("[Startup] 未找到窗口句柄，依赖StartHidden")
}

// positionWindowBottomRight 将窗口定位到屏幕右下角
func (a *App) positionWindowBottomRight() {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	getWindowRect := user32.NewProc("GetWindowRect")
	setWindowPos := user32.NewProc("SetWindowPos")
	systemParametersInfo := user32.NewProc("SystemParametersInfoW")

	title, _ := syscall.UTF16PtrFromString("PrismCast")
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}

	type RECT struct{ Left, Top, Right, Bottom int32 }
	var workArea RECT
	const SPI_GETWORKAREA = 0x0030
	systemParametersInfo.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&workArea)), 0)

	var winRect RECT
	getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&winRect)))
	winW := winRect.Right - winRect.Left
	winH := winRect.Bottom - winRect.Top

	const margin = 30
	x := workArea.Right - winW - margin
	y := workArea.Bottom - winH - margin

	if x < workArea.Left {
		x = workArea.Left + margin
	}
	if y < workArea.Top {
		y = workArea.Top + margin
	}

	const SWP_NOZORDER = 0x0004
	const SWP_NOSIZE = 0x0001
	setWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, SWP_NOZORDER|SWP_NOSIZE)
}

// runTray 设置系统托盘菜单
func (a *App) runTray() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Systray] runTray panic: %v", r)
		}
	}()
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	systray.Run(func() {
		systray.SetIcon(iconBytes)
		systray.SetTooltip("PrismCast - DLNA 投屏接收端")

		a.mShowHide = systray.AddMenuItem("显示主窗口", "切换窗口显隐")
		systray.AddSeparator()
		mDevice := systray.AddMenuItem(fmt.Sprintf("设备: %s", a.config.DeviceName), "当前设备信息")
		mDevice.Disable()
		systray.AddSeparator()
		a.mCastToggle = systray.AddMenuItem("投屏服务已启动", "点击停止/启动DLNA投屏服务")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出 PrismCast", "退出程序")

		a.applyCastMenuTitleLocked()
		a.applyShowHideTitleLocked()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Systray] select loop panic: %v", r)
				}
			}()
			for {
				select {
				case op, ok := <-a.systrayOps:
					if !ok {
						return
					}
					a.processSystrayOp(op)
				case <-a.mShowHide.ClickedCh:
					go a.toggleWindow()
				case <-a.mCastToggle.ClickedCh:
					go func() {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("[Systray] toggleCastService panic: %v", r)
							}
						}()
						a.toggleCastService()
						select {
						case a.systrayOps <- systrayOp{kind: "cast"}:
						default:
						}
					}()
				case <-mQuit.ClickedCh:
					a.quitApplication()
					return
				}
			}
		}()
	}, func() {
		log.Println("Systray exited cleanly")
	})
}

type systrayOp struct {
	kind string
}

func (a *App) processSystrayOp(op systrayOp) {
	switch op.kind {
	case "showhide":
		a.applyShowHideTitleLocked()
	case "cast":
		a.applyCastMenuTitleLocked()
	}
}

func (a *App) applyShowHideTitleLocked() {
	if a.closing.Load() || a.mShowHide == nil {
		return
	}
	a.windowMu.RLock()
	visible := a.windowVisible
	a.windowMu.RUnlock()
	if visible {
		a.mShowHide.SetTitle("隐藏主窗口")
	} else {
		a.mShowHide.SetTitle("显示主窗口")
	}
}

func (a *App) applyCastMenuTitleLocked() {
	if a.closing.Load() || a.mCastToggle == nil {
		return
	}
	a.castMu.RLock()
	enabled := a.castEnabled
	a.castMu.RUnlock()
	if enabled {
		a.mCastToggle.SetTitle("✓ 投屏服务已启动")
	} else {
		a.mCastToggle.SetTitle("✗ 投屏服务已停止")
	}
}

func (a *App) toggleCastService() {
	a.castMu.Lock()
	if a.castToggling {
		a.castMu.Unlock()
		return
	}
	a.castToggling = true
	shouldStart := !a.castEnabled
	a.castMu.Unlock()

	defer func() {
		a.castMu.Lock()
		a.castToggling = false
		a.castMu.Unlock()
	}()

	if shouldStart {
		if a.dlnaSrv != nil {
			if err := a.dlnaSrv.Start(); err != nil {
				log.Printf("[DLNA] 启动失败: %v", err)
				return
			}
			// 服务重启后再补一轮发现广播
			go func() {
				time.Sleep(1500 * time.Millisecond)
				if a.dlnaSrv != nil {
					a.dlnaSrv.RefreshDiscovery()
				}
			}()
		}
		a.player = player.NewMPVPlayer(a.config.PlayerPath, a.config.ImageViewerFirst)
		a.player.SetInitialVolume(a.config.Volume)
		a.player.SetEventHandler(func(event string) {
			if a.dlnaSrv != nil {
				a.dlnaSrv.NotifyPlaybackEnded(event)
			}
		})
		if a.dlnaSrv != nil {
			a.dlnaSrv.SetPlayer(a.player)
			a.player.SetBrowserPlayBaseURL(a.dlnaSrv.GetLocalBrowserURL())
		}
		a.castMu.Lock()
		a.castEnabled = true
		a.castMu.Unlock()
		log.Println("[DLNA] 投屏服务已启动")
	} else {
		if a.dlnaSrv != nil {
			a.dlnaSrv.Stop()
		}
		if a.player != nil {
			a.player.Cleanup()
		}
		a.castMu.Lock()
		a.castEnabled = false
		a.castMu.Unlock()
		log.Println("[DLNA] 投屏服务已停止")
	}
}

func (a *App) toggleWindow() {
	a.windowOpMu.Lock()
	defer a.windowOpMu.Unlock()

	a.windowMu.RLock()
	visible := a.windowVisible
	a.windowMu.RUnlock()

	if visible {
		runtime.WindowHide(a.ctx)
		a.setWindowState(false)
	} else {
		a.positionWindowBottomRight()
		runtime.WindowShow(a.ctx)
		a.setWindowState(true)
	}
}

func (a *App) setWindowState(visible bool) {
	if a.closing.Load() {
		return
	}
	a.windowMu.Lock()
	a.windowVisible = visible
	a.windowMu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "window-visibility", visible)
	}
	select {
	case a.systrayOps <- systrayOp{kind: "showhide"}:
	default:
	}
}

func (a *App) IsWindowVisible() bool {
	a.windowMu.RLock()
	defer a.windowMu.RUnlock()
	return a.windowVisible
}

func (a *App) quitApplication() {
	log.Println("=== 退出 PrismCast ===")

	a.closing.Store(true)
	time.Sleep(100 * time.Millisecond)

	if a.dlnaSrv != nil {
		a.dlnaSrv.Stop()
	}
	if a.player != nil {
		a.player.Cleanup()
	}

	go func() {
		time.Sleep(3 * time.Second)
		log.Println("[Exit] 超时强制退出")
		os.Exit(0)
	}()

	systray.Quit()
	runtime.Quit(a.ctx)
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}

func (a *App) shutdown(ctx context.Context) {
	log.Println("Wails shutdown hook triggered")
	if a.dlnaSrv != nil {
		a.dlnaSrv.Stop()
	}
	if a.player != nil {
		a.player.Cleanup()
	}
}

// ========== 对外绑定方法 ==========

func (a *App) GetDeviceInfo() map[string]interface{} {
	status := "stopped"
	playerStatus := "idle"
	if a.dlnaSrv != nil {
		status = a.dlnaSrv.Status()
	}
	if a.player != nil {
		playerStatus = a.player.Status()
	}

	castMediaInfo := map[string]interface{}{}
	if a.dlnaSrv != nil {
		cm := a.dlnaSrv.GetCastMedia()
		if cm != nil {
			castMediaInfo = map[string]interface{}{
				"uri": cm.URI, "title": cm.Title,
				"mediaType": cm.MediaType, "state": cm.State,
			}
		}
	}

	a.castMu.RLock()
	enabled := a.castEnabled
	a.castMu.RUnlock()

	return map[string]interface{}{
		"name": a.config.DeviceName, "uuid": a.config.DeviceUUID,
		"version": "1.0.0", "status": status, "player": playerStatus,
		"castEnabled": enabled, "castMedia": castMediaInfo,
	}
}

func (a *App) GetSettings() *config.Config { return a.config }

func (a *App) SaveSettings(cfg *config.Config) error {
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	a.config = cfg
	a.applyConfigLive(cfg)
	return nil
}

func (a *App) applyConfigLive(cfg *config.Config) {
	if a.player != nil {
		a.player.SetInitialVolume(cfg.Volume)
		a.player.SetImageViewerFirst(cfg.ImageViewerFirst)
		a.player.SetPlayerPath(cfg.PlayerPath)
	}
	if a.dlnaSrv != nil {
		a.dlnaSrv.SetDeviceName(cfg.DeviceName)
		a.dlnaSrv.RefreshDiscovery()
	}
	applog.SetLevel(cfg.LogLevel)
	if err := config.SetAutoStart(cfg.AutoStart, ""); err != nil {
		applog.Mainf("[Config] 设置开机自启动失败: %v", err)
	}
	applog.Mainf("[Config] 设置已应用（无需重启）")
}

func (a *App) GetLogPath() string {
	return applog.LogFilePath()
}

func (a *App) OpenLogFolder() error {
	return applog.OpenFolderInExplorer()
}

func (a *App) GetPlaybackStatus() map[string]interface{} { return a.player.GetStatus() }

func (a *App) ControlPlayer(command string) error {
	switch command {
	case "play":
		return a.player.Play()
	case "pause":
		return a.player.Pause()
	case "stop":
		return a.player.Stop()
	default:
		return fmt.Errorf("未知命令: %s", command)
	}
}

func (a *App) SetVolume(volume int) error { return a.player.SetVolume(volume) }

func (a *App) ToggleCastService() bool {
	a.toggleCastService()
	a.castMu.RLock()
	defer a.castMu.RUnlock()
	return a.castEnabled
}
func (a *App) ShowWindow() {
	a.windowOpMu.Lock()
	defer a.windowOpMu.Unlock()
	a.positionWindowBottomRight()
	runtime.WindowShow(a.ctx)
	a.setWindowState(true)
}
func (a *App) HideWindow() {
	a.windowOpMu.Lock()
	defer a.windowOpMu.Unlock()
	runtime.WindowHide(a.ctx)
	a.setWindowState(false)
}

// SetWindowTheme 动态切换 Windows 标题栏深浅色
func (a *App) SetWindowTheme(dark bool) {
	user32 := syscall.NewLazyDLL("user32.dll")
	dwmapi := syscall.NewLazyDLL("dwmapi.dll")
	findWindow := user32.NewProc("FindWindowW")
	dwmSetWindowAttribute := dwmapi.NewProc("DwmSetWindowAttribute")

	const DWMWA_USE_IMMERSIVE_DARK_MODE = 20

	title, _ := syscall.UTF16PtrFromString("PrismCast")
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}

	var darkMode int32 = 0
	if dark {
		darkMode = 1
	}

	ret, _, err := dwmSetWindowAttribute.Call(
		hwnd,
		DWMWA_USE_IMMERSIVE_DARK_MODE,
		uintptr(unsafe.Pointer(&darkMode)),
		uintptr(4),
	)

	if ret != 0 {
		log.Printf("[Theme] DwmSetWindowAttribute 失败 (HRESULT=0x%x): %v", ret, err)
	} else {
		log.Printf("[Theme] 标题栏主题已切换: dark=%v", dark)
	}
}

// RestartApp 重启程序
func (a *App) RestartApp() {
	log.Println("[Restart] 正在重启 PrismCast...")

	a.closing.Store(true)

	exePath, err := os.Executable()
	if err != nil {
		log.Printf("[Restart] 获取可执行文件路径失败: %v", err)
		return
	}

	if a.dlnaSrv != nil {
		a.dlnaSrv.Stop()
	}
	if a.player != nil {
		a.player.Cleanup()
	}

	singleinstance.Release()

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteW := shell32.NewProc("ShellExecuteW")

	hwnd := uintptr(0)
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(exePath)

	ret, _, _ := shellExecuteW.Call(
		hwnd,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		1,
	)
	if ret <= 32 {
		log.Printf("[Restart] ShellExecuteW 失败 (ret=%d)，尝试 CreateProcess", ret)
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		createProcess := kernel32.NewProc("CreateProcessW")
		exePathW, _ := syscall.UTF16PtrFromString(exePath)
		var si syscall.StartupInfo
		si.Cb = uint32(unsafe.Sizeof(si))
		var pi syscall.ProcessInformation
		createProcess.Call(
			uintptr(0),
			uintptr(unsafe.Pointer(exePathW)),
			uintptr(0), uintptr(0),
			uintptr(0),
			uintptr(0x00000008),
			uintptr(0), uintptr(0),
			uintptr(unsafe.Pointer(&si)),
			uintptr(unsafe.Pointer(&pi)))
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		log.Println("[Restart] 旧进程退出")
		systray.Quit()
		runtime.Quit(a.ctx)
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
}

// ========== 主入口 ==========

func main() {
	// 单实例检测必须最先执行，避免重复初始化 WebView2 / 托盘 / DLNA
	alreadyRunning, err := singleinstance.TryAcquire()
	if err != nil {
		log.Printf("[SingleInstance] 检测失败: %v", err)
	}
	if alreadyRunning {
		activated, actErr := singleinstance.RequestActivate()
		if actErr != nil {
			log.Printf("[SingleInstance] 激活已有实例失败: %v", actErr)
		}
		if !activated {
			user32 := syscall.NewLazyDLL("user32.dll")
			msgBoxW := user32.NewProc("MessageBoxW")
			title, _ := syscall.UTF16PtrFromString("PrismCast")
			msg, _ := syscall.UTF16PtrFromString("PrismCast 已在运行。\n\n请查看系统托盘区域的 PrismCast 图标。\n若未看到窗口，请右键托盘图标选择「显示主窗口」。")
			msgBoxW.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x40)
		}
		os.Exit(0)
	}

	app := NewApp()

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	app.config = cfg

	logFile, err := applog.Setup(cfg.LogLevel)
	if err != nil {
		log.Printf("[WARN] 无法创建日志文件: %v", err)
	} else if logFile != nil {
		defer logFile.Close()
	}
	applog.Println("=== PrismCast 启动 ===")
	applog.Println("[SingleInstance] 当前为唯一实例")

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	wv2Dir := filepath.Join(localAppData, "PrismCast", "WebView2")
	_ = os.MkdirAll(wv2Dir, 0o755)
	os.Setenv("WEBVIEW2_USER_DATA_DIR", wv2Dir)

	winTheme := windows.Light
	bgColor := &options.RGBA{R: 245, G: 243, B: 250, A: 255}
	if cfg.Theme == "dark" {
		winTheme = windows.Dark
		bgColor = &options.RGBA{R: 30, G: 16, B: 53, A: 255}
	}

	err = wails.Run(&options.App{
		Title:            "PrismCast",
		Width:            630,
		Height:           460,
		MinWidth:         530,
		MinHeight:        400,
		DisableResize:    false,
		Fullscreen:       false,
		Frameless:        true,
		StartHidden:      true,
		BackgroundColour: bgColor,
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			go func() {
				app.windowOpMu.Lock()
				defer app.windowOpMu.Unlock()
				runtime.WindowHide(ctx)
				app.setWindowState(false)
			}()
			return true
		},
		Bind: []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.Auto,
			DisableWindowIcon:    false,
			Theme:                winTheme,
		},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: false, HideTitle: false,
				HideTitleBar: false, FullSizeContent: false,
				UseToolbar: false, HideToolbarSeparator: true,
			},
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false, WindowIsTranslucent: false,
		},
		Linux: &linux.Options{Icon: nil, WindowIsTranslucent: false},
	})

	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
