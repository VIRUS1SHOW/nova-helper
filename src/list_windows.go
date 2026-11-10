package main

// Меню слева: группы документов (сворачиваются), документы, разделы текущего документа,
// «Избранное» и «Спросить ИИ». Плавное появление пунктов и подсветка при наведении.

import (
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	liHeader = iota
	liDoc
	liFavs
	liSep
	liSec
	liGroup
	liAI
)

type listItem struct {
	kind  int
	text  string
	count int
	idx   int
	group string
}

func (it listItem) key() string {
	switch it.kind {
	case liDoc:
		return "d:" + strconv.Itoa(it.idx)
	case liSec:
		return "s:" + strconv.Itoa(it.idx)
	case liGroup:
		return "g:" + it.group
	case liFavs:
		return "favs"
	case liAI:
		return "ai"
	case liHeader:
		return "h:" + it.text
	}
	return "sep:" + it.text
}

func clickable(k int) bool {
	return k == liDoc || k == liSec || k == liGroup || k == liFavs || k == liAI
}

// ------------------------------------------------------------------ построение списка

func rebuildList() {
	prev := map[string]bool{}
	for _, it := range app.items {
		prev[it.key()] = true
	}
	first := len(app.items) == 0

	items := []listItem{
		{kind: liSep, text: "top"},
		{kind: liAI, text: "Спросить ИИ"},
		{kind: liFavs, text: "Избранное", count: countFavs()},
		{kind: liSep, text: "mid"},
		{kind: liHeader, text: "ПРАВИЛА"},
	}
	for _, g := range Groups(app.docs) {
		n := 0
		for _, d := range app.docs {
			if d.Group == g {
				n++
			}
		}
		items = append(items, listItem{kind: liGroup, text: g, count: n, group: g})
		if !app.open[g] {
			continue
		}
		for i, d := range app.docs {
			if d.Group != g {
				continue
			}
			items = append(items, listItem{kind: liDoc, text: d.Short, count: len(d.Points), idx: i, group: g})
			if app.mode == modeDoc && app.cur == i {
				for si, sct := range d.Sections {
					if sct.Title != "" {
						items = append(items, listItem{kind: liSec, text: sct.Title, idx: si, group: g})
					}
				}
			}
		}
	}
	app.items = items

	for _, it := range app.items {
		k := it.key()
		if _, ok := app.appear[k]; !ok || !prev[k] {
			if app.anim && !first && it.kind != liSep && it.kind != liHeader {
				app.appear[k] = 0
			} else {
				app.appear[k] = 1
			}
		}
	}

	top := SendMessage(app.list, LB_GETTOPINDEX, 0, 0)
	SendMessage(app.list, WM_SETREDRAW, 0, 0)
	SendMessage(app.list, LB_RESETCONTENT, 0, 0)
	for i := range app.items {
		SendMessage(app.list, LB_ADDSTRING, 0, uintptr(i))
	}
	if !first {
		SendMessage(app.list, LB_SETTOPINDEX, top, 0)
	}
	SendMessage(app.list, WM_SETREDRAW, 1, 0)
	pInvalidateRect.Call(app.list, 0, 1)
	kickAnim()
}

// ensureOpen раскрывает группу текущего документа.
func ensureOpen(i int) {
	if i >= 0 && i < len(app.docs) {
		g := app.docs[i].Group
		if !app.open[g] {
			app.open[g] = true
			kickAnim()
		}
	}
}

func countFavs() int {
	n := 0
	for _, d := range app.docs {
		for _, p := range d.Points {
			if app.favs[FavKey(p)] {
				n++
			}
		}
	}
	return n
}

// ------------------------------------------------------------------ размеры и отрисовка

