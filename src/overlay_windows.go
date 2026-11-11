package main

// Режим «поверх игры» (оверлей). Это обычное прозрачное окно поверх остальных —
// в процесс игры программа не вмешивается, поэтому античит его не трогает.
// Работает, когда в игре выбран режим «Окно без рамки».
//
//   скрыто   —Alt+Q→  в игре: окно видно, но не забирает управление, мышь проходит сквозь
//   в игре   —Alt+Q→  поиск: окно активно, можно печатать
//   поиск    —Esc→    в игре (запрос и результаты остаются на экране)
//   поиск    —Alt+Q→  скрыто
//
// В режиме «в игре» Alt+↑/↓ и Alt+PgUp/PgDn листают текст, не отпуская игру.

const (
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_NOACTIVATE  = 0x08000000
	SW_SHOWNOACTIVATE = 4
	SW_SHOWNA         = 8
	MOD_ALT_ONLY      = 0x0001

	hkScrollUp   = 11
	hkScrollDown = 12
	hkPageUp     = 13
	hkPageDown   = 14
)

// ovPassive — окно показано «в игре»: не активное и прозрачное для мыши.
func setPassive(on bool) {
	ex, _, _ := pGetWindowLongPtrW.Call(app.hwnd, GWL_EXSTYLE)
	if on {
		ex |= WS_EX_TRANSPARENT | WS_EX_NOACTIVATE
	} else {
		ex &^= WS_EX_TRANSPARENT | WS_EX_NOACTIVATE
	}
	pSetWindowLongPtrW.Call(app.hwnd, GWL_EXSTYLE, ex)
	changed := app.ovPassive != on
	app.ovPassive = on
	scrollKeys(on)
	if changed {
		layout()
		applyOpacity()
		kickAnim()
	}
}

func scrollKeys(on bool) {
	ids := []struct {
		id uintptr
		vk uintptr
	}{{hkScrollUp, VK_UP}, {hkScrollDown, VK_DOWN}, {hkPageUp, VK_PRIOR}, {hkPageDown, VK_NEXT}}
	for _, k := range ids {
		pUnregisterHotKey.Call(app.hwnd, k.id)
		if on {
			pRegisterHotKey.Call(app.hwnd, k.id, MOD_ALT_ONLY, k.vk) // без NOREPEAT — можно зажать
		}
	}
}

// onScrollKey — листать текст, пока окно «в игре».
func onScrollKey(id uintptr) {
	switch id {
	case hkScrollUp:
		for i := 0; i < 3; i++ {
			SendMessage(app.rich, WM_VSCROLL, SB_LINEUP, 0)
		}
	case hkScrollDown:
		for i := 0; i < 3; i++ {
			SendMessage(app.rich, WM_VSCROLL, SB_LINEDOWN, 0)
		}
	case hkPageUp:
		SendMessage(app.rich, WM_VSCROLL, SB_PAGEUP, 0)
	case hkPageDown:
		SendMessage(app.rich, WM_VSCROLL, SB_PAGEDOWN, 0)
	}
}

// showPassive — показать окно поверх игры, не забирая у неё управление.
func showPassive() {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg != app.hwnd && fg != 0 {
		app.prevFg = fg
	}
	appearing := !isVisible() || app.fade == fadeOut
	setPassive(true)
	if appearing && app.anim && app.fade != fadeOut {
		app.fadeT = 0
		setAlpha(app.hwnd, 0)
	}
	pShowWindow.Call(app.hwnd, SW_SHOWNOACTIVATE)
	pSetWindowPos.Call(app.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	if appearing {
		startFade(fadeIn)
	}
	setStatus("Alt+↑/↓ — листать   ·   " + app.hkLabel + " — искать   ·   ещё раз " + app.hkLabel + " — спрятать")
}

// backToGame — из поиска обратно в игру; окно остаётся на экране.
func backToGame() {
	setPassive(true)
	pSetWindowPos.Call(app.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	if app.prevFg != 0 {
		if ok, _, _ := pIsWindow.Call(app.prevFg); ok != 0 {
			pSetForegroundWindow.Call(app.prevFg)
		}
	}
	setStatus("Alt+↑/↓ — листать   ·   " + app.hkLabel + " — искать   ·   ещё раз " + app.hkLabel + " — спрятать")
}

func toggleOverlay() {
	switch {
	case !isVisible() || app.fade == fadeOut:
		showPassive()
	case app.ovPassive:
		showWindow() // взять управление: можно печатать запрос
	default:
		hideWindow()
	}
}

// onDeactivate — пользователь кликнул в игру во время поиска: окно остаётся «в игре».
func onDeactivate(other uintptr) {
	if !app.overlay || app.ovPassive || !isVisible() || app.fade == fadeOut {
		return
	}
	if other != 0 {
		if root, _, _ := pGetAncestor.Call(other, 3 /*GA_ROOTOWNER*/); root == app.hwnd {
			return // окно настроек или сообщение самой программы
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(other, uintptr(unsafePtr(&pid)))
		if pid == uint32(myPID()) {
			return
		}
	}
	setPassive(true)
	pSetWindowPos.Call(app.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	setStatus("Alt+↑/↓ — листать   ·   " + app.hkLabel + " — искать   ·   ещё раз " + app.hkLabel + " — спрятать")
}

// leftWidths — ширина меню слева и области логотипа (в компактном оверлее меню скрыто).
func leftWidths(w int32) (side, brand int32) {
	if app.ovPassive && app.ovCompact {
		return 0, s(62)
	}
	sw := sideWidth(w)
	return sw, sw
}

func ovTargetAlpha() int {
	if app.ovPassive {
		return app.ovOpacity * 255 / 100
	}
	return app.opacity * 255 / 100
}
