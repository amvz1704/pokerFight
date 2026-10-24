# Makefile de pokerFight. Un solo lugar para los comandos del equipo.
BIN := bin
PKGS := ./...

.PHONY: ayuda build test cobertura fmt vet lint limpiar mesa torneo torneo-local campeonato campeonato-demo

ayuda: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

build: ## Compila todos los binarios en ./bin
	go build -o $(BIN)/ ./cmd/... ./bots/...

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

limpiar: ## Borra binarios, reportes y resultados de torneos
	rm -rf $(BIN) coverage.out coverage.html resultados campeonato-demo

mesa: build ## Levanta una mesa suelta en :9000, para conectarle bots a mano
	$(BIN)/mesa -addr :9000 -jugadores 6

torneo: build ## Corre el torneo definido en torneo.json
	$(BIN)/arena correr -config torneo.json

torneo-local: build ## Prueba de humo: round-robin con los bots de ejemplo (Go, Python y JS)
	$(BIN)/arena correr -config torneo.json

campeonato: build ## Levanta el campeonato de las bases (campeonato.json + inscritos.json) con su panel web
	$(BIN)/campeonato servir

# El demo vive en su propio directorio: con -forzar, si escribiera en la raiz pisaria
# el campeonato.json y el inscritos.json (con las credenciales) de un torneo real.
campeonato-demo: build ## Demo del campeonato: 24 bots de ejemplo y el panel web, todo en esta maquina
	mkdir -p campeonato-demo
	$(BIN)/campeonato init -demo 24 -forzar -config campeonato-demo/campeonato.json -inscritos campeonato-demo/inscritos.json
	$(BIN)/campeonato servir -config campeonato-demo/campeonato.json -inscritos campeonato-demo/inscritos.json
