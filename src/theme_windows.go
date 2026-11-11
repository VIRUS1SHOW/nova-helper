package main

// Цвета оформления, прозрачность и плавное появление окна.

import "math"

type accentDef struct {
	Name    string
	R, G, B int
}

var accents = []accentDef{
	{"Фиолетовый", 150, 136, 255},
	{"Синий", 90, 160, 255},
	{"Бирюзовый", 45, 212, 191},
	{"Зелёный", 79, 209, 139},
	{"Оранжевый", 255, 159, 67},
	{"Красный", 255, 107, 107},
	{"Розовый", 255, 122, 182},
}

func accentIndex(name string) int {
	n := Norm(name)
	for i, a := range accents {
		if Norm(a.Name) == n {
			return i
		}
	}
	return 0
}

// Цвета групп документов в меню слева.
var groupColors = map[string]uintptr{
	"Основные":                  RGB(90, 169, 255),
	"Криминал":                  RGB(255, 107, 107),
	"Госструктуры":              RGB(94, 200, 255),
	"Мероприятия":               RGB(255, 159, 67),
	"Неофициальные организации": RGB(79, 209, 139),
}

func groupColor(g string) uintptr {
	if c, ok := groupColors[g]; ok {
		return c
	}
	return clrMuted
}

// applyAccent пересоздаёт кисти под выбранный цвет и перерисовывает окно.
func applyAccent(i int) {
	if i < 0 || i >= len(accents) {
		i = 0
	}
	app.accent = i
	a := accents[i]
	clrAccent = RGB(a.R, a.G, a.B)
	clrActive = mix(clrPanel, clrAccent, 0.22)
	clrHover = mix(clrPanel, clrAccent, 0.10)
	palette[cAccent-1] = [3]int{a.R, a.G, a.B}
	for _, h := range []uintptr{app.brAccent, app.brActive, app.penAccent} {
		if h != 0 {
			pDeleteObject.Call(h)
		}
	}
	app.brAccent, _, _ = pCreateSolidBrush.Call(clrAccent)
	app.brActive, _, _ = pCreateSolidBrush.Call(clrActive)
	app.penAccent, _, _ = pCreatePen.Call(0, uintptr(s(1)), clrAccent)
	if app.hwnd != 0 {
		rerender()
		pInvalidateRect.Call(app.hwnd, 0, 1)
		pInvalidateRect.Call(app.list, 0, 1)
	}
}

// ---------------------------------------------------------------- прозрачность и появление

func targetAlpha() int { return ovTargetAlpha() }

func applyOpacity() {
	if app.fade == 0 {
		setAlpha(app.hwnd, targetAlpha())
	}
}

const (
	fadeIn  = 1
	fadeOut = -1
)

func startFade(dir int) {
	if !app.anim {
		if dir == fadeOut {
			pShowWindow.Call(app.hwnd, SW_HIDE)
		}
		setAlpha(app.hwnd, targetAlpha())
		app.fade = 0
		return
	}
	app.fade = dir
	kickAnim()
}

// stepFade — один кадр появления/исчезновения. Возвращает true, пока анимация идёт.
func stepFade(dt float64) bool {
	if app.fade == 0 {
		return false
	}
	app.fadeT += float64(app.fade) * dt / 0.16
	if app.fadeT >= 1 {
		app.fadeT = 1
		setAlpha(app.hwnd, targetAlpha())
		app.fade = 0
		return false
	}
	if app.fadeT <= 0 {
		app.fadeT = 0
		setAlpha(app.hwnd, 0)
		pShowWindow.Call(app.hwnd, SW_HIDE)
		app.fade = 0
		return false
	}
	setAlpha(app.hwnd, int(float64(targetAlpha())*easeOut(app.fadeT)))
	return true
}

func sin(x float64) float64 { return math.Sin(x) }
func cos(x float64) float64 { return math.Cos(x) }
