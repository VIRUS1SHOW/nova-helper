package main

// Окно «Настройки»: цвет оформления, прозрачность, анимации и подключение ИИ.

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	dlgClass = "NovaHelperSettingsWnd"

	idcAccent  = 201
	idcOpacity = 202
	idcAnim    = 203
	idcOverlay = 204
	idcOvOpac  = 205
	idcProv    = 210
	idcKey     = 211
	idcFolder  = 212
	idcModel   = 213
	idcURL     = 214
	idcCheck   = 220
	idcSave    = 221
	idcCancel  = 222

	wmCheckDone = WM_APP + 20

	dlgH = 770
)

var dlg struct {
	hwnd                            uintptr
	accent, opacity, opVal, anim    uintptr
	prov, key, folder, model, url   uintptr
	hint, result, folderLbl, urlLbl uintptr
	headers, labels                 []uintptr
	resultColor                     uintptr
	origAccent, origOpacity         int
	origAnim                        bool
	overlay, ovOpac, ovVal          uintptr
	origOverlay, origCompact        bool
	origOvOpacity                   int
	checking                        bool
	checkRes                        string
	checkOK                         bool
	registered                      bool
}

func openSettings(message string) {
	if app.dlg != 0 {
		pShowWindow.Call(app.dlg, SW_SHOW)
		pSetForegroundWindow.Call(app.dlg)
		if message != "" {
			setResult(message, clrOrange())
		}
		return
	}
	if !dlg.registered {
		cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
		wc := WNDCLASSEXW{
			LpfnWndProc:   syscall.NewCallback(dlgProc),
			HInstance:     app.hinst,
			HIcon:         app.iconBig,
			HIconSm:       app.iconSmall,
			HCursor:       cursor,
			HbrBackground: app.brPanel,
			LpszClassName: u16(dlgClass),
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		dlg.registered = true
	}
	dlg.origAccent, dlg.origOpacity, dlg.origAnim = app.accent, app.opacity, app.anim
	dlg.origOverlay, dlg.origCompact, dlg.origOvOpacity = app.overlay, app.ovCompact, app.ovOpacity

	style := uintptr(WS_CAPTION | WS_SYSMENU | WS_POPUP)
	cw, ch := s(540), s(dlgH)
	rc := RECT{0, 0, cw, ch}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	w, h := rc.Right-rc.Left, rc.Bottom-rc.Top
	var mr RECT
	pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&mr)))
	x := mr.Left + (mr.Right-mr.Left-w)/2
	y := mr.Top + (mr.Bottom-mr.Top-h)/2
	if y < 0 {
		y = 0
	}
	exStyle := uintptr(0)
	hwnd, _, _ := pCreateWindowExW.Call(exStyle, u16p(dlgClass), u16p("Настройки — Nova Helper"), style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), app.hwnd, 0, app.hinst, 0)
	dlg.hwnd = hwnd
	app.dlg = hwnd
	dark := int32(1)
	if r, _, _ := pDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4); r != 0 {
		pDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)
	}
	buildDialog()
	if app.onTop {
		pSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
	}
	pShowWindow.Call(hwnd, SW_SHOW)
	if message != "" {
		setResult(message, clrOrange())
		pSetFocus.Call(dlg.key)
	} else {
		pSetFocus.Call(dlg.prov)
	}
}

func clrOrange() uintptr { return RGB(255, 159, 67) }
func clrGreen() uintptr  { return RGB(79, 209, 139) }
func clrRed() uintptr    { return RGB(255, 107, 107) }

func ctl(class, text string, style uintptr, x, y, w, h float64, id uintptr, font uintptr) uintptr {
	hw, _, _ := pCreateWindowExW.Call(0, u16p(class), u16p(text), WS_CHILD|WS_VISIBLE|style,
		uintptr(s(x)), uintptr(s(y)), uintptr(s(w)), uintptr(s(h)), dlg.hwnd, id, app.hinst, 0)
	SendMessage(hw, WM_SETFONT, font, 1)
	return hw
}

func label(text string, x, y, w float64) uintptr {
	l := ctl("STATIC", text, SS_NOPREFIX, x, y+5, w, 22, 0, app.fDoc)
	dlg.labels = append(dlg.labels, l)
	return l
}

func dlgHeader(text string, y float64) {
	h := ctl("STATIC", text, SS_NOPREFIX, 24, y, 480, 20, 0, app.fHeader)
	dlg.headers = append(dlg.headers, h)
}

