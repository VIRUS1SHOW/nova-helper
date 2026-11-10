package main

// Автообновление: правила и сама программа скачиваются из репозитория
// github.com/VIRUS1SHOW/nova-helper (файл manifest.json описывает, что там лежит).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	appVersion  = "1.3.0"
	updateBase  = "https://raw.githubusercontent.com/VIRUS1SHOW/nova-helper/main/"
	managedList = "rules-managed.txt" // какие файлы в rules пришли из обновлений
)

type ManFile struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	SHA256 string `json:"sha256"`
}

type AppInfo struct {
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes"`
}

type Manifest struct {
	Rules []ManFile `json:"rules"`
	App   AppInfo   `json:"app"`
}

type UpdateResult struct {
	Changed []ManFile
	Removed []string
	NewApp  *AppInfo
	Err     error
	Manual  bool
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetch(base, path string) ([]byte, error) {
	url := base + path
	if strings.Contains(url, "?") {
		url += "&"
	} else {
		url += "?"
	}
	url += "t=" + strconv.FormatInt(time.Now().Unix(), 10) // обход кэша
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("сервер ответил %d на %s", resp.StatusCode, path)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func fileSHA(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return sha(b)
}

func safeName(n string) bool {
	return n != "" && n == filepath.Base(n) && !strings.ContainsAny(n, `/\:`) && !strings.HasPrefix(n, ".")
}

func writeAtomic(p string, data []byte) error {
	tmp := p + ".download"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	os.Remove(p)
	return os.Rename(tmp, p)
}

// CheckUpdates сверяет правила с репозиторием и скачивает изменившиеся файлы.
func CheckUpdates(base, appDir string) UpdateResult {
	var res UpdateResult
	raw, err := fetch(base, "manifest.json")
	if err != nil {
		res.Err = err
		return res
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		res.Err = errors.New("не удалось прочитать manifest.json: " + err.Error())
		return res
	}
	rulesDir := filepath.Join(appDir, "rules")
	os.MkdirAll(rulesDir, 0755)

	// какие файлы мы раньше получали из обновлений
	managed := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(appDir, managedList)); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); safeName(l) {
				managed[l] = true
			}
		}
	}
	now := map[string]bool{}
	for _, f := range m.Rules {
		if !safeName(f.Name) || !strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
			continue
		}
		now[f.Name] = true
		local := filepath.Join(rulesDir, f.Name)
		if strings.EqualFold(fileSHA(local), f.SHA256) {
			continue
		}
		data, err := fetch(base, "rules/"+f.Name)
		if err != nil {
			res.Err = err
			continue
		}
		if f.SHA256 != "" && !strings.EqualFold(sha(data), f.SHA256) {
			res.Err = errors.New("файл " + f.Name + " скачался с ошибкой, попробуем позже")
			continue
		}
		if err := writeAtomic(local, data); err != nil {
			res.Err = err
			continue
		}
		res.Changed = append(res.Changed, f)
	}
	// убрать документы, которые удалили из репозитория (только те, что пришли из обновлений)
	for n := range managed {
		if !now[n] {
			if os.Remove(filepath.Join(rulesDir, n)) == nil {
				res.Removed = append(res.Removed, n)
			}
		}
	}
	names := make([]string, 0, len(now))
	for n := range now {
		names = append(names, n)
	}
	sort.Strings(names)
	os.WriteFile(filepath.Join(appDir, managedList), []byte(strings.Join(names, "\r\n")+"\r\n"), 0644)

	if m.App.Version != "" && safeName(m.App.File) && versionLess(appVersion, m.App.Version) {
		a := m.App
		res.NewApp = &a
	}
	return res
}

// DownloadApp скачивает новую версию программы рядом с текущей (*.new).
func DownloadApp(base string, a *AppInfo, exePath string) error {
	data, err := fetch(base, "app/"+a.File)
	if err != nil {
		return err
	}
	if a.SHA256 == "" || !strings.EqualFold(sha(data), a.SHA256) {
		return errors.New("файл программы скачался с ошибкой, попробуйте позже")
	}
	return os.WriteFile(exePath+".new", data, 0755)
}

// SwapApp ставит скачанную версию на место текущей. Запущенный exe в Windows
// нельзя перезаписать, но можно переименовать — этим и пользуемся.
func SwapApp(exePath string) error {
	old := exePath + ".old"
	os.Remove(old)
	if err := os.Rename(exePath, old); err != nil {
		return err
	}
	if err := os.Rename(exePath+".new", exePath); err != nil {
		os.Rename(old, exePath)
		return err
	}
	return nil
}

func CleanupOldApp(exePath string) {
	for i := 0; i < 10; i++ {
		if err := os.Remove(exePath + ".old"); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}
