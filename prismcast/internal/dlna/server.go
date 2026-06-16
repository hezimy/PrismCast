package dlna

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/hezimy/PrismCast/internal/applog"
)

//go:embed icon.png
var iconData []byte

// Server implements a DLNA Media Renderer
type Server struct {
	deviceName string
	deviceUUID string
	player     MediaPlayer
	httpServer *http.Server
	router     *mux.Router
	ssdpSrv    *SSDPServer
	mu         sync.RWMutex
	status     string
	location   string
	port       int
	castMedia  *CastMedia
}

// SetPlayer 更新播放器引用（用于服务重启后同步新player）
func (s *Server) SetPlayer(p MediaPlayer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.player = p
}

// SetDeviceName 更新 DLNA 设备显示名称并刷新 SSDP 广播
func (s *Server) SetDeviceName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.mu.Lock()
	s.deviceName = name
	ssdp := s.ssdpSrv
	s.mu.Unlock()
	if ssdp != nil {
		ssdp.SetDeviceName(name)
	}
}

// getPlayer 线程安全地获取当前播放器引用。
// Go interface 由 type+data 两个字组成，并发读写会读到半更新值，
// 因此所有 handler 都必须通过本方法取引用，而不能直接读 s.player。
func (s *Server) getPlayer() MediaPlayer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.player
}

// CastMedia 投屏媒体信息（供前端展示）
type CastMedia struct {
	URI       string `json:"uri"`
	Title     string `json:"title"`
	MediaType string `json:"mediaType"`
	State     string `json:"state"`
	Timestamp int64  `json:"timestamp"`
}

// MediaPlayer interface for the player backend
type MediaPlayer interface {
	Load(uri string, title string) error
	Play() error
	Pause() error
	Stop() error
	Seek(position float64) error
	SetVolume(volume int) error
	SetMute(mute bool) error
	GetStatus() map[string]interface{}
}

// TransportState represents the current transport state
type TransportState string

const (
	StateStopped    TransportState = "STOPPED"
	StatePlaying    TransportState = "PLAYING"
	StatePaused     TransportState = "PAUSED_PLAYBACK"
	StateTransition TransportState = "TRANSITIONING"
	StateNoMedia    TransportState = "NO_MEDIA_PRESENT"
)

// NewServer creates a new DLNA server
func NewServer(deviceName, deviceUUID string, player MediaPlayer) *Server {
	if deviceUUID == "" {
		deviceUUID = uuid.New().String()
	}
	return &Server{
		deviceName: deviceName,
		deviceUUID: deviceUUID,
		player:     player,
		status:     "stopped",
	}
}

// SetIconData allows setting custom icon data for the DLNA server
func (s *Server) SetIconData(data []byte) {
	if len(data) > 0 {
		iconData = data
	}
}

// Start starts the DLNA server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == "running" {
		return nil
	}

	const defaultPort = 8765

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", defaultPort))
	if err != nil {
		applog.Mainf("Port %d in use, trying dynamic port...", defaultPort)
		listener, err = net.Listen("tcp", ":0")
		if err != nil {
			return fmt.Errorf("failed to bind any port: %w", err)
		}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	s.port = port

	ip, err := getLocalIP()
	if err != nil {
		return fmt.Errorf("failed to get local IP: %w", err)
	}

	s.location = fmt.Sprintf("http://%s:%d", ip, port)

	AddFirewallRules(port)

	s.router = mux.NewRouter()
	s.setupRoutes()

	loggedRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applog.Verbosef("[HTTP] %s %s from %s SOAPACTION=%q", r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("SOAPACTION"))
		s.router.ServeHTTP(w, r)
	})

	recoveryRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				applog.Mainf("[HTTP] PANIC in handler: %v", rec)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		loggedRouter.ServeHTTP(w, r)
	})

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: recoveryRouter,
	}

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applog.Mainf("DLNA HTTP server error: %v", err)
		}
	}()

	// 等待 HTTP 绑定完成后再广播，避免手机收到 NOTIFY 却连不上 description.xml
	time.Sleep(300 * time.Millisecond)

	// 清除手机端同 UUID 的旧缓存，避免必须手动停一次服务才能被发现
	SendStaleByeBye(s.deviceUUID)
	time.Sleep(300 * time.Millisecond)

	var ssdpErr error
	for attempt := 1; attempt <= 3; attempt++ {
		s.ssdpSrv = NewSSDPServer(s.deviceName, s.deviceUUID, s.location)
		ssdpErr = s.ssdpSrv.Start()
		if ssdpErr == nil {
			break
		}
		applog.Verbosef("[SSDP] start attempt %d/3 failed: %v", attempt, ssdpErr)
		time.Sleep(500 * time.Millisecond)
	}
	if ssdpErr != nil {
		applog.Mainf("SSDP server start error after retries: %v", ssdpErr)
	}

	s.status = "running"
	applog.Mainf("DLNA server started at %s", s.location)
	return nil
}

