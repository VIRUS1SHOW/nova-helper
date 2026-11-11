package main

// Вопрос к правилам через ИИ: подбор подходящих пунктов, запрос к сервису,
// разбор ответа. Не зависит от Windows — проверяется тестами на любой системе.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ------------------------------------------------------------------ сервисы

type AIProvider struct {
	ID        string // значение в settings.ini
	Name      string // как показывать в настройках
	Kind      string // "anthropic" или "openai" (OpenAI-совместимый формат)
	BaseURL   string
	Model     string // модель по умолчанию; {folder} заменяется на ID каталога
	NeedKey   bool
	NeedFold  bool // Яндекс: нужен ID каталога
	Hint      string
	KeyHeader string // если не "Authorization: Bearer"
}

var AIProviders = []AIProvider{
	{ID: "yandex", Name: "Алиса AI (Яндекс)", Kind: "openai", BaseURL: "https://ai.api.cloud.yandex.net/v1",
		Model: "gpt://{folder}/aliceai-llm-flash", NeedKey: true, NeedFold: true, KeyHeader: "Api-Key",
		Hint: "aistudio.yandex.cloud → «Создать API-ключ»; ID каталога — в адресе консоли. Модели: aliceai-llm-flash (быстрая), aliceai-llm."},
	{ID: "claude", Name: "Claude (Anthropic)", Kind: "anthropic", BaseURL: "https://api.anthropic.com",
		Model: "claude-haiku-5-5", NeedKey: true,
		Hint: "Ключ из console.anthropic.com. Модели: claude-haiku-5-5 (быстрая), claude-sonnet-5-5 (умнее)."},
	{ID: "deepseek", Name: "DeepSeek", Kind: "openai", BaseURL: "https://api.deepseek.com",
		Model: "deepseek-flash", NeedKey: true, Hint: "Ключ: platform.deepseek.com → API keys. Модели: deepseek-flash (быстрая), deepseek-v4-pro (умнее)."},
	{ID: "openrouter", Name: "OpenRouter", Kind: "openai", BaseURL: "https://openrouter.ai/api/v1",
		Model: "deepseek/deepseek-chat", NeedKey: true, Hint: "Один ключ на десятки моделей: openrouter.ai/keys."},
	{ID: "openai", Name: "OpenAI (ChatGPT)", Kind: "openai", BaseURL: "https://api.openai.com/v1",
		Model: "gpt-4.1-mini", NeedKey: true, Hint: "Ключ из platform.openai.com."},
	{ID: "gemini", Name: "Google Gemini", Kind: "openai", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Model: "gemini-2.5-flash", NeedKey: true, Hint: "Ключ из aistudio.google.com."},
	{ID: "ollama", Name: "Ollama (на этом ПК)", Kind: "openai", BaseURL: "http://localhost:11434/v1",
		Model: "qwen2.5:7b", Hint: "Бесплатно и без ключа. Нужен установленный Ollama и скачанная модель."},
	{ID: "lmstudio", Name: "LM Studio (на этом ПК)", Kind: "openai", BaseURL: "http://localhost:1234/v1",
		Model: "local-model", Hint: "Бесплатно и без ключа. Запустите сервер в LM Studio."},
	{ID: "custom", Name: "Свой адрес (OpenAI-совместимый)", Kind: "openai", BaseURL: "",
		Model: "", Hint: "Любой сервис с OpenAI-совместимым API: укажите адрес и модель."},
}

func ProviderByID(id string) AIProvider {
	for _, p := range AIProviders {
		if p.ID == id {
			return p
		}
	}
	return AIProviders[0]
}

type AIConfig struct {
	Provider string
	Key      string
	Model    string
	BaseURL  string
	Folder   string
}

// Effective — итоговые адрес и модель с учётом значений по умолчанию.
func (c AIConfig) Effective() (AIProvider, string, string) {
	p := ProviderByID(c.Provider)
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = p.BaseURL
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = p.Model
	}
	if p.NeedFold && model != "" && !strings.Contains(model, "://") && !strings.Contains(model, "{folder}") {
		model = "gpt://{folder}/" + model // можно вписать просто «aliceai-llm»
	}
	model = strings.ReplaceAll(model, "{folder}", strings.TrimSpace(c.Folder))
	return p, base, model
}

// Problem — что не заполнено в настройках (пусто, если всё в порядке).
func (c AIConfig) Problem() string {
	p, base, model := c.Effective()
	switch {
	case p.NeedKey && strings.TrimSpace(c.Key) == "":
		return "Не указан ключ для «" + p.Name + "»."
	case p.NeedFold && strings.TrimSpace(c.Folder) == "":
		return "Для Алисы AI нужен ID каталога Yandex Cloud."
	case base == "":
		return "Не указан адрес сервиса."
	case model == "":
		return "Не указана модель."
	}
	return ""
}

