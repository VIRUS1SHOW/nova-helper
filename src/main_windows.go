package main

// Nova Helper — окно с правилами, горячая клавиша и значок в трее.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	appTitle      = "Nova Helper"
	className     = "NovaHelperWnd"
	autostartName = "Nova Helper"

	idEdit = 101
	idList = 102
	idRich = 103

	tmrSearch  = 1
	tmrSection = 2
	tmrFlash   = 3
	tmrUpdate  = 4

	wmUpdate   = WM_APP + 5
	wmAppReady = WM_APP + 6
	wmAskApp   = WM_APP + 7
	wmAI       = WM_APP + 8

	wmTray         = WM_APP + 1
	wmShowExisting = WM_APP + 2
	hotkeyID       = 1

	modeDoc    = 0
	modeSearch = 1
	modeFavs   = 2
	modeAI     = 3

	maxResults = 150
)

var (
	clrBg      = RGB(13, 15, 20)
	clrPanel   = RGB(20, 24, 33)
	clrPanel2  = RGB(27, 32, 44)
	clrLine    = RGB(38, 44, 58)
	clrText    = RGB(231, 233, 238)
	clrMuted   = RGB(138, 147, 164)
	clrFaint   = RGB(93, 101, 118)
	clrAccent  = RGB(150, 136, 255)
	clrActive  = RGB(37, 34, 66)
	clrStatusT = RGB(110, 118, 134)
	clrHover   = RGB(30, 33, 52)
)

var app struct {
	hwnd, edit, list, rich uintptr
	hinst                  uintptr
	iconBig, iconSmall     uintptr

	fUI, fSmall, fHeader, fSearch, fBrand, fBrandSmall, fStatus, fLogo uintptr
	fDoc, fIcon, fPill                                                 uintptr
	brBg, brPanel, brPanel2, brActive, brAccent                        uintptr
	penLine, penAccent                                                 uintptr

	scale  float64
	dir    string
	docs   []*Doc
	errs   []string
	favs   map[string]bool
	mode   int
	cur    int
	curSec int
	rend   Rendered
	items  []listItem
	status string

	prevFg       uintptr
	hkMods, hkVK uintptr
	hkLabel      string
	onTop        bool
	zoom         int
	taskbarMsg   uint32
	closeHinted  bool
	flashStart   int

	checking   bool
	pendingApp *AppInfo
	askedApp   bool
	restart    bool
	exePath    string

	// оформление
	accent  int
	opacity int
	anim    bool

	// режим поверх игры
	overlay   bool
	ovPassive bool
	ovCompact bool
	ovOpacity int

	// меню слева и анимации
	open      map[string]bool
	openSet   bool
	appear    map[string]float64
	hover     map[string]float64
	openAmt   map[string]float64
	hoverKey  string
	animOn    bool
	hoverTop  int // 1 — кнопка ИИ, 2 — настройки
	aiHover   float64
	gearHover float64
	gearSpin  float64
	focusAmt  float64
	fade      int
	fadeT     float64
	rcAI      RECT
	rcGear    RECT

	// ИИ
	ai         AIConfig
	aiMode     bool
	aiBusy     bool
	aiReq      int
	dots       float64
	statusDots float64
	aiView     aiState
	dlg        uintptr
}

func s(px float64) int32 { return int32(px*app.scale + 0.5) }