// RefreshDiscovery 再次广播 SSDP alive，用于启动后延迟刷新
func (s *Server) RefreshDiscovery() {
	s.mu.RLock()
	ssdp := s.ssdpSrv
	s.mu.RUnlock()
	if ssdp != nil {
		ssdp.SendAliveBurst()
	}
}

// Stop stops the DLNA server
func (s *Server) Stop() {
	s.mu.Lock()
	if s.status != "running" {
		s.mu.Unlock()
		return
	}
	httpSrv := s.httpServer
	ssdpSrv := s.ssdpSrv
	s.status = "stopped"
	s.mu.Unlock()

	if ssdpSrv != nil {
		ssdpSrv.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if httpSrv != nil {
		if err := httpSrv.Shutdown(ctx); err != nil {
			applog.Mainf("DLNA server shutdown error: %v", err)
		}
	}
	applog.Mainf("DLNA server stopped")
}

// Status returns server status
func (s *Server) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// GetLocation returns the server location URL
func (s *Server) GetLocation() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.location
}

// GetLocalBrowserURL 返回本机浏览器播放页基址（127.0.0.1，Chrome 在此地址下才允许 MSE/HLS）
func (s *Server) GetLocalBrowserURL() string {
	s.mu.RLock()
	port := s.port
	s.mu.RUnlock()
	if port == 0 {
		port = 8765
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (s *Server) requestBaseURL(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return s.GetLocalBrowserURL()
	}
	return "http://" + host
}

// GetCastMedia returns current cast media info (for frontend display)
func (s *Server) GetCastMedia() *CastMedia {
	s.mu.RLock()
	cm := s.castMedia
	p := s.player
	s.mu.RUnlock()

	if cm == nil {
		return nil
	}

	if p != nil && (cm.State == "playing" || cm.State == "paused" || cm.State == "loading") {
		playerState := ""
		if ps := p.GetStatus(); ps != nil {
			playerState, _ = ps["state"].(string)
		}
		if playerState == "stopped" || playerState == "idle" {
			s.mu.Lock()
			if s.castMedia != nil && (s.castMedia.State == "playing" || s.castMedia.State == "paused" || s.castMedia.State == "loading") {
				s.castMedia.State = "stopped"
			}
			s.mu.Unlock()
		} else if playerState == "error" {
			s.mu.Lock()
			if s.castMedia != nil {
				s.castMedia.State = "error"
			}
			s.mu.Unlock()
		} else if cm.State == "loading" && (playerState == "playing" || playerState == "paused") {
			s.mu.Lock()
			if s.castMedia != nil && s.castMedia.State == "loading" {
				s.castMedia.State = playerState
			}
			s.mu.Unlock()
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.castMedia
}

// NotifyPlaybackEnded 播放器退出后同步状态并刷新 SSDP 广播
func (s *Server) NotifyPlaybackEnded(event string) {
	s.mu.Lock()
	if s.castMedia != nil {
		if event == "error" {
			s.castMedia.State = "error"
		} else {
			s.castMedia.State = "stopped"
			s.castMedia.URI = ""
			s.castMedia.Title = ""
		}
	}
	ssdp := s.ssdpSrv
	s.mu.Unlock()

	if ssdp != nil {
		ssdp.SendAliveBurst()
	}
}

func (s *Server) setupRoutes() {
	// Device description
	s.router.HandleFunc("/", s.handleDeviceDescription).Methods("GET")
	s.router.HandleFunc("/description.xml", s.handleDeviceDescription).Methods("GET")

	// SCPD XMLs
	s.router.HandleFunc("/upnp/service/AVTransport.xml", s.handleAVTransportSCPD).Methods("GET")
	s.router.HandleFunc("/upnp/service/RenderingControl.xml", s.handleRenderingControlSCPD).Methods("GET")
	s.router.HandleFunc("/upnp/service/ConnectionManager.xml", s.handleConnectionManagerSCPD).Methods("GET")

	// UPnP services
	s.router.HandleFunc("/upnp/control/AVTransport", s.handleAVTransport).Methods("POST")
	s.router.HandleFunc("/upnp/control/RenderingControl", s.handleRenderingControl).Methods("POST")
	s.router.HandleFunc("/upnp/control/ConnectionManager", s.handleConnectionManager).Methods("POST")

	// Event subscriptions
	s.router.HandleFunc("/upnp/event/AVTransport", s.handleEventSubscription).Methods("SUBSCRIBE", "UNSUBSCRIBE")
	s.router.HandleFunc("/upnp/event/RenderingControl", s.handleEventSubscription).Methods("SUBSCRIBE", "UNSUBSCRIBE")

	// Icons
	s.router.HandleFunc("/icon.png", s.handleIcon).Methods("GET")

	// 无 mpv 时浏览器 HLS 播放（hls.js + 同源代理，避免直接下载 m3u8）
	s.router.HandleFunc("/hls.min.js", s.handleHLSScript).Methods("GET")
	s.router.HandleFunc("/browser-play", s.handleBrowserPlay).Methods("GET")
	s.router.HandleFunc("/media-proxy", s.handleMediaProxy).Methods("GET")
}

func (s *Server) handleAVTransportSCPD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(scpdAVTransport))
}

func (s *Server) handleRenderingControlSCPD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(scpdRenderingControl))
}

