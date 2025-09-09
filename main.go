package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type Profile struct {
	Name        string
	Title       string
	Company     string
	Location    string
	Role        string
	URL         string
	SourceQuery string
	CapturedAt  time.Time
}

func main() {
	var (
		email       = flag.String("email", "", "Email do LinkedIn")
		password    = flag.String("password", "", "Senha do LinkedIn")
		query       = flag.String("query", "", "Texto da busca (ex: \"software engineer\")")
		maxPages    = flag.Int("max-pages", 1, "Número máximo de páginas para capturar (>=1)")
		headless    = flag.Bool("headless", true, "Rodar Chromium em modo headless")
		sendInvites = flag.Bool("send-invites", false, "Enviar convites após capturar (cautela!)")
		outDir      = flag.String("out-dir", "data", "Diretório de saída para CSV")
		dumpHTML    = flag.Bool("dump-html", false, "Salvar HTML da página de resultados para depuração")
	)
	flag.Parse()

	*query = sanitizeQuotes(*query)

	if *email == "" || *password == "" || *query == "" {
		log.Fatal("uso: --email --password --query [--max-pages N] [--headless=false] [--send-invites] [--out-dir data]")
	}
	if *maxPages < 1 {
		*maxPages = 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("criando pasta de saída: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", *headless),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "pt-BR"),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36"),
	)
	if p := os.Getenv("CHROME_PATH"); p != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(p))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer allocCancel()

	bctx, bcancel := chromedp.NewContext(allocCtx)
	defer bcancel()

	if err := chromedp.Run(bctx, chromedp.Navigate("about:blank")); err != nil {
		log.Fatalf("inicializando chrome: %v", err)
	}

	log.Printf("➡️  Login no LinkedIn (headless=%v)", *headless)
	if err := loginLinkedIn(bctx, *email, *password, *headless); err != nil {
		log.Fatalf("falha no login: %v", err)
	}
	log.Println("✅ Login ok")

	log.Printf("➡️  Buscando (desktop): %q", *query)
	if err := runSearchViaURL(bctx, *query); err != nil {
		log.Fatalf("falha ao executar busca: %v", err)
	}
	log.Println("🔎 Resultados carregados")

	if *dumpHTML {
		if err := dumpPageHTML(bctx, filepath.Join(*outDir, "results_page_1.html")); err != nil {
			log.Printf("warn: dump html falhou: %v", err)
		} else {
			log.Printf("📝 HTML salvo: %s", filepath.Join(*outDir, "results_page_1.html"))
		}
	}

	var all []Profile
	for page := 1; page <= *maxPages; page++ {
		log.Printf("➡️  Capturando página %d/%d…", page, *maxPages)
		items, err := scrapeCurrentPage(bctx, *query)
		if err != nil {
			log.Printf("aviso: erro capturando página %d: %v", page, err)
		}

		// >>> Ajustes pedidos: nome via URL quando vazio + não repetir título/região
		for i := range items {
			// normaliza nome vindo da UI (remove prefixos de status) e cai para URL se vazio
			items[i].Name = strings.TrimSpace(strings.TrimPrefix(items[i].Name, "O status está off-line"))
			if items[i].Name == "" {
				if n := guessNameFromURL(items[i].URL); n != "" {
					items[i].Name = n
				}
			}

			// evita repetir título e região (se iguais, zera região)
			if items[i].Title != "" && items[i].Location != "" &&
				strings.EqualFold(items[i].Title, items[i].Location) {
				items[i].Location = ""
			}
		}
		// <<< fim dos ajustes

		log.Printf("   • perfis capturados na página %d: %d", page, len(items))
		all = append(all, items...)

		if page < *maxPages {
			ok := goNextPage(bctx)
			if !ok {
				log.Println("ℹ️  Não encontrei 'Avançar' (ou fim dos resultados). Encerrando paginação.")
				break
			}
			randomSleep(1500, 3000)
		}
	}

	log.Printf("📦 Total capturado: %d perfis", len(all))

	if *sendInvites {
		log.Printf("➡️  Enviando convites (heurística simples)…")
		sent := sendConnectInvites(bctx, 20)
		log.Printf("✅ Convites enviados: %d", sent)
	}

	filename := filepath.Join(*outDir, fmt.Sprintf("linkedin_%s.csv", time.Now().Format("20060102_150405")))
	if err := writeCSV(filename, all); err != nil {
		log.Fatalf("erro salvando CSV: %v", err)
	}
	log.Printf("💾 CSV salvo em: %s", filename)

	log.Println("🏁 Fim.")
}

