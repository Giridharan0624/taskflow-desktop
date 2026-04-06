//go:build windows

package monitor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32ForScreenshot       = windows.NewLazySystemDLL("gdi32.dll")
	pCreateCompatibleDCSS    = gdi32ForScreenshot.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmapSS = gdi32ForScreenshot.NewProc("CreateCompatibleBitmap")
	pSelectObjectSS          = gdi32ForScreenshot.NewProc("SelectObject")
	pBitBlt                  = gdi32ForScreenshot.NewProc("BitBlt")
	pDeleteDCSS              = gdi32ForScreenshot.NewProc("DeleteDC")
	pDeleteObjectSS          = gdi32ForScreenshot.NewProc("DeleteObject")
	pGetDIBits               = gdi32ForScreenshot.NewProc("GetDIBits")

	user32ForScreenshot  = windows.NewLazySystemDLL("user32.dll")
	pGetDCSS             = user32ForScreenshot.NewProc("GetDC")
	pReleaseDCSS         = user32ForScreenshot.NewProc("ReleaseDC")
	pGetSystemMetrics    = user32ForScreenshot.NewProc("GetSystemMetrics")
	pGetDesktopWindow    = user32ForScreenshot.NewProc("GetDesktopWindow")
	pOpenInputDesktop    = user32ForScreenshot.NewProc("OpenInputDesktop")
	pCloseDesktop        = user32ForScreenshot.NewProc("CloseDesktop")
)

const (
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
	SRCCOPY     = 0x00CC0020
	DIB_RGB_COLORS = 0
	BI_RGB         = 0
)

// BITMAPINFOHEADER for GetDIBits
type BITMAPINFOHEADER struct {
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

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// ScreenshotCapture handles taking screenshots via Win32 APIs.
type ScreenshotCapture struct{}

// NewScreenshotCapture creates a new screenshot capture instance.
func NewScreenshotCapture() *ScreenshotCapture {
	return &ScreenshotCapture{}
}

// IsScreenLocked checks if the Windows workstation is locked
// by attempting to open the input desktop. If the secure desktop
// (lock screen / UAC) is active, this call fails — meaning the
// screen is truly locked. Idle time alone is NOT a reliable signal.
func (sc *ScreenshotCapture) IsScreenLocked() bool {
	hDesk, _, _ := pOpenInputDesktop.Call(0, 0, 0) // DESKTOP_NONE
	if hDesk == 0 {
		return true // secure desktop active → locked
	}
	pCloseDesktop.Call(hDesk)
	return false
}

// CaptureScreen takes a screenshot and returns it as JPEG bytes.
// Returns nil if screen is locked or capture fails.
func (sc *ScreenshotCapture) CaptureScreen(quality int) ([]byte, error) {
	if sc.IsScreenLocked() {
		return nil, fmt.Errorf("screen is locked")
	}

	// Get screen dimensions
	width, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
	height, _, _ := pGetSystemMetrics.Call(SM_CYSCREEN)

	if width == 0 || height == 0 {
		return nil, fmt.Errorf("failed to get screen dimensions")
	}

	w := int(width)
	h := int(height)

	// Get the desktop DC
	desktopHwnd, _, _ := pGetDesktopWindow.Call()
	hdcScreen, _, _ := pGetDCSS.Call(desktopHwnd)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("failed to get desktop DC")
	}
	defer pReleaseDCSS.Call(desktopHwnd, hdcScreen)

	// Create compatible DC and bitmap
	hdcMem, _, _ := pCreateCompatibleDCSS.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("failed to create compatible DC")
	}
	defer pDeleteDCSS.Call(hdcMem)

	hBitmap, _, _ := pCreateCompatibleBitmapSS.Call(hdcScreen, uintptr(w), uintptr(h))
	if hBitmap == 0 {
		return nil, fmt.Errorf("failed to create compatible bitmap")
	}
	defer pDeleteObjectSS.Call(hBitmap)

	// Select bitmap into memory DC
	pSelectObjectSS.Call(hdcMem, hBitmap)

	// BitBlt — copy screen to memory DC
	ret, _, _ := pBitBlt.Call(
		hdcMem, 0, 0, uintptr(w), uintptr(h),
		hdcScreen, 0, 0,
		SRCCOPY,
	)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	// Read bitmap pixels via GetDIBits
	bmi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(w),
			BiHeight:      -int32(h), // Negative = top-down
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: BI_RGB,
		},
	}

	pixels := make([]byte, w*h*4)
	ret, _, _ = pGetDIBits.Call(
		hdcMem, hBitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bmi)),
		DIB_RGB_COLORS,
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	// Convert BGRA pixels to Go image.RGBA
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			// Windows uses BGRA, Go uses RGBA
			img.Pix[(y*w+x)*4+0] = pixels[i+2] // R
			img.Pix[(y*w+x)*4+1] = pixels[i+1] // G
			img.Pix[(y*w+x)*4+2] = pixels[i+0] // B
			img.Pix[(y*w+x)*4+3] = 255          // A
		}
	}

	// Scale down to 50% for smaller file size
	scaledW := w / 2
	scaledH := h / 2
	scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	for y := 0; y < scaledH; y++ {
		for x := 0; x < scaledW; x++ {
			srcX := x * 2
			srcY := y * 2
			srcIdx := (srcY*w + srcX) * 4
			dstIdx := (y*scaledW + x) * 4
			scaled.Pix[dstIdx+0] = img.Pix[srcIdx+0]
			scaled.Pix[dstIdx+1] = img.Pix[srcIdx+1]
			scaled.Pix[dstIdx+2] = img.Pix[srcIdx+2]
			scaled.Pix[dstIdx+3] = 255
		}
	}

	// Encode to JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("JPEG encode failed: %w", err)
	}

	log.Printf("Screenshot captured: %dx%d → %dx%d, %d KB JPEG",
		w, h, scaledW, scaledH, buf.Len()/1024)

	return buf.Bytes(), nil
}

// CaptureScreenDefault takes a screenshot with default quality (60).
func (sc *ScreenshotCapture) CaptureScreenDefault() ([]byte, error) {
	return sc.CaptureScreen(60)
}

// ShowNotification logs a notification (actual display is via tray balloon in activity.go).
func ShowNotification(title, message string) {
	log.Printf("Notification: %s - %s", title, message)
}
