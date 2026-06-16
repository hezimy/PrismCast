//go:build windows

package player

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"sync"
	"syscall"
	"unsafe"
)

// Windows GDI constants
const (
	_WM_DESTROY     = 0x0002
	_WM_PAINT       = 0x000F
	_WM_CLOSE       = 0x0010
	_WM_KEYDOWN     = 0x0100
	_WM_SHOW_IMAGE  = 0x8001 // 自定义消息：显示新图片
	_WM_USER        = 0x0400
	_VK_ESCAPE      = 0x1B
	_SRCCOPY        = 0x00CC0020
	_BLACKNESS      = 0x00000042
	_DIB_RGB_COLORS = 0
	_BI_RGB         = 0
	_CW_USEDEFAULT  = 0x80000000
)

// Windows GDI types
type _POINT struct{ X, Y int32 }
type _RECT struct{ Left, Top, Right, Bottom int32 }
type _PAINTSTRUCT struct {
	HDC      uintptr
	FErase   int32
	RcPaint  _RECT
	FErase2  int32
	RcPaint2 [16]byte
}
type _BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// ImageViewer 同进程内的图片查看器窗口
// 使用Windows GDI绘制，单窗口复用，切换图片不关闭/重建窗口
// 内部维护HBITMAP缓存，同一URL不重复解码
type ImageViewer struct {
	hwnd      uintptr
	ready     chan struct{}
	readyOnce sync.Once

	currentIdx int // 当前显示的images索引（viewer goroutine维护）

	// HBITMAP缓存：URL -> {idx, hbmp}（viewer goroutine维护）
	cachedURLs []string
	cachedBmps []uintptr
	urlToIdx   map[string]int
}

// NewImageViewer 创建图片查看器（不创建窗口，首次Show时创建）
func NewImageViewer() *ImageViewer {
	return &ImageViewer{
		ready:    make(chan struct{}),
		urlToIdx: make(map[string]int),
	}
}

// Show 在查看器窗口中显示图片（线程安全）
// 首次调用会创建窗口，后续调用在同一窗口内切换图片
// url用于缓存索引，data为图片文件的原始字节
func (v *ImageViewer) Show(data []byte, url string) error {
	v.readyOnce.Do(func() { go v.run() })
	<-v.ready

	if v.hwnd == 0 {
		return fmt.Errorf("viewer window not ready")
	}

	// 通过PostMessage将数据指针发送到viewer goroutine
	// goroutine负责解码、创建HBITMAP、缓存、显示
	msg := &_showMsg{
		data:  data,
		url:   url,
		reply: make(chan error, 1),
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	postMsg := user32.NewProc("PostMessageW")
	postMsg.Call(v.hwnd, _WM_SHOW_IMAGE, uintptr(unsafe.Pointer(msg)), 0)

	return <-msg.reply
}

// _showMsg 通过PostMessage传递的消息数据
type _showMsg struct {
	data  []byte
	url   string
	reply chan error
}

// run 在独立goroutine中运行窗口消息循环
func (v *ImageViewer) run() {
	user32 := syscall.NewLazyDLL("user32.dll")
	registerClass := user32.NewProc("RegisterClassExW")
	createWindow := user32.NewProc("CreateWindowExW")
	getMessage := user32.NewProc("GetMessageW")
	translateMessage := user32.NewProc("TranslateMessage")
	dispatchMessage := user32.NewProc("DispatchMessageW")
	showWindow := user32.NewProc("ShowWindow")
	defWindowProc := user32.NewProc("DefWindowProcW")

	className, _ := syscall.UTF16PtrFromString("PrismCastImgViewer")
	windowTitle, _ := syscall.UTF16PtrFromString("PrismCast - 图片查看")

	wndProc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		switch msg {
		case _WM_SHOW_IMAGE:
			return v.handleShowImage(hwnd, wp, defWindowProc)
		case _WM_PAINT:
			return v.handlePaint(hwnd, defWindowProc)
		case _WM_CLOSE:
			// 隐藏而非销毁，窗口可复用
			showWindow.Call(hwnd, 0) // SW_HIDE
			return 0
		case _WM_KEYDOWN:
			if wp == _VK_ESCAPE {
				showWindow.Call(hwnd, 0)
				return 0
			}
		case _WM_DESTROY:
			v.releaseAllBitmaps()
			return 0
		}
		ret, _, _ := defWindowProc.Call(hwnd, msg, wp, lp)
		return ret
	})

	type _WNDCLASSEX struct {
		CbSize        uint32
		Style         uint32
		LpfnWndProc   uintptr
		CbClsExtra    int32
		CbWndExtra    int32
		HInstance     uintptr
		HIcon         uintptr
		HCursor       uintptr
		HbrBackground uintptr
		LpszMenuName  *uint16
		LpszClassName *uint16
		HIconSm       uintptr
	}

	wc := _WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(_WNDCLASSEX{})),
		LpfnWndProc:   wndProc,
		HCursor:       0,
		HbrBackground: 0,
		LpszClassName: className,
	}
	wc.HbrBackground, _, _ = syscall.NewLazyDLL("gdi32.dll").NewProc("GetStockObject").Call(4) // BLACK_BRUSH

	registerClass.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := createWindow.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		0x00CF0000, // WS_OVERLAPPEDWINDOW
		_CW_USEDEFAULT, _CW_USEDEFAULT,
		1024, 768,
		0, 0, 0, 0,
	)

	if hwnd == 0 {
		log.Printf("[Viewer] 窗口创建失败")
		close(v.ready)
		return
	}

	v.hwnd = hwnd
	showWindow.Call(hwnd, 1) // SW_SHOWNORMAL
	close(v.ready)

	log.Printf("[Viewer] 图片查看器窗口已创建")

	var msg [20]uintptr // MSG结构体足够大
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if ret == 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

