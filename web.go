package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// =================== TYPES ===================

type runPayload struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Query       string `json:"query"`
	MaxPages    int    `json:"max_pages"`
	Headless    bool   `json:"headless"`
	SendInvites bool   `json:"send_invites"`
	DumpHTML    bool   `json:"dump_html"`
	OutDir      string `json:"out_dir"`
}

type row struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	Role        string `json:"role"`
	URL         string `json:"url"`
	SourceQuery string `json:"source_query"`
	CapturedAt  string `json:"captured_at"`
}

type runResponse struct {
	Ok        bool   `json:"ok"`
	Message   string `json:"message"`
	CSVPath   string `json:"csv_path"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Results   []row  `json:"results,omitempty"`
}

type streamEvent struct {
	Type string      `json:"type"` // "log" | "done"
	Msg  string      `json:"msg,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// =================== HTML (template) ===================

var pageTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="pt-BR"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>GoLinkedIn • Crawler UI</title>
<link rel="icon" href="data:,">
<script src="https://cdn.tailwindcss.com"></script>
<script>
tailwind.config = { theme: { extend: {
  colors:{ primary:{DEFAULT:'hsl(200 98% 39%)', glow:'hsl(200 100% 50%)'}, accent:'hsl(158 64% 52%)', warning:'hsl(38 92% 50%)', success:'hsl(142 76% 36%)' },
  boxShadow:{ card:'0 2px 10px -1px rgba(18,38,63,.12)' }
}}}
</script>
<style>
.gradient-text{background:linear-gradient(135deg,hsl(200 98% 39%),hsl(200 100% 50%));-webkit-background-clip:text;background-clip:text;color:transparent}
.table-wrap{max-height:420px;overflow:auto} th,td{white-space:nowrap}
</style>
</head>
<body class="bg-gray-50 text-gray-900">
<div class="max-w-7xl mx-auto px-4 py-8">
  <header class="text-center mb-8">
    <h1 class="text-3xl font-bold gradient-text">GoLinkedIn</h1>
  </header>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
    <!-- Config -->
    <div class="lg:col-span-1">
      <div class="bg-white border rounded-xl shadow-card p-5">
        <h2 class="text-lg font-semibold mb-4 flex items-center">
          <svg class="w-5 h-5 mr-2 text-primary" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
          Credenciais
        </h2>
        <div class="space-y-3">
          <label class="block">
            <span class="text-sm">Email</span>
            <input id="email" type="email" class="mt-1 w-full border rounded-md px-3 py-2 focus:ring-2 focus:ring-primary" placeholder="seu.email@exemplo.com">
          </label>
          <label class="block">
            <span class="text-sm">Senha</span>
            <input id="password" type="password" class="mt-1 w-full border rounded-md px-3 py-2 focus:ring-2 focus:ring-primary" placeholder="Sua senha">
          </label>
        </div>
        <hr class="my-4">
        <h2 class="text-lg font-semibold mb-3 flex items-center">
          <svg class="w-5 h-5 mr-2 text-primary" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          Busca
        </h2>
        <div class="space-y-3">
          <label class="block">
            <span class="text-sm">Query</span>
            <input id="query" type="text" class="mt-1 w-full border rounded-md px-3 py-2 focus:ring-2 focus:ring-primary" placeholder='Ex.: "Boticário"'>
          </label>
          <div class="grid grid-cols-2 gap-3">
            <label class="block">
              <span class="text-sm">Páginas</span>
              <input id="max-pages" type="number" min="1" value="2" class="mt-1 w-full border rounded-md px-3 py-2 focus:ring-2 focus:ring-primary">
            </label>
            <label class="block">
              <span class="text-sm">Out dir</span>
              <input id="out-dir" type="text" value="data" class="mt-1 w-full border rounded-md px-3 py-2 focus:ring-2 focus:ring-primary">
            </label>
          </div>
          <div class="grid grid-cols-3 gap-3 text-sm">
            <label class="inline-flex items-center"><input id="headless" type="checkbox" class="mr-2">Headless</label>
            <label class="inline-flex items-center"><input id="send-invites" type="checkbox" class="mr-2">Convites</label>
            <label class="inline-flex items-center"><input id="dump-html" type="checkbox" class="mr-2">Dump HTML</label>
          </div>
        </div>

        <button id="runBtn" class="mt-5 w-full py-2 rounded-lg bg-primary text-white font-medium hover:opacity-90">
          ▶️ Iniciar Crawler
        </button>
      </div>
    </div>

    <!-- Execução -->
    <div class="lg:col-span-2 space-y-6">
      <div class="bg-white border rounded-xl shadow-card p-5">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold flex items-center">
            <svg class="w-5 h-5 mr-2 text-primary" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
            Execução & Logs
          </h2>
          <span id="statusBadge" class="text-xs px-2 py-1 rounded-full bg-gray-100 text-gray-600">Aguardando</span>
        </div>

        <div class="space-y-3">
          <div>
            <div class="flex justify-between text-xs mb-1">
              <span>Progresso</span><span id="progressLabel">0%</span>
            </div>
            <div class="w-full bg-gray-200 rounded-full h-2">
              <div id="progressBar" class="h-2 rounded-full bg-primary" style="width:0%"></div>
            </div>
          </div>

          <div id="logBox" class="border rounded-md bg-gray-50 h-64 overflow-y-auto p-3 text-xs font-mono text-gray-800">
            Aguardando logs…
          </div>

          <div class="flex items-center justify-between">
            <div class="text-xs text-gray-500">
              <span id="startedAt">—</span> • <span id="endedAt">—</span>
            </div>
            <a id="csvLink" href="#" class="hidden text-sm px-3 py-1 rounded-md border hover:bg-gray-100">Baixar CSV</a>
          </div>
        </div>
      </div>

      <!-- Resultados -->
      <div class="bg-white border rounded-xl shadow-card p-5">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold flex items-center">
            <svg class="w-5 h-5 mr-2 text-primary" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="9" ry="3"></ellipse><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path></svg>
            Resultados
          </h2>
          <span id="resultsBadge" class="text-xs px-2 py-1 rounded-full bg-gray-100 text-gray-600">0 itens</span>
        </div>

        <div id="noResults" class="text-sm text-gray-500">Nenhum resultado ainda. Execute o crawler.</div>

        <div id="resultsWrap" class="table-wrap hidden border rounded-md">
          <table class="min-w-full divide-y divide-gray-200 text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Nome</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Título</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Região</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Empresa</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Cargo/Resumo</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">URL</th>
                <th class="px-3 py-2 text-left font-medium text-gray-700">Capturado</th>
              </tr>
            </thead>
            <tbody id="resultsBody" class="divide-y divide-gray-200"></tbody>
          </table>
        </div>
      </div>
    </div>
  </div>

  <footer class="text-center text-xs text-gray-500 mt-8"></footer>
</div>

<script>
(function () {
  const runBtn = document.getElementById('runBtn');
  const logBox = document.getElementById('logBox');
  const statusBadge = document.getElementById('statusBadge');
  const progressBar = document.getElementById('progressBar');
  const progressLabel = document.getElementById('progressLabel');
  const csvLink = document.getElementById('csvLink');
  const startedAt = document.getElementById('startedAt');
  const endedAt = document.getElementById('endedAt');
  const resultsBadge = document.getElementById('resultsBadge');
  const noResults = document.getElementById('noResults');
  const resultsWrap = document.getElementById('resultsWrap');
  const resultsBody = document.getElementById('resultsBody');

  function appendLog(line) {
    if (logBox.textContent.trim() === 'Aguardando logs…') logBox.textContent = '';
    const p = document.createElement('div');
    p.textContent = line;
    logBox.appendChild(p);
    logBox.scrollTop = logBox.scrollHeight;
  }

  function setStatus(txt, color) {
    statusBadge.textContent = txt;
    statusBadge.className = 'text-xs px-2 py-1 rounded-full ' + color;
  }

  function fakeProgressStart() {
    let v = 0;
    const id = setInterval(() => {
      v = Math.min(95, v + Math.random()*8);
      progressBar.style.width = v.toFixed(0) + '%';
      progressLabel.textContent = v.toFixed(0) + '%';
      if (v >= 95) clearInterval(id);
    }, 600);
    return () => { progressBar.style.width = '100%'; progressLabel.textContent = '100%'; clearInterval(id); }
  }

  function renderResults(rows) {
    resultsBody.innerHTML = '';
    if (!rows || !rows.length) {
      noResults.classList.remove('hidden');
      resultsWrap.classList.add('hidden');
      resultsBadge.textContent = '0 itens';
      return;
    }
    noResults.classList.add('hidden');
    resultsWrap.classList.remove('hidden');
    resultsBadge.textContent = rows.length + ' itens';

    for (const r of rows) {
      const tr = document.createElement('tr');
      tr.innerHTML =
        '<td class="px-3 py-2">'+escapeHTML(r.name||'')+'</td>'+
        '<td class="px-3 py-2">'+escapeHTML(r.title||'')+'</td>'+
        '<td class="px-3 py-2">'+escapeHTML(r.location||'')+'</td>'+
        '<td class="px-3 py-2">'+escapeHTML(r.company||'')+'</td>'+
        '<td class="px-3 py-2">'+escapeHTML(r.role||'')+'</td>'+
        '<td class="px-3 py-2"><a href="'+encodeURI(r.url||'#')+'" target="_blank" class="text-primary underline">abrir</a></td>'+
        '<td class="px-3 py-2">'+escapeHTML(r.captured_at||'')+'</td>';
      resultsBody.appendChild(tr);
    }
  }

  function escapeHTML(s){return (s||'').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]));}

  runBtn.addEventListener('click', async () => {
    const payload = {
      email:       document.getElementById('email').value.trim(),
      password:    document.getElementById('password').value,
      query:       document.getElementById('query').value.trim(),
      max_pages:   parseInt(document.getElementById('max-pages').value || '1', 10),
      headless:    document.getElementById('headless').checked,
      send_invites:document.getElementById('send-invites').checked,
      dump_html:   document.getElementById('dump-html').checked,
      out_dir:     document.getElementById('out-dir').value.trim() || 'data'
    };

    csvLink.classList.add('hidden');
    startedAt.textContent = '—';
    endedAt.textContent = '—';
    progressBar.style.width = '0%';
    progressLabel.textContent = '0%';
    logBox.textContent = 'Aguardando logs…';
    renderResults([]);

    setStatus('Iniciando', 'bg-primary/10 text-primary');

    const stopProgress = fakeProgressStart();

    const resp = await fetch('/run', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(payload)
    });

    if (!resp.ok) {
      setStatus('Erro HTTP', 'bg-red-100 text-red-700');
      appendLog('Erro: ' + resp.status + ' ' + resp.statusText);
      stopProgress();
      return;
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let finalData = null;

    startedAt.textContent = new Date().toLocaleTimeString();

    while (true) {
      const {value, done} = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, {stream:true});
      const parts = buffer.split('\n');
      buffer = parts.pop();

      for (const line of parts) {
        if (!line) continue;
        try {
          const ev = JSON.parse(line);
          if (ev.type === 'log') {
            appendLog(ev.msg);
          } else if (ev.type === 'done') {
            finalData = ev.data;
          }
        } catch {
          appendLog(line);
        }
      }
    }

    stopProgress();
    endedAt.textContent = new Date().toLocaleTimeString();

    if (finalData) {
      if (finalData.csv_path) {
        csvLink.href = '/download?path=' + encodeURIComponent(finalData.csv_path);
        csvLink.classList.remove('hidden');
      }
      if (finalData.results) {
        renderResults(finalData.results);
      }
      if (finalData.ok) {
        setStatus('Concluído', 'bg-green-100 text-green-700');
        appendLog('✅ Finalizado com sucesso.');
      } else {
        setStatus('Concluído (com avisos)', 'bg-yellow-100 text-yellow-700');
        appendLog('⚠️ Execução terminou com avisos/erro. Verifique logs e resultados exibidos.');
      }
    } else {
      setStatus('Falhou', 'bg-red-100 text-red-700');
      appendLog('❌ Erro ao executar. Veja logs acima.');
    }
  });
})();
</script>
</body></html>`))

// =================== SERVER ===================

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/run", handleRun)
	mux.HandleFunc("/download", handleDownload)

	addr := ":8080"
	log.Printf("Servidor rodando em http://localhost%v ...", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTmpl.Execute(w, nil)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path vazio", http.StatusBadRequest)
		return
	}
	clean := filepath.Clean(path)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(clean))
	http.ServeFile(w, r, clean)
}

func writeEvent(w http.ResponseWriter, ev streamEvent) {
	b, _ := json.Marshal(ev)
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")

	var p runPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("payload inválido: %v", err)})
		writeEvent(w, streamEvent{Type: "done", Data: runResponse{Ok: false, Message: "payload inválido"}})
		return
	}

	if strings.TrimSpace(p.Email) == "" || strings.TrimSpace(p.Password) == "" || strings.TrimSpace(p.Query) == "" {
		writeEvent(w, streamEvent{Type: "log", Msg: "Preencha email, senha e query."})
		writeEvent(w, streamEvent{Type: "done", Data: runResponse{Ok: false, Message: "campos obrigatórios ausentes"}})
		return
	}

	start := time.Now()
	writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("▶️ Iniciando crawler para %q ...", p.Query)})

	// ============ Runner detection ============
	// Se CRAWLER_BIN estiver setado e existir, executa diretamente o binário.
	// Senão, fallback para "go run main.go" (uso fora do Docker).
	crawlerBin := os.Getenv("CRAWLER_BIN")
	useBin := false
	if crawlerBin != "" {
		if st, err := os.Stat(crawlerBin); err == nil && !st.IsDir() {
			useBin = true
		} else {
			writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("Aviso: CRAWLER_BIN=%q não encontrado. Usando 'go run main.go'.", crawlerBin)})
		}
	}

	var cmd *exec.Cmd
	if useBin {
		// executa /app/crawler
		args := []string{
			"--email", p.Email,
			"--password", p.Password,
			"--query", p.Query,
			"--max-pages", fmt.Sprint(p.MaxPages),
			"--out-dir", p.OutDir,
		}
		if !p.Headless {
			args = append(args, "--headless=false")
		}
		if p.SendInvites {
			args = append(args, "--send-invites")
		}
		if p.DumpHTML {
			args = append(args, "--dump-html")
		}
		writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("Runner: %s %s", crawlerBin, strings.Join(maskArgs(args), " "))})
		cmd = exec.CommandContext(ctx, crawlerBin, args...)
	} else {
		// fallback: go run main.go
		args := []string{"run", "main.go",
			"--email", p.Email,
			"--password", p.Password,
			"--query", p.Query,
			"--max-pages", fmt.Sprint(p.MaxPages),
			"--out-dir", p.OutDir,
		}
		if !p.Headless {
			args = append(args, "--headless=false")
		}
		if p.SendInvites {
			args = append(args, "--send-invites")
		}
		if p.DumpHTML {
			args = append(args, "--dump-html")
		}
		writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("Runner: go %s", strings.Join(maskArgs(args), " "))})
		cmd = exec.CommandContext(ctx, "go", args...)
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("Erro ao iniciar: %v", err)})
		writeEvent(w, streamEvent{Type: "done", Data: runResponse{Ok: false, Message: err.Error(), StartedAt: start.Format(time.RFC3339)}})
		return
	}

	outReader := bufio.NewScanner(stdout)
	errReader := bufio.NewScanner(stderr)

	doneCh := make(chan struct{})
	go func() {
		for outReader.Scan() {
			writeEvent(w, streamEvent{Type: "log", Msg: outReader.Text()})
		}
		doneCh <- struct{}{}
	}()
	go func() {
		for errReader.Scan() {
			writeEvent(w, streamEvent{Type: "log", Msg: errReader.Text()})
		}
		doneCh <- struct{}{}
	}()

	waitErr := cmd.Wait()
	<-doneCh
	<-doneCh

	csvPath := findLatestCSV(p.OutDir)
	var preview []row
	if csvPath != "" {
		if rows, err := readCSVLimited(csvPath, 200); err == nil {
			preview = rows
		} else {
			writeEvent(w, streamEvent{Type: "log", Msg: fmt.Sprintf("Aviso: não foi possível pré-ler CSV: %v", err)})
		}
	}

	ok := waitErr == nil
	msg := "ok"
	if waitErr != nil {
		msg = waitErr.Error()
	}

	writeEvent(w, streamEvent{
		Type: "done",
		Data: runResponse{
			Ok:        ok,
			Message:   msg,
			CSVPath:   csvPath,
			StartedAt: start.Format(time.RFC3339),
			EndedAt:   time.Now().Format(time.RFC3339),
			Results:   preview,
		},
	})
}

func maskArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "--password" {
			out[i+1] = "********"
		}
	}
	return out
}

func findLatestCSV(outDir string) string {
	entries, err := filepath.Glob(filepath.Join(outDir, "linkedin_*.csv"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	var best string
	for _, e := range entries {
		if best == "" || e > best {
			best = e
		}
	}
	return best
}

func readCSVLimited(path string, limit int) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.ReuseRecord = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	idx := map[string]int{}
	for i, h := range records[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var out []row
	for i := 1; i < len(records) && (limit <= 0 || len(out) < limit); i++ {
		rec := records[i]
		get := func(k string) string {
			j, ok := idx[k]
			if !ok || j >= len(rec) {
				return ""
			}
			return rec[j]
		}
		out = append(out, row{
			Name:        get("name"),
			Title:       get("title"),
			Company:     get("company"),
			Location:    get("location"),
			Role:        get("role"),
			URL:         get("url"),
			SourceQuery: get("source_query"),
			CapturedAt:  get("captured_at"),
		})
	}
	return out, nil
}

// util (não usado agora)
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func runShell(ctx context.Context, command string) (string, error) {
	var b bytes.Buffer
	var e bytes.Buffer
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Stdout = &b
	cmd.Stderr = &e
	err := cmd.Run()
	out := b.String() + e.String()
	if err != nil {
		return out, fmt.Errorf("cmd err: %w", err)
	}
	return out, nil
}
