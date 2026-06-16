package dlna

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hezimy/PrismCast/internal/applog"
)

const (
	ssdpAddr   = "239.255.255.250"
	ssdpPort   = 1900
	ssdpMaxAge = 1800
)

// ssdpNotifyEntry represents one NT/USN pair for SSDP NOTIFY
type ssdpNotifyEntry struct {
	NT  string
	USN string
}

// SSDPServer handles SSDP multicast discovery
// Implementation approach:
// - Single socket bound to 0.0.0.0:1900 with SO_REUSEADDR
// - Join multicast group on all interfaces
// - M-SEARCH responses sent from the listen socket itself
// - Separate send sockets per interface for NOTIFY (with IP_MULTICAST_IF set)
type SSDPServer struct {
	deviceName string
	deviceUUID string
	location   string
	localIPs   []string
	running    bool
	mu         sync.Mutex
	sock       *net.UDPConn
	sendSocks  []*net.UDPConn
	notifyStop chan struct{}
	wg         sync.WaitGroup
	bootID     int64
}

// NewSSDPServer creates a new SSDP server
func NewSSDPServer(deviceName, deviceUUID, location string) *SSDPServer {
	return &SSDPServer{
		deviceName: deviceName,
		deviceUUID: deviceUUID,
		location:   location,
		bootID:     time.Now().Unix(),
	}
}

// SetDeviceName 更新 NOTIFY/M-SEARCH 响应中的设备友好名称
func (s *SSDPServer) SetDeviceName(name string) {
	s.mu.Lock()
	s.deviceName = name
	s.mu.Unlock()
}

func notifyEntriesForUUID(deviceUUID string) []ssdpNotifyEntry {
	return []ssdpNotifyEntry{
		{"upnp:rootdevice", fmt.Sprintf("uuid:%s::upnp:rootdevice", deviceUUID)},
		{fmt.Sprintf("uuid:%s", deviceUUID), fmt.Sprintf("uuid:%s", deviceUUID)},
		{"urn:schemas-upnp-org:device:MediaRenderer:1", fmt.Sprintf("uuid:%s::urn:schemas-upnp-org:device:MediaRenderer:1", deviceUUID)},
		{"urn:schemas-upnp-org:service:AVTransport:1", fmt.Sprintf("uuid:%s::urn:schemas-upnp-org:service:AVTransport:1", deviceUUID)},
		{"urn:schemas-upnp-org:service:RenderingControl:1", fmt.Sprintf("uuid:%s::urn:schemas-upnp-org:service:RenderingControl:1", deviceUUID)},
		{"urn:schemas-upnp-org:service:ConnectionManager:1", fmt.Sprintf("uuid:%s::urn:schemas-upnp-org:service:ConnectionManager:1", deviceUUID)},
	}
}

// getNotifyList returns all NT/USN pairs that should be advertised
func (s *SSDPServer) getNotifyList() []ssdpNotifyEntry {
	return notifyEntriesForUUID(s.deviceUUID)
}

// SendStaleByeBye 启动前发送 byebye，清除手机端缓存的旧 LOCATION（同 UUID 上次会话残留）
func SendStaleByeBye(deviceUUID string) {
	if deviceUUID == "" {
		return
	}
	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
	if err != nil {
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		applog.Mainf("[SSDP] SendStaleByeBye: bind failed: %v", err)
		return
	}
	defer conn.Close()

	dest := &net.UDPAddr{IP: net.ParseIP(ssdpAddr), Port: ssdpPort}
	for _, entry := range notifyEntriesForUUID(deviceUUID) {
		msg := fmt.Sprintf(
			"NOTIFY * HTTP/1.1\r\n"+
				"HOST: %s:%d\r\n"+
				"NT: %s\r\n"+
				"NTS: ssdp:byebye\r\n"+
				"USN: %s\r\n"+
				"\r\n",
			ssdpAddr, ssdpPort,
			entry.NT,
			entry.USN,
		)
		if _, err := conn.WriteToUDP([]byte(msg), dest); err != nil {
			applog.Verbosef("[SSDP] SendStaleByeBye error: %v", err)
		}
	}
	applog.Verbosef("[SSDP] Stale byebye sent for uuid:%s", deviceUUID)
}