func (s *Server) handleConnectionManagerSCPD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(scpdConnectionManager))
}

func (s *Server) handleDeviceDescription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	w.Write([]byte(s.generateDeviceDescription()))
}

func (s *Server) generateDeviceDescription() string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>%s</friendlyName>
    <manufacturer>PrismCast</manufacturer>
    <manufacturerURL>https://github.com/hezimy/PrismCast</manufacturerURL>
    <modelDescription>PrismCast DLNA Media Renderer</modelDescription>
    <modelName>PrismCast</modelName>
    <modelNumber>1.0</modelNumber>
    <modelURL>https://github.com/hezimy/PrismCast</modelURL>
    <serialNumber>1</serialNumber>
    <UDN>uuid:%s</UDN>
    <iconList>
      <icon>
        <mimetype>image/png</mimetype>
        <width>256</width>
        <height>256</height>
        <depth>24</depth>
        <url>/icon.png</url>
      </icon>
      <icon>
        <mimetype>image/png</mimetype>
        <width>48</width>
        <height>48</height>
        <depth>24</depth>
        <url>/icon.png</url>
      </icon>
    </iconList>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:AVTransport</serviceId>
        <SCPDURL>/upnp/service/AVTransport.xml</SCPDURL>
        <controlURL>/upnp/control/AVTransport</controlURL>
        <eventSubURL>/upnp/event/AVTransport</eventSubURL>
      </service>
      <service>
        <serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:RenderingControl</serviceId>
        <SCPDURL>/upnp/service/RenderingControl.xml</SCPDURL>
        <controlURL>/upnp/control/RenderingControl</controlURL>
        <eventSubURL>/upnp/event/RenderingControl</eventSubURL>
      </service>
      <service>
        <serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId>
        <SCPDURL>/upnp/service/ConnectionManager.xml</SCPDURL>
        <controlURL>/upnp/control/ConnectionManager</controlURL>
        <eventSubURL>/upnp/event/ConnectionManager</eventSubURL>
      </service>
    </serviceList>
  </device>
