package dlna

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hezimy/PrismCast/internal/applog"
	"github.com/hezimy/PrismCast/internal/player"
)

//go:embed hls.min.js
var hlsJS []byte

const proxyURLPlaceholder = "__PROXY_URL__"

const browserPlayHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PrismCast</title>
<script src="/hls.min.js"></script>
<style>
html,body{margin:0;height:100%;background:#000;color:#fff;font-family:sans-serif}
#wrap{position:relative;width:100%;height:100%}
video{width:100%;height:100%;object-fit:contain;background:#000}
#status{position:absolute;left:0;right:0;top:0;padding:12px 16px;background:rgba(0,0,0,.72);font-size:14px;line-height:1.5;z-index:2;white-space:pre-wrap}
#status.err{color:#ffb4b4}
</style>
</head>
<body>
<div id="wrap">
<div id="status">正在加载播放器…</div>
<video id="v" controls autoplay playsinline></video>
</div>
<script>
(function () {
  const src = __PROXY_URL__;
  const status = document.getElementById('status');
  const v = document.getElementById('v');
  function show(msg, isErr) {
    status.textContent = msg;
    status.className = isErr ? 'err' : '';
  }
  try {
    if (typeof MediaSource === 'undefined' && !(window.WebKitMediaSource)) {
      show('浏览器禁止在当前地址使用 HLS（需通过 127.0.0.1 打开）', true);
      return;
    }
    if (!window.Hls) {
      show('HLS 组件加载失败，请刷新页面重试', true);
      return;
    }
    if (!Hls.isSupported()) {
      if (v.canPlayType('application/vnd.apple.mpegurl')) {
        show('正在使用浏览器原生 HLS…');
        v.src = src;
        v.addEventListener('loadedmetadata', () => { show(''); });
        v.addEventListener('error', () => show('播放失败，请查看 PrismCast 日志', true));
        v.play().catch(() => {});
        return;
      }
      show('当前浏览器不支持 HLS 播放', true);
      return;
    }
    const hls = new Hls({ enableWorker: false });
    hls.on(Hls.Events.MANIFEST_LOADING, () => show('正在获取播放列表…'));
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      show('');
      v.play().catch(() => {});
    });
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (data && data.fatal) {
        show('播放失败: ' + (data.details || data.type || 'unknown'), true);
      }
    });
    hls.loadSource(src);
    hls.attachMedia(v);
  } catch (e) {
    show('播放器初始化失败: ' + (e && e.message ? e.message : e), true);
  }
})();
</script>
</body>
</html>`

func renderBrowserPlayHTML(proxyURL string) []byte {
	quoted, err := json.Marshal(proxyURL)
	if err != nil {
		quoted = []byte(`""`)
	}
	return []byte(strings.Replace(browserPlayHTML, proxyURLPlaceholder, string(quoted), 1))
}

func (s *Server) handleHLSScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(hlsJS)
}

func (s *Server) handleBrowserPlay(w http.ResponseWriter, r *http.Request) {
	rawURI := strings.TrimSpace(r.URL.Query().Get("uri"))
	if rawURI == "" {
		http.Error(w, "missing uri", http.StatusBadRequest)
		return
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "invalid uri", http.StatusBadRequest)
		return
	}

	base := s.requestBaseURL(r)
	proxyURL := buildMediaProxyURL(base, rawURI, rawURI)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(renderBrowserPlayHTML(proxyURL))
}

func buildMediaProxyURL(proxyBase, rawURI, castOrigin string) string {
	u := strings.TrimRight(proxyBase, "/") + "/media-proxy?uri=" + url.QueryEscape(rawURI)
	if castOrigin != "" {
		u += "&origin=" + url.QueryEscape(castOrigin)
	}
	return u
}

func (s *Server) handleMediaProxy(w http.ResponseWriter, r *http.Request) {
	rawURI := strings.TrimSpace(r.URL.Query().Get("uri"))
	if rawURI == "" {
		http.Error(w, "missing uri", http.StatusBadRequest)
		return
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "invalid uri", http.StatusBadRequest)
		return
	}

	applog.Verbosef("[BrowserPlay] proxy %s", truncateURL(rawURI))

	castOrigin := strings.TrimSpace(r.URL.Query().Get("origin"))

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURI, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rangeH := r.Header.Get("Range"); rangeH != "" {
		req.Header.Set("Range", rangeH)
	}
	player.ApplyUpstreamHeaders(req, rawURI, castOrigin)

	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			if player.IsPreviewStreamHost(req.URL.String()) && strings.Contains(strings.ToLower(rawURI), "jiunuow.com") {
				applog.Verbosef("[BrowserPlay] WARN jiunuow 重定向到预览 CDN: %s", req.URL.Host)
			}
			player.ApplyUpstreamHeaders(req, req.URL.String(), castOrigin)
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		applog.Verbosef("[BrowserPlay] proxy fetch failed: %s err=%v", truncateURL(rawURI), err)
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		http.Error(w, "read upstream failed", http.StatusBadGateway)
		return
	}

	if resp.StatusCode >= 400 {
		applog.Verbosef("[BrowserPlay] upstream HTTP %d: %s", resp.StatusCode, truncateURL(rawURI))
		http.Error(w, fmt.Sprintf("upstream HTTP %d", resp.StatusCode), resp.StatusCode)
		return
	}

	proxyBase := s.requestBaseURL(r)
	baseURL := resp.Request.URL
	if baseURL == nil {
		baseURL = parsed
	}

	if isM3U8Response(rawURI, resp.Header.Get("Content-Type"), body) {
		if player.IsPreviewStreamHost(baseURL.String()) && strings.Contains(strings.ToLower(castOrigin), "jiunuow.com") {
			applog.Verbosef("[BrowserPlay] WARN 播放列表来自预览 CDN，可能只有约 1 分钟片段")
		}
		body = rewriteM3U8Playlist(body, baseURL, proxyBase, castOrigin)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	} else {
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	if resp.StatusCode == http.StatusPartialContent {
		w.WriteHeader(http.StatusPartialContent)
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			w.Header().Set("Content-Range", cr)
		}
	} else {
		w.WriteHeader(resp.StatusCode)
	}
	_, _ = w.Write(body)
}

func isM3U8Response(rawURI, contentType string, body []byte) bool {
	lower := strings.ToLower(rawURI)
	if strings.Contains(lower, ".m3u8") || strings.Contains(lower, "mpegurl") {
		return true
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") {
		return true
	}
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "#EXTM3U")
}

func rewriteM3U8Playlist(body []byte, base *url.URL, proxyBase, castOrigin string) []byte {
	var out strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out.WriteString("\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "URI=") {
				out.WriteString(rewriteM3U8TagURI(trimmed, base, proxyBase, castOrigin))
			} else {
				out.WriteString(line)
			}
			out.WriteString("\n")
			continue
		}
		abs := resolvePlaylistURL(base, trimmed)
		out.WriteString(buildMediaProxyURL(proxyBase, abs, castOrigin) + "\n")
	}
	return []byte(out.String())
}

func rewriteM3U8TagURI(line string, base *url.URL, proxyBase, castOrigin string) string {
	const key = "URI="
	idx := strings.Index(line, key)
	if idx < 0 {
		return line
	}
	start := idx + len(key)
	quote := byte(0)
	if start < len(line) {
		switch line[start] {
		case '"':
			quote = '"'
			start++
		case '\'':
			quote = '\''
			start++
		}
	}
	end := len(line)
	if quote != 0 {
		if j := strings.IndexByte(line[start:], quote); j >= 0 {
			end = start + j
		}
	} else {
		if j := strings.IndexAny(line[start:], " \t"); j >= 0 {
			end = start + j
		}
	}
	if start >= end {
		return line
	}
	uriPart := line[start:end]
	abs := resolvePlaylistURL(base, uriPart)
	replaced := buildMediaProxyURL(proxyBase, abs, castOrigin)
	return line[:start] + replaced + line[end:]
}

func resolvePlaylistURL(base *url.URL, ref string) string {
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(refURL).String()
}

func truncateURL(raw string) string {
	if len(raw) <= 80 {
		return raw
	}
	return raw[:80] + "..."
}