// Start starts the SSDP server
func (s *SSDPServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	s.bootID = time.Now().Unix()

	// Get all local IPs
	s.localIPs = getLocalIPs()

	// Create the listen socket using ListenMulticastUDP
	// This handles SO_REUSEADDR on Windows and joins the multicast group
	multicastAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ssdpAddr, ssdpPort))
	if err != nil {
		return fmt.Errorf("failed to resolve SSDP multicast address: %w", err)
	}

	var conn *net.UDPConn
	for attempt := 1; attempt <= 3; attempt++ {
		conn, err = net.ListenMulticastUDP("udp", nil, multicastAddr)
		if err == nil {
			break
		}
		applog.Verbosef("[SSDP] bind attempt %d failed: %v", attempt, err)
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("failed to bind SSDP socket: %w", err)
	}

	s.sock = conn

	// Create per-interface send sockets
	// Each socket has IP_MULTICAST_IF set to the interface IP
	s.sendSocks = nil
	for _, ip := range s.localIPs {
		sendAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:0", ip))
		sendConn, err := net.ListenUDP("udp", sendAddr)
		if err != nil {
			applog.Verbosef("Warning: could not create send socket for %s: %v", ip, err)
			continue
		}
		s.sendSocks = append(s.sendSocks, sendConn)
	}

	// If no per-interface send sockets, create a generic one
	if len(s.sendSocks) == 0 {
		sendAddr, _ := net.ResolveUDPAddr("udp", "0.0.0.0:0")
		sendConn, err := net.ListenUDP("udp", sendAddr)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to create SSDP send socket: %w", err)
		}
		s.sendSocks = append(s.sendSocks, sendConn)
	}

	s.running = true
	s.notifyStop = make(chan struct{})

	// Start listener
	s.wg.Add(1)
	go s.listenLoop()

	// Start notify loop
	s.wg.Add(1)
	go s.notifyLoop()

	applog.Mainf("SSDP server started on 0.0.0.0:%d", ssdpPort)
	applog.Verbosef("SSDP detail UUID: %s Location: %s IPs: %v", s.deviceUUID, s.location, s.localIPs)

	// 立即广播 alive，不等待 notifyLoop 的 1s 间隔
	go s.SendAliveBurst()

	return nil
}

// Stop stops the SSDP server
func (s *SSDPServer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.notifyStop)
	if s.sock != nil {
		s.sock.Close()
	}
	for _, sc := range s.sendSocks {
		sc.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	applog.Mainf("SSDP server stopped")
}

func (s *SSDPServer) listenLoop() {
	defer s.wg.Done()

	buf := make([]byte, 8192)

	for {
		s.mu.Lock()
		running := s.running
		sock := s.sock
		s.mu.Unlock()

		if !running || sock == nil {
			return
		}

		sock.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := sock.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.mu.Lock()
			stillRunning := s.running
			s.mu.Unlock()
			if !stillRunning {
				return
			}
			continue
		}

		msg := string(buf[:n])
		if strings.Contains(msg, "M-SEARCH") {
			applog.Verbosef("[SSDP] Received M-SEARCH from %s", addr)
			s.handleMSearch(msg, addr)
		}
	}
}

func (s *SSDPServer) getResponseLocation(remoteAddr *net.UDPAddr) string {
	remoteIP := remoteAddr.IP
	if remoteIP.IsLoopback() {
		return s.location
	}

	bestIP := ""
	bestMatchLen := 0
	for _, localIP := range s.localIPs {
		lip := net.ParseIP(localIP)
		if lip == nil {
			continue
		}
		remoteBytes := remoteIP.To4()
		localBytes := lip.To4()
		if remoteBytes == nil || localBytes == nil {
			continue
		}
		matchLen := 0
		for i := 0; i < 3; i++ {
			if remoteBytes[i] == localBytes[i] {
				matchLen++
			} else {
				break
			}
		}
		if matchLen > bestMatchLen {
			bestMatchLen = matchLen
			bestIP = localIP
		}
	}
	if bestIP != "" {
		return fmt.Sprintf("http://%s:%d", bestIP, s.getPort())
	}
	return s.location
}

func (s *SSDPServer) getPort() int {
	for i := len(s.location) - 1; i >= 0; i-- {
		if s.location[i] == ':' {
			var port int
			fmt.Sscanf(s.location[i+1:], "%d", &port)
			return port
		}
	}
	return 8765
}