</root>`, s.deviceName, s.deviceUUID)
}

func (s *Server) handleAVTransport(w http.ResponseWriter, r *http.Request) {
	soapAction := r.Header.Get("SOAPACTION")
	if soapAction == "" {
		http.Error(w, "Missing SOAPACTION", http.StatusBadRequest)
		return
	}

	action := extractAction(soapAction)
	applog.Verbosef("AVTransport action: %s", action)

	switch action {
	case "SetAVTransportURI":
		s.handleSetAVTransportURI(w, r)
	case "Play":
		s.handlePlay(w, r)
	case "Pause":
		s.handlePause(w, r)
	case "Stop":
		s.handleStop(w, r)
	case "Seek":
		s.handleSeek(w, r)
	case "GetPositionInfo":
		s.handleGetPositionInfo(w, r)
	case "GetTransportInfo":
		s.handleGetTransportInfo(w, r)
	default:
		body := fmt.Sprintf("<u:%sResponse xmlns:u=\"urn:schemas-upnp-org:service:AVTransport:1\"></u:%sResponse>", action, action)
		s.writeSOAPResponse(w, action, body)
	}
}

func (s *Server) handleSetAVTransportURI(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	var bodyStr string
	if err == nil {
		bodyStr = string(bodyBytes)
	}
	uri := decodeSOAPString(extractValueFromXML(bodyStr, "CurrentURI"))
	metadata := extractValueFromXML(bodyStr, "CurrentURIMetaData")

	if resURI := extractResURIFromDIDL(metadata); resURI != "" && resURI != uri {
		applog.Verbosef("[DLNA] 使用 DIDL <res> URI 替代 CurrentURI")
		uri = resURI
	}

	title := extractTitleFromDIDL(metadata)
	if title == "" {
		title = extractFilenameFromURI(uri)
	}
	if decoded, err := url.QueryUnescape(title); err == nil && decoded != "" {
		title = decoded
	}

	if uri != "" {
		mediaType := detectMediaType(uri)
		applog.Mainf("[DLNA] 收到投屏请求 URI=%s 类型=%s Title=%s", uri, mediaType, title)
		if strings.Contains(strings.ToLower(uri), "jiunuow.com") && metadata != "" {
			metaPreview := metadata
			if len(metaPreview) > 500 {
				metaPreview = metaPreview[:500] + "..."
			}
			applog.Verbosef("[DLNA] DIDL metadata: %s", metaPreview)
		}

		s.mu.Lock()
		s.castMedia = &CastMedia{
			URI:       uri,
			Title:     title,
			MediaType: mediaType,
			State:     "loading",
			Timestamp: time.Now().Unix(),
		}
		p := s.player
		s.mu.Unlock()

		if p == nil {
			s.mu.Lock()
			if s.castMedia != nil {
				s.castMedia.State = "error"
			}
			s.mu.Unlock()
		} else if err := p.Load(uri, title); err != nil {
			s.mu.Lock()
			if s.castMedia != nil {
				s.castMedia.State = "error"
			}
			s.mu.Unlock()
		} else {
			s.mu.Lock()
			if s.castMedia != nil {
				s.castMedia.State = "playing"
			}
			s.mu.Unlock()
		}
	} else {
		applog.Mainf("[DLNA] WARNING: uri为空，无法处理投屏请求")
	}

	s.writeSOAPResponse(w, "SetAVTransportURI", `<u:SetAVTransportURIResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"></u:SetAVTransportURIResponse>`)
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	if p := s.getPlayer(); p != nil {
		_ = p.Play()
	}
	s.mu.Lock()
	if s.castMedia != nil && s.castMedia.State != "error" {
		s.castMedia.State = "playing"
	}
	s.mu.Unlock()
	s.writeSOAPResponse(w, "Play", `<u:PlayResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"></u:PlayResponse>`)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if p := s.getPlayer(); p != nil {
		_ = p.Pause()
	}
	s.mu.Lock()
	if s.castMedia != nil {
		s.castMedia.State = "paused"
	}
	s.mu.Unlock()
	s.writeSOAPResponse(w, "Pause", `<u:PauseResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"></u:PauseResponse>`)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if p := s.getPlayer(); p != nil {
		_ = p.Stop()
	}
	s.mu.Lock()
	if s.castMedia != nil {
		s.castMedia.State = "stopped"
		s.castMedia.URI = ""
		s.castMedia.Title = ""
	}
	s.mu.Unlock()
	s.writeSOAPResponse(w, "Stop", `<u:StopResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"></u:StopResponse>`)
}

