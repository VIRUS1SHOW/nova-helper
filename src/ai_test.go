package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelevant(t *testing.T) {
	docs, errs := LoadDocs("../rules")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	cases := map[string][]string{
		"можно ли стрелять в зелёной зоне если по мне стреляют": {"Зелёные зоны"},
		"сколько можно требовать выкуп за лидера":               {"Похищения и ограбления", "Банкоматы, дома, 24/7"},
		"что будет за дм":                  {"Общие правила"},
		"во сколько начинается инкассация": {"Инкассация"},
	}
	for q, want := range cases {
		pts := Relevant(docs, q, 12)
		if len(pts) == 0 {
			t.Errorf("%q: ничего не найдено", q)
			continue
		}
		ok := false
		for _, p := range pts[:minInt(6, len(pts))] {
			for _, w := range want {
				if p.Doc.Short == w {
					ok = true
				}
			}
		}
		if !ok {
			var got []string
			for _, p := range pts[:minInt(6, len(pts))] {
				got = append(got, p.Doc.Short+" "+p.Num)
			}
			t.Errorf("%q: ожидали %v в первых результатах, получили %v", q, want, got)
		}
	}
	ctx, used := BuildContext(Relevant(docs, "дм", 30), 6000)
	if len(ctx) > 6500 || len(used) == 0 {
		t.Errorf("контекст: %d символов, %d пунктов", len(ctx), len(used))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestAskOpenAICompatible(t *testing.T) {
	var gotAuth, gotFolder, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		gotFolder = r.Header.Get("x-folder-id")
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		json.Unmarshal(b, &req)
		gotModel = req.Model
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || !strings.Contains(req.Messages[1].Content, "Вопрос игрока: тест") {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"**Нельзя** (п. 4.1, «Общие правила»)."}}]}`))
	}))
	defer srv.Close()
	c := AIConfig{Provider: "yandex", Key: "k1", Folder: "b1gfolder", Model: "aliceai-llm", BaseURL: srv.URL + "/v1"}
	ans, err := AskAI(c, "тест", "ctx")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Api-Key k1" || gotFolder != "b1gfolder" || gotModel != "gpt://b1gfolder/aliceai-llm" {
		t.Errorf("заголовки/модель: %q %q %q", gotAuth, gotFolder, gotModel)
	}
	if CleanAnswer(ans)[0] != "Нельзя (п. 4.1, «Общие правила»)." {
		t.Errorf("ответ: %q", CleanAnswer(ans))
	}
	c2 := AIConfig{Provider: "deepseek", Key: "k2", BaseURL: srv.URL + "/v1"}
	if _, err := AskAI(c2, "тест", "ctx"); err != nil || gotAuth != "Bearer k2" || gotModel != "deepseek-flash" {
		t.Errorf("deepseek: %v %q %q", err, gotAuth, gotModel)
	}
}

func TestAskAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "sk" || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(401)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		w.Write([]byte(`{"content":[{"type":"text","text":"Ответ (п. 2.1)"}]}`))
	}))
	defer srv.Close()
	ans, err := AskAI(AIConfig{Provider: "claude", Key: "sk", BaseURL: srv.URL}, "q", "c")
	if err != nil || ans != "Ответ (п. 2.1)" {
		t.Fatalf("%q %v", ans, err)
	}
	_, err = AskAI(AIConfig{Provider: "claude", Key: "bad", BaseURL: srv.URL}, "q", "c")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("ошибка: %v", err)
	}
	if (AIConfig{Provider: "claude"}).Problem() == "" || (AIConfig{Provider: "ollama"}).Problem() != "" {
		t.Error("проверка настроек")
	}
}

func TestCitedAndSearchSynonyms(t *testing.T) {
	docs, _ := LoadDocs("../rules")
	ctx := Relevant(docs, "что такое дм", 30)
	if ctx[0].Num != "4.1" || ctx[0].Doc.Short != "Общие правила" {
		t.Errorf("«что такое дм»: первым %s %s", ctx[0].Doc.Short, ctx[0].Num)
	}
	c := Cited("Это DM (п. 4.1, «Общие правила»), см. также пункты 7.2 и 7.7.", ctx)
	if len(c) == 0 || c[0].Num != "4.1" {
		t.Errorf("ссылки: %v", c)
	}
	found, _ := Search(docs, "дм")
	if len(found) == 0 {
		t.Error("поиск «дм» ничего не нашёл")
	}
	if g := Groups(docs); len(g) != 5 {
		t.Errorf("групп: %v", g)
	}
}