func measureItem(mis *MEASUREITEMSTRUCT) {
	i := int(mis.ItemData)
	if i < 0 || i >= len(app.items) {
		mis.ItemHeight = uint32(s(30))
		return
	}
	it := app.items[i]
	switch it.kind {
	case liHeader:
		mis.ItemHeight = uint32(s(28))
	case liSep:
		mis.ItemHeight = uint32(s(8))
	case liGroup:
		mis.ItemHeight = uint32(s(36))
	case liDoc:
		mis.ItemHeight = uint32(s(31))
	case liSec:
		var rc RECT
		pGetClientRect.Call(app.list, uintptr(unsafe.Pointer(&rc)))
		width := rc.Right
		if width < s(100) {
			width = sideWidth(s(1000))
		}
		hdc, _, _ := pGetDC.Call(app.list)
		pSelectObject.Call(hdc, app.fSmall)
		r := RECT{0, 0, width - s(64), 0}
		drawText(hdc, it.text, &r, DT_CALCRECT|DT_WORDBREAK|DT_NOPREFIX)
		pReleaseDC.Call(app.list, hdc)
		mis.ItemHeight = uint32(r.Bottom + s(10))
	default:
		mis.ItemHeight = uint32(s(36))
	}
}

func roundFill(hdc uintptr, l, t, r, b, rad int32, c uintptr) {
	pSelectObject.Call(hdc, mustBrush(c))
	pSelectObject.Call(hdc, mustPen(c))
	pRoundRect.Call(hdc, uintptr(l), uintptr(t), uintptr(r), uintptr(b), uintptr(rad), uintptr(rad))
}

func circle(hdc uintptr, cx, cy, rad int32, c uintptr) {
	pSelectObject.Call(hdc, mustBrush(c))
	pSelectObject.Call(hdc, mustPen(c))
	pEllipse.Call(hdc, uintptr(cx-rad), uintptr(cy-rad), uintptr(cx+rad+1), uintptr(cy+rad+1))
}

// chevron — треугольник, поворачивается от «▸» (t=0) к «▾» (t=1).
func chevron(hdc uintptr, cx, cy int32, t float64, c uintptr) {
	sz := float64(s(4))
	// точки «▸»: (-0.6,-1) (0.9,0) (-0.6,1); поворот на 90° * t
	pts := [3][2]float64{{-0.6, -1}, {0.9, 0}, {-0.6, 1}}
	ang := t * 3.14159265 / 2
	cs, sn := cos(ang), sin(ang)
	var out [3]POINT
	for i, p := range pts {
		x := p[0]*cs - p[1]*sn
		y := p[0]*sn + p[1]*cs
		out[i] = POINT{cx + int32(x*sz+0.5), cy + int32(y*sz+0.5)}
	}
	pSelectObject.Call(hdc, mustBrush(c))
	pSelectObject.Call(hdc, mustPen(c))
	pPolygon.Call(hdc, uintptr(unsafe.Pointer(&out[0])), 3)
}

