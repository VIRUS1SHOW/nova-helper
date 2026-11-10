//go:build windows

// Установщик Nova Helper: один exe, внутри которого программа и правила (payload.zip).
// Ставит программу для текущего пользователя в %LOCALAPPDATA%\Programs\Nova Helper
// (без прав администратора, чтобы автообновление могло менять файлы),
// создаёт ярлыки, запись в «Приложения и возможности» и деинсталлятор.
// Запуск с ключом /uninstall удаляет программу.
package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed payload.zip
var payload []byte

const (
	appName    = "Nova Helper"
	appExe     = "NovaHelper.exe"
	appVersion = "1.3.0"
	publisher  = "VIRUS1SHOW"
	regUninst  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\NovaHelper`
	regRun     = `Software\Microsoft\Windows\CurrentVersion\Run`
	runName    = "Nova Helper"
)

// ------------------------------------------------------------------ WinAPI

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassExW   = user32.NewProc("RegisterClassExW")
	pCreateWindowExW    = user32.NewProc("CreateWindowExW")
	pDefWindowProcW     = user32.NewProc("DefWindowProcW")
	pGetMessageW        = user32.NewProc("GetMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessageW   = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW   = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pSendMessageW       = user32.NewProc("SendMessageW")
	pMessageBoxW        = user32.NewProc("MessageBoxW")
	pSetWindowTextW     = user32.NewProc("SetWindowTextW")
	pEnableWindow       = user32.NewProc("EnableWindow")
	pShowWindow         = user32.NewProc("ShowWindow")
	pUpdateWindow       = user32.NewProc("UpdateWindow")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pAdjustWindowRectEx = user32.NewProc("AdjustWindowRectEx")
	pGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	pGetDC              = user32.NewProc("GetDC")
	pReleaseDC          = user32.NewProc("ReleaseDC")
	pGetSysColorBrush   = user32.NewProc("GetSysColorBrush")
	pLoadCursorW        = user32.NewProc("LoadCursorW")
	pSetFocus           = user32.NewProc("SetFocus")

	pCreateFontW      = gdi32.NewProc("CreateFontW")
	pGetDeviceCaps    = gdi32.NewProc("GetDeviceCaps")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")

	pRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	pRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	pRegDeleteTreeW  = advapi32.NewProc("RegDeleteTreeW")
	pRegCloseKey     = advapi32.NewProc("RegCloseKey")

	pExtractIconExW = shell32.NewProc("ExtractIconExW")
)

const (
	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_MINIMIZEBOX      = 0x00020000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_TABSTOP          = 0x00010000
	WS_EX_DLGMODALFRAME = 0x00000001

	BS_PUSHBUTTON    = 0x0
	BS_DEFPUSHBUTTON = 0x1
	BS_AUTOCHECKBOX  = 0x3
	SS_LEFT          = 0x0
	SS_PATHELLIPSIS  = 0x8000
	SS_NOPREFIX      = 0x80

	WM_DESTROY        = 0x0002
	WM_CLOSE          = 0x0010
	WM_SETFONT        = 0x0030
	WM_COMMAND        = 0x0111
	WM_CTLCOLORSTATIC = 0x0138
	BM_GETCHECK       = 0x00F0
	BM_SETCHECK       = 0x00F1
	BST_CHECKED       = 1

	MB_OK           = 0x0
	MB_YESNO        = 0x4
	MB_ICONINFO     = 0x40
	MB_ICONWARN     = 0x30
	MB_ICONERROR    = 0x10
	MB_ICONQUESTION = 0x20
	IDYES           = 6

	COLOR_WINDOW = 5
	SM_CXSCREEN  = 0
	SM_CYSCREEN  = 1
	LOGPIXELSY   = 90
	TRANSPARENT  = 1
	SW_SHOW      = 5
	IDC_ARROW    = 32512

	HKEY_CURRENT_USER = 0x80000001
	KEY_ALL_ACCESS    = 0xF003F
	KEY_SET_VALUE     = 0x0002
	REG_SZ            = 1
	REG_DWORD         = 4

	CREATE_NO_WINDOW = 0x08000000

	idInstall = 101
	idCancel  = 102
	idDesktop = 201
	idAutorun = 202
	idLaunch  = 203
)

type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type RECT struct{ Left, Top, Right, Bottom int32 }

func u16(s string) *uint16  { p, _ := syscall.UTF16PtrFromString(s); return p }
func u16p(s string) uintptr { return uintptr(unsafe.Pointer(u16(s))) }

func msgBox(h uintptr, text string, flags uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(h, u16p(text), u16p("Установка "+appName), flags)
	return r
}

// ------------------------------------------------------------------ пути

func installDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(base, "Programs", appName)
}

func startMenuLink() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, appName+".lnk")
}

// ------------------------------------------------------------------ реестр

func regSetString(h uintptr, name, val string) {
	d, _ := syscall.UTF16FromString(val)
	pRegSetValueExW.Call(h, u16p(name), 0, REG_SZ, uintptr(unsafe.Pointer(&d[0])), uintptr(len(d)*2))
}

func regSetDword(h uintptr, name string, val uint32) {
	pRegSetValueExW.Call(h, u16p(name), 0, REG_DWORD, uintptr(unsafe.Pointer(&val)), 4)
}

func writeUninstallKey(dir string, sizeKB uint32) error {
	var h uintptr
	r, _, _ := pRegCreateKeyExW.Call(HKEY_CURRENT_USER, u16p(regUninst), 0, 0, 0, KEY_ALL_ACCESS, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r != 0 {
		return fmt.Errorf("не удалось записать в реестр (код %d)", r)
	}
	defer pRegCloseKey.Call(h)
	exe := filepath.Join(dir, appExe)
	regSetString(h, "DisplayName", appName)
	regSetString(h, "DisplayVersion", appVersion)
	regSetString(h, "Publisher", publisher)
	regSetString(h, "DisplayIcon", exe+",0")
	regSetString(h, "InstallLocation", dir)
	regSetString(h, "UninstallString", `"`+filepath.Join(dir, "Uninstall.exe")+`" /uninstall`)
	regSetString(h, "URLInfoAbout", "https://github.com/VIRUS1SHOW/nova-helper")
	regSetDword(h, "NoModify", 1)
	regSetDword(h, "NoRepair", 1)
	regSetDword(h, "EstimatedSize", sizeKB)
	return nil
}

func setAutorun(exe string, on bool) {
	var h uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, u16p(regRun), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return
	}
	defer pRegCloseKey.Call(h)
	if on {
		regSetString(h, runName, `"`+exe+`" --tray`)
	} else {
		pRegDeleteValueW.Call(h, u16p(runName))
	}
}

// ------------------------------------------------------------------ процессы

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	return c
}

// Закрывает запущенную программу, чтобы можно было заменить exe.
func stopApp() {
	hidden("taskkill", "/F", "/IM", appExe).Run()
	time.Sleep(700 * time.Millisecond)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Ярлыки создаются через WScript.Shell в скрытом PowerShell.
func makeShortcuts(dir string, desktop bool) error {
	exe := filepath.Join(dir, appExe)
	one := func(path string) string {
		return "$s=$w.CreateShortcut(" + path + ");$s.TargetPath=" + psQuote(exe) +
			";$s.WorkingDirectory=" + psQuote(dir) + ";$s.IconLocation=" + psQuote(exe+",0") +
			";$s.Description=" + psQuote("Правила NOVA RP поверх игры") + ";$s.Save();"
	}
	script := "$ErrorActionPreference='Stop';$w=New-Object -ComObject WScript.Shell;" + one(psQuote(startMenuLink()))
	if desktop {
		script += one("(Join-Path ([Environment]::GetFolderPath('Desktop')) " + psQuote(appName+".lnk") + ")")
	}
	out, err := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ярлыки: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeShortcuts() {
	os.Remove(startMenuLink())
	script := "Remove-Item -LiteralPath (Join-Path ([Environment]::GetFolderPath('Desktop')) " + psQuote(appName+".lnk") + ") -ErrorAction SilentlyContinue"
	hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()
}

// ------------------------------------------------------------------ установка

func extract(dir string) (uint32, error) {
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		if strings.Contains(name, "..") {
			continue
		}
		dst := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(dst, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(dst), 0755)
		rc, err := f.Open()
		if err != nil {
			return 0, err
		}
		tmp := dst + ".new"
		out, err := os.Create(tmp)
		if err != nil {
			rc.Close()
			return 0, fmt.Errorf("не удалось записать %s: %v", f.Name, err)
		}
		n, err := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			os.Remove(tmp)
			return 0, err
		}
		os.Remove(dst)
		if err := os.Rename(tmp, dst); err != nil {
			return 0, fmt.Errorf("не удалось заменить %s: %v", f.Name, err)
		}
		total += uint64(n)
	}
	return uint32(total / 1024), nil
}

func copySelf(dst string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Clean(self), filepath.Clean(dst)) {
		return nil
	}
	b, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0755)
}

func doInstall(desktop, autorun bool) error {
	dir := installDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	stopApp()
	size, err := extract(dir)
	if err != nil {
		return err
	}
	if err := copySelf(filepath.Join(dir, "Uninstall.exe")); err != nil {
		return fmt.Errorf("деинсталлятор: %v", err)
	}
	if err := writeUninstallKey(dir, size); err != nil {
		return err
	}
	setAutorun(filepath.Join(dir, appExe), autorun)
	if err := makeShortcuts(dir, desktop); err != nil {
		// Без ярлыков программа всё равно работает — не считаем это провалом.
		msgBox(0, "Программа установлена, но ярлыки создать не удалось.\n\n"+err.Error(), MB_OK|MB_ICONWARN)
	}
	return nil
}

func doUninstall() {
	dir := installDir()
	if msgBox(0, "Удалить "+appName+" с этого компьютера?\n\nНастройки и избранное тоже будут удалены.", MB_YESNO|MB_ICONQUESTION) != IDYES {
		return
	}
	stopApp()
	setAutorun("", false)
	removeShortcuts()
	pRegDeleteTreeW.Call(HKEY_CURRENT_USER, u16p(regUninst))

	self, _ := os.Executable()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(self)) {
			continue
		}
		os.RemoveAll(p)
	}
	// Сам себя удалить нельзя, пока работаем: папку уберёт cmd через пару секунд.
	hidden("cmd", "/c", "ping 127.0.0.1 -n 3 >nul & rmdir /s /q \""+dir+"\"").Start()
	msgBox(0, appName+" удалён.", MB_OK|MB_ICONINFO)
}

// ------------------------------------------------------------------ окно

var ui struct {
	hinst, hwnd              uintptr
	btnInstall, btnCancel    uintptr
	chkDesk, chkAuto, chkRun uintptr
	status                   uintptr
	font, fontTitle          uintptr
	scale                    float64
	done                     bool
	icon, iconSm             uintptr
}

func s(px float64) uintptr { return uintptr(int32(px*ui.scale + 0.5)) }

func child(class, text string, style uintptr, x, y, w, h float64, id uintptr) uintptr {
	hw, _, _ := pCreateWindowExW.Call(0, u16p(class), u16p(text), WS_CHILD|WS_VISIBLE|style,
		s(x), s(y), s(w), s(h), ui.hwnd, id, ui.hinst, 0)
	pSendMessageW.Call(hw, WM_SETFONT, ui.font, 1)
	return hw
}

func setStatus(t string) {
	pSetWindowTextW.Call(ui.status, u16p(t))
	pUpdateWindow.Call(ui.hwnd)
}

func checked(h uintptr) bool {
	r, _, _ := pSendMessageW.Call(h, BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}

func onInstall() {
	if ui.done {
		pDestroyWindow.Call(ui.hwnd)
		return
	}
	for _, h := range []uintptr{ui.btnInstall, ui.btnCancel, ui.chkDesk, ui.chkAuto, ui.chkRun} {
		pEnableWindow.Call(h, 0)
	}
	setStatus("Устанавливаю…")
	if err := doInstall(checked(ui.chkDesk), checked(ui.chkAuto)); err != nil {
		setStatus("Ошибка установки")
		msgBox(ui.hwnd, "Не удалось установить "+appName+".\n\n"+err.Error(), MB_OK|MB_ICONERROR)
		for _, h := range []uintptr{ui.btnInstall, ui.btnCancel, ui.chkDesk, ui.chkAuto, ui.chkRun} {
			pEnableWindow.Call(h, 1)
		}
		return
	}
	if checked(ui.chkRun) {
		c := exec.Command(filepath.Join(installDir(), appExe))
		c.Dir = installDir()
		c.Start()
	}
	ui.done = true
	setStatus("Готово! Открыть или спрятать: Alt+Q")
	pSetWindowTextW.Call(ui.btnInstall, u16p("Закрыть"))
	pEnableWindow.Call(ui.btnInstall, 1)
	pSetFocus.Call(ui.btnInstall)
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch wp & 0xFFFF {
		case idInstall:
			onInstall()
		case idCancel:
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case WM_CTLCOLORSTATIC:
		pSetBkMode.Call(wp, TRANSPARENT)
		if lp == ui.status && ui.done {
			pSetTextColor.Call(wp, 0x00007A1E) // зелёный (BGR)
		}
		b, _, _ := pGetSysColorBrush.Call(COLOR_WINDOW)
		return b
	case WM_CLOSE:
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func font(pt float64, weight uintptr) uintptr {
	f, _, _ := pCreateFontW.Call(uintptr(int32(-pt*ui.scale*96/72+0.5)), 0, 0, 0, weight, 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE*/, 0, u16p("Segoe UI"))
	return f
}

func runUI() {
	ui.hinst, _, _ = pGetModuleHandleW.Call(0)
	dc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(dc, LOGPIXELSY)
	pReleaseDC.Call(0, dc)
	if dpi == 0 {
		dpi = 96
	}
	ui.scale = float64(dpi) / 96
	ui.font = font(9.5, 400)
	ui.fontTitle = font(15, 600)

	self, _ := os.Executable()
	pExtractIconExW.Call(u16p(self), 0, uintptr(unsafe.Pointer(&ui.icon)), uintptr(unsafe.Pointer(&ui.iconSm)), 1)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	bg, _, _ := pGetSysColorBrush.Call(COLOR_WINDOW)
	wc := WNDCLASSEXW{
		WndProc: syscall.NewCallback(wndProc), Instance: ui.hinst, Icon: ui.icon, IconSm: ui.iconSm,
		Cursor: cur, Background: bg, ClassName: u16("NovaHelperSetupWnd"),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	style := uintptr(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX)
	rc := RECT{0, 0, int32(s(460)), int32(s(318))}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, WS_EX_DLGMODALFRAME)
	w, h := rc.Right-rc.Left, rc.Bottom-rc.Top
	sw, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
	sh, _, _ := pGetSystemMetrics.Call(SM_CYSCREEN)
	ui.hwnd, _, _ = pCreateWindowExW.Call(WS_EX_DLGMODALFRAME, u16p("NovaHelperSetupWnd"), u16p("Установка "+appName),
		style, uintptr((int32(sw)-w)/2), uintptr((int32(sh)-h)/2), uintptr(w), uintptr(h), 0, 0, ui.hinst, 0)

	dir := installDir()
	_, err := os.Stat(filepath.Join(dir, appExe))
	update := err == nil

	t := child("STATIC", appName+" "+appVersion, SS_LEFT|SS_NOPREFIX, 24, 18, 412, 30, 0)
	pSendMessageW.Call(t, WM_SETFONT, ui.fontTitle, 1)
	desc := "Правила NOVA RP поверх игры: поиск, избранное, автообновление.\nОткрыть или спрятать — Alt+Q."
	child("STATIC", desc, SS_LEFT|SS_NOPREFIX, 24, 54, 412, 40, 0)
	label := "Папка установки:"
	if update {
		label = "Уже установлено — будет обновлено (настройки и избранное сохранятся):"
	}
	child("STATIC", label, SS_LEFT|SS_NOPREFIX, 24, 102, 412, 20, 0)
	child("STATIC", dir, SS_LEFT|SS_NOPREFIX|SS_PATHELLIPSIS, 24, 122, 412, 20, 0)

	ui.chkDesk = child("BUTTON", "Ярлык на рабочем столе", BS_AUTOCHECKBOX|WS_TABSTOP, 24, 156, 412, 22, idDesktop)
	ui.chkAuto = child("BUTTON", "Запускать вместе с Windows (в трее)", BS_AUTOCHECKBOX|WS_TABSTOP, 24, 182, 412, 22, idAutorun)
	ui.chkRun = child("BUTTON", "Запустить после установки", BS_AUTOCHECKBOX|WS_TABSTOP, 24, 208, 412, 22, idLaunch)
	pSendMessageW.Call(ui.chkDesk, BM_SETCHECK, BST_CHECKED, 0)
	pSendMessageW.Call(ui.chkRun, BM_SETCHECK, BST_CHECKED, 0)

	ui.status = child("STATIC", "", SS_LEFT|SS_NOPREFIX, 24, 246, 412, 20, 0)
	btn := "Установить"
	if update {
		btn = "Обновить"
	}
	ui.btnInstall = child("BUTTON", btn, BS_DEFPUSHBUTTON|WS_TABSTOP, 236, 274, 110, 30, idInstall)
	ui.btnCancel = child("BUTTON", "Отмена", BS_PUSHBUTTON|WS_TABSTOP, 354, 274, 82, 30, idCancel)

	pShowWindow.Call(ui.hwnd, SW_SHOW)
	pUpdateWindow.Call(ui.hwnd)
	pSetFocus.Call(ui.btnInstall)

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if r, _, _ := pIsDialogMessageW.Call(ui.hwnd, uintptr(unsafe.Pointer(&m))); r != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func main() {
	runtime.LockOSThread()
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, "/uninstall") || strings.EqualFold(a, "--uninstall") {
			doUninstall()
			return
		}
	}
	// Не даём запустить два установщика одновременно.
	_, _, e := pCreateMutexW.Call(0, 0, u16p("Local\\NovaHelperSetupMutex"))
	if e == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		return
	}
	runUI()
}