func editBox(id uintptr, extra uintptr, x, y, w float64) uintptr {
	e := ctl("EDIT", "", WS_TABSTOP|ES_AUTOHSCROLL|extra|0x00800000 /*WS_BORDER*/, x, y, w, 30, id, app.fDoc)
	pSetWindowTheme.Call(e, u16p("DarkMode_CFD"), 0)
	return e
}

func combo(id uintptr, items []string, sel int, x, y, w float64) uintptr {
	c := ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWNLIST|CBS_HASSTRINGS, x, y, w, 300, id, app.fDoc)
	pSetWindowTheme.Call(c, u16p("DarkMode_CFD"), 0)
	for _, it := range items {
		SendMessage(c, CB_ADDSTRING, 0, u16p(it))
	}
	SendMessage(c, CB_SETCURSEL, uintptr(sel), 0)
	return c
}

func button(text string, id uintptr, def bool, x, y, w float64) uintptr {
	st := uintptr(WS_TABSTOP | BS_PUSHBUTTON)
	if def {
		st = WS_TABSTOP | BS_DEFPUSHBTN
	}
	b := ctl("BUTTON", text, st, x, y, w, 32, id, app.fDoc)
	pSetWindowTheme.Call(b, u16p("DarkMode_Explorer"), 0)
	return b
}

func buildDialog() {
	dlg.headers, dlg.labels = nil, nil
	t := ctl("STATIC", "Настройки", SS_NOPREFIX, 24, 16, 480, 30, 0, app.fBrand)
	dlg.headers = append(dlg.headers, t)

	const lx, fx, fw = 24.0, 170.0, 346.0
	y := 62.0
	dlgHeader("ОФОРМЛЕНИЕ", y)
	y += 26
	var names []string
	for _, a := range accents {
		names = append(names, a.Name)
	}
	label("Цвет акцента", lx, y, 140)
	dlg.accent = combo(idcAccent, names, app.accent, fx, y, fw)
	y += 42
	label("Непрозрачность", lx, y, 140)
	dlg.opacity = ctl("msctls_trackbar32", "", WS_TABSTOP|TBS_HORZ|TBS_NOTICKS, fx-6, y+2, fw-56, 28, idcOpacity, app.fDoc)
	SendMessage(dlg.opacity, TBM_SETRANGE, 1, uintptr(40|100<<16))
	SendMessage(dlg.opacity, TBM_SETPOS, 1, uintptr(app.opacity))
	dlg.opVal = ctl("STATIC", strconv.Itoa(app.opacity)+"%", SS_NOPREFIX|0x2 /*SS_RIGHT*/, fx+fw-56, y+5, 56, 22, 0, app.fDoc)
	dlg.labels = append(dlg.labels, dlg.opVal)
	y += 42
	label("Анимации", lx, y, 140)
	sel := 0
	if !app.anim {
		sel = 1
	}
	dlg.anim = combo(idcAnim, []string{"Включены — плавно", "Выключены — мгновенно"}, sel, fx, y, fw)

	y += 56
	dlgHeader("РЕЖИМ ПОВЕРХ ИГРЫ", y)
	y += 26
	label("Поверх игры", lx, y, 140)
	ovSel := 0
	switch {
	case !app.overlay:
		ovSel = 2
	case app.ovCompact:
		ovSel = 1
	}
	dlg.overlay = combo(idcOverlay, []string{"Включён — игра не сворачивается", "Включён, компактно — без меню слева", "Выключен — обычное окно"}, ovSel, fx, y, fw)
	y += 42
	label("Прозрачность в игре", lx, y, 145)
	dlg.ovOpac = ctl("msctls_trackbar32", "", WS_TABSTOP|TBS_HORZ|TBS_NOTICKS, fx-6, y+2, fw-56, 28, idcOvOpac, app.fDoc)
	SendMessage(dlg.ovOpac, TBM_SETRANGE, 1, uintptr(30|100<<16))
	SendMessage(dlg.ovOpac, TBM_SETPOS, 1, uintptr(app.ovOpacity))
	dlg.ovVal = ctl("STATIC", strconv.Itoa(app.ovOpacity)+"%", SS_NOPREFIX|0x2, fx+fw-56, y+5, 56, 22, 0, app.fDoc)
	dlg.labels = append(dlg.labels, dlg.ovVal)

	y += 56
	dlgHeader("ИИ — ВОПРОСЫ К ПРАВИЛАМ", y)
	y += 26
	var provs []string
	cur := 0
	for i, p := range AIProviders {
		provs = append(provs, p.Name)
		if p.ID == app.ai.Provider {
			cur = i
		}
	}
	label("Сервис", lx, y, 140)
	dlg.prov = combo(idcProv, provs, cur, fx, y, fw)
	y += 40
	dlg.hint = ctl("STATIC", "", SS_NOPREFIX, fx, y, fw, 44, 0, app.fSmall)
	y += 50
	label("Ключ", lx, y, 140)
	dlg.key = editBox(idcKey, ES_PASSWORD, fx, y, fw)
	pSetWindowTextW.Call(dlg.key, u16p(app.ai.Key))
	y += 42
	dlg.folderLbl = label("ID каталога", lx, y, 140)
	dlg.folder = editBox(idcFolder, 0, fx, y, fw)
	pSetWindowTextW.Call(dlg.folder, u16p(app.ai.Folder))
	SendMessage(dlg.folder, EM_SETCUEBANNER, 1, u16p("b1g… (Yandex Cloud → каталог)"))
	y += 42
	label("Модель", lx, y, 140)
	dlg.model = editBox(idcModel, 0, fx, y, fw)
	pSetWindowTextW.Call(dlg.model, u16p(app.ai.Model))
	y += 42
	dlg.urlLbl = label("Адрес API", lx, y, 140)
	dlg.url = editBox(idcURL, 0, fx, y, fw)
	pSetWindowTextW.Call(dlg.url, u16p(app.ai.BaseURL))
	y += 46
	button("Проверить", idcCheck, false, fx, y, 120)
	dlg.result = ctl("STATIC", "", SS_NOPREFIX, fx+132, y-2, fw-132, 48, 0, app.fSmall)
	dlg.resultColor = clrMuted

	button("Сохранить", idcSave, true, 540-24-120-8-110, dlgH-24-32, 120)
	button("Отмена", idcCancel, false, 540-24-110, dlgH-24-32, 110)
	onProviderChanged()
}