func (s *Server) handleSeek(w http.ResponseWriter, r *http.Request) {
	target := extractValueFromBody(r, "Target")
	seconds := parseTime(target)
	if p := s.getPlayer(); p != nil {
		_ = p.Seek(seconds)
	}
	s.writeSOAPResponse(w, "Seek", `<u:SeekResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"></u:SeekResponse>`)
}

func (s *Server) handleGetPositionInfo(w http.ResponseWriter, r *http.Request) {
	var status map[string]interface{}
	if p := s.getPlayer(); p != nil {
		status = p.GetStatus()
	}
	position, _ := status["position"].(float64)
	duration, _ := status["duration"].(float64)
	uri, _ := status["uri"].(string)

	posStr := formatDuration(position)
	durStr := formatDuration(duration)

	response := fmt.Sprintf(`<u:GetPositionInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
    <Track>1</Track>
    <TrackDuration>%s</TrackDuration>
    <TrackMetaData></TrackMetaData>
    <TrackURI>%s</TrackURI>
    <RelTime>%s</RelTime>
    <AbsTime>%s</AbsTime>
    <RelCount>2147483647</RelCount>
    <AbsCount>2147483647</AbsCount>
  </u:GetPositionInfoResponse>`, durStr, uri, posStr, posStr)

	s.writeSOAPResponse(w, "GetPositionInfo", response)
}

func (s *Server) handleGetTransportInfo(w http.ResponseWriter, r *http.Request) {
	transportState := string(StateStopped)
	if cm := s.GetCastMedia(); cm != nil {
		switch cm.State {
		case "loading":
			transportState = string(StateTransition)
		case "playing":
			transportState = string(StatePlaying)
		case "paused":
			transportState = string(StatePaused)
		case "error":
			transportState = string(StateNoMedia)
		case "stopped":
			transportState = string(StateStopped)
		default:
			if p := s.getPlayer(); p != nil {
				if ps := p.GetStatus(); ps != nil {
					state, _ := ps["state"].(string)
					transportState = mapStateToTransport(state)
				}
			}
		}
	}

	response := fmt.Sprintf(`<u:GetTransportInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
    <CurrentTransportState>%s</CurrentTransportState>
    <CurrentTransportStatus>OK</CurrentTransportStatus>
    <CurrentSpeed>1</CurrentSpeed>
  </u:GetTransportInfoResponse>`, transportState)

	s.writeSOAPResponse(w, "GetTransportInfo", response)
}

