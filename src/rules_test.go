package main

import (
	"strings"
	"testing"
)

func TestLoadAndSearch(t *testing.T) {
	docs, errs := LoadDocs("../rules")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, d := range docs {
		t.Logf("%s | %s | %s | разделов %d | пунктов %d", d.File, d.Short, d.Updated, len(d.Sections), len(d.Points))
		if len(d.Sections) == 0 {
			t.Errorf("%s: документ разобран пустым", d.File)
		}
	}
	f, _ := Search(docs, "4.15")
	if len(f) < 2 {
		t.Errorf("4.15: %d", len(f))
	}
	f, _ = Search(docs, "DM")
	t.Logf("DM: %d", len(f))
	bron, _ := Search(docs, "бронежилет")
	f = bron
	t.Logf("бронежилет: %d", len(f))
	f, _ = Search(docs, "Ёлка")
	t.Logf("ёлка: %d", len(f))

	// заметки и списки распознаются
	p := docs[0].Points[0]
	for _, d := range docs {
		for _, x := range d.Points {
			if x.Num == "4.5" && d.File == "01-obshie-pravila.txt" {
				p = x
			}
		}
	}
	kinds := []string{}
	for _, it := range p.Items {
		kinds = append(kinds, it.Kind+":"+it.Label)
	}
	t.Logf("4.5 items: %v pun=%v", kinds, p.Pun)

	r := RenderDoc(docs[0], map[string]bool{}, nil)
	t.Logf("RTF %d байт, позиций %d, пунктов %d", len(r.RTF), r.Length, len(r.Points))
	if strings.Count(r.RTF, "{") != strings.Count(r.RTF, "}")+strings.Count(r.RTF, "\\{")-strings.Count(r.RTF, "\\}") {
		t.Log("скобки: проверить")
	}
	l := RenderList("Найдено", bron, []string{"бронежилет"}, nil, nil, nil)
	if !strings.Contains(l.RTF, "\\highlight12") {
		t.Error("нет подсветки")
	}
}

func TestHotkey(t *testing.T) {
	for _, c := range []string{"Alt+Q", "alt + q", "Ctrl+Shift+F", "F10", "Alt+Й", "Q", "Alt+F25", "Num5"} {
		m, v, l, ok := ParseHotkey(c)
		t.Logf("%q -> mods=%d vk=%#x label=%q ok=%v", c, m, v, l, ok)
	}
}
