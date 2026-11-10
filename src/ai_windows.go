package main

// ИИ в окне программы: режим вопроса, запрос в фоне, показ ответа.
// Кнопки «Спросить ИИ» и «Настройки» в верхней панели.

import (
	"strings"
	"sync"
	"unsafe"
)

type aiState struct {
	Question string
	Answer   []string
	Cited    []*Point
	Related  []*Point
	Meta     string
	Err      string
	Thinking bool
	Intro    bool
}

var (
	aiMu  sync.Mutex
	aiOut = map[int]struct {
		ans string
		err error
	}{}
	aiCtx = map[int][]*Point{}
)

const aiCue = "Спросите ИИ: можно ли стрелять в зелёной зоне, если по мне стреляют?"
const searchCue = "Поиск по всем правилам: DM, маска, 4.15…   ·   Ctrl+Enter — спросить ИИ"

func enterAIMode() {
	if !app.aiMode {
		app.aiMode = true
		SendMessage(app.edit, EM_SETCUEBANNER, 1, u16p(aiCue))
		pKillTimer.Call(app.hwnd, tmrSearch)
	}
	if app.mode != modeAI {
		if app.aiView.Question == "" {
			app.aiView = aiState{Intro: true}
		}
		// если ответ уже был — возвращаемся к нему
		app.mode = modeAI
		renderAIView()
		rebuildList()
	}
	pSetFocus.Call(app.edit)
	SendMessage(app.edit, EM_SETSEL, 0, ^uintptr(0))
	setStatus("Напишите вопрос и нажмите Enter   ·   Esc — выйти из режима ИИ   ·   Ctrl+I — включить/выключить")
	kickAnim()
	invalidateSearchBox()
	pInvalidateRect.Call(app.list, 0, 0)
}

func exitAIMode() {
	if !app.aiMode {
		return
	}
	app.aiMode = false
	SendMessage(app.edit, EM_SETCUEBANNER, 1, u16p(searchCue))
	kickAnim()
	invalidateSearchBox()
	pInvalidateRect.Call(app.list, 0, 0)
	if app.mode == modeAI && !app.aiBusy && app.aiView.Intro {
		showDoc(app.cur)
	} else {
		setStatus("Режим ИИ выключен   ·   Ctrl+I — включить снова")
	}
}

func providerLine() string {
	p, _, model := app.ai.Effective()
	if p.ID == "yandex" {
		if i := strings.LastIndex(model, "/"); i >= 0 {
			model = model[i+1:]
		}
	}
	return p.Name + "  ·  " + model
}

// startAsk — отправить вопрос ИИ.
func startAsk(q string) {
	q = strings.TrimSpace(q)
	if !app.aiMode {
		enterAIMode()
	}
	if q == "" {
		setStatus("Сначала напишите вопрос в поле сверху")
		pSetFocus.Call(app.edit)
		return
	}
	if app.aiBusy {
		setStatus("ИИ ещё отвечает на прошлый вопрос — подождите пару секунд")
		return
	}
	if pr := app.ai.Problem(); pr != "" {
		app.aiView = aiState{Question: q, Err: pr + " Откройте настройки и укажите сервис и ключ."}
		app.mode = modeAI
		renderAIView()
		rebuildList()
		openSettings(pr)
		return
	}
	pts := Relevant(app.docs, q, 40)
	ctx, used := BuildContext(pts, 14000)
	app.aiReq++
	id := app.aiReq
	aiMu.Lock()
	aiCtx[id] = used
	aiMu.Unlock()
	app.aiBusy = true
	app.dots = 0
	app.mode = modeAI
	app.aiView = aiState{Question: q, Thinking: true, Related: firstN(used, 4), Meta: providerLine()}
	renderAIView()
	rebuildList()
	setStatus("ИИ думает…")
	kickAnim()
	cfg := app.ai
	go func() {
		ans, err := AskAI(cfg, q, ctx)
		aiMu.Lock()
		aiOut[id] = struct {
			ans string
			err error
		}{ans, err}
		aiMu.Unlock()
		PostMessage(app.hwnd, wmAI, uintptr(id), 0)
	}()
}

func firstN(p []*Point, n int) []*Point {
	if len(p) > n {
		return p[:n]
	}
	return p
}

func onAIResult(id int) {
	aiMu.Lock()
	res, ok := aiOut[id]
	used := aiCtx[id]
	delete(aiOut, id)
	delete(aiCtx, id)
	aiMu.Unlock()
	if !ok || id != app.aiReq {
		return
	}
	app.aiBusy = false
	v := app.aiView
	v.Thinking = false
	if res.err != nil {
		v.Err = res.err.Error()
		setStatus("Не получилось получить ответ ИИ")
	} else {
		v.Answer = CleanAnswer(res.ans)
		v.Cited = Cited(res.ans, used)
		if len(v.Cited) == 0 {
			v.Related = firstN(used, 5)
		} else {
			v.Related = nil
		}
		setStatus("Ответ готов   ·   Двойной клик по пункту — открыть в документе   ·   Enter — задать новый вопрос")
	}
	app.aiView = v
	if app.mode == modeAI {
		renderAIView()
	}
	pInvalidateRect.Call(app.list, 0, 0)
	if !isVisible() {
		balloon("Ответ ИИ готов", "Нажмите "+app.hkLabel+", чтобы посмотреть.")
	}
}

func renderAIView() {
	readZoom()
	v := app.aiView
	if v.Intro {
		setRTF(RenderAIIntro(providerLine(), app.ai.Problem(), app.errs))
		return
	}
	setRTF(RenderAI(v.Question, v.Answer, v.Cited, v.Related, app.favs, v.Meta, v.Err, v.Thinking, app.errs))
}