func main() {
	runtime.LockOSThread()

	exe, _ := os.Executable()
	app.dir = filepath.Dir(exe)
	app.exePath = exe
	startHidden := len(os.Args) > 1 && os.Args[1] == "--tray"
	justUpdated := len(os.Args) > 1 && os.Args[1] == "--updated"

	// одна копия программы (после обновления ждём, пока старая версия закроется)
	var e error
	for i := 0; ; i++ {
		var hm uintptr
		hm, _, e = pCreateMutexW.Call(0, 0, u16p("Local\\NovaHelperMutex"))
		if !justUpdated || e != syscall.Errno(ERROR_ALREADY_EXISTS) || i > 60 {
			break
		}
		pCloseHandle.Call(hm)
		time.Sleep(250 * time.Millisecond)
	}
	if e == syscall.Errno(ERROR_ALREADY_EXISTS) {
		if h, _, _ := pFindWindowW.Call(u16p(className), 0); h != 0 {
			PostMessage(h, wmShowExisting, 0, 0)
		}
		return
	}

	app.appear = map[string]float64{}
	app.hover = map[string]float64{}
	app.openAmt = map[string]float64{}
	loadSettings()
	loadState()
	loadFavs()
	app.docs, app.errs = LoadDocs(filepath.Join(app.dir, "rules"))
	if !app.openSet {
		app.open["Основные"] = true
	}
	if app.cur < len(app.docs) {
		app.open[app.docs[app.cur].Group] = true
	}
	for g, on := range app.open {
		app.openAmt[g] = b2f(on)
	}

	icc := INITCOMMONCONTROLSEX{DwSize: 8, DwICC: 0x4000 | 0x00FF}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	pLoadLibraryW.Call(u16p("Msftedit.dll"))

	app.hinst, _, _ = pGetModuleHandleW.Call(0)
	pExtractIconExW.Call(u16p(exe), 0, uintptr(unsafe.Pointer(&app.iconBig)), uintptr(unsafe.Pointer(&app.iconSmall)), 1)

	hdc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, LOGPIXELSY)
	pReleaseDC.Call(0, hdc)
	app.scale = float64(dpi) / 96
	if app.scale < 1 {
		app.scale = 1
	}

	makeGDI()

	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	wc := WNDCLASSEXW{
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     app.hinst,
		HIcon:         app.iconBig,
		HIconSm:       app.iconSmall,
		HCursor:       cursor,
		HbrBackground: app.brBg,
		LpszClassName: u16(className),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	app.taskbarMsg = uint32(must(pRegisterWindowMessageW.Call(u16p("TaskbarCreated"))))

	w, h := s(860), s(900)
	cx, _, _ := pGetSystemMetrics.Call(0)
	cy, _, _ := pGetSystemMetrics.Call(1)
	x, y := (int32(cx)-w)/2, (int32(cy)-h)/2
	app.hwnd, _, _ = pCreateWindowExW.Call(WS_EX_LAYERED, u16p(className), u16p(appTitle),
		WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, app.hinst, 0)

	// тёмный заголовок окна (Windows 10 20H1+ и 11)
	dark := int32(1)
	if r, _, _ := pDwmSetWindowAttribute.Call(app.hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4); r != 0 {
		pDwmSetWindowAttribute.Call(app.hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)
	}

	setAlpha(app.hwnd, targetAlpha())
	createChildren()
	hookList()
	restorePlacement()
	addTray(startHidden)
	registerHotkey()

	if len(app.docs) > 0 {
		if app.cur >= len(app.docs) {
			app.cur = 0
		}
		showDoc(app.cur)
	} else {
		app.mode = modeDoc
		setRTF(RenderList("Правила не загрузились", nil, nil, app.favs,
			[]string{"Проверьте, что рядом с программой есть папка rules с файлами правил (.txt)."}, app.errs))
		rebuildList()
	}

	pSetTimer.Call(app.hwnd, tmrSection, 250, 0)
	pSetTimer.Call(app.hwnd, tmrUpdate, 3000, 0) // первая проверка обновлений через 3 секунды
	go CleanupOldApp(exe)
	if justUpdated {
		balloon(appTitle, "Программа обновлена до версии "+appVersion+".")
	}
	if !startHidden {
		showWindow()
	}

	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if app.dlg != 0 {
			root, _, _ := pGetAncestor.Call(msg.Hwnd, GA_ROOT)
			if root == app.dlg {
				if msg.Message == WM_KEYDOWN && msg.WParam == VK_ESCAPE {
					closeSettings(false)
					continue
				}
				if r, _, _ := pIsDialogMessageW.Call(app.dlg, uintptr(unsafe.Pointer(&msg))); r != 0 {
					continue
				}
				pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
				pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
				continue
			}
		}
		if msg.Message == WM_KEYDOWN && handleKey(msg.WParam) {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if app.restart {
		exec.Command(app.exePath, "--updated").Start()
	}
}

func must(r uintptr, _ uintptr, _ error) uintptr { return r }

func makeGDI() {
	app.fUI = createFont(int(s(14.5)), FW_SEMIBOLD, "Segoe UI")
	app.fSmall = createFont(int(s(13)), FW_NORMAL, "Segoe UI")
	app.fHeader = createFont(int(s(11)), FW_BOLD, "Segoe UI")
	app.fSearch = createFont(int(s(15.5)), FW_NORMAL, "Segoe UI")
	app.fBrand = createFont(int(s(15)), FW_BOLD, "Segoe UI")
	app.fBrandSmall = createFont(int(s(11.5)), FW_NORMAL, "Segoe UI")
	app.fStatus = createFont(int(s(12)), FW_NORMAL, "Segoe UI")
	app.fLogo = createFont(int(s(15)), 800, "Segoe UI")
	app.fDoc = createFont(int(s(13.5)), FW_NORMAL, "Segoe UI")
	app.fIcon = createFont(int(s(15)), FW_NORMAL, "Segoe UI Symbol")
	app.fPill = createFont(int(s(13)), FW_SEMIBOLD, "Segoe UI")
	app.brBg, _, _ = pCreateSolidBrush.Call(clrBg)
	app.brPanel, _, _ = pCreateSolidBrush.Call(clrPanel)
	app.brPanel2, _, _ = pCreateSolidBrush.Call(clrPanel2)
	app.penLine, _, _ = pCreatePen.Call(0, uintptr(s(1)), clrLine)
	applyAccent(app.accent)
}

func createChildren() {
	app.edit, _, _ = pCreateWindowExW.Call(0, u16p("EDIT"), 0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL, 0, 0, 10, 10, app.hwnd, idEdit, app.hinst, 0)
	SendMessage(app.edit, WM_SETFONT, app.fSearch, 1)
	SendMessage(app.edit, EM_SETCUEBANNER, 1, u16p(searchCue))

	app.list, _, _ = pCreateWindowExW.Call(0, u16p("LISTBOX"), 0,
		WS_CHILD|WS_VISIBLE|WS_VSCROLL|LBS_NOTIFY|LBS_OWNERDRAWVARIABLE|LBS_NOINTEGRALHEIGHT,
		0, 0, 10, 10, app.hwnd, idList, app.hinst, 0)
	pSetWindowTheme.Call(app.list, u16p("DarkMode_Explorer"), 0)

	app.rich, _, _ = pCreateWindowExW.Call(0, u16p("RICHEDIT50W"), 0,
		WS_CHILD|WS_VISIBLE|WS_VSCROLL|ES_MULTILINE|ES_READONLY|ES_NOHIDESEL|ES_AUTOVSCROLL,
		0, 0, 10, 10, app.hwnd, idRich, app.hinst, 0)
	if app.rich == 0 {
		messageBox(0, "Не удалось создать окно текста (RichEdit). Нужен Windows 7 или новее.", appTitle, MB_ICONWARNING)
	}
	pSetWindowTheme.Call(app.rich, u16p("DarkMode_Explorer"), 0)
	SendMessage(app.rich, EM_EXLIMITTEXT, 0, 0x7FFFFFFE)
	SendMessage(app.rich, EM_SETBKGNDCOLOR, 0, clrBg)
	SendMessage(app.rich, EM_AUTOURLDETECT, 1, 0)
	SendMessage(app.rich, EM_SETEVENTMASK, 0, ENM_LINK|ENM_MOUSEEVENTS)
	layout()
}

// ------------------------------------------------------------------ раскладка

func sideWidth(w int32) int32 {
	sw := s(250)
	if w < s(760) {
		sw = s(190)
	}
	return sw
}

const (
	topH    = 58
	statusH = 26
)

func layout() {
	var rc RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.Right, rc.Bottom
	sw, brand := leftWidths(w)
	th, sh := s(topH), s(statusH)
	boxX, boxY, boxH := brand+s(16), s(12), s(34)
	gw, aw := s(34), s(132)
	if w-brand < s(520) {
		aw = s(40) // узкое окно — только значок
	}
	app.rcGear = RECT{w - s(16) - gw, boxY, w - s(16), boxY + boxH}
	app.rcAI = RECT{app.rcGear.Left - s(8) - aw, boxY, app.rcGear.Left - s(8), boxY + boxH}
	boxR := app.rcAI.Left - s(10)
	editH := s(22)
	pMoveWindow.Call(app.edit, uintptr(boxX+s(34)), uintptr(boxY+(boxH-editH)/2), uintptr(maxI(boxR-boxX-s(44), s(50))), uintptr(editH), 1)
	richX := sw + 1
	if sw == 0 {
		richX = 0
		pShowWindow.Call(app.list, SW_HIDE)
	} else {
		pMoveWindow.Call(app.list, 0, uintptr(th), uintptr(sw), uintptr(maxI(h-th-sh, 10)), 1)
		pShowWindow.Call(app.list, SW_SHOWNA)
	}
	pMoveWindow.Call(app.rich, uintptr(richX), uintptr(th), uintptr(maxI(w-richX, 10)), uintptr(maxI(h-th-sh, 10)), 1)
	var rr RECT
	pGetClientRect.Call(app.rich, uintptr(unsafe.Pointer(&rr)))
	rr.Left += s(26)
	rr.Top += s(16)
	rr.Right -= s(22)
	SendMessage(app.rich, EM_SETRECT, 0, uintptr(unsafe.Pointer(&rr)))
	pInvalidateRect.Call(app.hwnd, 0, 1)
}

func maxI(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func paintMain() {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(app.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(app.hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.Right, rc.Bottom
	sw, brand := leftWidths(w)
	th, sh := s(topH), s(statusH)

	top := RECT{0, 0, w, th}
	fillRect(hdc, &top, app.brPanel)
	line := RECT{0, th - s(1), w, th}
	fillRect(hdc, &line, mustBrush(clrLine))
	if sw > 0 {
		vline := RECT{sw, th, sw + 1, h - sh}
		fillRect(hdc, &vline, mustBrush(clrLine))
	}
	st := RECT{0, h - sh, w, h}
	fillRect(hdc, &st, app.brPanel)
	stl := RECT{0, h - sh, w, h - sh + 1}
	fillRect(hdc, &stl, mustBrush(clrLine))

	pSetBkMode.Call(hdc, TRANSPARENT)

	// логотип
	pSelectObject.Call(hdc, app.brAccent)
	pSelectObject.Call(hdc, app.penAccent)
	lx, ly, ls := s(16), (th-s(30))/2, s(30)
	pRoundRect.Call(hdc, uintptr(lx), uintptr(ly), uintptr(lx+ls), uintptr(ly+ls), uintptr(s(9)), uintptr(s(9)))
	pSelectObject.Call(hdc, app.fLogo)
	pSetTextColor.Call(hdc, clrBg)
	lr := RECT{lx, ly, lx + ls, ly + ls}
	drawText(hdc, "N", &lr, 0x0001|DT_VCENTER|DT_SINGLELINE) // DT_CENTER

	// название
	if brand > s(200) {
		pSelectObject.Call(hdc, app.fBrand)
		pSetTextColor.Call(hdc, clrText)
		tr := RECT{lx + ls + s(10), ly - s(1), sw, ly + s(17)}
		drawText(hdc, "Nova Helper", &tr, DT_SINGLELINE|DT_END_ELLIPSIS|DT_NOPREFIX)
		pSelectObject.Call(hdc, app.fBrandSmall)
		pSetTextColor.Call(hdc, clrMuted)
		tr2 := RECT{lx + ls + s(10), ly + s(17), sw, ly + s(33)}
		drawText(hdc, "правила NOVA RP", &tr2, DT_SINGLELINE|DT_NOPREFIX)
	}

	// поле поиска
	boxX, boxY, boxH := brand+s(16), s(12), s(34)
	boxR := app.rcAI.Left - s(10)
	border := mix(clrLine, clrAccent, easeOut(app.focusAmt))
	if app.focusAmt > 0 {
		// мягкое свечение вокруг поля при фокусе
		glow := mix(clrPanel, clrAccent, 0.18*easeOut(app.focusAmt))
		roundFill(hdc, boxX-s(2), boxY-s(2), boxR+s(2), boxY+boxH+s(2), s(13), glow)
	}
	pSelectObject.Call(hdc, app.brPanel2)
	pSelectObject.Call(hdc, mustPenW(border, s(1)))
	pRoundRect.Call(hdc, uintptr(boxX), uintptr(boxY), uintptr(boxR), uintptr(boxY+boxH), uintptr(s(10)), uintptr(s(10)))
	paintTopButtons(hdc)
	// значок лупы (в режиме ИИ — звёздочка)
	if app.aiMode {
		pSelectObject.Call(hdc, app.fIcon)
		pSetTextColor.Call(hdc, clrAccent)
		ir := RECT{boxX + s(6), boxY, boxX + s(32), boxY + boxH}
		drawText(hdc, "✦", &ir, DT_SINGLELINE|DT_VCENTER|0x0001)
	} else {
		pen := mustPenW(mix(clrMuted, clrAccent, app.focusAmt), s(2))
		pSelectObject.Call(hdc, pen)
		nullBrush, _, _ := pGetStockObject.Call(5)
		pSelectObject.Call(hdc, nullBrush)
		cx, cy, rr := boxX+s(18), boxY+boxH/2-s(2), s(6)
		pEllipse.Call(hdc, uintptr(cx-rr), uintptr(cy-rr), uintptr(cx+rr), uintptr(cy+rr))
		pMoveToEx.Call(hdc, uintptr(cx+rr-s(1)), uintptr(cy+rr-s(1)), 0)
		pLineTo.Call(hdc, uintptr(cx+rr+s(5)), uintptr(cy+rr+s(5)))
	}

	// строка состояния
	pSelectObject.Call(hdc, app.fStatus)
	pSetTextColor.Call(hdc, clrFaint)
	vr := RECT{s(14), h - sh, w - s(14), h}
	drawText(hdc, "v"+appVersion, &vr, DT_VCENTER|DT_SINGLELINE|DT_RIGHT|DT_NOPREFIX)
	pSetTextColor.Call(hdc, clrStatusT)
	sr := RECT{s(14), h - sh, w - s(70), h}
	drawText(hdc, app.status, &sr, DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS|DT_NOPREFIX)
}

var brushCache = map[uintptr]uintptr{}

func mustBrush(c uintptr) uintptr {
	if b, ok := brushCache[c]; ok {
		return b
	}
	b, _, _ := pCreateSolidBrush.Call(c)
	brushCache[c] = b
	return b
}

func mustFocus() uintptr { f, _, _ := pGetFocus.Call(); return f }

func setStatus(t string) {
	app.status = t
	var rc RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&rc)))
	r := RECT{0, rc.Bottom - s(statusH), rc.Right, rc.Bottom}
	pInvalidateRect.Call(app.hwnd, uintptr(unsafe.Pointer(&r)), 1)
}

func invalidateSearchBox() {
	var rc RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&rc)))
	r := RECT{0, 0, rc.Right, s(topH)}
	pInvalidateRect.Call(app.hwnd, uintptr(unsafe.Pointer(&r)), 1)
}