func dlgProvider() AIProvider {
	i := int(SendMessage(dlg.prov, CB_GETCURSEL, 0, 0))
	if i < 0 || i >= len(AIProviders) {
		i = 0
	}
	return AIProviders[i]
}

func onProviderChanged() {
	p := dlgProvider()
	pSetWindowTextW.Call(dlg.hint, u16p(p.Hint))
	en := uintptr(0)
	if p.NeedFold {
		en = 1
	}
	pEnableWindow.Call(dlg.folder, en)
	cue := p.Model
	if cue == "" {
		cue = "название модели"
	} else {
		cue = "по умолчанию: " + strings.ReplaceAll(cue, "gpt://{folder}/", "")
	}
	SendMessage(dlg.model, EM_SETCUEBANNER, 1, u16p(cue))
	u := p.BaseURL
	if u == "" {
		u = "https://…/v1"
	} else {
		u = "по умолчанию: " + u
	}
	SendMessage(dlg.url, EM_SETCUEBANNER, 1, u16p(u))
	kcue := "вставьте ключ"
	if !p.NeedKey {
		kcue = "не нужен"
	}
	SendMessage(dlg.key, EM_SETCUEBANNER, 1, u16p(kcue))
	pInvalidateRect.Call(dlg.hwnd, 0, 1)
}

func dlgConfig() AIConfig {
	return AIConfig{
		Provider: dlgProvider().ID,
		Key:      strings.TrimSpace(getWindowText(dlg.key)),
		Folder:   strings.TrimSpace(getWindowText(dlg.folder)),
		Model:    strings.TrimSpace(getWindowText(dlg.model)),
		BaseURL:  strings.TrimSpace(getWindowText(dlg.url)),
	}
}

func setResult(t string, c uintptr) {
	dlg.resultColor = c
	pSetWindowTextW.Call(dlg.result, u16p(t))
	pInvalidateRect.Call(dlg.result, 0, 1)
}

