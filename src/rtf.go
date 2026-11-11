package main

// Построение оформленного текста (RTF) для окна Nova Helper.
// Параллельно считаются позиции символов, чтобы потом прокручивать
// к нужному пункту и понимать, по какому пункту кликнули.

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

const (
	cText   = 1
	cMuted  = 2
	cAccent = 3
	cCyan   = 4
	cRed    = 5
	cOrange = 6
	cYellow = 7
	cBlue   = 8
	cViolet = 9
	cGreen  = 10
	cPink   = 11
	cHlBg   = 12
	cFaint  = 13
	cWhite  = 14
	cPara   = 15
)

var palette = [][3]int{
	{231, 233, 238}, // 1 текст
	{138, 147, 164}, // 2 приглушённый
	{150, 136, 255}, // 3 акцент
	{94, 200, 255},  // 4 голубой
	{255, 107, 107}, // 5 красный
	{255, 159, 67},  // 6 оранжевый
	{245, 208, 76},  // 7 жёлтый
	{90, 169, 255},  // 8 синий
	{180, 140, 255}, // 9 фиолетовый
	{79, 209, 139},  // 10 зелёный
	{255, 122, 182}, // 11 розовый
	{110, 92, 20},   // 12 фон подсветки
	{93, 101, 118},  // 13 бледный
	{255, 255, 255}, // 14 белый
	{207, 211, 220}, // 15 текст абзацев
}

type PointRange struct {
	Start, End int
	P          *Point
}

type Rendered struct {
	RTF      string
	Points   []PointRange
	Sections []int // позиция начала каждого раздела (по индексу раздела)
	Length   int
}

type rtfB struct {
	b   strings.Builder
	pos int
	hl  []string
}

func (r *rtfB) raw(s string) { r.b.WriteString(s) }

func (r *rtfB) plain(s string) {
	for _, c := range s {
		switch {
		case c == '\\' || c == '{' || c == '}':
			r.b.WriteByte('\\')
			r.b.WriteRune(c)
			r.pos++
		case c == '\t':
			r.b.WriteString("\\tab ")
			r.pos++
		case c == '\n' || c == '\r':
			r.b.WriteString(" ")
			r.pos++
		case c < 0x80:
			r.b.WriteRune(c)
			r.pos++
		default:
			for _, u := range utf16.Encode([]rune{c}) {
				fmt.Fprintf(&r.b, "\\u%d?", int16(u))
				r.pos++
			}
		}
	}
}

// text выводит текст с подсветкой найденных слов.
func (r *rtfB) text(s string) {
	if len(r.hl) == 0 || s == "" {
		r.plain(s)
		return
	}
	rs := []rune(s)
	ns := []rune(Norm(s))
	mark := make([]bool, len(rs))
	any := false
	for _, w := range r.hl {
		wr := []rune(w)
		if len(wr) == 0 {
			continue
		}
		for i := 0; i+len(wr) <= len(ns); i++ {
			if string(ns[i:i+len(wr)]) == w {
				for j := i; j < i+len(wr); j++ {
					mark[j] = true
				}
				any = true
			}
		}
	}
	if !any {
		r.plain(s)
		return
	}
	i := 0
	for i < len(rs) {
		j := i
		for j < len(rs) && mark[j] == mark[i] {
			j++
		}
		if mark[i] {
			r.raw(fmt.Sprintf("{\\highlight%d ", cHlBg))
			r.plain(string(rs[i:j]))
			r.raw("}")
		} else {
			r.plain(string(rs[i:j]))
		}
		i = j
	}
}

func (r *rtfB) par() { r.raw("\\par\n"); r.pos++ }

// para начинает новый абзац: отступ слева, первая строка, интервалы (twips).
func (r *rtfB) para(li, fi, sb, sa int) {
	r.raw(fmt.Sprintf("\\pard\\plain\\f0\\li%d\\fi%d\\sb%d\\sa%d\\sl276\\slmult1\\fs22\\cf%d ", li, fi, sb, sa, cText))
}

func (r *rtfB) span(format, s string) {
	r.raw("{" + format + " ")
	r.text(s)
	r.raw("}")
}

func punColor(p string) int {
	t := strings.ToUpper(p)
	switch {
	case strings.Contains(t, "BAN") || strings.Contains(t, "НАВСЕГДА"):
		return cRed
	case strings.Contains(t, "JAIL"):
		return cOrange
	case strings.Contains(t, "WARN"):
		return cYellow
	case strings.Contains(t, "MUTE"):
		return cBlue
	case strings.Contains(t, "KICK"):
		return cViolet
	case strings.Contains(t, "ИЗЪЯТ") || strings.Contains(t, "ОБНУЛ"):
		return cPink
	}
	return cMuted
}

