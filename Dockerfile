# ========= BUILDER =========
FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Cache de deps
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Código
COPY . .

# Compila binários (web + crawler)
# (CGO desabilitado para binários portáveis)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/web ./web.go && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/crawler ./main.go

# ========= RUNTIME =========
FROM debian:bookworm-slim

# Necessários pro Chromium headless
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      chromium \
      fonts-liberation \
      ca-certificates \
      tzdata \
      tini \
    && rm -rf /var/lib/apt/lists/*

# Usuário não-root opcional (melhor prática)
RUN useradd -ms /bin/bash appuser
USER appuser

WORKDIR /app

# Copia binários
COPY --from=builder /out/web /app/web
COPY --from=builder /out/crawler /app/crawler

# Variáveis para o seu código
# CHROME_PATH para o chromedp encontrar o browser
# CRAWLER_BIN para o web.go chamar o binário do crawler (em vez de go run)
ENV CHROME_PATH=/usr/bin/chromium \
    CRAWLER_BIN=/app/crawler \
    TZ=America/Sao_Paulo

# Pasta de saída (vai ser mapeada pelo compose)
VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/usr/bin/tini","--"]
CMD ["/app/web"]