var penCache = map[uintptr]uintptr{}
var penWCache = map[[2]uintptr]uintptr{}

func mustPenW(c uintptr, w int32) uintptr {
	k := [2]uintptr{c, uintptr(w)}
	if p, ok := penWCache[k]; ok {
		return p
	}
	p, _, _ := pCreatePen.Call(0, uintptr(w), c)
	penWCache[k] = p
	return p
}

func mustPen(c uintptr) uintptr {
	if p, ok := penCache[c]; ok {
		return p
	}
	p, _, _ := pCreatePen.Call(0, 1, c)
	penCache[c] = p
	return p
}

// ------------------------------------------------------------------ содержимое

func setRTF(r Rendered) {
	app.rend = r
	SendMessage(app.rich, WM_SETREDRAW, 0, 0)
	b := append([]byte(r.RTF), 0)
	st := SETTEXTEX{Flags: 0, Codepage: 0}
	SendMessage(app.rich, EM_SETTEXTEX, uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&b[0])))
	applyZoom()
	SendMessage(app.rich, EM_SETSEL, 0, 0)
	SendMessage(app.rich, WM_VSCROLL, SB_TOP, 0)
	SendMessage(app.rich, WM_SETREDRAW, 1, 0)
	pInvalidateRect.Call(app.rich, 0, 1)
	runtime.KeepAlive(b)
}