// =============== Login ===============

func loginLinkedIn(ctx context.Context, email, password string, headless bool) error {
	const loginURL = "https://www.linkedin.com/checkpoint/lg/sign-in-another-account"
	const feedURL = "https://www.linkedin.com/feed/"

	// 1) abrir login e preencher
	if err := chromedp.Run(ctx,
		chromedp.Navigate(loginURL),
		chromedp.WaitVisible(`#username`, chromedp.ByQuery),
		chromedp.SetValue(`#username`, email, chromedp.ByQuery),
		chromedp.SetValue(`#password, input[name="session_password"]`, password, chromedp.ByQuery),
	); err != nil {
		return err
	}

	// 2) enviar (click + fallbacks)
	clickTried := chromedp.Run(ctx,
		chromedp.WaitVisible(`button[data-litms-control-urn="login-submit"], button[type="submit"]`, chromedp.ByQuery),
		chromedp.ScrollIntoView(`button[data-litms-control-urn="login-submit"], button[type="submit"]`, chromedp.ByQuery),
		chromedp.Click(`button[data-litms-control-urn="login-submit"], button[type="submit"]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	)
	if clickTried != nil {
		_ = chromedp.Run(ctx, chromedp.Submit(`form`))
		_ = chromedp.Run(ctx, chromedp.Focus(`#password, input[name="session_password"]`), chromedp.KeyEvent("\r"))
	}

	// 3) captcha em iframe (challenge "antigo")
	if isCaptcha(ctx) {
		if headless {
			return errors.New("captcha (iframe) detectado em modo headless; rode com --headless=false para resolver manualmente")
		}
		log.Println("⏳ Captcha (iframe) detectado. Resolva manualmente. Esperando até 180s…")
		if err := waitDisappear(ctx, 180*time.Second, `iframe[src*="captcha"], iframe[src*="challenge"]`); err != nil {
			return errors.New("timeout aguardando captcha (iframe)")
		}
	}

	// 4) checkpoint challenge (página inteira)
	if isCheckpointChallenge(ctx) {
		if headless {
			return errors.New("checkpoint challenge (página inteira) detectado em modo headless; rode com --headless=false para resolver manualmente")
		}
		log.Println("⏳ Challenge detectado. Tentando clicar 'Iniciar desafio' e aguardando você resolver… (até 5 min)")

		// tenta iniciar o desafio (se botão estiver presente)
		_ = chromedp.Run(ctx,
			chromedp.ActionFunc(func(c context.Context) error {
				var clicked bool
				return chromedp.EvaluateAsDevTools(`(()=>{
          const b = document.querySelector('[data-theme="home.verifyButton"], button.sc-nkuzb1-0, button:contains("Iniciar desafio")');
          if(!b) return false;
          b.scrollIntoView({behavior:'instant', block:'center'});
          b.click();
          return true;
        })()`, &clicked).Do(c)
			}),
			chromedp.Sleep(1200*time.Millisecond),
		)

		// espera challenge sumir OU feed/search ficar visível
		err := waitUntil(ctx, 5*time.Minute, `
      (()=>{
        const stillChallenge = (()=>{
          const href = location.href || "";
          if (href.includes("/checkpoint/challenge/")) return true;
          const h2 = document.querySelector('[data-theme="home.title"], h2.sc-1io4bok-0');
          const btn = document.querySelector('[data-theme="home.verifyButton"]');
          const txt = (h2?.textContent||"") + " " + (btn?.textContent||"");
          return /Proteger a sua conta|Iniciar desafio/i.test(txt);
        })();

        if (stillChallenge) return false;

        // sinal de que “saí” do challenge e estou logado
        if (document.querySelector('input[placeholder*="Pesquisar"], input[placeholder*="Search"]')) return true;
        if ((location.href||"").includes("/feed/")) return true;
        return false;
      })()
    `)
		if err != nil {
			return errors.New("timeout aguardando resolução do challenge")
		}
	}

	// 5) 2FA (quando aparece campo one-time code/pin)
	if has2FA(ctx) {
		log.Println("⏳ 2FA detectada. Insira o código. Aguardando 180s…")
		if err := waitDisappear(ctx, 180*time.Second, `input[autocomplete="one-time-code"], input[name*="pin"]`); err != nil {
			return errors.New("timeout aguardando 2FA")
		}
	}

	// 6) valida indo ao feed
	return chromedp.Run(ctx,
		chromedp.Navigate(feedURL),
		chromedp.WaitReady(`body`, chromedp.ByQuery),
	)
}

func isCheckpointChallenge(ctx context.Context) bool {
	var on bool
	_ = chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`
      (()=>{
        const href = location.href || "";
        if (href.includes("/checkpoint/challenge/")) return true;

        // Heurística pela UI do desafio que você mostrou
        const h2 = document.querySelector('[data-theme="home.title"], h2.sc-1io4bok-0');
        const btn = document.querySelector('[data-theme="home.verifyButton"]');
        const txt = (h2?.textContent || "") + " " + (btn?.textContent || "");
        if (/Proteger a sua conta|Iniciar desafio/i.test(txt)) return true;

        return false;
      })()
    `, &on),
	)
	return on
}

