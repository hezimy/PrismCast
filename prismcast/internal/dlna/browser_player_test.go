package dlna

import (
	"strings"
	"testing"
)

func TestRenderBrowserPlayHTML(t *testing.T) {
	proxyURL := "http://127.0.0.1:8765/media-proxy?uri=https%3A%2F%2Fexample.com%2Findex.m3u8"
	html := string(renderBrowserPlayHTML(proxyURL))

	if strings.Contains(html, proxyURLPlaceholder) {
		t.Fatal("proxy URL placeholder was not replaced")
	}
	if !strings.Contains(html, proxyURL) {
		t.Fatalf("rendered html missing proxy url: %s", html)
	}
	if !strings.Contains(html, "const src = \""+proxyURL+"\"") {
		t.Fatalf("rendered html missing js src assignment")
	}
	if strings.Contains(html, "%!(MISSING)") {
		t.Fatalf("fmt corruption leaked into html: %s", html)
	}
}
