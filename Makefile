# Makefile de pokerFight. Un solo lugar para los comandos del equipo.
BIN := bin
PKGS := ./...

.PHONY: ayuda build test cobertura fmt vet lint limpiar mesa casino bots torneo-local

ayuda: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

build: ## Compila todos los binarios en ./bin
	go build -o $(BIN)/mesa ./cmd/mesa
	go build -o $(BIN)/casino ./cmd/casino
	go build -o $(BIN)/bot ./cmd/bot
	go build -o $(BIN)/bot-aleatorio ./bots/aleatorio
	go build -o $(BIN)/bot-conservador ./bots/conservador

test: ## Corre los tests con race detector
	go test -race $(PKGS)

cobertura: ## Genera reporte de cobertura HTML
	go test -coverprofile=coverage.out $(PKGS)
	go tool cover -html=coverage.out -o coverage.html

fmt: ## Formatea el codigo
	go fmt $(PKGS)

vet: ## Analisis estatico del toolchain
	go vet $(PKGS)

lint: fmt vet ## Atajo de fmt + vet

limpiar: ## Borra binarios y reportes
	rm -rf $(BIN) coverage.out coverage.html

mesa: build ## Levanta una mesa local en :9000
	$(BIN)/mesa -addr :9000 -jugadores 6

torneo-local: build ## Mesa + 2 bots de ejemplo, para prueba de humo
	./scripts/torneo-local.sh