func noteColor(k string) int {
	switch k {
	case "prim":
		return cBlue
	case "utoch":
		return cCyan
	case "iskl":
		return cGreen
	case "poyas":
		return cViolet
	}
	return cMuted
}

func header() string {
	var b strings.Builder
	b.WriteString("{\\rtf1\\ansi\\ansicpg1252\\deff0\\uc1{\\fonttbl{\\f0\\fswiss\\fcharset0 Segoe UI;}{\\f1\\fnil\\fcharset0 Segoe UI Symbol;}}{\\colortbl ;")
	for _, c := range palette {
		fmt.Fprintf(&b, "\\red%d\\green%d\\blue%d;", c[0], c[1], c[2])
	}
	b.WriteString("}\\viewkind4\n")
	return b.String()
}

const numIndent = 900 // отступ текста пункта от номера

// inlinePun выводит текст, у которого после «|» идут наказания (в заметках).
func (r *rtfB) textWithPun(s string, color int) {
	t, pun := SplitPun(s)
	r.span(fmt.Sprintf("\\cf%d", color), t)
	for _, p := range pun {
		r.plain("  ")
		r.span(fmt.Sprintf("\\b\\fs19\\cf%d", punColor(p)), "["+p+"]")
	}
}

func (r *rtfB) items(items []Item, base int) {
	for _, it := range items {
		switch it.Kind {
		case "li":
			r.para(base+300, -300, 0, 30)
			r.raw(fmt.Sprintf("\\tx%d ", base+300))
			r.span(fmt.Sprintf("\\cf%d", cMuted), "•\t")
			r.textWithPun(it.Text, cPara)
			r.par()
		case "note":
			r.para(base, 0, 40, 40)
			r.span(fmt.Sprintf("\\b\\cf%d", noteColor(it.Note)), it.Label+":")
			r.plain(" ")
			r.textWithPun(it.Text, cPara)
			r.par()
		case "sub":
			r.para(base, 0, 240, 60)
			r.span(fmt.Sprintf("\\b\\fs19\\cf%d", cCyan), strings.ToUpper(it.Text))
			r.par()
		default:
			r.para(base, 0, 40, 60)
			r.textWithPun(it.Text, cPara)
			r.par()
		}
	}
}

func (r *rtfB) point(p *Point, fav bool) PointRange {
	start := r.pos
	r.para(numIndent, -numIndent, 200, 30)
	r.raw(fmt.Sprintf("\\tx%d ", numIndent))
	if fav {
		r.span(fmt.Sprintf("\\f1\\cf%d", cYellow), "★ ")
	}
	r.span(fmt.Sprintf("\\b\\cf%d", cAccent), p.Num)
	r.plain("\t")
	r.span(fmt.Sprintf("\\cf%d", cText), p.Text)
	r.par()
	if len(p.Pun) > 0 {
		r.para(numIndent, 0, 20, 30)
		for i, x := range p.Pun {
			if i > 0 {
				r.span(fmt.Sprintf("\\cf%d", cFaint), "   ·   ")
			}
			r.span(fmt.Sprintf("\\b\\fs19\\cf%d", punColor(x)), x)
		}
		r.par()
	}
	r.items(p.Items, numIndent)
	return PointRange{Start: start, End: r.pos, P: p}
}

func (r *rtfB) title(s string) {
	r.para(0, 0, 0, 60)
	r.span(fmt.Sprintf("\\b\\fs38\\cf%d", cWhite), s)
	r.par()
}

func (r *rtfB) errors(errs []string) {
	for _, e := range errs {
		r.para(0, 0, 0, 120)
		r.span(fmt.Sprintf("\\b\\cf%d", cRed), "⚠ "+e)
		r.par()
	}
}

// RenderDoc — полный документ.
func RenderDoc(d *Doc, favs map[string]bool, errs []string) Rendered {
	r := &rtfB{}
	r.raw(header())
	r.errors(errs)
	r.title(d.Title)
	r.para(0, 0, 0, 40)
	meta := []string{}
	if d.Updated != "" {
		meta = append(meta, "Обновлено: "+d.Updated)
	}
	meta = append(meta, fmt.Sprintf("Пунктов: %d", len(d.Points)))
	r.span(fmt.Sprintf("\\fs18\\cf%d", cMuted), strings.Join(meta, "    ·    "))
	r.par()
	if d.Source != "" {
		r.para(0, 0, 0, 80)
		r.span(fmt.Sprintf("\\fs18\\cf%d", cMuted), "Тема на форуме: ")
		r.span(fmt.Sprintf("\\fs18\\cf%d", cCyan), d.Source)
		r.par()
	}
	out := Rendered{}
	for _, s := range d.Sections {
		out.Sections = append(out.Sections, r.pos)
		if s.Title != "" {
			r.para(0, 0, 420, 60)
			r.span(fmt.Sprintf("\\b\\fs30\\cf%d", cWhite), s.Title)
			r.par()
		}
		for _, bl := range s.Blocks {
			if bl.Point != nil {
				out.Points = append(out.Points, r.point(bl.Point, favs[FavKey(bl.Point)]))
			} else {
				r.items([]Item{*bl.Item}, 0)
			}
		}
	}
	r.para(0, 0, 400, 0)
	r.par()
	r.raw("}")
	out.RTF = r.b.String()
	out.Length = r.pos
	return out
}

