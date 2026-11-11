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
	"ПоверхВсехОкон=да\r\n" +
	"\r\n" +
	"; Оформление (удобнее менять в окне «Настройки» — значок шестерёнки)\r\n" +
	"; Цвета: Фиолетовый, Синий, Бирюзовый, Зелёный, Оранжевый, Красный, Розовый\r\n" +
	"Акцент=Фиолетовый\r\n" +
	"; Непрозрачность окна в процентах: от 40 до 100\r\n" +
	"Прозрачность=100\r\n" +
	"Анимации=да\r\n" +
	"\r\n" +
	"; Режим поверх игры: окно не сворачивает игру (в игре нужен режим «Окно без рамки»).\r\n" +
	"; да / нет / компактно (без меню слева)\r\n" +
	"Оверлей=да\r\n" +
	"; Непрозрачность окна поверх игры: от 30 до 100\r\n" +
	"Прозрачность_Оверлея=85\r\n" +
	"\r\n" +
	"; ИИ: yandex, claude, deepseek, openrouter, openai, gemini, ollama, lmstudio, custom\r\n" +
	"; Ключ после сохранения в окне настроек хранится зашифрованным.\r\n" +
	"ИИ_Сервис=yandex\r\n" +
	"ИИ_Ключ=\r\n" +
	"ИИ_Каталог=\r\n" +
	"ИИ_Модель=\r\n" +
	"ИИ_Адрес=\r\n"

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
	app.onTop = !isNo(v)

	app.accent = accentIndex(m["акцент"])
	app.opacity = 100
	if o, err := strconv.Atoi(strings.TrimSuffix(m["прозрачность"], "%")); err == nil {
		app.opacity = clampInt(o, 40, 100)
	}
	app.anim = !isNo(strings.ToLower(m["анимации"]))
	ov := strings.ToLower(m["оверлей"])
	app.overlay = !isNo(ov)
	app.ovCompact = strings.HasPrefix(ov, "компакт")
	app.ovOpacity = 85
	if o, err := strconv.Atoi(strings.TrimSuffix(m["прозрачность_оверлея"], "%")); err == nil {
		app.ovOpacity = clampInt(o, 30, 100)
	}

	app.ai = AIConfig{
		Provider: m["ии_сервис"],
		Key:      unprotect(m["ии_ключ"]),
		Folder:   m["ии_каталог"],
		Model:    m["ии_модель"],
		BaseURL:  m["ии_адрес"],
	}
	if app.ai.Provider == "" {
		app.ai.Provider = AIProviders[0].ID
	}
	// ключ, вписанный вручную открытым текстом, сразу шифруем
	if k := m["ии_ключ"]; k != "" && !strings.HasPrefix(k, "dpapi:") {
		if enc := protect(k); enc != "" {
			setSetting("ИИ_Ключ", enc)
		}
	}
}

func isNo(v string) bool {
	return v == "нет" || v == "no" || v == "0" || v == "false" || v == "выкл"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// setSetting меняет одну строку settings.ini, сохраняя комментарии и остальные строки.
func setSetting(key, val string) {
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		b = []byte(defaultSettings)
	}
	lines := strings.Split(string(b), "\n")
	lk := strings.ToLower(key)
	found := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if ok && strings.ToLower(strings.TrimSpace(k)) == lk {
			lines[i] = key + "=" + val + "\r"
			found = true
		}
	}
	out := strings.Join(lines, "\n")
	if !found {
		out = strings.TrimRight(out, "\r\n") + "\r\n" + key + "=" + val + "\r\n"
	}
	os.WriteFile(settingsPath(), []byte(out), 0644)
}

func saveAISettings() {
	setSetting("ИИ_Сервис", app.ai.Provider)
	setSetting("ИИ_Ключ", protect(strings.TrimSpace(app.ai.Key)))
	setSetting("ИИ_Каталог", strings.TrimSpace(app.ai.Folder))
	setSetting("ИИ_Модель", strings.TrimSpace(app.ai.Model))
	setSetting("ИИ_Адрес", strings.TrimSpace(app.ai.BaseURL))
}

func saveLookSettings() {
	setSetting("Акцент", accents[app.accent].Name)
	setSetting("Прозрачность", strconv.Itoa(app.opacity))
	if app.anim {
		setSetting("Анимации", "да")
	} else {
		setSetting("Анимации", "нет")
	}
	switch {
	case !app.overlay:
		setSetting("Оверлей", "нет")
	case app.ovCompact:
		setSetting("Оверлей", "компактно")
	default:
		setSetting("Оверлей", "да")
	}
	setSetting("Прозрачность_Оверлея", strconv.Itoa(app.ovOpacity))
}

func saveSettingOnTop() {
	if app.onTop {
		setSetting("ПоверхВсехОкон", "да")
	} else {
		setSetting("ПоверхВсехОкон", "нет")
	}
}

type winState struct {
	X, Y, W, H, Zoom, Doc int
	Has                   bool
}

var state winState

func openGroups() []string {
	var out []string
	for g, on := range app.open {
		if on {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

func loadState() {
	app.open = map[string]bool{}
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
	if v, ok := m["open"]; ok {
		app.openSet = true
		for _, g := range strings.Split(v, "|") {
			if g = strings.TrimSpace(g); g != "" {
				app.open[g] = true
			}
		}
	}
}

func writeState(x, y, w, h int) {
	s := "; Служебный файл: положение окна и масштаб. Можно удалить — настройки сбросятся.\r\n" +
		"x=" + strconv.Itoa(x) + "\r\ny=" + strconv.Itoa(y) + "\r\nw=" + strconv.Itoa(w) + "\r\nh=" + strconv.Itoa(h) +
		"\r\nzoom=" + strconv.Itoa(app.zoom) + "\r\ndoc=" + strconv.Itoa(app.cur) +
		"\r\nopen=" + strings.Join(openGroups(), "|") + "\r\n"
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