func waitUntil(ctx context.Context, timeout time.Duration, jsCond string) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var ok bool
		err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(jsCond, &ok))
		if err == nil && ok {
			return nil
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return errors.New("timeout aguardando condição")
}

func isCaptcha(ctx context.Context) bool {
	var n int
	_ = chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`document.querySelectorAll('iframe[src*="captcha"], iframe[src*="challenge"]').length`, &n),
	)
	return n > 0
}

func has2FA(ctx context.Context) bool {
	var n int
	_ = chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`document.querySelectorAll('input[autocomplete="one-time-code"], input[name*="pin"]').length`, &n),
	)
	return n > 0
}

func waitDisappear(ctx context.Context, timeout time.Duration, css string) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var n int
		err := chromedp.Run(ctx,
			chromedp.EvaluateAsDevTools(fmt.Sprintf(`document.querySelectorAll(%q).length`, css), &n),
		)
		if err == nil && n == 0 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("timeout")
}

// =============== Busca via URL ===============

func runSearchViaURL(ctx context.Context, q string) error {
	desktop := "https://www.linkedin.com/search/results/people/?keywords=" + url.QueryEscape(q)
	mobile := "https://www.linkedin.com/m/search/results/people/?keywords=" + url.QueryEscape(q)

	// tenta desktop
	if err := chromedp.Run(ctx,
		chromedp.Navigate(desktop),
		chromedp.WaitReady("body", chromedp.ByQuery),
		waitDOMComplete(),
		chromedp.Sleep(500*time.Millisecond),
		waitForResults(),
	); err == nil {
		return nil
	}

	// fallback: mobile
	return chromedp.Run(ctx,
		chromedp.Navigate(mobile),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(700*time.Millisecond),
		waitForResults(),
	)
}

func waitDOMComplete() chromedp.Action {
	return chromedp.EvaluateAsDevTools(`new Promise(r=>{
        if (document.readyState==='complete') return r(true);
        window.addEventListener('load', ()=>r(true), {once:true});
    })`, nil)
}

func waitForResults() chromedp.Action {
	// qualquer contêiner típico de resultados serve
	sel := `main .search-results-container,
            main ul.reusable-search__entity-result-list,
            main .reusable-search__entity-result-list,
            main [data-view-name="search-entity-result-universal-template"],
            main [data-chameleon-result-urn]`
	return chromedp.WaitVisible(sel, chromedp.ByQuery)
}

// =============== Paginação ===============