// rerender перерисовывает текущий вид (после смены цвета оформления).
func rerender() {
	if app.rich == 0 {
		return
	}
	first := int(SendMessage(app.rich, EM_GETFIRSTVISIBLELINE, 0, 0))
	switch app.mode {
	case modeDoc:
		if app.cur < len(app.docs) {
			readZoom()
			setRTF(RenderDoc(app.docs[app.cur], app.favs, app.errs))
		}
	case modeFavs:
		showFavs()
	case modeSearch:
		doSearch()
	case modeAI:
		renderAIView()
	}
	SendMessage(app.rich, EM_LINESCROLL, 0, uintptr(first))
}

// ------------------------------------------------------------------ кнопки в верхней панели

func inRect(r RECT, x, y int32) bool { return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom }

func onTopMouse(lp uintptr) {
	x, y := int32(int16(lp&0xFFFF)), int32(int16((lp>>16)&0xFFFF))
	h := 0
	switch {
	case inRect(app.rcAI, x, y):
		h = 1
	case inRect(app.rcGear, x, y):
		h = 2
	}
	if h != app.hoverTop {
		app.hoverTop = h
		kickAnim()
	}
	trackLeave(app.hwnd)
}

func onTopClick(lp uintptr) {
	x, y := int32(int16(lp&0xFFFF)), int32(int16((lp>>16)&0xFFFF))
	switch {
	case inRect(app.rcAI, x, y):
		if app.aiMode {
			if q := strings.TrimSpace(getWindowText(app.edit)); q != "" {
				startAsk(q)
			} else {
				exitAIMode()
			}
		} else {
			q := strings.TrimSpace(getWindowText(app.edit))
			enterAIMode()
			if q != "" && app.mode == modeAI && app.aiView.Intro {
				// в поле уже был текст — сразу спрашиваем
				startAsk(q)
			}
		}
	case inRect(app.rcGear, x, y):
		openSettings("")
	}
}

func paintTopButtons(hdc uintptr) {
	// «Спросить ИИ»
	r := app.rcAI
	hv := easeOut(app.aiHover)
	var bg, fg, border uintptr
	if app.aiMode {
		bg = mix(clrAccent, mix(clrAccent, RGB(255, 255, 255), 0.15), hv)
		fg = clrBg
		border = bg
	} else {
		bg = mix(clrPanel2, mix(clrPanel2, clrAccent, 0.28), hv)
		fg = mix(clrAccent, mix(clrAccent, RGB(255, 255, 255), 0.4), hv)
		border = mix(mix(clrLine, clrAccent, 0.45), clrAccent, hv)
	}
	pSelectObject.Call(hdc, mustBrush(bg))
	pSelectObject.Call(hdc, mustPenW(border, s(1)))
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(s(10)), uintptr(s(10)))
	pSetTextColor.Call(hdc, fg)
	wide := r.Right-r.Left > s(60)
	if wide {
		pSelectObject.Call(hdc, app.fIcon)
		ir := RECT{r.Left + s(10), r.Top, r.Left + s(30), r.Bottom}
		drawText(hdc, "✦", &ir, DT_SINGLELINE|DT_VCENTER|0x0001)
		pSelectObject.Call(hdc, app.fPill)
		tr := RECT{r.Left + s(32), r.Top, r.Right - s(8), r.Bottom}
		label := "Спросить ИИ"
		if app.aiBusy {
			label = "Думаю…"
		}
		drawText(hdc, label, &tr, DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX)
	} else {
		pSelectObject.Call(hdc, app.fIcon)
		drawText(hdc, "✦", &r, DT_SINGLELINE|DT_VCENTER|0x0001)
	}

	// шестерёнка «Настройки»
	g := app.rcGear
	gh := easeOut(app.gearHover)
	pSelectObject.Call(hdc, mustBrush(mix(clrPanel2, mix(clrPanel2, clrAccent, 0.2), gh)))
	pSelectObject.Call(hdc, mustPenW(mix(clrLine, clrAccent, gh), s(1)))
	pRoundRect.Call(hdc, uintptr(g.Left), uintptr(g.Top), uintptr(g.Right), uintptr(g.Bottom), uintptr(s(10)), uintptr(s(10)))
	drawGear(hdc, (g.Left+g.Right)/2, (g.Top+g.Bottom)/2, app.gearSpin, mix(clrMuted, clrText, gh), mix(clrPanel2, mix(clrPanel2, clrAccent, 0.2), gh))
}

// drawGear рисует шестерёнку; при наведении она плавно поворачивается.
func drawGear(hdc uintptr, cx, cy int32, spin float64, c, bg uintptr) {
	const teeth = 8
	ro, ri := float64(s(8)), float64(s(6))
	var pts [teeth * 4]POINT
	base := spin * 3.14159265 / 4
	for i := 0; i < teeth; i++ {
		a := base + float64(i)*2*3.14159265/teeth
		w := 0.22
		for j, d := range []struct{ ang, r float64 }{{a - w*1.4, ri}, {a - w, ro}, {a + w, ro}, {a + w*1.4, ri}} {
			pts[i*4+j] = POINT{cx + int32(cos(d.ang)*d.r+0.5), cy + int32(sin(d.ang)*d.r+0.5)}
		}
	}
	pSelectObject.Call(hdc, mustBrush(c))
	pSelectObject.Call(hdc, mustPen(c))
	pPolygon.Call(hdc, uintptr(unsafe.Pointer(&pts[0])), teeth*4)
	hole := s(3)
	circle(hdc, cx, cy, hole, bg)
}
