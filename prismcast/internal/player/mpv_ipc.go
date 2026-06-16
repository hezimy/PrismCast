package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hezimy/PrismCast/internal/applog"
)

type mpvIPCClient struct {
	pipePath string
	mu       sync.Mutex
}

func newMPVIPCClient(pipePath string) *mpvIPCClient {
	return &mpvIPCClient{pipePath: pipePath}
}

func (c *mpvIPCClient) dial(timeout time.Duration) (io.ReadWriteCloser, error) {
	if runtime.GOOS == "windows" {
		return dialWindowsPipe(c.pipePath, timeout)
	}
	return dialUnixSocket(c.pipePath, timeout)
}

func dialUnixSocket(path string, timeout time.Duration) (io.ReadWriteCloser, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("无法连接 mpv IPC socket: %s", path)
}

func dialWindowsPipe(path string, timeout time.Duration) (io.ReadWriteCloser, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("无法连接 mpv IPC pipe: %s", path)
}

type mpvResponse struct {
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	RequestID int             `json:"request_id"`
}

func (c *mpvIPCClient) command(args ...interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := c.dial(3 * time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	payload := map[string]interface{}{
		"command": args,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')

	if _, err := conn.Write(body); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var resp mpvResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" && resp.Error != "success" {
		return nil, fmt.Errorf("mpv IPC error: %s", resp.Error)
	}
	return resp.Data, nil
}

func (c *mpvIPCClient) setProperty(name string, value interface{}) error {
	_, err := c.command("set_property", name, value)
	return err
}

func (c *mpvIPCClient) getPropertyFloat(name string) (float64, error) {
	data, err := c.command("get_property", name)
	if err != nil {
		return 0, err
	}
	if string(data) == "null" {
		return 0, nil
	}
	var val float64
	if err := json.Unmarshal(data, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func (c *mpvIPCClient) getPropertyBool(name string) (bool, error) {
	data, err := c.command("get_property", name)
	if err != nil {
		return false, err
	}
	var val bool
	if err := json.Unmarshal(data, &val); err != nil {
		return false, err
	}
	return val, nil
}

func (c *mpvIPCClient) play() error {
	return c.setProperty("pause", false)
}

func (c *mpvIPCClient) pause() error {
	return c.setProperty("pause", true)
}

func (c *mpvIPCClient) seek(seconds float64) error {
	_, err := c.command("seek", seconds, "absolute")
	return err
}

func (c *mpvIPCClient) setVolume(volume float64) error {
	return c.setProperty("volume", volume)
}

func (c *mpvIPCClient) keypress(key string) error {
	_, err := c.command("keypress", key)
	return err
}

func adjustMPVVolumeByKeypress(ipc *mpvIPCClient, delta int) error {
	if ipc == nil || delta == 0 {
		return nil
	}
	// mpv 默认：0 增大音量，9 减小音量（走 JSON IPC，无需窗口在前台）
	key := "9"
	steps := -delta
	if delta > 0 {
		key = "0"
		steps = delta
	}
	if steps > 100 {
		steps = 100
	}
	for i := 0; i < steps; i++ {
		if err := ipc.keypress(key); err != nil {
			return err
		}
	}
	return nil
}

func clampDLNAVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func (c *mpvIPCClient) setMute(mute bool) error {
	return c.setProperty("mute", mute)
}

func ipcPipePathForPID(pid int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\prismcast_mpv_%d`, pid)
	}
	return fmt.Sprintf("%s/prismcast_mpv_%d.sock", os.TempDir(), pid)
}

func newIPCPath() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\prismcast_mpv_%d`, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s/prismcast_mpv_%d.sock", os.TempDir(), time.Now().UnixNano())
}

func (p *MPVPlayer) startIPCMonitor() {
	p.mu.RLock()
	ipc := p.ipc
	done := p.ipcDone
	p.mu.RUnlock()
	if ipc == nil || done == nil {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			p.pollMPVStatus(ipc)
		}
	}
}

func (p *MPVPlayer) pollMPVStatus(ipc *mpvIPCClient) {
	pos, errPos := ipc.getPropertyFloat("time-pos")
	dur, errDur := ipc.getPropertyFloat("duration")
	paused, errPause := ipc.getPropertyBool("pause")
	eof, errEOF := ipc.getPropertyBool("eof-reached")

	if errPos != nil && errDur != nil && errPause != nil {
		return
	}

	notifyStopped := false
	p.mu.Lock()
	if errPos == nil {
		p.status.Position = pos
	}
	if errDur == nil && dur > 0 {
		p.status.Duration = dur
	}
	if errPause == nil {
		if paused {
			p.status.State = "paused"
		} else if p.status.State != "stopped" && p.status.State != "idle" {
			p.status.State = "playing"
		}
	}
	if errEOF == nil && eof && p.status.State != "stopped" && p.status.State != "idle" {
		p.status.State = "stopped"
		p.status.URI = ""
		p.status.Position = 0
		notifyStopped = true
	}
	p.mu.Unlock()
	if notifyStopped {
		applog.Mainf("[MPV-IPC] 播放结束 (eof-reached)")
		p.emitEvent("stopped")
	}
}

func (p *MPVPlayer) connectIPCAfterStart() {
	p.mu.RLock()
	ipc := p.ipc
	done := p.ipcDone
	p.mu.RUnlock()
	if ipc == nil || done == nil {
		return
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		if _, err := ipc.dial(500 * time.Millisecond); err == nil {
			applog.Verbosef("[MPV-IPC] 已连接: %s", ipc.pipePath)
			go p.startIPCMonitor()
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	applog.Verbosef("[MPV-IPC] 连接超时: %s", ipc.pipePath)
}

func (p *MPVPlayer) clearIPC() {
	p.mu.Lock()
	if p.ipcDone != nil {
		select {
		case <-p.ipcDone:
		default:
			close(p.ipcDone)
		}
	}
	p.ipc = nil
	p.ipcDone = nil
	p.mu.Unlock()
}

func decodeHTMLEntities(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	return replacer.Replace(s)
}