// handleShowImage 处理WM_SHOW_IMAGE消息（在viewer goroutine中执行）
func (v *ImageViewer) handleShowImage(hwnd uintptr, wp uintptr, defWindowProc *syscall.LazyProc) uintptr {
	msg := (*_showMsg)(unsafe.Pointer(wp))

	// 检查HBITMAP缓存
	if idx, ok := v.urlToIdx[msg.url]; ok && idx < len(v.cachedBmps) {
		// 命中缓存，直接复用
		v.currentIdx = idx
		log.Printf("[Viewer] 缓存命中: %s (idx=%d)", msg.url, idx)
		msg.reply <- nil
		// 触发重绘
		user32 := syscall.NewLazyDLL("user32.dll")
		user32.NewProc("InvalidateRect").Call(hwnd, 0, 1)
		user32.NewProc("ShowWindow").Call(hwnd, 1)
		return 0
	}

	// 解码图片
	img, _, err := image.Decode(bytes.NewReader(msg.data))
	if err != nil {
		log.Printf("[Viewer] 图片解码失败: %v", err)
		msg.reply <- fmt.Errorf("decode: %w", err)
		return 0
	}

	// 创建HBITMAP
	hbmp := createHBitmapFromImage(img)
	if hbmp == 0 {
		log.Printf("[Viewer] 创建位图失败")
		msg.reply <- fmt.Errorf("create bitmap failed")
		return 0
	}

	// 加入缓存
	idx := len(v.cachedBmps)
	v.cachedURLs = append(v.cachedURLs, msg.url)
	v.cachedBmps = append(v.cachedBmps, hbmp)
	v.urlToIdx[msg.url] = idx
	v.currentIdx = idx

	log.Printf("[Viewer] 新图片已缓存 (idx=%d, %dx%d): %s", idx, img.Bounds().Dx(), img.Bounds().Dy(), msg.url)
	msg.reply <- nil

	// 触发重绘并显示窗口
	user32 := syscall.NewLazyDLL("user32.dll")
	user32.NewProc("InvalidateRect").Call(hwnd, 0, 1)
	user32.NewProc("ShowWindow").Call(hwnd, 1)
	return 0
}