func runCheck() {
	if dlg.checking {
		return
	}
	cfg := dlgConfig()
	if pr := cfg.Problem(); pr != "" {
		setResult(pr, clrOrange())
		return
	}
	dlg.checking = true
	setResult("Проверяю связь…", clrMuted)
	h := dlg.hwnd
	go func() {
		ans, err := AskAI(cfg, "Проверка связи. Ответь одним словом: работает.", "(пунктов нет — это проверка)")
		if err != nil {
			dlg.checkRes, dlg.checkOK = err.Error(), false
		} else {
			a := []rune(strings.TrimSpace(ans))
			if len(a) > 60 {
				a = append(a[:60], '…')
			}
			dlg.checkRes, dlg.checkOK = "Работает! Ответ: «"+string(a)+"»", true
		}
		PostMessage(h, wmCheckDone, 0, 0)
	}()
}

func closeSettings(save bool) {
	if dlg.hwnd == 0 {
		return
	}
	if save {
		app.ai = dlgConfig()
		saveAISettings()
		saveLookSettings()
		if app.mode == modeAI && app.aiView.Intro {
			renderAIView()
		}
		setStatus("Настройки сохранены")
	} else {
		if app.accent != dlg.origAccent {
			applyAccent(dlg.origAccent)
		}
		app.opacity, app.anim = dlg.origOpacity, dlg.origAnim
		app.overlay, app.ovCompact, app.ovOpacity = dlg.origOverlay, dlg.origCompact, dlg.origOvOpacity
		applyOpacity()
	}
	pDestroyWindow.Call(dlg.hwnd)
	dlg.hwnd = 0
	app.dlg = 0
	pSetForegroundWindow.Call(app.hwnd)
	pSetFocus.Call(app.edit)
}

func dlgProc(hwnd uintptr, msg uintptr, wp, lp uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id, code := wp&0xFFFF, (wp>>16)&0xFFFF
		switch {
		case id == idcAccent && code == CBN_SELCHANGE:
			applyAccent(int(SendMessage(dlg.accent, CB_GETCURSEL, 0, 0)))
			pInvalidateRect.Call(hwnd, 0, 1)
		case id == idcOverlay && code == CBN_SELCHANGE:
			sel := SendMessage(dlg.overlay, CB_GETCURSEL, 0, 0)
			app.overlay, app.ovCompact = sel != 2, sel == 1
		case id == idcAnim && code == CBN_SELCHANGE:
			app.anim = SendMessage(dlg.anim, CB_GETCURSEL, 0, 0) == 0
			kickAnim()
		case id == idcProv && code == CBN_SELCHANGE:
			onProviderChanged()
			setResult("", clrMuted)
		case id == idcCheck:
			runCheck()
		case id == idcSave || id == 1: // IDOK — Enter
			closeSettings(true)
		case id == idcCancel || id == 2: // IDCANCEL
			closeSettings(false)
		}
		return 0
	case WM_HSCROLL:
		if lp == dlg.ovOpac {
			app.ovOpacity = clampInt(int(SendMessage(dlg.ovOpac, TBM_GETPOS, 0, 0)), 30, 100)
			pSetWindowTextW.Call(dlg.ovVal, u16p(strconv.Itoa(app.ovOpacity)+"%"))
		}
		if lp == dlg.opacity {
			app.opacity = clampInt(int(SendMessage(dlg.opacity, TBM_GETPOS, 0, 0)), 40, 100)
			pSetWindowTextW.Call(dlg.opVal, u16p(strconv.Itoa(app.opacity)+"%"))
			applyOpacity()
		}
		return 0
	case wmCheckDone:
		dlg.checking = false
		if dlg.checkOK {
			setResult(dlg.checkRes, clrGreen())
		} else {
			setResult(dlg.checkRes, clrRed())
		}
		return 0
	case WM_CTLCOLORSTATIC:
		pSetBkMode.Call(wp, TRANSPARENT)
		c := clrMuted
		switch {
		case lp == dlg.result:
			c = dlg.resultColor
		case lp == dlg.hint:
			c = clrFaint
		case containsH(dlg.headers, lp):
			c = clrAccent
			if lp == dlg.headers[0] {
				c = clrText
			}
		case containsH(dlg.labels, lp):
			c = mix(clrMuted, clrText, 0.5)
		}
		pSetTextColor.Call(wp, c)
		pSetBkColor.Call(wp, clrPanel)
		return app.brPanel
	case WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX:
		pSetTextColor.Call(wp, clrText)
		pSetBkColor.Call(wp, clrPanel2)
		return app.brPanel2
	case WM_CTLCOLORBTN:
		return app.brPanel
	case WM_CLOSE:
		closeSettings(false)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func containsH(list []uintptr, h uintptr) bool {
	for _, x := range list {
		if x == h {
			return true
		}
	}
	return false
}