func applyZoom() {
	if app.zoom <= 0 || app.zoom == 100 {
		SendMessage(app.rich, EM_SETZOOM, 0, 0)
	} else {
		SendMessage(app.rich, EM_SETZOOM, uintptr(app.zoom), 100)
	}
}

func readZoom() {
	var num, den int32
	SendMessage(app.rich, WM_USER+224, uintptr(unsafe.Pointer(&num)), uintptr(unsafe.Pointer(&den)))
	if num > 0 && den > 0 {
		app.zoom = int(num * 100 / den)
	} else {
		app.zoom = 100
	}
}

func changeZoom(delta int) {
	readZoom()
	z := app.zoom + delta
	if delta == 0 {
		z = 100
	}
	if z < 60 {
		z = 60
	}
	if z > 250 {
		z = 250
	}
	app.zoom = z
	applyZoom()
	saveState()
}

func docStatus() string {
	if app.cur >= len(app.docs) {
		return ""
	}
	d := app.docs[app.cur]
	upd := ""
	if d.Updated != "" {
		upd = "Обновлено " + d.Updated + "   ·   "
	}
	return upd + "Правый клик по пункту — копировать или в избранное   ·   Ctrl+I — спросить ИИ   ·   Ctrl+, — настройки   ·   " + escHint()
}

func escHint() string {
	if app.overlay {
		return "Esc — вернуться в игру"
	}
	return "Esc — спрятать"
}

func showDoc(i int) {
	if i < 0 || i >= len(app.docs) {
		return
	}
	readZoom()
	app.mode = modeDoc
	app.cur = i
	app.curSec = 0
	ensureOpen(i)
	setRTF(RenderDoc(app.docs[i], app.favs, app.errs))
	rebuildList()
	setStatus(docStatus())
	saveState()
}

func showFavs() {
	readZoom()
	app.mode = modeFavs
	var pts []*Point
	for _, d := range app.docs {
		for _, p := range d.Points {
			if app.favs[FavKey(p)] {
				pts = append(pts, p)
			}
		}
	}
	SortByDoc(pts, app.docs)
	setRTF(RenderList("★  Избранное", pts, nil, app.favs,
		[]string{"Здесь пока пусто.", "Нажмите правой кнопкой мыши на любой пункт и выберите «В избранное»."}, app.errs))
	rebuildList()
	setStatus(fmt.Sprintf("В избранном: %d   ·   Двойной клик по пункту — открыть его в документе", len(pts)))
}

func doSearch() {
	q := getWindowText(app.edit)
	if strings.TrimSpace(q) == "" {
		if app.mode == modeSearch {
			showDoc(app.cur)
		}
		return
	}
	readZoom()
	found, words := Search(app.docs, q)
	title := fmt.Sprintf("Найдено: %d", len(found))
	shown := found
	if len(shown) > maxResults {
		shown = shown[:maxResults]
		title += fmt.Sprintf(" (показаны первые %d)", maxResults)
	}
	app.mode = modeSearch
	setRTF(RenderList(title, shown, words, app.favs,
		[]string{"Ничего не нашлось по запросу «" + q + "».", "Попробуйте другое слово или его часть: «брон» найдёт и «бронежилет», и «бронежилеты»."}, app.errs))
	rebuildList()
	setStatus(fmt.Sprintf("Найдено: %d   ·   Двойной клик по пункту — открыть его в документе   ·   Esc — очистить поиск", len(found)))
}

func clearSearchSilently() {
	if getWindowText(app.edit) != "" {
		pSetWindowTextW.Call(app.edit, u16p(""))
		pKillTimer.Call(app.hwnd, tmrSearch)
	}
}

func scrollToPos(pos int) {
	line := int(SendMessage(app.rich, EM_EXLINEFROMCHAR, 0, uintptr(pos)))
	first := int(SendMessage(app.rich, EM_GETFIRSTVISIBLELINE, 0, 0))
	SendMessage(app.rich, EM_LINESCROLL, 0, uintptr(line-first))
}

func pointAt(pos int) *PointRange {
	for i := range app.rend.Points {
		pr := &app.rend.Points[i]
		if pos >= pr.Start && pos < pr.End {
			return pr
		}
	}
	return nil
}