// RenderList — результаты поиска или избранное.
func RenderList(title string, pts []*Point, hl []string, favs map[string]bool, empty []string, errs []string) Rendered {
	r := &rtfB{}
	r.raw(header())
	r.errors(errs)
	r.title(title)
	out := Rendered{}
	if len(pts) == 0 {
		for i, e := range empty {
			r.para(0, 0, 200, 40)
			c := cText
			if i > 0 {
				c = cMuted
			}
			r.span(fmt.Sprintf("\\cf%d", c), e)
			r.par()
		}
	}
	r.hl = hl
	last := ""
	for _, p := range pts {
		label := p.Doc.Short
		if p.Sec != nil && p.Sec.Title != "" {
			label += "  ·  " + p.Sec.Title
		}
		if label != last {
			r.hl = nil
			r.para(0, 0, 360, 0)
			r.span(fmt.Sprintf("\\fs18\\cf%d", cMuted), label)
			r.par()
			r.hl = hl
			last = label
		}
		out.Points = append(out.Points, r.point(p, favs[FavKey(p)]))
	}
	r.hl = nil
	r.para(0, 0, 400, 0)
	r.par()
	r.raw("}")
	out.RTF = r.b.String()
	out.Length = r.pos
	return out
}

// SortByDoc сохраняет порядок документов (для избранного).
func SortByDoc(pts []*Point, docs []*Doc) {
	idx := map[*Doc]int{}
	for i, d := range docs {
		idx[d] = i
	}
	sort.SliceStable(pts, func(a, b int) bool {
		if idx[pts[a].Doc] != idx[pts[b].Doc] {
			return idx[pts[a].Doc] < idx[pts[b].Doc]
		}
		return pts[a].Index < pts[b].Index
	})
}

// ------------------------------------------------------------------ ИИ

// answerLine выводит строку ответа, выделяя ссылки на пункты.
func (r *rtfB) answerLine(s string) {
	locs := append(citeRe.FindAllStringIndex(s, -1), artCiteRe.FindAllStringIndex(s, -1)...)
	sort.Slice(locs, func(a, b int) bool { return locs[a][0] < locs[b][0] })
	last := 0
	for _, l := range locs {
		if l[0] < last {
			continue
		}
		if l[0] > last {
			r.span(fmt.Sprintf("\\cf%d", cText), s[last:l[0]])
		}
		r.span(fmt.Sprintf("\\b\\cf%d", cAccent), s[l[0]:l[1]])
		last = l[1]
	}
	if last < len(s) {
		r.span(fmt.Sprintf("\\cf%d", cText), s[last:])
	}
}

func (r *rtfB) smallMuted(s string, sb, sa int) {
	r.para(0, 0, sb, sa)
	r.span(fmt.Sprintf("\\fs18\\cf%d", cMuted), s)
	r.par()
}