func goNextPage(ctx context.Context) bool {
	sel := `button[aria-label="Avançar"], a[aria-label="Avançar"]`
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.ScrollIntoView(sel, chromedp.ByQuery),
		chromedp.Click(sel, chromedp.ByQuery),
		waitForResults(),
	); err == nil {
		return true
	}
	return false
}

// =============== Coleta ===============

func scrapeCurrentPage(ctx context.Context, sourceQuery string) ([]Profile, error) {
	js := `(() => {
	  const clean = s => (s || '').replace(/\u00a0/g,' ').replace(/\s+/g,' ').trim();
	  const getText = el => el ? clean(el.textContent || "") : "";

	  const looksLikeCity = (txt) => {
	    if (!txt) return false;
	    if (txt.includes(',')) return true;
	    return /s\u00e3o paulo|sp|rio de janeiro|rj|lisboa|porto|belo horizonte|curitiba|brasil|brazil|london|new york/i.test(txt);
	  };

	  let cards = Array.from(document.querySelectorAll('main ul.reusable-search__entity-result-list > li'));
	  if (cards.length === 0) {
	    cards = Array.from(document.querySelectorAll('main [data-view-name="search-entity-result-universal-template"], main [data-chameleon-result-urn]'));
	  }

	  const out = [];
	  const seen = new Set();

	  for (const card of cards) {
	    const isInsight = (el) => !!el.closest('.entity-result__insights, .reusable-search-simple-insight, .reusable-search-simple-insight__text-container');
	    let a = null;
	    const candidates = card.querySelectorAll('a[data-test-app-aware-link][href*="/in/"], a[href*="/in/"]');
	    for (const cand of candidates) { if (!isInsight(cand)) { a = cand; break; } }
	    if (!a) continue;

	    let href = a.getAttribute('href') || '';
	    try { const u = new URL(href, location.origin); href = u.origin + u.pathname; } catch {}
	    if (!href.includes('/in/')) continue;
	    if (seen.has(href)) continue;
	    seen.add(href);

	    let name = "";
	    const hidden = a.querySelector('span[aria-hidden="true"]');
	    name = getText(hidden) || getText(a);
	    name = name.replace(/^O status est\u00e1 off-line/i, '').trim();

	    let title = "";
	    for (const sel of [
	      '.entity-result__primary-subtitle',
	      '.artdeco-entity-lockup__subtitle',
	      '.linked-area div[dir="ltr"]:nth-of-type(2)',
	      '.t-14.t-black.t-normal'
	    ]) {
	      const el = card.querySelector(sel);
	      if (getText(el)) { title = getText(el); break; }
	    }

	    let location = "";
	    const locNodes = Array.from(card.querySelectorAll('div.t-14.t-normal, .reusable-search-secondary-subtitle, .entity-result__secondary-subtitle'));
	    for (const el of locNodes) {
	      const txt = getText(el);
	      if (!txt) continue;
	      if (/conex\u00e3o.*grau/i.test(txt)) continue;
	      if (looksLikeCity(txt)) { location = txt; break; }
	    }

	    let role = "";
	    let company = "";
	    const summary = card.querySelector('p.entity-result__summary--2-lines');
	    if (summary) {
	      const txt = getText(summary);
	      role = txt;
	      const mCompany = txt.match(/\b(?:em|do|da|no|na)\s+([^|–-]+)$/i);
	      if (mCompany) company = clean(mCompany[1]);
	    }
	    if (!company) {
	      const c2 = card.querySelector('.entity-result__secondary-subtitle, .artdeco-entity-lockup__caption');
	      if (getText(c2)) company = getText(c2);
	    }

	    out.push({ name, title, company, location, role, url: href });
	  }

	  return out;
	})()`

	var rows []map[string]string
	if err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(js, &rows)); err != nil {
		return nil, fmt.Errorf("falha extraindo resultados: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("nenhum resultado encontrado na página (UI mudou ou bloqueio ativo)")
	}

	now := time.Now()
	out := make([]Profile, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		u := clean(r["url"])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true

		name := clean(r["name"])
		title := clean(r["title"])
		company := clean(r["company"])
		location := clean(r["location"])
		role := clean(r["role"])

		// nome via URL quando vazio
		if name == "" {
			if n := guessNameFromURL(u); n != "" {
				name = n
			}
		}
		// não repetir título/região
		if title != "" && location != "" && strings.EqualFold(title, location) {
			location = ""
		}

		out = append(out, Profile{
			Name:        name,
			Title:       title,
			Company:     company,
			Location:    location,
			Role:        role,
			URL:         u,
			SourceQuery: sourceQuery,
			CapturedAt:  now,
		})
	}
	return out, nil
}