func drawItem(dis *DRAWITEMSTRUCT) {
	if dis.ItemID == 0xFFFFFFFF {
		return
	}
	i := int(dis.ItemData)
	if i < 0 || i >= len(app.items) {
		return
	}
	it := app.items[i]
	hdc := dis.HDC
	rc := dis.RcItem
	fillRect(hdc, &rc, app.brPanel)
	pSetBkMode.Call(hdc, TRANSPARENT)

	k := it.key()
	e := easeOut(app.appear[k])
	hv := app.hover[k]
	dx := int32(float64(s(14)) * (1 - e)) // новые пункты «въезжают» слева
	fade := func(c uintptr) uintptr { return mix(clrPanel, c, e) }

	active := false
	switch it.kind {
	case liDoc:
		active = app.mode == modeDoc && app.cur == it.idx
	case liFavs:
		active = app.mode == modeFavs
	case liAI:
		active = app.mode == modeAI || app.aiMode
	case liSec:
		active = app.mode == modeDoc && app.curSec == it.idx
	}

	// фон при наведении / выбранный пункт
	if clickable(it.kind) && it.kind != liSec {
		l := rc.Left + s(8)
		if it.kind == liDoc {
			l = rc.Left + s(22)
		}
		bg := clrPanel
		if hv > 0 {
			bg = mix(clrPanel, clrHover, hv)
		}
		if active {
			bg = mix(clrActive, mix(clrActive, clrAccent, 0.25), hv)
		}
		if bg != clrPanel {
			roundFill(hdc, l+dx, rc.Top+s(2), rc.Right-s(8), rc.Bottom-s(2), s(9), fade(bg))
		}
		if active && it.kind == liDoc {
			bar := RECT{l + dx, rc.Top + s(8), l + dx + s(3), rc.Bottom - s(8)}
			fillRect(hdc, &bar, mustBrush(fade(clrAccent)))
		}
	}

	switch it.kind {
	case liHeader:
		pSelectObject.Call(hdc, app.fHeader)
		pSetTextColor.Call(hdc, fade(clrFaint))
		r := RECT{rc.Left + s(18), rc.Top + s(6), rc.Right - s(10), rc.Bottom}
		drawText(hdc, it.text, &r, DT_SINGLELINE|DT_VCENTER|DT_NOPREFIX)

	case liSep:
		if it.text == "mid" {
			ln := RECT{rc.Left + s(18), rc.Top + s(4), rc.Right - s(18), rc.Top + s(5)}
			fillRect(hdc, &ln, mustBrush(clrLine))
		}

	case liGroup:
		gc := groupColor(it.group)
		cy := (rc.Top + rc.Bottom) / 2
		chevron(hdc, rc.Left+s(22)+dx, cy, app.openAmt[it.group], fade(mix(clrFaint, clrText, hv)))
		circle(hdc, rc.Left+s(38)+dx, cy, s(4), fade(gc))
		pSelectObject.Call(hdc, app.fUI)
		pSetTextColor.Call(hdc, fade(mix(mix(clrMuted, clrText, 0.55), clrText, hv)))
		r := RECT{rc.Left + s(50) + dx, rc.Top, rc.Right - s(48), rc.Bottom}
		drawText(hdc, it.text, &r, DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX)
		pill(hdc, rc, it.count, fade(mix(clrPanel2, gc, 0.18)), fade(mix(clrMuted, gc, 0.5)))

	case liDoc:
		// направляющая линия группы
		guide := RECT{rc.Left + s(22), rc.Top, rc.Left + s(23), rc.Bottom}
		fillRect(hdc, &guide, mustBrush(clrLine))
		pSelectObject.Call(hdc, app.fDoc)
		c := mix(clrMuted, clrText, hv)
		if active {
			c = clrText
		}
		pSetTextColor.Call(hdc, fade(c))
		r := RECT{rc.Left + s(34) + dx, rc.Top, rc.Right - s(52), rc.Bottom}
		drawText(hdc, it.text, &r, DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX)
		pSelectObject.Call(hdc, app.fSmall)
		pSetTextColor.Call(hdc, fade(clrFaint))
		r2 := RECT{rc.Right - s(60), rc.Top, rc.Right - s(18), rc.Bottom}
		drawText(hdc, strconv.Itoa(it.count), &r2, DT_SINGLELINE|DT_VCENTER|DT_RIGHT)

	case liSec:
		guide := RECT{rc.Left + s(22), rc.Top, rc.Left + s(23), rc.Bottom}
		fillRect(hdc, &guide, mustBrush(clrLine))
		c := mix(clrFaint, clrText, hv)
		if active {
			bar := RECT{rc.Left + s(36) + dx, rc.Top + s(4), rc.Left + s(38) + dx, rc.Bottom - s(4)}
			fillRect(hdc, &bar, mustBrush(fade(clrAccent)))
			c = clrText
		}
		pSetTextColor.Call(hdc, fade(c))
		pSelectObject.Call(hdc, app.fSmall)
		r := RECT{rc.Left + s(46) + dx, rc.Top + s(5), rc.Right - s(16), rc.Bottom}
		drawText(hdc, it.text, &r, DT_WORDBREAK|DT_NOPREFIX)

	case liFavs, liAI:
		cy := (rc.Top + rc.Bottom) / 2
		ic := RGB(245, 208, 76)
		sym := "★"
		if it.kind == liAI {
			ic = clrAccent
			sym = "✦"
		}
		pSelectObject.Call(hdc, app.fIcon)
		pSetTextColor.Call(hdc, fade(ic))
		ir := RECT{rc.Left + s(18) + dx, cy - s(12), rc.Left + s(42) + dx, cy + s(12)}
		drawText(hdc, sym, &ir, DT_SINGLELINE|DT_VCENTER|0x0001)
		pSelectObject.Call(hdc, app.fUI)
		c := mix(clrMuted, clrText, hv)
		if active {
			c = clrText
		}
		pSetTextColor.Call(hdc, fade(c))
		r := RECT{rc.Left + s(48) + dx, rc.Top, rc.Right - s(46), rc.Bottom}
		drawText(hdc, it.text, &r, DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX)
		if it.kind == liFavs && it.count > 0 {
			pill(hdc, rc, it.count, fade(clrPanel2), fade(clrMuted))
		}
		if it.kind == liAI && app.aiBusy {
			// бегущие точки, пока ИИ думает
			for d := 0; d < 3; d++ {
				ph := app.dots - float64(d)*0.33
				a := 0.35 + 0.65*(0.5+0.5*sin(ph*6.2832))
				circle(hdc, rc.Right-s(40)+int32(d)*s(9), cy, s(2), mix(clrPanel, clrAccent, a))
			}
		}
	}
}

