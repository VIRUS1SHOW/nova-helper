package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdater(t *testing.T) {
	files := map[string]string{
		"rules/01-a.txt": "Название: А\n---\n## 1. Раздел\n1.1 Пункт | WARN\n",
		"rules/02-b.txt": "Название: Б\n---\n## 1. Раздел\n1.1 Пункт\n",
		"app/new.exe":    "EXE-v2",
	}
	man := Manifest{
		Rules: []ManFile{{Name: "01-a.txt", SHA256: sha([]byte(files["rules/01-a.txt"]))},
			{Name: "02-b.txt", SHA256: sha([]byte(files["rules/02-b.txt"]))}},
		App: AppInfo{Version: "99.0.0", File: "new.exe", SHA256: sha([]byte(files["app/new.exe"]))},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path[1:]
		if p == "manifest.json" {
			json.NewEncoder(w).Encode(man)
			return
		}
		if s, ok := files[p]; ok {
			w.Write([]byte(s))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	base := srv.URL + "/"

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "rules"), 0755)
	os.WriteFile(filepath.Join(dir, "rules", "99-my.txt"), []byte("мой файл"), 0644)
	os.WriteFile(filepath.Join(dir, "rules", "02-b.txt"), []byte(files["rules/02-b.txt"]), 0644)

	r := CheckUpdates(base, dir)
	if r.Err != nil || len(r.Changed) != 1 || r.Changed[0].Name != "01-a.txt" || r.NewApp == nil {
		t.Fatalf("первая проверка: %+v", r)
	}
	r = CheckUpdates(base, dir)
	if len(r.Changed) != 0 {
		t.Fatalf("повторно не должно ничего качать: %+v", r)
	}
	// документ удалили из репозитория
	man.Rules = man.Rules[:1]
	r = CheckUpdates(base, dir)
	if len(r.Removed) != 1 || r.Removed[0] != "02-b.txt" {
		t.Fatalf("удаление: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules", "99-my.txt")); err != nil {
		t.Fatal("свой файл пользователя не должен удаляться")
	}
	// битая контрольная сумма
	man.Rules[0].SHA256 = "00"
	os.Remove(filepath.Join(dir, "rules", "01-a.txt"))
	r = CheckUpdates(base, dir)
	if r.Err == nil || len(r.Changed) != 0 {
		t.Fatalf("битый файл должен отклоняться: %+v", r)
	}
	// обновление программы
	exe := filepath.Join(dir, "app.exe")
	os.WriteFile(exe, []byte("EXE-v1"), 0755)
	if err := DownloadApp(base, r.NewApp, exe); err != nil {
		t.Fatal(err)
	}
	if err := SwapApp(exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "EXE-v2" {
		t.Fatal("exe не заменился")
	}
	CleanupOldApp(exe)
	if !versionLess("1.1.0", "1.2.0") || versionLess("1.10.0", "1.9.9") || versionLess("1.1.0", "1.1.0") {
		t.Fatal("сравнение версий")
	}
}