// =============== Convites (opcional) ===============

func sendConnectInvites(ctx context.Context, max int) int {
	sent := 0
	for sent < max {
		var clicked bool
		err := chromedp.Run(ctx,
			chromedp.EvaluateAsDevTools(`
			(() => {
				const candidates = Array.from(document.querySelectorAll('button, a')).filter(b => {
					const t = (b.innerText || '').toLowerCase();
					return t.includes('conectar') || t.includes('connect');
				});
				for (const b of candidates) {
					if (b.disabled) continue;
					b.scrollIntoView({behavior:'instant', block:'center'});
					b.click();
					return true;
				}
				return false;
			})()
			`, &clicked),
		)
		if err != nil || !clicked {
			break
		}
		_ = chromedp.Run(ctx,
			chromedp.Sleep(400*time.Millisecond),
			clickIfExists(`button[aria-label*="Enviar sem nota"], button[aria-label*="Send without a note"]`),
		)
		sent++
		randomSleep(900, 1800)
	}
	return sent
}

// =============== CSV ===============

func writeCSV(path string, items []Profile) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{"name", "title", "company", "location", "role", "url", "source_query", "captured_at"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, p := range items {
		rec := []string{
			p.Name,
			p.Title,
			p.Company,
			p.Location,
			p.Role,
			p.URL,
			p.SourceQuery,
			p.CapturedAt.Format("2006-01-02 15:04:05"),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return w.Error()
}

// =============== Helpers ===============

func clickIfExists(sel string) chromedp.ActionFunc {
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		el.scrollIntoView({behavior:'instant', block:'center'});
		el.click();
		return true;
	})()`, sel)
	return func(ctx context.Context) error {
		var ok bool
		return chromedp.EvaluateAsDevTools(js, &ok).Do(ctx)
	}
}

func randomSleep(minMs, maxMs int) {
	if maxMs < minMs {
		maxMs = minMs
	}
	d := time.Duration(minMs+rand.Intn(maxMs-minMs+1)) * time.Millisecond
	time.Sleep(d)
}

func clean(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\u00a0", " "))
}

func sanitizeQuotes(s string) string {
	repl := map[rune]rune{
		'“': '"', '”': '"', '‟': '"', '〝': '"', '〞': '"',
		'‘': '\'', '’': '\'', '‛': '\'', '‚': '\'', '‹': '\'', '›': '\'',
	}
	var b strings.Builder
	for _, r := range s {
		if rr, ok := repl[r]; ok {
			b.WriteRune(rr)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dumpPageHTML(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var html string
	if err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(`document.documentElement.outerHTML`, &html)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(html), 0o644)
}

// --------- Novos helpers para as duas regras ---------

func guessNameFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	seg := u.Path // ex: /in/daniela-mendes-659a2952
	idx := strings.Index(seg, "/in/")
	if idx >= 0 {
		seg = seg[idx+len("/in/"):]
	}
	if seg == "" {
		return ""
	}
	if j := strings.Index(seg, "/"); j >= 0 {
		seg = seg[:j]
	}
	parts := strings.Split(seg, "-")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// ignora tokens com dígitos (ids do linkedin) tipo 659a2952
		if hasDigit(p) {
			continue
		}
		kept = append(kept, toTitleCase(p))
	}
	return strings.Join(kept, " ")
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func toTitleCase(s string) string {
	s = strings.ToLower(s)
	if s == "" {
		return s
	}
	// mantém preposições comuns minúsculas quando no meio
	preps := map[string]bool{"de": true, "da": true, "do": true, "dos": true, "das": true, "e": true}
	words := strings.Fields(s)
	for i, w := range words {
		if i > 0 && preps[w] {
			continue
		}
		runes := []rune(w)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
