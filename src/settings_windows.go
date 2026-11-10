package main

// Настройки (settings.ini), состояние окна (state.ini) и избранное (favorites.txt).

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

const defaultSettings = "; Настройки Nova Helper\r\n" +
	"; После изменения сохраните файл и перезапустите Nova Helper\r\n" +
	"; (правый клик по значку в трее → Выход, затем снова запустите программу).\r\n" +
	"\r\n" +
	"; Клавиша, которая открывает и прячет Nova Helper.\r\n" +
	"; Примеры: Alt+Q, Ctrl+Shift+F, F10, Alt+F1\r\n" +
	"Клавиша=Alt+Q\r\n" +
	"\r\n" +
	"; Окно поверх всех окон (да / нет)\r\n" +
	"ПоверхВсехОкон=да\r\n"

func readKV(path string) (map[string]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, false
	}
	m := map[string]string{}
	for _, l := range strings.Split(strings.TrimPrefix(string(b), "\uFEFF"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, ";") || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if ok {
			m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return m, true
}

func settingsPath() string  { return app.dir + string(os.PathSeparator) + "settings.ini" }
func statePath() string     { return app.dir + string(os.PathSeparator) + "state.ini" }
func favoritesPath() string { return app.dir + string(os.PathSeparator) + "favorites.txt" }

func loadSettings() {
	m, ok := readKV(settingsPath())
	if !ok {
		os.WriteFile(settingsPath(), []byte(defaultSettings), 0644)
		m, _ = readKV(settingsPath())
	}
	key := m["клавиша"]
	mods, vk, label, good := ParseHotkey(key)
	if !good {
		mods, vk, label, _ = ParseHotkey("Alt+Q")
	}
	app.hkMods, app.hkVK, app.hkLabel = mods, vk, label
	v := strings.ToLower(m["поверхвсехокон"])
	app.onTop = !(v == "нет" || v == "no" || v == "0" || v == "false")
}

func saveSettingOnTop() {
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		b = []byte(defaultSettings)
	}
	val := "нет"
	if app.onTop {
		val = "да"
	}
	lines := strings.Split(string(b), "\n")
	found := false
	for i, l := range lines {
		t := strings.ToLower(strings.TrimSpace(l))
		if strings.HasPrefix(t, "поверхвсехокон") {
			lines[i] = "ПоверхВсехОкон=" + val + "\r"
			found = true
		}
	}
	out := strings.Join(lines, "\n")
	if !found {
		out = strings.TrimRight(out, "\r\n") + "\r\nПоверхВсехОкон=" + val + "\r\n"
	}
	os.WriteFile(settingsPath(), []byte(out), 0644)
}

type winState struct {
	X, Y, W, H, Zoom, Doc int
	Has                   bool
}

var state winState

func loadState() {
	m, ok := readKV(statePath())
	if !ok {
		app.zoom = 100
		return
	}
	n := func(k string) int { v, _ := strconv.Atoi(m[k]); return v }
	state = winState{X: n("x"), Y: n("y"), W: n("w"), H: n("h"), Zoom: n("zoom"), Doc: n("doc")}
	state.Has = state.W > 200 && state.H > 200
	app.zoom = state.Zoom
	if app.zoom <= 0 {
		app.zoom = 100
	}
	app.cur = state.Doc
	if app.cur < 0 {
		app.cur = 0
	}
}

func writeState(x, y, w, h int) {
	s := "; Служебный файл: положение окна и масштаб. Можно удалить — настройки сбросятся.\r\n" +
		"x=" + strconv.Itoa(x) + "\r\ny=" + strconv.Itoa(y) + "\r\nw=" + strconv.Itoa(w) + "\r\nh=" + strconv.Itoa(h) +
		"\r\nzoom=" + strconv.Itoa(app.zoom) + "\r\ndoc=" + strconv.Itoa(app.cur) + "\r\n"
	os.WriteFile(statePath(), []byte(s), 0644)
}

func loadFavs() {
	app.favs = map[string]bool{}
	b, err := os.ReadFile(favoritesPath())
	if err != nil {
		return
	}
	for _, l := range strings.Split(strings.TrimPrefix(string(b), "\uFEFF"), "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" && !strings.HasPrefix(l, ";") {
			app.favs[l] = true
		}
	}
}

func saveFavs() {
	keys := make([]string, 0, len(app.favs))
	for k := range app.favs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := "; Избранные пункты (заполняется программой)\r\n" + strings.Join(keys, "\r\n") + "\r\n"
	os.WriteFile(favoritesPath(), []byte(s), 0644)
}

func restorePlacement() {
	if !state.Has {
		return
	}
	vx, _, _ := pGetSystemMetrics.Call(76)
	vy, _, _ := pGetSystemMetrics.Call(77)
	vw, _, _ := pGetSystemMetrics.Call(78)
	vh, _, _ := pGetSystemMetrics.Call(79)
	x0, y0 := int(int32(vx)), int(int32(vy))
	if state.X < x0-50 || state.Y < y0-20 || state.X+100 > x0+int(vw) || state.Y+60 > y0+int(vh) {
		return // окно оказалось бы за пределами экрана
	}
	pSetWindowPos.Call(app.hwnd, 0, uintptr(state.X), uintptr(state.Y), uintptr(state.W), uintptr(state.H), 0x0004|SWP_NOACTIVATE)
}

func saveState() {
	if app.hwnd == 0 {
		return
	}
	readZoom()
	var wp WINDOWPLACEMENT
	wp.Length = uint32(unsafe.Sizeof(wp))
	pGetWindowPlacement.Call(app.hwnd, uintptr(unsafe.Pointer(&wp)))
	r := wp.RcNormalPosition
	if r.Right-r.Left < 200 {
		return
	}
	writeState(int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top))
}