// ------------------------------------------------------------------ подбор пунктов

var stopWords = map[string]bool{
	"что": true, "как": true, "где": true, "когда": true, "можно": true, "нельзя": true, "ли": true,
	"если": true, "или": true, "это": true, "для": true, "при": true, "его": true, "она": true, "они": true,
	"мне": true, "меня": true, "нас": true, "вас": true, "них": true, "так": true, "уже": true, "еще": true,
	"был": true, "была": true, "было": true, "быть": true, "есть": true, "будет": true, "чем": true,
	"мой": true, "моя": true, "мое": true, "мои": true, "тот": true, "эта": true, "этот": true, "эти": true,
	"какой": true, "какая": true, "какие": true, "какое": true, "сколько": true, "почему": true, "зачем": true,
	"правило": true, "правила": true, "правилам": true, "пункт": true, "по": true, "за": true, "на": true,
	"не": true, "да": true, "нет": true, "ну": true, "вот": true, "там": true, "тут": true, "все": true,
	"такое": true, "такой": true, "такая": true, "такие": true, "также": true, "тоже": true, "означает": true,
	"значит": true, "делать": true, "могу": true, "может": true, "надо": true, "нужно": true,
	"кто": true, "кого": true, "чего": true, "чтобы": true, "только": true, "очень": true, "просто": true,
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+(?:\.\d+)*`)

func stem(w string) string {
	r := []rune(w)
	if len(r) > 6 {
		r = r[:len(r)-2]
	} else if len(r) > 4 {
		r = r[:len(r)-1]
	}
	return string(r)
}

// queryTerms — основы слов вопроса с синонимами.
func queryTerms(q string) (terms [][]string, nums []string) {
	terms, nums, _ = queryTermsW(q)
	return
}

// queryTermsW дополнительно возвращает вес слова: сокращения из шпаргалки (дм, мг…) весят больше.
func queryTermsW(q string) (terms [][]string, nums []string, weight []float64) {
	seen := map[string]bool{}
	for _, w := range wordRe.FindAllString(Norm(q), -1) {
		if numQueryRe.MatchString(w) && strings.Contains(w, ".") {
			nums = append(nums, strings.TrimSuffix(w, "."))
			continue
		}
		rs := []rune(w)
		_, syn := synonyms[w]
		if !syn && (len(rs) < 3 || stopWords[w] || unicode.IsDigit(rs[0])) {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		var alt []string
		for _, a := range Expand(w) {
			alt = append(alt, stemShort(a))
		}
		terms = append(terms, alt)
		if syn {
			weight = append(weight, 2.5)
		} else {
			weight = append(weight, 1)
		}
	}
	return terms, nums, weight
}

func pointHaystack(p *Point) string {
	h := p.search + " " + Norm(p.Doc.Title) + " " + Norm(p.Doc.Short)
	if p.Sec != nil {
		h += " " + Norm(p.Sec.Title)
	}
	return h
}

func stemShort(a string) string {
	if isShortAbbr(a) {
		return a
	}
	return stem(a)
}

// Relevant подбирает пункты, которые больше всего подходят к вопросу.
func Relevant(docs []*Doc, q string, limit int) []*Point {
	terms, nums, weight := queryTermsW(q)
	var all []*Point
	for _, d := range docs {
		all = append(all, d.Points...)
	}
	if len(all) == 0 {
		return nil
	}
	hay := make([]string, len(all))
	hw := make([]map[string]bool, len(all))
	for i, p := range all {
		hay[i] = pointHaystack(p)
		hw[i] = wordSet(hay[i])
	}
	// редкие слова важнее частых
	idf := make([]float64, len(terms))
	for ti, alt := range terms {
		df := 0
		for hi, h := range hay {
			for _, a := range alt {
				if termIn(h, hw[hi], a) {
					df++
					break
				}
			}
		}
		idf[ti] = math.Log(1 + float64(len(all))/float64(1+df))
	}
	type sc struct {
		p *Point
		s float64
	}
	var res []sc
	for i, p := range all {
		score := 0.0
		matched := 0
		for ti, alt := range terms {
			best := 0
			for _, a := range alt {
				if n := termCount(hay[i], hw[i], a); n > best {
					best = n
				}
			}
			if best > 0 {
				matched++
				score += weight[ti] * idf[ti] * (1 + math.Log(float64(best)))
			}
		}
		if matched > 1 {
			score *= 1 + 0.35*float64(matched-1) // совпало несколько слов — сильно лучше
		}
		for _, n := range nums {
			if p.Num == n {
				score += 25
			}
		}
		if score > 0 {
			res = append(res, sc{p, score})
		}
	}
	sort.SliceStable(res, func(a, b int) bool { return res[a].s > res[b].s })
	if len(res) > limit {
		res = res[:limit]
	}
	out := make([]*Point, len(res))
	for i, r := range res {
		out[i] = r.p
	}
	return out
}

// ------------------------------------------------------------------ запрос

const aiSystem = `Ты — помощник по правилам GTA 5 RolePlay-проекта NOVA RP. Отвечай по-русски, коротко и по делу.
Опирайся ТОЛЬКО на пункты правил, которые даны ниже. Ничего не выдумывай.
После каждого утверждения указывай пункт в формате (п. 4.1, «Общие правила»), а для статей законов — (ст. 5, «Юрисдикция»).
Если в данных пунктах ответа нет — честно скажи, что в правилах это прямо не описано, и посоветуй уточнить у администрации.
Если за нарушение положено наказание, назови его так, как в правилах.
Пункты с пометкой «Законы штата» — это IC-законы штата San Andreas (их применяют полиция, прокуратура и суд в игре: розыск, срок, штраф).
Остальные пункты — OOC-правила проекта (их применяет администрация: JAIL, WARN, BAN, MUTE). Не путай одно с другим и, если важно, укажи, что это — закон или правило проекта.
Не используй таблицы и заголовки. Можно короткий список через «- ».`

// BuildContext — текст пунктов для ИИ (с ограничением по длине).
func BuildContext(pts []*Point, maxChars int) (string, []*Point) {
	var b strings.Builder
	var used []*Point
	for _, p := range pts {
		var e strings.Builder
		if p.Doc.Group == "Законы штата" {
			e.WriteString("[Законы штата: ")
		} else {
			e.WriteString("[Правила проекта: ")
		}
		fmt.Fprintf(&e, "«%s»", p.Doc.Short)
		if p.Sec != nil && p.Sec.Title != "" {
			fmt.Fprintf(&e, " / %s", p.Sec.Title)
		}
		fmt.Fprintf(&e, "] п. %s: %s", p.Num, p.Text)
		if len(p.Pun) > 0 {
			e.WriteString(" | Наказание: " + strings.Join(p.Pun, "; "))
		}
		for _, it := range p.Items {
			switch it.Kind {
			case "note":
				e.WriteString("\n  " + it.Label + ": " + it.Text)
			default:
				e.WriteString("\n  - " + it.Text)
			}
		}
		e.WriteString("\n\n")
		if b.Len()+e.Len() > maxChars && len(used) > 0 {
			break
		}
		b.WriteString(e.String())
		used = append(used, p)
	}
	return b.String(), used
}

func aiUserMessage(question, context string) string {
	return "Пункты правил:\n\n" + context + "\nВопрос игрока: " + question
}

var aiClient = &http.Client{Timeout: 90 * time.Second}

// AskAI отправляет вопрос и возвращает текст ответа.
func AskAI(c AIConfig, question, context string) (string, error) {
	if pr := c.Problem(); pr != "" {
		return "", fmt.Errorf("%s", pr)
	}
	p, base, model := c.Effective()
	user := aiUserMessage(question, context)
	var req *http.Request
	var err error
	if p.Kind == "anthropic" {
		body, _ := json.Marshal(map[string]any{
			"model":      model,
			"max_tokens": 900,
			"system":     aiSystem,
			"messages":   []map[string]string{{"role": "user", "content": user}},
		})
		req, err = http.NewRequest("POST", base+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("x-api-key", strings.TrimSpace(c.Key))
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		body, _ := json.Marshal(map[string]any{
			"model":       model,
			"max_tokens":  900,
			"temperature": 0.2,
			"messages": []map[string]string{
				{"role": "system", "content": aiSystem},
				{"role": "user", "content": user},
			},
		})
		req, err = http.NewRequest("POST", base+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		if k := strings.TrimSpace(c.Key); k != "" {
			if p.KeyHeader != "" {
				req.Header.Set("Authorization", p.KeyHeader+" "+k)
			} else {
				req.Header.Set("Authorization", "Bearer "+k)
			}
		}
		if f := strings.TrimSpace(c.Folder); f != "" {
			req.Header.Set("x-folder-id", f)
			req.Header.Set("OpenAI-Project", f)
		}
		if p.ID == "openrouter" {
			req.Header.Set("HTTP-Referer", "https://github.com/VIRUS1SHOW/nova-helper")
			req.Header.Set("X-Title", "Nova Helper")
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NovaHelper/"+appVersion)

	resp, err := aiClient.Do(req)
	if err != nil {
		if strings.Contains(base, "localhost") {
			return "", fmt.Errorf("не удалось подключиться к %s — запущена ли программа (Ollama / LM Studio)?", base)
		}
		return "", fmt.Errorf("нет связи с сервисом: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s", httpProblem(resp.StatusCode, raw))
	}
	if p.Kind == "anthropic" {
		var r struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return "", fmt.Errorf("непонятный ответ сервиса")
		}
		var out []string
		for _, c := range r.Content {
			if c.Type == "text" {
				out = append(out, c.Text)
			}
		}
		if len(out) == 0 {
			return "", fmt.Errorf("сервис вернул пустой ответ")
		}
		return strings.TrimSpace(strings.Join(out, "\n")), nil
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Choices) == 0 {
		return "", fmt.Errorf("непонятный ответ сервиса")
	}
	t := strings.TrimSpace(r.Choices[0].Message.Content)
	if t == "" {
		return "", fmt.Errorf("сервис вернул пустой ответ")
	}
	return t, nil
}

func httpProblem(code int, raw []byte) string {
	msg := ""
	var e struct {
		Error any    `json:"error"`
		Msg   string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		switch v := e.Error.(type) {
		case string:
			msg = v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				msg = m
			}
		}
		if msg == "" {
			msg = e.Msg
		}
	}
	if len([]rune(msg)) > 220 {
		msg = string([]rune(msg)[:220]) + "…"
	}
	hint := ""
	switch code {
	case 401, 403:
		hint = "Ключ не подошёл или у него нет доступа. Проверьте ключ в настройках."
	case 404:
		hint = "Адрес или модель не найдены. Проверьте название модели."
	case 402:
		hint = "На счёте сервиса закончились деньги."
	case 429:
		hint = "Слишком много запросов или закончился лимит. Подождите немного."
	default:
		if code >= 500 {
			hint = "Сервис временно не отвечает. Попробуйте позже."
		}
	}
	out := fmt.Sprintf("ошибка %d", code)
	if hint != "" {
		out += ". " + hint
	}
	if msg != "" {
		out += "\n(" + msg + ")"
	}
	return out
}

// ------------------------------------------------------------------ разбор ответа

var citeRe = regexp.MustCompile(`(?i)(?:п\.|пункт[а-я]*|пп\.)\s*((?:\d+(?:\.\d+)+)(?:\s*(?:,|и)\s*\d+(?:\.\d+)+)*)(?:[^«\n]{0,8}«([^»\n]{1,60})»)?`)
var numInRe = regexp.MustCompile(`\d+(?:\.\d+)+`)

// ссылки на статьи законов: «ст. 5», «статья 12»
var artCiteRe = regexp.MustCompile(`(?i)(?:ст\.|стать[а-я]*)\s*(\d+(?:\.\d+)*)(?:[^«\n]{0,8}«([^»\n]{1,60})»)?`)

// Cited — пункты из контекста, на которые сослался ИИ (в порядке упоминания).
// Номера пунктов повторяются в разных документах, поэтому учитываем название в «ёлочках».
func Cited(answer string, ctx []*Point) []*Point {
	var out []*Point
	seen := map[*Point]bool{}
	type ref struct {
		nums []string
		doc  string
	}
	var refs []ref
	for _, m := range citeRe.FindAllStringSubmatch(answer, -1) {
		refs = append(refs, ref{numInRe.FindAllString(m[1], -1), m[2]})
	}
	for _, m := range artCiteRe.FindAllStringSubmatch(answer, -1) {
		refs = append(refs, ref{[]string{"ст. " + m[1]}, m[2]})
	}
	for _, rf := range refs {
		docName := Norm(strings.TrimSpace(rf.doc))
		for _, n := range rf.nums {
			var cand []*Point
			for _, p := range ctx {
				if p.Num == n {
					cand = append(cand, p)
				}
			}
			if docName != "" {
				var best []*Point
				for _, p := range cand {
					t, sh := Norm(p.Doc.Title), Norm(p.Doc.Short)
					if strings.Contains(t, docName) || strings.Contains(docName, sh) || strings.Contains(sh, docName) {
						best = append(best, p)
					}
				}
				if len(best) > 0 {
					cand = best
				}
			}
			for _, p := range cand {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// CleanAnswer убирает разметку Markdown, которую RichEdit не покажет.
func CleanAnswer(s string) []string {
	s = strings.ReplaceAll(s, "\r", "")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		t = strings.TrimLeft(t, "#")
		t = strings.ReplaceAll(t, "**", "")
		t = strings.ReplaceAll(t, "__", "")
		t = strings.ReplaceAll(t, "`", "")
		t = strings.TrimSpace(t)
		for _, b := range []string{"* ", "• "} {
			if strings.HasPrefix(t, b) {
				t = "- " + strings.TrimPrefix(t, b)
			}
		}
		out = append(out, t)
	}
	// убрать лишние пустые строки
	var res []string
	for i, l := range out {
		if l == "" && (i == 0 || out[i-1] == "") {
			continue
		}
		res = append(res, l)
	}
	for len(res) > 0 && res[len(res)-1] == "" {
		res = res[:len(res)-1]
	}
	return res
}