// RenderAI — вопрос, ответ ИИ и пункты, на которые он сослался.
func RenderAI(q string, answer []string, cited, related []*Point, favs map[string]bool, meta, errText string, thinking bool, errs []string) Rendered {
	r := &rtfB{}
	r.raw(header())
	r.errors(errs)
	r.para(0, 0, 0, 40)
	r.span(fmt.Sprintf("\\f1\\fs34\\cf%d", cAccent), "✦ ")
	r.span(fmt.Sprintf("\\b\\fs34\\cf%d", cWhite), "Вопрос к ИИ")
	r.par()
	r.para(0, 0, 60, 120)
	r.span(fmt.Sprintf("\\i\\fs24\\cf%d", cPara), "«"+q+"»")
	r.par()
	out := Rendered{}

	switch {
	case thinking:
		r.para(0, 0, 120, 60)
		r.span(fmt.Sprintf("\\b\\cf%d", cAccent), "Думаю над ответом…")
		r.par()
		r.smallMuted("Подбираю подходящие пункты правил и отправляю их ИИ. Обычно это занимает 3–15 секунд.", 0, 0)
		if meta != "" {
			r.smallMuted(meta, 40, 0)
		}
	case errText != "":
		r.para(0, 0, 120, 60)
		r.span(fmt.Sprintf("\\b\\cf%d", cRed), "Не получилось получить ответ")
		r.par()
		for _, l := range strings.Split(errText, "\n") {
			r.para(0, 0, 0, 40)
			r.span(fmt.Sprintf("\\cf%d", cPara), l)
			r.par()
		}
		r.smallMuted("Проверьте настройки ИИ: значок шестерёнки справа сверху или Ctrl+, (запятая).", 120, 0)
	default:
		for _, l := range answer {
			if l == "" {
				r.para(0, 0, 0, 0)
				r.par()
				continue
			}
			if strings.HasPrefix(l, "- ") {
				r.para(300, -300, 20, 40)
				r.raw("\\tx300 ")
				r.span(fmt.Sprintf("\\cf%d", cAccent), "•\t")
				r.answerLine(strings.TrimPrefix(l, "- "))
				r.par()
				continue
			}
			r.para(0, 0, 40, 80)
			r.answerLine(l)
			r.par()
		}
		if meta != "" {
			r.smallMuted("Ответил: "+meta, 160, 0)
		}
		r.para(0, 0, 40, 0)
		r.span(fmt.Sprintf("\\fs18\\cf%d", cYellow), "ИИ может ошибаться. Сверяйтесь с пунктами ниже — окончательное решение всегда за администрацией.")
		r.par()
	}

	pts, label := cited, "Пункты, на которые ссылается ответ"
	if len(pts) == 0 && len(related) > 0 {
		pts, label = related, "Похожие пункты правил"
	}
	if len(pts) > 0 {
		r.para(0, 0, 480, 0)
		r.span(fmt.Sprintf("\\b\\fs26\\cf%d", cWhite), label)
		r.par()
		last := ""
		for _, p := range pts {
			lb := p.Doc.Short
			if p.Sec != nil && p.Sec.Title != "" {
				lb += "  ·  " + p.Sec.Title
			}
			if lb != last {
				r.para(0, 0, 240, 0)
				r.span(fmt.Sprintf("\\fs18\\cf%d", cMuted), lb)
				r.par()
				last = lb
			}
			out.Points = append(out.Points, r.point(p, favs[FavKey(p)]))
		}
	}
	r.para(0, 0, 400, 0)
	r.par()
	r.raw("}")
	out.RTF = r.b.String()
	out.Length = r.pos
	return out
}

// RenderAIIntro — подсказка при входе в режим ИИ.
func RenderAIIntro(provider, problem string, errs []string) Rendered {
	r := &rtfB{}
	r.raw(header())
	r.errors(errs)
	r.para(0, 0, 0, 40)
	r.span(fmt.Sprintf("\\f1\\fs34\\cf%d", cAccent), "✦ ")
	r.span(fmt.Sprintf("\\b\\fs34\\cf%d", cWhite), "Спросить ИИ по правилам")
	r.par()
	r.para(0, 0, 60, 60)
	r.span(fmt.Sprintf("\\cf%d", cPara), "Напишите вопрос своими словами в поле сверху и нажмите Enter. ИИ получит подходящие пункты правил и ответит со ссылками на них — каждый пункт можно открыть двойным кликом.")
	r.par()
	if problem != "" {
		r.para(0, 0, 160, 40)
		r.span(fmt.Sprintf("\\b\\cf%d", cOrange), "Сначала настройте ИИ")
		r.par()
		r.para(0, 0, 0, 40)
		r.span(fmt.Sprintf("\\cf%d", cPara), problem+" Нажмите на шестерёнку справа сверху (или Ctrl+, ), выберите сервис и вставьте свой ключ.")
		r.par()
	} else {
		r.smallMuted("Сейчас используется: "+provider+"   ·   сменить — шестерёнка справа сверху", 120, 0)
	}
	r.para(0, 0, 360, 80)
	r.span(fmt.Sprintf("\\b\\fs26\\cf%d", cWhite), "Например")
	r.par()
	for _, ex := range []string{
		"Можно ли отстреливаться в зелёной зоне, если по мне открыли огонь?",
		"Сколько можно требовать за выкуп лидера фракции?",
		"Что будет за DM и чем он отличается от DB?",
		"Сколько человек нужно, чтобы ограбить магазин 24/7?",
		"Можно ли второму персонажу быть в банде, если первый в LSPD?",
	} {
		r.para(300, -300, 20, 40)
		r.raw("\\tx300 ")
		r.span(fmt.Sprintf("\\cf%d", cAccent), "•\t")
		r.span(fmt.Sprintf("\\cf%d", cPara), ex)
		r.par()
	}
	r.smallMuted("Ключ хранится только на вашем компьютере в зашифрованном виде. Запросы оплачиваются по тарифам выбранного сервиса (Ollama и LM Studio — бесплатно).", 360, 0)
	r.para(0, 0, 400, 0)
	r.par()
	r.raw("}")
	out := Rendered{RTF: r.b.String(), Length: r.pos}
	return out
}