func (s *SSDPServer) handleMSearch(msg string, addr *net.UDPAddr) {
	stTarget := "ssdp:all"
	mx := 3

	for _, line := range strings.Split(msg, "\r\n") {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "st:") {
			stTarget = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(lower, "mx:") {
			val := strings.TrimSpace(line[3:])
			if _, err := fmt.Sscanf(val, "%d", &mx); err != nil || mx < 1 {
				mx = 1
			}
			if mx > 5 {
				mx = 5
			}
		}
	}

	applog.Verbosef("M-SEARCH from %s, ST: %s, MX: %d", addr, stTarget, mx)

	// Random delay per UPnP spec
	delayMs := rand.Intn(mx * 1000)
	if delayMs > 1000 {
		delayMs = 1000
	}
	time.Sleep(time.Duration(delayMs) * time.Millisecond)

	s.mu.Lock()
	running := s.running
	sock := s.sock
	s.mu.Unlock()

	if !running || sock == nil {
		return
	}

	// Send M-SEARCH response from the LISTEN socket itself
	// This is critical because some devices expect the response from port 1900
	for _, entry := range s.getNotifyList() {
		if stTarget == "ssdp:all" || stTarget == entry.NT {
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\n"+
					"CACHE-CONTROL: max-age=%d\r\n"+
					"EXT:\r\n"+
					"LOCATION: %s\r\n"+
					"SERVER: Windows/10 UPnP/1.0 PrismCast/1.0\r\n"+
					"ST: %s\r\n"+
					"USN: %s\r\n"+
					"BOOTID.UPNP.ORG: %d\r\n"+
					"CONFIGID.UPNP.ORG: 1\r\n"+
					"\r\n",
				ssdpMaxAge,
				s.getResponseLocation(addr),
				entry.NT,
				entry.USN,
				s.bootID,
			)
			sock.WriteToUDP([]byte(resp), addr)
		}
	}

	applog.Verbosef("M-SEARCH response sent to %s (ST: %s)", addr, stTarget)
}

func (s *SSDPServer) notifyLoop() {
	defer s.wg.Done()

	// Send 3 NOTIFYs at startup with 1s intervals
	for i := 0; i < 3; i++ {
		s.sendNotify()
		time.Sleep(1 * time.Second)
	}

	// Every 5 seconds for the first minute
	startup := time.NewTimer(60 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer startup.Stop()
	defer ticker.Stop()

	for {
		select {
		case <-s.notifyStop:
			s.sendByeBye()
			return
		case <-startup.C:
			// After first minute, switch to every 30 seconds
			ticker.Stop()
			ticker = time.NewTicker(30 * time.Second)
		case <-ticker.C:
			s.sendNotify()
		}
	}
}

func (s *SSDPServer) sendNotify() {
	s.mu.Lock()
	socks := s.sendSocks
	s.mu.Unlock()

	if len(socks) == 0 {
		applog.Mainf("[WARN] sendNotify: no send sockets available!")
		return
	}

	dest := &net.UDPAddr{IP: net.ParseIP(ssdpAddr), Port: ssdpPort}

	for _, entry := range s.getNotifyList() {
		msg := fmt.Sprintf(
			"NOTIFY * HTTP/1.1\r\n"+
				"HOST: %s:%d\r\n"+
				"CACHE-CONTROL: max-age=%d\r\n"+
				"LOCATION: %s\r\n"+
				"NT: %s\r\n"+
				"NTS: ssdp:alive\r\n"+
				"SERVER: Windows/10 UPnP/1.0 PrismCast/1.0\r\n"+
				"USN: %s\r\n"+
				"BOOTID.UPNP.ORG: %d\r\n"+
				"CONFIGID.UPNP.ORG: 1\r\n"+
				"\r\n",
			ssdpAddr, ssdpPort,
			ssdpMaxAge,
			s.location,
			entry.NT,
			entry.USN,
			s.bootID,
		)
		// Send NOTIFY from ALL interface sockets
		for _, sock := range socks {
			_, err := sock.WriteToUDP([]byte(msg), dest)
			if err != nil {
				applog.Verbosef("[WARN] NOTIFY send error on socket: %v", err)
			}
		}
	}
	applog.Verbosef("[SSDP] NOTIFY ssdp:alive sent (%d entries, %d sockets)", len(s.getNotifyList()), len(socks))
}

// SendAliveBurst 立即发送多轮 SSDP NOTIFY，帮助手机端快速重新发现设备
func (s *SSDPServer) SendAliveBurst() {
	go func() {
		for i := 0; i < 3; i++ {
			s.sendNotify()
			time.Sleep(300 * time.Millisecond)
		}
		applog.Verbosef("[SSDP] NOTIFY alive burst sent")
	}()
}

func (s *SSDPServer) sendByeBye() {
	s.mu.Lock()
	socks := s.sendSocks
	s.mu.Unlock()

	if len(socks) == 0 {
		return
	}

	dest := &net.UDPAddr{IP: net.ParseIP(ssdpAddr), Port: ssdpPort}

	for _, entry := range s.getNotifyList() {
		msg := fmt.Sprintf(
			"NOTIFY * HTTP/1.1\r\n"+
				"HOST: %s:%d\r\n"+
				"NT: %s\r\n"+
				"NTS: ssdp:byebye\r\n"+
				"USN: %s\r\n"+
				"\r\n",
			ssdpAddr, ssdpPort,
			entry.NT,
			entry.USN,
		)
		for _, sock := range socks {
			sock.WriteToUDP([]byte(msg), dest)
		}
	}
}

// getLocalIPs returns all local IPv4 addresses
func getLocalIPs() []string {
	var ips []string

	interfaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}

	// Add Windows mobile hotspot IP
	ips = append(ips, "192.168.137.1")

	return ips
}
