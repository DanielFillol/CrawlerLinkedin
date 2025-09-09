# GoLinkedIn • Crawler com Interface Web

Um **crawler do LinkedIn em Go**, usando **Chromedp**, com exportação para **CSV** e uma interface web amigável para não-devs.

---

## Funcionalidades

- 🔎 Busca de perfis do LinkedIn a partir de uma query.
- 📊 Captura estruturada: **Nome, Título, Empresa, Localização, Cargo, URL, Query, Data**.
- 💾 Exportação automática para CSV.
- 🌐 Interface Web (`web.go`) feita em **TailwindCSS**, para rodar via navegador.
- 📡 Logs em tempo real na UI.
- 📝 Preview dos resultados em uma tabela.
- 📬 Envio automático de convites (opcional, uso responsável).
- 🐳 Deploy simplificado com **Docker + Docker Compose**.
- 🖥️ Suporte a **Xvfb + noVNC** para rodar Chromium em containers e resolver captchas.

---

## Stack

- [Go](https://go.dev/) >= 1.22
- [Chromedp](https://github.com/chromedp/chromedp)
- [TailwindCSS](https://tailwindcss.com/)
- [Docker](https://www.docker.com/) + [Docker Compose](https://docs.docker.com/compose/)
- [Xvfb](https://www.x.org/releases/X11R7.7/doc/man/man1/Xvfb.1.xhtml) + [noVNC](https://novnc.com/)

---

## Como rodar:

### 1. Clone o repositório
```bash
git clone https://github.com/seuuser/golinkedin.git
cd golinkedin
```

### 2. Build com Docker
```bash
docker compose build
```

### 3. Suba o ambiente
```bash
docker compose up
```

### 4. Acesse os serviços
   - UI Web: http://localhost:8080
   - noVNC (para captchas): http://localhost:7900
   - Login do VNC (caso configurado):
```makefile
user: vnc
pass: vnc
```
---
## Uso
 1. Acesse a interface http://localhost:8080.
 2. Insira:
 3. Email e senha do LinkedIn.
 4. Query de busca (ex.: "Software Engineer" ou "Musa").
 5. Número de páginas a capturar.
 6. Pasta de saída (default: data).
 7. Clique em ▶️ Iniciar Crawler.
 8. Veja logs em tempo real e os resultados na tabela.
 9. Baixe o CSV gerado.
---
## Estrutura
```bash
├── main.go        # Lógica principal do crawler
├── web.go         # Interface web (UI + servidor)
├── Dockerfile     # Build da aplicação Go
├── docker-compose.yml # Orquestração com noVNC + crawler
├── data/          # Pasta de saída dos CSVs
└── README.md
```
---
## Aviso legal
Este projeto é para fins educacionais.
Automatizar interações no LinkedIn pode violar seus Termos de Uso.
Use com responsabilidade e apenas em contextos permitidos.