// pill — число в скруглённой плашке справа.
func pill(hdc uintptr, rc RECT, n int, bg, fg uintptr) {
	t := strconv.Itoa(n)
	w := s(14) + int32(len(t))*s(7)
	cy := (rc.Top + rc.Bottom) / 2
	l, r := rc.Right-s(16)-w, rc.Right-s(16)
	roundFill(hdc, l, cy-s(9), r, cy+s(9), s(18), bg)
	pSelectObject.Call(hdc, app.fSmall)
	pSetTextColor.Call(hdc, fg)
	tr := RECT{l, cy - s(9), r, cy + s(9)}
	drawText(hdc, t, &tr, DT_SINGLELINE|DT_VCENTER|0x0001)
}

// ------------------------------------------------------------------ клики и наведение

func onListClick() {
	sel := int32(SendMessage(app.list, LB_GETCURSEL, 0, 0))
	if sel < 0 {
		return
	}
	i := int(SendMessage(app.list, LB_GETITEMDATA, uintptr(sel), 0))
	SendMessage(app.list, LB_SETCURSEL, ^uintptr(0), 0)
	if i < 0 || i >= len(app.items) {
		return
	}
	it := app.items[i]
	switch it.kind {
	case liGroup:
		app.open[it.group] = !app.open[it.group]
		rebuildList()
		saveState()
	case liDoc:
		clearSearchSilently()
		exitAIMode()
		showDoc(it.idx)
	case liFavs:
		clearSearchSilently()
		exitAIMode()
		showFavs()
	case liAI:
		enterAIMode()
	case liSec:
		if it.idx < len(app.rend.Sections) {
			scrollToPos(app.rend.Sections[it.idx])
			app.curSec = it.idx
			pInvalidateRect.Call(app.list, 0, 0)
		}
	}
}

func listItemAt(lp uintptr) (listItem, bool) {
	r := SendMessage(app.list, LB_ITEMFROMPOINT, 0, lp)
	if r>>16 != 0 {
		return listItem{}, false
	}
	i := int(SendMessage(app.list, LB_GETITEMDATA, r&0xFFFF, 0))
	if i < 0 || i >= len(app.items) {
		return listItem{}, false
	}
	return app.items[i], true
}

var listSubclassCB = syscall.NewCallback(func(hwnd, msg, wp, lp, id, ref uintptr) uintptr {
	switch msg {
	case WM_MOUSEMOVE:
		k := ""
		if it, ok := listItemAt(lp); ok && clickable(it.kind) {
			k = it.key()
		}
		if k != app.hoverKey {
			app.hoverKey = k
			kickAnim()
		}
		trackLeave(hwnd)
	case WM_MOUSELEAVE:
		if app.hoverKey != "" {
			app.hoverKey = ""
			kickAnim()
		}
	case WM_SETCURSOR:
		if app.hoverKey != "" {
			handCursor()
			return 1
		}
	}
	r, _, _ := pDefSubclassProc.Call(hwnd, msg, wp, lp)
	return r
})

