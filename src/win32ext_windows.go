package main

// Дополнительные функции Windows для версии 1.3: прозрачность окна, отслеживание мыши,
// стандартные элементы окна настроек и шифрование ключа ИИ (DPAPI).

import (
	"encoding/base64"
	"syscall"
	"unsafe"
)

var (
	crypt32 = syscall.NewLazyDLL("crypt32.dll")

	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	pTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	pSetCursor                  = user32.NewProc("SetCursor")
	pIsDialogMessageW           = user32.NewProc("IsDialogMessageW")
	pGetAncestor                = user32.NewProc("GetAncestor")
	pEnableWindow               = user32.NewProc("EnableWindow")
	pGetSysColorBrush           = user32.NewProc("GetSysColorBrush")
	pAdjustWindowRectEx         = user32.NewProc("AdjustWindowRectEx")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pSetCapture                 = user32.NewProc("SetCapture")
	pReleaseCapture             = user32.NewProc("ReleaseCapture")
	pPolygon                    = gdi32.NewProc("Polygon")

	pSetWindowSubclass = comctl32.NewProc("SetWindowSubclass")
	pDefSubclassProc   = comctl32.NewProc("DefSubclassProc")

	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

const (
	WS_EX_LAYERED    = 0x00080000
	WS_EX_TOOLWINDOW = 0x00000080
	LWA_ALPHA        = 0x2
	GWL_EXSTYLE      = ^uintptr(19) // -20

	WS_CAPTION       = 0x00C00000
	WS_SYSMENU       = 0x00080000
	WS_POPUP         = 0x80000000
	WS_DISABLED      = 0x08000000
	ES_PASSWORD      = 0x0020
	BS_PUSHBUTTON    = 0x0000
	BS_DEFPUSHBTN    = 0x0001
	SS_NOPREFIX      = 0x0080
	CBS_DROPDOWNLIST = 0x0003
	CBS_HASSTRINGS   = 0x0200
	CB_ADDSTRING     = 0x0143
	CB_GETCURSEL     = 0x0147
	CB_SETCURSEL     = 0x014E
	CBN_SELCHANGE    = 1
	TBS_HORZ         = 0x0000
	TBS_NOTICKS      = 0x0010
	TBM_SETRANGE     = WM_USER + 6
	TBM_SETPOS       = WM_USER + 5
	TBM_GETPOS       = WM_USER
	WM_HSCROLL       = 0x0114
	WM_MOUSEMOVE     = 0x0200
	WM_LBUTTONDOWN   = 0x0201
	WM_MOUSELEAVE    = 0x02A3
	WM_SETCURSOR     = 0x0020
	WM_NCDESTROY     = 0x0082
	WM_CTLCOLORBTN   = 0x0135
	WM_ACTIVATE      = 0x0006
	TME_LEAVE        = 0x2
	LB_ITEMFROMPOINT = 0x01A9
	LB_GETITEMRECT   = 0x0198
	LB_GETCOUNT      = 0x018B
	LB_GETTOPINDEX   = 0x018E
	IDC_HAND         = 32649
	GA_ROOT          = 2
	VK_TAB           = 0x09
)

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

func trackLeave(h uintptr) {
	t := TRACKMOUSEEVENT{DwFlags: TME_LEAVE, HwndTrack: h}
	t.CbSize = uint32(unsafe.Sizeof(t))
	pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
}

func setAlpha(h uintptr, a int) {
	if a < 0 {
		a = 0
	}
	if a > 255 {
		a = 255
	}
	pSetLayeredWindowAttributes.Call(h, 0, uintptr(a), LWA_ALPHA)
}

func handCursor() {
	c, _, _ := pLoadCursorW.Call(0, IDC_HAND)
	pSetCursor.Call(c)
}

func arrowCursor() {
	c, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	pSetCursor.Call(c)
}

// ---- цвета ----

func rgbOf(c uintptr) (int, int, int) {
	return int(c & 0xFF), int((c >> 8) & 0xFF), int((c >> 16) & 0xFF)
}

// mix смешивает два цвета: t=0 — a, t=1 — b.
func mix(a, b uintptr, t float64) uintptr {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := rgbOf(a)
	br, bg, bb := rgbOf(b)
	f := func(x, y int) int { return x + int(float64(y-x)*t+0.5) }
	return RGB(f(ar, br), f(ag, bg), f(ab, bb))
}

func easeOut(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	u := 1 - t
	return 1 - u*u*u
}

// ---- DPAPI: ключ ИИ хранится зашифрованным, расшифровать может только этот пользователь Windows ----

type dataBlob struct {
	CbData uint32
	PbData *byte
}

func protect(s string) string {
	if s == "" {
		return ""
	}
	b := []byte(s)
	in := dataBlob{CbData: uint32(len(b)), PbData: &b[0]}
	var out dataBlob
	r, _, _ := pCryptProtectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0x1 /*UI_FORBIDDEN*/, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return ""
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.PbData)))
	enc := unsafe.Slice(out.PbData, out.CbData)
	return "dpapi:" + base64.StdEncoding.EncodeToString(enc)
}

func unprotect(s string) string {
	const pre = "dpapi:"
	if len(s) < len(pre) || s[:len(pre)] != pre {
		return s // ключ вписан вручную открытым текстом
	}
	raw, err := base64.StdEncoding.DecodeString(s[len(pre):])
	if err != nil || len(raw) == 0 {
		return ""
	}
	in := dataBlob{CbData: uint32(len(raw)), PbData: &raw[0]}
	var out dataBlob
	r, _, _ := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0x1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return ""
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.PbData)))
	return string(unsafe.Slice(out.PbData, out.CbData))
}