func charFromClient(x, y int32) int {
	pt := POINT{x, y}
	return int(SendMessage(app.rich, EM_CHARFROMPOS, 0, uintptr(unsafe.Pointer(&pt))))
}

func openInDoc(p *Point) {
	di := -1
	for i, d := range app.docs {
		if d == p.Doc {
			di = i
		}
	}
	if di < 0 {
		return
	}
	clearSearchSilently()
	showDoc(di)
	for _, pr := range app.rend.Points {
		if pr.P == p {
			scrollToPos(maxInt(pr.Start-1, 0))
			flash(pr)
			break
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func flash(pr PointRange) {
	SendMessage(app.rich, EM_SETSEL, uintptr(pr.Start), uintptr(pr.End-1))
	app.flashStart = pr.Start
	pSetTimer.Call(app.hwnd, tmrFlash, 1400, 0)
}

func toggleFav(p *Point) {
	k := FavKey(p)
	if app.favs[k] {
		delete(app.favs, k)
	} else {
		app.favs[k] = true
	}
	saveFavs()
	// перерисовать текущий вид, сохранив прокрутку
	first := int(SendMessage(app.rich, EM_GETFIRSTVISIBLELINE, 0, 0))
	switch app.mode {
	case modeDoc:
		readZoom()
		setRTF(RenderDoc(app.docs[app.cur], app.favs, app.errs))
		rebuildList()
	case modeFavs:
		showFavs()
	case modeSearch:
		doSearch()
	case modeAI:
		renderAIView()
		rebuildList()
	}
	SendMessage(app.rich, EM_LINESCROLL, 0, uintptr(first))
}

func onContextMenu(lParam uintptr) {
	x, y := int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF))
	var pt POINT
	if lParam == ^uintptr(0) || (x == -1 && y == -1) {
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	} else {
		pt = POINT{x, y}
	}
	cp := pt
	pScreenToClient.Call(app.rich, uintptr(unsafe.Pointer(&cp)))
	pr := pointAt(charFromClient(cp.X, cp.Y))

	var sel CHARRANGE
	SendMessage(app.rich, EM_EXGETSEL, 0, uintptr(unsafe.Pointer(&sel)))
	hasSel := sel.CpMax > sel.CpMin

	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	if pr != nil {
		pAppendMenuW.Call(m, MF_STRING, 1, u16p("Скопировать пункт "+pr.P.Num))
		if app.favs[FavKey(pr.P)] {
			pAppendMenuW.Call(m, MF_STRING, 2, u16p("Убрать из избранного"))
		} else {
			pAppendMenuW.Call(m, MF_STRING, 2, u16p("★  В избранное"))
		}
		if app.mode != modeDoc {
			pAppendMenuW.Call(m, MF_STRING, 3, u16p("Открыть в документе"))
		}
	}
	if hasSel {
		if pr != nil {
			pAppendMenuW.Call(m, MF_SEPARATOR, 0, 0)
		}
		pAppendMenuW.Call(m, MF_STRING, 4, u16p("Копировать выделенное\tCtrl+C"))
	}
	if pr == nil && !hasSel {
		return
	}
	cmd, _, _ := pTrackPopupMenu.Call(m, TPM_RETURNCMD|TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, app.hwnd, 0)
	switch cmd {
	case 1:
		if setClipboard(app.hwnd, PointText(pr.P)) {
			setStatus("Пункт " + pr.P.Num + " скопирован — можно вставить в жалобу или чат (Ctrl+V)")
		}
	case 2:
		toggleFav(pr.P)
	case 3:
		openInDoc(pr.P)
	case 4:
		SendMessage(app.rich, 0x0301, 0, 0) // WM_COPY
	}
}

func onNotify(lParam uintptr) uintptr {
	hdr := (*NMHDR)(unsafe.Pointer(lParam))
	if hdr.HwndFrom != app.rich {
		return 0
	}
	switch hdr.Code {
	case EN_LINK:
		l := (*ENLINK)(unsafe.Pointer(lParam))
		if l.Msg == WM_LBUTTONUP {
			n := l.Chrg.CpMax - l.Chrg.CpMin
			if n > 0 && n < 2048 {
				buf := make([]uint16, n+1)
				tr := TEXTRANGEW{Chrg: l.Chrg, LpstrText: &buf[0]}
				SendMessage(app.rich, EM_GETTEXTRANGE, 0, uintptr(unsafe.Pointer(&tr)))
				url := syscall.UTF16ToString(buf)
				if strings.HasPrefix(url, "http") {
					pShellExecuteW.Call(app.hwnd, u16p("open"), u16p(url), 0, 0, SW_SHOWNORMAL)
				}
			}
			return 1
		}
	case EN_MSGFILTER:
		mf := (*MSGFILTER)(unsafe.Pointer(lParam))
		if mf.Msg == WM_RBUTTONUP {
			pt := POINT{int32(int16(mf.LParam & 0xFFFF)), int32(int16((mf.LParam >> 16) & 0xFFFF))}
			pClientToScreen.Call(app.rich, uintptr(unsafe.Pointer(&pt)))
			ctxPending = pt
			PostMessage(app.hwnd, WM_APP+4, 0, 0)
			return 1
		}
		if mf.Msg == WM_LBUTTONDBLCLK && app.mode != modeDoc {
			x, y := int32(int16(mf.LParam&0xFFFF)), int32(int16((mf.LParam>>16)&0xFFFF))
			if pr := pointAt(charFromClient(x, y)); pr != nil {
				pendingOpen = pr.P
				PostMessage(app.hwnd, WM_APP+3, 0, 0)
				return 1
			}
		}
	}
	return 0
}

var pendingOpen *Point
var ctxPending POINT

func trackSection() {
	if app.mode != modeDoc || len(app.rend.Sections) == 0 {
		return
	}
	if v, _, _ := pIsWindowVisible.Call(app.hwnd); v == 0 {
		return
	}
	first := SendMessage(app.rich, EM_GETFIRSTVISIBLELINE, 0, 0)
	pos := int(SendMessage(app.rich, EM_LINEINDEX, first, 0)) + 2
	cur := 0
	for i, st := range app.rend.Sections {
		if st <= pos {
			cur = i
		}
	}
	if cur != app.curSec {
		app.curSec = cur
		pInvalidateRect.Call(app.list, 0, 0)
	}
}

// ------------------------------------------------------------------ клавиатура

func keyDown(vk int) bool {
	r, _, _ := pGetKeyState.Call(uintptr(vk))
	return int16(r) < 0
}

func handleKey(vk uintptr) bool {
	ctrl := keyDown(VK_CONTROL)
	focus := mustFocus()
	switch {
	case vk == VK_ESCAPE && app.overlay:
		// в оверлее Esc сразу возвращает в игру, запрос и ответ остаются на экране
		backToGame()
		return true
	case vk == VK_ESCAPE:
		switch {
		case getWindowText(app.edit) != "":
			pSetWindowTextW.Call(app.edit, u16p(""))
			pKillTimer.Call(app.hwnd, tmrSearch)
			if !app.aiMode {
				doSearch()
			}
			pSetFocus.Call(app.edit)
		case app.aiMode:
			exitAIMode()
		default:
			hideWindow()
		}
		return true
	case ctrl && vk == VK_RETURN:
		startAsk(getWindowText(app.edit))
		return true
	case ctrl && vk == 'I':
		if app.aiMode {
			exitAIMode()
		} else {
			enterAIMode()
		}
		return true
	case ctrl && vk == 0xBC: // Ctrl+, — настройки
		openSettings("")
		return true
	case ctrl && (vk == 'F' || vk == 'L'):
		pSetFocus.Call(app.edit)
		SendMessage(app.edit, EM_SETSEL, 0, ^uintptr(0))
		return true
	case vk == VK_F5:
		reloadRules()
		return true
	case ctrl && (vk == VK_OEM_PLUS || vk == VK_ADD):
		changeZoom(10)
		return true
	case ctrl && (vk == VK_OEM_MINUS || vk == VK_SUBTRACT):
		changeZoom(-10)
		return true
	case ctrl && vk == '0':
		changeZoom(0)
		return true
	}
	if focus == app.edit {
		switch {
		case ctrl && vk == 'A':
			SendMessage(app.edit, EM_SETSEL, 0, ^uintptr(0))
			return true
		case vk == VK_RETURN:
			pKillTimer.Call(app.hwnd, tmrSearch)
			if app.aiMode {
				startAsk(getWindowText(app.edit))
			} else {
				doSearch()
			}
			return true
		case vk == VK_DOWN:
			SendMessage(app.rich, WM_VSCROLL, SB_LINEDOWN, 0)
			SendMessage(app.rich, WM_VSCROLL, SB_LINEDOWN, 0)
			return true
		case vk == VK_UP:
			SendMessage(app.rich, WM_VSCROLL, SB_LINEUP, 0)
			SendMessage(app.rich, WM_VSCROLL, SB_LINEUP, 0)
			return true
		case vk == VK_NEXT:
			SendMessage(app.rich, WM_VSCROLL, SB_PAGEDOWN, 0)
			return true
		case vk == VK_PRIOR:
			SendMessage(app.rich, WM_VSCROLL, SB_PAGEUP, 0)
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ показать / спрятать

func isVisible() bool { v, _, _ := pIsWindowVisible.Call(app.hwnd); return v != 0 }

func showWindow() {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg != app.hwnd && fg != 0 {
		app.prevFg = fg
	}
	if app.ovPassive {
		setPassive(false)
		setStatus("Напишите запрос и нажмите Enter   ·   Esc — вернуться в игру (окно останется на экране)")
	}
	appearing := !isVisible() || app.fade == fadeOut
	if appearing && app.anim && app.fade != fadeOut {
		app.fadeT = 0
		setAlpha(app.hwnd, 0)
	}
	if ic, _, _ := pIsIconic.Call(app.hwnd); ic != 0 {
		pShowWindow.Call(app.hwnd, SW_RESTORE)
	} else {
		pShowWindow.Call(app.hwnd, SW_SHOW)
	}
	if appearing {
		startFade(fadeIn)
	}
	after := HWND_NOTOPMOST
	if app.onTop || app.overlay {
		after = HWND_TOPMOST
	}
	pSetWindowPos.Call(app.hwnd, after, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
	pSetForegroundWindow.Call(app.hwnd)
	pSetFocus.Call(app.edit)
	SendMessage(app.edit, EM_SETSEL, 0, ^uintptr(0))
	if app.pendingApp != nil && !app.askedApp {
		PostMessage(app.hwnd, wmAskApp, 0, 0)
	}
}

func hideWindow() { hideWindowHint(false) }

func hideWindowHint(hint bool) {
	saveState()
	scrollKeys(false)
	startFade(fadeOut)
	if app.prevFg != 0 {
		if ok, _, _ := pIsWindow.Call(app.prevFg); ok != 0 {
			pSetForegroundWindow.Call(app.prevFg)
		}
	}
	if hint && !app.closeHinted {
		app.closeHinted = true
		balloon("Nova Helper спрятан в трей", "Нажмите "+app.hkLabel+", чтобы открыть его снова. Закрыть программу — правый клик по значку в трее → Выход.")
	}
}

func toggle() {
	if app.overlay {
		toggleOverlay()
		return
	}
	if isVisible() && app.fade != fadeOut {
		if ic, _, _ := pIsIconic.Call(app.hwnd); ic != 0 {
			showWindow()
			return
		}
		hideWindow()
	} else {
		showWindow()
	}
}

// ------------------------------------------------------------------ трей

func trayData() NOTIFYICONDATAW {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = app.hwnd
	nid.UID = 1
	return nid
}

func addTray(withBalloon bool) {
	nid := trayData()
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = wmTray
	nid.HIcon = app.iconSmall
	copyUTF16(nid.SzTip[:], appTitle+" — "+app.hkLabel)
	pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
	if withBalloon {
		balloon(appTitle, "Программа запущена. "+app.hkLabel+" — открыть или спрятать Nova Helper.")
	}
}

func balloon(title, text string) {
	nid := trayData()
	nid.UFlags = NIF_INFO
	nid.DwInfoFlags = NIIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func removeTray() {
	nid := trayData()
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

func trayMenu() {
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	pAppendMenuW.Call(m, MF_STRING, 1, u16p("Открыть / спрятать\t"+app.hkLabel))
	pAppendMenuW.Call(m, MF_STRING, 2, u16p("Перечитать правила\tF5"))
	pAppendMenuW.Call(m, MF_STRING, 3, u16p("Открыть папку с правилами"))
	pAppendMenuW.Call(m, MF_STRING, 10, u16p("Настройки…\tCtrl+,"))
	pAppendMenuW.Call(m, MF_STRING, 4, u16p("Открыть файл settings.ini"))
	flags := uintptr(MF_STRING)
	if autostartEnabled(autostartName) {
		flags |= MF_CHECKED
	}
	pAppendMenuW.Call(m, flags, 5, u16p("Запускать вместе с Windows"))
	flags = MF_STRING
	if app.onTop {
		flags |= MF_CHECKED
	}
	pAppendMenuW.Call(m, flags, 6, u16p("Поверх всех окон"))
	pAppendMenuW.Call(m, MF_SEPARATOR, 0, 0)
	if app.pendingApp != nil {
		pAppendMenuW.Call(m, MF_STRING, 8, u16p("Обновить программу до версии "+app.pendingApp.Version))
	}
	pAppendMenuW.Call(m, MF_STRING, 7, u16p("Проверить обновления"))
	pAppendMenuW.Call(m, MF_STRING|MF_GRAYED, 0, u16p("Версия "+appVersion))
	pAppendMenuW.Call(m, MF_SEPARATOR, 0, 0)
	pAppendMenuW.Call(m, MF_STRING, 9, u16p("Выход"))

	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(app.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, TPM_RETURNCMD|TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, app.hwnd, 0)
	PostMessage(app.hwnd, 0, 0, 0)
	switch cmd {
	case 1:
		toggle()
	case 2:
		reloadRules()
		showWindow()
	case 3:
		pShellExecuteW.Call(app.hwnd, u16p("open"), u16p(filepath.Join(app.dir, "rules")), 0, 0, SW_SHOWNORMAL)
	case 4:
		pShellExecuteW.Call(app.hwnd, u16p("open"), u16p("notepad.exe"), u16p(`"`+settingsPath()+`"`), 0, SW_SHOWNORMAL)
	case 5:
		exe, _ := os.Executable()
		on := !autostartEnabled(autostartName)
		if !setAutostart(autostartName, `"`+exe+`" --tray`, on) {
			messageBox(app.hwnd, "Не получилось изменить автозапуск.", appTitle, MB_ICONWARNING)
		}
	case 6:
		app.onTop = !app.onTop
		saveSettingOnTop()
		after := HWND_NOTOPMOST
		if app.onTop {
			after = HWND_TOPMOST
		}
		pSetWindowPos.Call(app.hwnd, after, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	case 7:
		startCheck(true)
	case 8:
		askApp()
	case 9:
		saveState()
		pDestroyWindow.Call(app.hwnd)
	case 10:
		showWindow()
		openSettings("")
	}
}

// ------------------------------------------------------------------ обновления

var (
	updMu  sync.Mutex
	updRes *UpdateResult
	appErr error
)

func startCheck(manual bool) {
	if app.checking {
		return
	}
	app.checking = true
	go func() {
		r := CheckUpdates(updateBase, app.dir)
		r.Manual = manual
		updMu.Lock()
		updRes = &r
		updMu.Unlock()
		PostMessage(app.hwnd, wmUpdate, 0, 0)
	}()
}

func onUpdateResult() {
	updMu.Lock()
	r := updRes
	updRes = nil
	updMu.Unlock()
	app.checking = false
	if r == nil {
		return
	}
	if len(r.Changed) > 0 || len(r.Removed) > 0 {
		reloadRules()
		var names []string
		for _, f := range r.Changed {
			if f.Title != "" {
				names = append(names, f.Title)
			} else {
				names = append(names, strings.TrimSuffix(f.Name, ".txt"))
			}
		}
		text := "Обновлено: " + strings.Join(names, ", ")
		if len(names) == 0 {
			text = "Список документов изменился."
		}
		balloon("Правила обновлены", text)
		setStatus("Правила обновлены: " + strings.Join(names, ", "))
	}
	if r.NewApp != nil {
		newer := app.pendingApp == nil || app.pendingApp.Version != r.NewApp.Version
		app.pendingApp = r.NewApp
		if newer {
			app.askedApp = false
		}
		if r.Manual || (isVisible() && !app.askedApp) {
			askApp()
		} else if newer {
			balloon("Есть новая версия программы", "Откройте Nova Helper, чтобы обновить его до версии "+r.NewApp.Version+".")
		}
	}
	if r.Manual && r.NewApp == nil {
		if r.Err != nil {
			messageBox(app.hwnd, "Не получилось проверить обновления.\n\n"+r.Err.Error()+"\n\nПроверьте подключение к интернету и попробуйте позже.", appTitle, MB_ICONWARNING|MB_TOPMOST)
		} else if len(r.Changed) == 0 && len(r.Removed) == 0 {
			messageBox(app.hwnd, "Всё актуально: и правила, и программа самой свежей версии.", appTitle, MB_ICONINFO|MB_TOPMOST)
		}
	}
}

func askApp() {
	a := app.pendingApp
	if a == nil {
		return
	}
	app.askedApp = true
	text := "Доступна новая версия программы: " + a.Version + " (у вас " + appVersion + ")."
	if a.Notes != "" {
		text += "\n\nЧто нового: " + a.Notes
	}
	text += "\n\nОбновить сейчас? Это займёт несколько секунд, избранное и настройки сохранятся."
	r, _, _ := pMessageBoxW.Call(app.hwnd, u16p(text), u16p(appTitle), 0x4|MB_ICONINFO|MB_TOPMOST) // MB_YESNO
	if r != 6 {                                                                                    // IDYES
		return
	}
	setStatus("Скачиваю новую версию программы…")
	go func() {
		err := DownloadApp(updateBase, a, app.exePath)
		updMu.Lock()
		appErr = err
		updMu.Unlock()
		PostMessage(app.hwnd, wmAppReady, 0, 0)
	}()
}

func onAppReady() {
	updMu.Lock()
	err := appErr
	updMu.Unlock()
	if err == nil {
		err = SwapApp(app.exePath)
	}
	if err != nil {
		messageBox(app.hwnd, "Не получилось обновить программу:\n\n"+err.Error(), appTitle, MB_ICONWARNING|MB_TOPMOST)
		setStatus("")
		return
	}
	saveState()
	app.restart = true
	pDestroyWindow.Call(app.hwnd)
}

func reloadRules() {
	app.docs, app.errs = LoadDocs(filepath.Join(app.dir, "rules"))
	if app.cur >= len(app.docs) {
		app.cur = 0
	}
	switch {
	case len(app.docs) == 0:
		setRTF(RenderList("Правила не загрузились", nil, nil, app.favs,
			[]string{"Проверьте, что рядом с программой есть папка rules с файлами правил (.txt)."}, app.errs))
		rebuildList()
	case strings.TrimSpace(getWindowText(app.edit)) != "":
		doSearch()
	case app.mode == modeFavs:
		showFavs()
	case app.mode == modeAI:
		rebuildList()
	default:
		showDoc(app.cur)
	}
	setStatus("Правила перечитаны из папки rules   ·   документов: " + strconv.Itoa(len(app.docs)))
}

// ------------------------------------------------------------------ горячая клавиша

func registerHotkey() {
	r, _, _ := pRegisterHotKey.Call(app.hwnd, hotkeyID, app.hkMods|MOD_NOREPEAT, app.hkVK)
	if r == 0 {
		messageBox(app.hwnd, "Клавиша "+app.hkLabel+" уже занята другой программой.\n\nПоменяйте её в файле settings.ini (строка «Клавиша=») и перезапустите Nova Helper.\nПока Nova Helper можно открывать двойным кликом по значку в трее.", appTitle, MB_ICONWARNING|MB_TOPMOST)
	}
}

// ------------------------------------------------------------------ оконная процедура

func wndProc(hwnd uintptr, msg uintptr, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_SIZE:
		if app.rich != 0 {
			layout()
		}
		return 0
	case WM_PAINT:
		paintMain()
		return 0
	case WM_GETMINMAXINFO:
		mmi := (*MINMAXINFO)(unsafe.Pointer(lParam))
		mmi.PtMinTrackSize = POINT{s(520), s(420)}
		return 0
	case WM_SETFOCUS:
		pSetFocus.Call(app.edit)
		return 0
	case WM_MEASUREITEM:
		mis := (*MEASUREITEMSTRUCT)(unsafe.Pointer(lParam))
		if mis.CtlID == idList {
			measureItem(mis)
			return 1
		}
	case WM_DRAWITEM:
		dis := (*DRAWITEMSTRUCT)(unsafe.Pointer(lParam))
		if dis.CtlID == idList {
			drawItem(dis)
			return 1
		}
	case WM_CTLCOLOREDIT:
		if lParam == app.edit {
			pSetTextColor.Call(wParam, clrText)
			pSetBkColor.Call(wParam, clrPanel2)
			return app.brPanel2
		}
	case WM_CTLCOLORLISTBOX:
		pSetBkColor.Call(wParam, clrPanel)
		return app.brPanel
	case WM_COMMAND:
		id, code := wParam&0xFFFF, (wParam>>16)&0xFFFF
		switch {
		case id == idEdit && code == EN_CHANGE:
			if !app.aiMode {
				pSetTimer.Call(hwnd, tmrSearch, 160, 0)
			}
		case id == idEdit && (code == 0x0100 || code == 0x0200): // EN_SETFOCUS / EN_KILLFOCUS
			kickAnim()
			invalidateSearchBox()
		case id == idList && code == LBN_SELCHANGE:
			onListClick()
		}
		return 0
	case WM_NOTIFY:
		return onNotify(lParam)
	case WM_CONTEXTMENU:
		if wParam == app.rich {
			onContextMenu(lParam)
			return 0
		}
	case WM_TIMER:
		switch wParam {
		case tmrSearch:
			pKillTimer.Call(hwnd, tmrSearch)
			doSearch()
		case tmrSection:
			trackSection()
		case tmrUpdate:
			pSetTimer.Call(hwnd, tmrUpdate, 6*60*60*1000, 0) // дальше — раз в 6 часов
			startCheck(false)
		case tmrFlash:
			pKillTimer.Call(hwnd, tmrFlash)
			SendMessage(app.rich, EM_SETSEL, uintptr(app.flashStart), uintptr(app.flashStart))
		case tmrAnim:
			animTick()
		}
		return 0
	case wmAI:
		onAIResult(int(wParam))
		return 0
	case WM_MOUSEMOVE:
		onTopMouse(lParam)
		return 0
	case WM_MOUSELEAVE:
		if app.hoverTop != 0 {
			app.hoverTop = 0
			kickAnim()
		}
		return 0
	case WM_LBUTTONDOWN:
		onTopClick(lParam)
		return 0
	case WM_SETCURSOR:
		if wParam == hwnd && app.hoverTop != 0 && lParam&0xFFFF == 1 { // HTCLIENT
			handCursor()
			return 1
		}
	case WM_APP + 3:
		if pendingOpen != nil {
			p := pendingOpen
			pendingOpen = nil
			openInDoc(p)
		}
		return 0
	case WM_APP + 4:
		pt := ctxPending
		onContextMenu(uintptr(uint16(pt.X)) | uintptr(uint16(pt.Y))<<16)
		return 0
	case WM_HOTKEY:
		if wParam == hotkeyID {
			toggle()
		} else if app.ovPassive && isVisible() {
			onScrollKey(wParam)
		}
		return 0
	case wmTray:
		switch lParam & 0xFFFF {
		case WM_LBUTTONUP:
			toggle()
		case WM_RBUTTONUP:
			trayMenu()
		}
		return 0
	case wmUpdate:
		onUpdateResult()
		return 0
	case wmAppReady:
		onAppReady()
		return 0
	case wmAskApp:
		if app.pendingApp != nil && !app.askedApp {
			askApp()
		}
		return 0
	case wmShowExisting:
		showWindow()
		return 0
	case WM_ACTIVATE:
		if wParam&0xFFFF == 0 { // WA_INACTIVE
			onDeactivate(lParam)
		}
	case WM_CLOSE:
		hideWindowHint(true)
		return 0
	case WM_DESTROY:
		pUnregisterHotKey.Call(hwnd, hotkeyID)
		scrollKeys(false)
		removeTray()
		pPostQuitMessage.Call(0)
		return 0
	default:
		if app.taskbarMsg != 0 && uint32(msg) == app.taskbarMsg {
			addTray(false)
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}
