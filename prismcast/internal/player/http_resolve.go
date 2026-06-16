package player

import (
	"strings"
)

const mpvHTTPUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// mpvStreamArgsForURI 按来源生成 mpv HTTP 参数。
// jiunuow 等 HLS 源必须保持 mpv 默认行为（不附加 UA/Referer），否则会被重定向到预览流。
func mpvStreamArgsForURI(uri string) []string {
	lower := strings.ToLower(uri)

	if strings.Contains(lower, "moedot.net") {
		return []string{"--network-timeout=60"}
	}

	if strings.Contains(lower, "bytetos.com") {
		return []string{
			"--user-agent=" + mpvHTTPUserAgent,
			"--referrer=https://www.google.com/",
			"--network-timeout=60",
		}
	}

	// jiunuow / m3u8 / 加密 HLS：与旧版一致，不附加任何 HTTP 参数
	if strings.Contains(lower, "jiunuow.com") || strings.Contains(lower, "m3u8") ||
		strings.Contains(lower, "/hls/decrypt/") {
		return nil
	}

	return []string{
		"--user-agent=" + mpvHTTPUserAgent,
		"--network-timeout=60",
	}
}
