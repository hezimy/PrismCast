package applog

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// 日志文件统一 UTF-8；新建文件写入 BOM，便于 Windows 记事本正确识别编码。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type Level int32

const (
	LevelOff Level = iota
	LevelMain
	LevelVerbose
)

var (
	level atomic.Int32
	mu    sync.Mutex
	file  *os.File
)

func parseLevel(s string) Level {
	switch s {
	case "off", "none", "disabled":
		return LevelOff
	case "verbose", "detail", "detailed":
		return LevelVerbose
	default:
		return LevelMain
	}
}

func LevelName() string {
	switch Level(level.Load()) {
	case LevelOff:
		return "off"
	case LevelVerbose:
		return "verbose"
	default:
		return "main"
	}
}

// NormalizeLevel 校验并迁移旧配置
func NormalizeLevel(levelStr string, legacyVerbose bool) string {
	if levelStr != "" {
		switch levelStr {
		case "off", "main", "verbose":
			return levelStr
		}
	}
	if legacyVerbose {
		return "verbose"
	}
	if levelStr == "" {
		return "main"
	}
	return "main"
}

// Setup 按档位初始化日志输出
func Setup(levelStr string) (*os.File, error) {
	SetLevel(levelStr)
	mu.Lock()
	defer mu.Unlock()
	return file, nil
}

// SetLevel 运行时切换档位（保存设置后即时生效）
func SetLevel(levelStr string) {
	prev := Level(level.Load())
	next := parseLevel(levelStr)
	level.Store(int32(next))

	mu.Lock()
	defer mu.Unlock()

	switch next {
	case LevelOff:
		log.SetOutput(io.Discard)
		if file != nil {
			_ = file.Close()
			file = nil
		}
	case LevelMain, LevelVerbose:
		if file == nil {
			f, err := openLogFileLocked()
			if err != nil {
				log.SetOutput(io.Discard)
				return
			}
			file = f
		} else {
			log.SetOutput(&utf8LogWriter{file: file})
			log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		}
	}

	if prev != next {
		msg := map[Level]string{
			LevelOff:     "[Log] 日志已关闭",
			LevelMain:    "[Log] 日志档位：主要",
			LevelVerbose: "[Log] 日志档位：详细",
		}
		if next != LevelOff {
			log.Println(msg[next])
		}
	}
}

func openLogFile() (*os.File, error) {
	mu.Lock()
	defer mu.Unlock()
	return openLogFileLocked()
}

func openLogFileLocked() (*os.File, error) {
	dir := LogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := LogFilePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	if err := ensureUTF8BOM(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	out := &utf8LogWriter{file: f}
	log.SetOutput(out)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return f, nil
}

// ensureUTF8BOM 仅在空文件开头写入 UTF-8 BOM（删除后重建、首次创建）
func ensureUTF8BOM(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 0 {
		return nil
	}
	_, err = f.Write(utf8BOM)
	return err
}

// utf8LogWriter 保证写入内容为 UTF-8 文本（Go 字符串本身为 UTF-8）
type utf8LogWriter struct {
	file *os.File
}

func (w *utf8LogWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

// LogDir 返回日志目录（Windows: %TEMP%\PrismCast）
func LogDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), "PrismCast")
	}
	return filepath.Join(os.TempDir(), "prismcast")
}

// LogFilePath 返回主日志文件路径
func LogFilePath() string {
	return filepath.Join(LogDir(), "prismcast.log")
}

// Mainf 主要日志：投屏、启停、错误（主要/详细档位写入）
func Mainf(format string, args ...interface{}) {
	if Level(level.Load()) >= LevelMain {
		log.Printf(format, args...)
	}
}

// Verbosef 详细日志：HTTP/SOAP/SSDP 等
func Verbosef(format string, args ...interface{}) {
	if Level(level.Load()) >= LevelVerbose {
		log.Printf(format, args...)
	}
}

// Println 兼容旧调用，等同主要日志
func Println(v ...interface{}) {
	if Level(level.Load()) >= LevelMain {
		log.Println(v...)
	}
}

// Printf 兼容旧调用，等同主要日志
func Printf(format string, args ...interface{}) {
	Mainf(format, args...)
}

// Errorf 错误信息，主要档位及以上
func Errorf(format string, args ...interface{}) {
	Mainf(format, args...)
}

// OpenFolderInExplorer 在资源管理器中打开日志目录
func OpenFolderInExplorer() error {
	dir := LogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return openWindowsExplorer(dir)
	case "darwin":
		return execOpen("open", dir)
	default:
		return execOpen("xdg-open", dir)
	}
}

func execOpen(cmd, path string) error {
	p, err := os.StartProcess(cmd, []string{cmd, path}, &os.ProcAttr{})
	if err != nil {
		return err
	}
	_ = p.Release()
	return nil
}

// Legacy helpers
func SetVerbose(on bool) {
	if on {
		SetLevel("verbose")
	} else {
		SetLevel("main")
	}
}

func Verbose() bool {
	return Level(level.Load()) >= LevelVerbose
}