func (s *Server) handleRenderingControl(w http.ResponseWriter, r *http.Request) {
	soapAction := r.Header.Get("SOAPACTION")
	action := extractAction(soapAction)
	applog.Verbosef("RenderingControl action: %s", action)

	p := s.getPlayer()

	switch action {
	case "SetVolume":
		volStr := extractValueFromBody(r, "DesiredVolume")
		vol, _ := strconv.Atoi(volStr)
		if p != nil {
			if av, ok := p.(interface{ AdjustVolumeFromDLNA(int, string) error }); ok {
				_ = av.AdjustVolumeFromDLNA(vol, "dlna")
			} else {
				_ = p.SetVolume(vol)
			}
		}
	case "SetMute":
		muteStr := extractValueFromBody(r, "DesiredMute")
		mute := muteStr == "1" || strings.ToLower(muteStr) == "true"
		if p != nil {
			_ = p.SetMute(mute)
		}
	case "GetVolume":
		var vol int
		if p != nil {
			status := p.GetStatus()
			vol, _ = status["volume"].(int)
		}
		s.writeSOAPResponse(w, "GetVolume", fmt.Sprintf(`<u:GetVolumeResponse xmlns:u="urn:schemas-upnp-org:service:RenderingControl:1"><CurrentVolume>%d</CurrentVolume></u:GetVolumeResponse>`, vol))
		return
	}

	s.writeSOAPResponse(w, action, fmt.Sprintf(`<u:%sResponse xmlns:u="urn:schemas-upnp-org:service:RenderingControl:1"></u:%sResponse>`, action, action))
}

func (s *Server) handleConnectionManager(w http.ResponseWriter, r *http.Request) {
	soapAction := r.Header.Get("SOAPACTION")
	action := extractAction(soapAction)

	var response string
	switch action {
	case "GetProtocolInfo":
		response = `<u:GetProtocolInfoResponse xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1">
      <Source></Source>
      <Sink>http-get:*:video/mp4:*,http-get:*:video/x-matroska:*,http-get:*:video/avi:*,http-get:*:audio/mpeg:*,http-get:*:audio/mp4:*,http-get:*:audio/flac:*,http-get:*:image/jpeg:*,http-get:*:image/png:*</Sink>
    </u:GetProtocolInfoResponse>`
	case "GetCurrentConnectionIDs":
		response = `<u:GetCurrentConnectionIDsResponse xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"><ConnectionIDs>0</ConnectionIDs></u:GetCurrentConnectionIDsResponse>`
	default:
		response = fmt.Sprintf(`<u:%sResponse xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"></u:%sResponse>`, action, action)
	}

	s.writeSOAPResponse(w, action, response)
}

func (s *Server) handleEventSubscription(w http.ResponseWriter, r *http.Request) {
	// Minimal event subscription support
	w.Header().Set("SID", fmt.Sprintf("uuid:%s", uuid.New().String()))
	w.Header().Set("TIMEOUT", "Second-1800")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	if len(iconData) > 0 {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(iconData)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
}

func (s *Server) writeSOAPResponse(w http.ResponseWriter, action, body string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("EXT", "")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    %s
  </s:Body>
</s:Envelope>`, body)
}

func extractAction(soapAction string) string {
	// Extract action from "urn:schemas-upnp-org:service:AVTransport:1#Play"
	parts := strings.Split(soapAction, "#")
	if len(parts) > 1 {
		return strings.TrimSuffix(parts[1], `"`)
	}
	return ""
}

// extractValueFromBody 从 SOAP 请求体中提取指定 key 的值
// 支持两种标签格式：<Key>value</Key> 和 <ns:Key>value</ns:Key>
// 注意：r.Body 只能读取一次，因此 handleSetAVTransportURI 需要先读取 body 再分别提取
func extractValueFromBody(r *http.Request, key string) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	return extractValueFromXML(string(body), key)
}