// handlePaint 处理WM_PAINT消息（在viewer goroutine中执行）
func (v *ImageViewer) handlePaint(hwnd uintptr, defWindowProc *syscall.LazyProc) uintptr {
	user32 := syscall.NewLazyDLL("user32.dll")
	gdi32 := syscall.NewLazyDLL("gdi32.dll")

	var ps _PAINTSTRUCT
	hdc, _, _ := user32.NewProc("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return 0
	}
	defer user32.NewProc("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	// 获取客户区大小
	var rc _RECT
	user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top

	// 黑色背景
	gdi32.NewProc("PatBlt").Call(hdc, 0, 0, uintptr(w), uintptr(h), _BLACKNESS)

	// 绘制当前图片（等比缩放居中）
	if v.currentIdx >= 0 && v.currentIdx < len(v.cachedBmps) {
		hbmp := v.cachedBmps[v.currentIdx]

		memDC, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(hdc)
		defer gdi32.NewProc("DeleteDC").Call(memDC)

		oldBmp, _, _ := gdi32.NewProc("SelectObject").Call(memDC, hbmp)
		defer gdi32.NewProc("SelectObject").Call(memDC, oldBmp)

		// 获取位图尺寸
		type _BITMAP struct {
			BmType       int32
			BmWidth      int32
			BmHeight     int32
			BmWidthBytes int32
			BmPlanes     uint16
			BmBitsPixel  uint16
			BmBits       uintptr
		}
		var bm _BITMAP
		gdi32.NewProc("GetObjectW").Call(hbmp, uintptr(unsafe.Sizeof(bm)), uintptr(unsafe.Pointer(&bm)))
		bw := bm.BmWidth
		bh := bm.BmHeight

		if bw > 0 && bh > 0 && w > 0 && h > 0 {
			// 等比缩放
			scale := float64(w) / float64(bw)
			if s2 := float64(h) / float64(bh); s2 < scale {
				scale = s2
			}
			if scale > 1.0 {
				scale = 1.0 // 不放大
			}
			dw := int32(float64(bw) * scale)
			dh := int32(float64(bh) * scale)
			dx := (w - dw) / 2
			dy := (h - dh) / 2

			gdi32.NewProc("SetStretchBltMode").Call(hdc, 4) // HALFTONE
			gdi32.NewProc("StretchBlt").Call(hdc,
				uintptr(dx), uintptr(dy), uintptr(dw), uintptr(dh),
				memDC, 0, 0, uintptr(bw), uintptr(bh), _SRCCOPY)
		}
	}

	return 0
}

// releaseAllBitmaps 释放所有缓存的GDI位图资源（viewer goroutine中调用）
func (v *ImageViewer) releaseAllBitmaps() {
	gdi32 := syscall.NewLazyDLL("gdi32.dll")
	for _, hbmp := range v.cachedBmps {
		if hbmp != 0 {
			gdi32.NewProc("DeleteObject").Call(hbmp)
		}
	}
	v.cachedBmps = nil
	v.cachedURLs = nil
	v.urlToIdx = make(map[string]int)
	v.currentIdx = -1
	log.Printf("[Viewer] 已释放所有位图缓存")
}

// Close 关闭查看器窗口并释放资源
func (v *ImageViewer) Close() {
	if v.hwnd == 0 {
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	// PostMessage WM_CLOSE到viewer goroutine
	user32.NewProc("PostMessageW").Call(v.hwnd, _WM_CLOSE, 0, 0)
	// 释放所有位图（通过PostMessage异步执行更安全，但Close通常在退出时调用，直接释放也可）
	// 注意：如果viewer goroutine仍在使用这些HBITMAP，可能有竞态
	// 安全做法是PostMessage一个自定义清理消息
	v.hwnd = 0
}

// createHBitmapFromImage 将Go image.Image转换为Windows HBITMAP
func createHBitmapFromImage(img image.Image) uintptr {
	gdi32 := syscall.NewLazyDLL("gdi32.dll")

	bounds := img.Bounds()
	width := int32(bounds.Dx())
	height := int32(bounds.Dy())
	if width <= 0 || height <= 0 {
		return 0
	}

	// DIB行宽必须4字节对齐
	stride := int(((width*32 + 31) / 32) * 4)
	pixels := make([]byte, stride*int(height))

	for y := 0; y < int(height); y++ {
		// DIB bottom-up: 行0 = 图片底部
		rowOff := (int(height) - 1 - y) * int(stride)
		for x := 0; x < int(width); x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			off := rowOff + x*4
			pixels[off] = byte(b >> 8)   // Blue
			pixels[off+1] = byte(g >> 8) // Green
			pixels[off+2] = byte(r >> 8) // Red
			pixels[off+3] = 0            // Reserved
		}
	}

	bi := _BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(_BITMAPINFOHEADER{})),
		BiWidth:       width,
		BiHeight:      height,
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: _BI_RGB,
	}

	var bits uintptr
	hbmp, _, _ := gdi32.NewProc("CreateDIBSection").Call(
		0,
		uintptr(unsafe.Pointer(&bi)),
		_DIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbmp == 0 {
		return 0
	}

	// 复制像素数据到DIB
	dst := unsafe.Slice((*byte)(unsafe.Pointer(bits)), len(pixels))
	copy(dst, pixels)

	return hbmp
}