func hookList() {
	pSetWindowSubclass.Call(app.list, listSubclassCB, 1, 0)
}

// ------------------------------------------------------------------ анимации

const tmrAnim = 6

var lastTick time.Time

func kickAnim() {
	if !app.anim {
		snapAnim()
		return
	}
	if !app.animOn {
		app.animOn = true
		lastTick = time.Now()
		pSetTimer.Call(app.hwnd, tmrAnim, 15, 0)
	}
}

// snapAnim — без анимаций: сразу конечные значения.
func snapAnim() {
	for k := range app.appear {
		app.appear[k] = 1
	}
	for k := range app.hover {
		app.hover[k] = 0
	}
	if app.hoverKey != "" {
		app.hover[app.hoverKey] = 1
	}
	for _, g := range Groups(app.docs) {
		app.openAmt[g] = b2f(app.open[g])
	}
	app.aiHover, app.gearHover = b2f(app.hoverTop == 1), b2f(app.hoverTop == 2)
	app.focusAmt = b2f(mustFocus() == app.edit || app.aiMode)
	pInvalidateRect.Call(app.list, 0, 0)
	invalidateSearchBox()
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// approach двигает v к цели со скоростью 1/dur в секунду.
func approach(v *float64, target, dt, dur float64) bool {
	if *v == target {
		return false
	}
	step := dt / dur
	if *v < target {
		*v += step
		if *v > target {
			*v = target
		}
	} else {
		*v -= step
		if *v < target {
			*v = target
		}
	}
	return true
}

func animTick() {
	now := time.Now()
	dt := now.Sub(lastTick).Seconds()
	lastTick = now
	if dt > 0.05 {
		dt = 0.05
	}
	listDirty, topDirty := false, false
	busy := false

	for k, v := range app.appear {
		if v < 1 {
			app.appear[k] = v + dt/0.24
			if app.appear[k] > 1 {
				app.appear[k] = 1
			}
			listDirty = true
		}
	}
	for _, it := range app.items {
		if clickable(it.kind) {
			k := it.key()
			v := app.hover[k]
			if approach(&v, b2f(k == app.hoverKey), dt, 0.12) {
				app.hover[k] = v
				listDirty = true
			}
		}
	}
	for _, g := range Groups(app.docs) {
		v := app.openAmt[g]
		if approach(&v, b2f(app.open[g]), dt, 0.18) {
			app.openAmt[g] = v
			listDirty = true
		}
	}
	if approach(&app.aiHover, b2f(app.hoverTop == 1), dt, 0.12) {
		topDirty = true
	}
	if approach(&app.gearHover, b2f(app.hoverTop == 2), dt, 0.12) {
		topDirty = true
	}
	if approach(&app.gearSpin, b2f(app.hoverTop == 2), dt, 0.35) {
		topDirty = true
	}
	if approach(&app.focusAmt, b2f(mustFocus() == app.edit || app.aiMode), dt, 0.15) {
		topDirty = true
	}
	if app.aiBusy {
		app.dots += dt / 1.2
		listDirty = true
		busy = true
		app.statusDots += dt
		if app.statusDots > 0.4 {
			app.statusDots = 0
			n := (int(app.dots*3) % 3) + 1
			setStatus("ИИ думает" + "...."[:n] + "   ·   Ответ обычно приходит за 3–15 секунд")
		}
	}
	if stepFade(dt) {
		busy = true
	}
	if listDirty {
		pInvalidateRect.Call(app.list, 0, 0)
	}
	if topDirty {
		invalidateSearchBox()
	}
	if !listDirty && !topDirty && !busy {
		pKillTimer.Call(app.hwnd, tmrAnim)
		app.animOn = false
	}
}