// extractValueFromXML 从 XML 字符串中提取指定 key 的值，支持命名空间前缀
func extractValueFromXML(xmlContent string, key string) string {
	// 尝试无命名空间前缀的标签：<Key>...</Key>
	tag := fmt.Sprintf("<%s>", key)
	endTag := fmt.Sprintf("</%s>", key)
	start := strings.Index(xmlContent, tag)
	end := strings.Index(xmlContent, endTag)
	if start != -1 && end != -1 {
		return xmlContent[start+len(tag) : end]
	}

	// 尝试有命名空间前缀的标签：<ns:Key>...</ns:Key>
	// 使用更通用的匹配：查找 ":Key>" 和 "</ns:Key>"
	colonKey := fmt.Sprintf(":%s>", key)
	start = strings.Index(xmlContent, colonKey)
	if start != -1 {
		// 向前找到 '<' 以确定标签起始位置
		tagStart := strings.LastIndex(xmlContent[:start], "<")
		if tagStart != -1 {
			fullTag := xmlContent[tagStart : start+len(colonKey)]
			// 构造结束标签：</ + 前缀 + :Key>
			colonPos := strings.Index(fullTag, ":")
			endNsTag := "</" + fullTag[1:colonPos+1] + key + ">"
			end = strings.Index(xmlContent, endNsTag)
			if end != -1 {
				return xmlContent[start+len(colonKey) : end]
			}
		}
	}
	return ""
}

func parseTime(t string) float64 {
	parts := strings.Split(t, ":")
	if len(parts) == 3 {
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		s, _ := strconv.ParseFloat(parts[2], 64)
		return float64(h*3600+m*60) + s
	}
	if len(parts) == 2 {
		m, _ := strconv.Atoi(parts[0])
		s, _ := strconv.ParseFloat(parts[1], 64)
		return float64(m*60) + s
	}
	sec, _ := strconv.ParseFloat(t, 64)
	return sec
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "00:00:00"
	}
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("00:%02d:%02d", m, s)
}

// detectMediaType 根据URI扩展名或MIME类型判断媒体类型
func detectMediaType(uri string) string {
	uri = strings.ToLower(uri)

	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico", ".heic", ".heif", ".avif", ".tiff", ".tif"}
	for _, ext := range imageExts {
		if strings.HasSuffix(uri, ext) {
			return "image"
		}
	}

	audioExts := []string{".mp3", ".wav", ".flac", ".aac", ".ogg", ".wma", ".m4a", ".opus", ".ape", ".alac"}
	for _, ext := range audioExts {
		if strings.HasSuffix(uri, ext) {
			return "audio"
		}
	}

	videoExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".rmvb", ".ts", ".mts", ".m2ts", ".mpg", ".mpeg", ".3gp"}
	for _, ext := range videoExts {
		if strings.HasSuffix(uri, ext) {
			return "video"
		}
	}

	docExts := []string{".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".rtf"}
	for _, ext := range docExts {
		if strings.HasSuffix(uri, ext) {
			return "document"
		}
	}

	if strings.Contains(uri, "image") || strings.Contains(uri, "photo") || strings.Contains(uri, "jpeg") || strings.Contains(uri, "png") {
		return "image"
	}
	if strings.Contains(uri, "audio") || strings.Contains(uri, "music") || strings.Contains(uri, "sound") {
		return "audio"
	}
	if strings.Contains(uri, "m3u8") || strings.Contains(uri, "video") || strings.Contains(uri, ".mp4") {
		return "video"
	}

	return "unknown"
}

func mapStateToTransport(state string) string {
	switch state {
	case "playing":
		return string(StatePlaying)
	case "paused":
		return string(StatePaused)
	case "stopped", "idle":
		return string(StateStopped)
	default:
		return string(StateNoMedia)
	}
}

