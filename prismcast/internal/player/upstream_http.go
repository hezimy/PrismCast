package player

import (
	"net/http"
	"strings"
)

// UpstreamMPVUserAgent mpv 默认 User-Agent，jiunuow 等源需与此一致才能拿到正片
const UpstreamMPVUserAgent = "libmpv"

// IsPreviewStreamHost 预览流/短片段 CDN 域名
func IsPreviewStreamHost(rawURI string) bool {
	lower := strings.ToLower(rawURI)
	return strings.Contains(lower, "xn--55qx2ai23bz99b.cn") ||
		strings.Contains(lower, "app.xn--55qx2ai23bz99b.cn")
}

// ApplyUpstreamHeaders 为上游 HTTP 请求设置与 mpv 一致的请求头
func ApplyUpstreamHeaders(req *http.Request, rawURI, castOrigin string) {
	lower := strings.ToLower(rawURI)

	switch {
	case strings.Contains(lower, "moedot.net"):
		req.Header.Set("User-Agent", mpvHTTPUserAgent)
	case strings.Contains(lower, "bytetos.com"):
		req.Header.Set("User-Agent", mpvHTTPUserAgent)
		req.Header.Set("Referer", "https://www.google.com/")
	case strings.Contains(lower, "jiunuow.com"):
		req.Header.Set("User-Agent", UpstreamMPVUserAgent)
	default:
		req.Header.Set("User-Agent", UpstreamMPVUserAgent)
		if castOrigin != "" {
			req.Header.Set("Referer", castOrigin)
		}
	}
}