func getLocalIP() (string, error) {
	type ifaceIP struct {
		ip        string
		ifaceName string
		score     int
	}
	var candidates []ifaceIP

	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				score := 0
				nameLower := strings.ToLower(iface.Name)
				ipStr := ipnet.IP.String()

				if strings.Contains(nameLower, "wi-fi") || strings.Contains(nameLower, "wlan") || strings.Contains(nameLower, "wifi") {
					score += 50
				} else if strings.Contains(nameLower, "ethernet") || strings.Contains(nameLower, "eth") {
					score += 30
				}

				if strings.Contains(nameLower, "virtual") || strings.Contains(nameLower, "vethernet") ||
					strings.Contains(nameLower, "hyper") || strings.Contains(nameLower, "wsl") ||
					strings.Contains(nameLower, "vpn") || strings.Contains(nameLower, "docker") ||
					strings.Contains(nameLower, "vmware") || strings.Contains(nameLower, "vbox") {
					score -= 30
				}

				if strings.HasPrefix(ipStr, "192.168.0.") || strings.HasPrefix(ipStr, "192.168.1.") {
					score += 20
				} else if strings.HasPrefix(ipStr, "192.168.") {
					score += 10
				} else if strings.HasPrefix(ipStr, "10.") {
					score += 5
				}

				candidates = append(candidates, ifaceIP{ip: ipStr, ifaceName: iface.Name, score: score})
				applog.Verbosef("[NET] Interface %s → %s (score: %d)", iface.Name, ipStr, score)
			}
		}
	}

	if len(candidates) > 0 {
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.score > best.score {
				best = c
			}
		}
		applog.Verbosef("[NET] Selected IP: %s (interface: %s)", best.ip, best.ifaceName)
		return best.ip, nil
	}

	return "127.0.0.1", fmt.Errorf("no valid non-loopback IPv4 address found")
}

// extractTitleFromDIDL 从DIDL-Lite XML元数据中提取dc:title
func extractTitleFromDIDL(metadata string) string {
	metadata = decodeSOAPString(metadata)
	if metadata == "" {
		return ""
	}
	if start := strings.Index(metadata, "<dc:title>"); start != -1 {
		start += len("<dc:title>")
		if end := strings.Index(metadata[start:], "</dc:title>"); end != -1 {
			return metadata[start : start+end]
		}
	}
	if start := strings.Index(metadata, "<title>"); start != -1 {
		start += len("<title>")
		if end := strings.Index(metadata[start:], "</title>"); end != -1 {
			return metadata[start : start+end]
		}
	}
	return ""
}

// decodeSOAPString 解码 SOAP/DIDL 中的 HTML 实体
func decodeSOAPString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	s = replacer.Replace(s)
	if decoded, err := url.QueryUnescape(s); err == nil && decoded != "" {
		s = decoded
	}
	return s
}

// extractResURIFromDIDL 从 DIDL-Lite 元数据提取 <res> 内的媒体地址
func extractResURIFromDIDL(metadata string) string {
	metadata = decodeSOAPString(metadata)
	if metadata == "" {
		return ""
	}
	start := strings.Index(metadata, "<res")
	if start == -1 {
		return ""
	}
	gt := strings.Index(metadata[start:], ">")
	if gt == -1 {
		return ""
	}
	contentStart := start + gt + 1
	end := strings.Index(metadata[contentStart:], "</res>")
	if end == -1 {
		end = strings.Index(metadata[contentStart:], "</")
		if end == -1 {
			return ""
		}
	}
	uri := strings.TrimSpace(metadata[contentStart : contentStart+end])
	if strings.HasPrefix(strings.ToLower(uri), "http") {
		return uri
	}
	return ""
}

// extractFilenameFromURI 从URI中提取文件名作为备选title
func extractFilenameFromURI(uri string) string {
	if idx := strings.Index(uri, "?"); idx != -1 {
		uri = uri[:idx]
	}
	parts := strings.Split(uri, "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if idx := strings.LastIndex(last, "."); idx > 0 {
			return last[:idx]
		}
		return last
	}
	return ""
}
