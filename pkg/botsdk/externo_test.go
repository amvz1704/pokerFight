package botsdk_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestUnBotDeOtroModuloCompila compila, desde un módulo Go aparte, los bots
// que le mostramos a los participantes: los ejemplos de README.md y
// docs/torneo.md, y los bots de bots/ (que la documentación presenta como
// punto de partida para copiar).
//
// Hace falta compilarlos desde afuera porque dentro de este módulo todo
// compila siempre: la restricción de Go sobre los paquetes internal/ solo
// aparece cuando quien importa vive en otro módulo, que es justo el caso de
// cualquier participante. Un ejemplo que importe internal/protocolo pasa todos
// los demás tests y no le sirve a nadie.
func TestUnBotDeOtroModuloCompila(t *testing.T) {
	if testing.Short() {
		t.Skip("compila módulos con el toolchain de Go; se salta con -short")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no está el comando go en el PATH")
	}

	raiz, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	fuentes := map[string]string{}
	for _, doc := range []string{"README.md", "docs/torneo.md"} {
		for i, codigo := range botsDeEjemplo(t, filepath.Join(raiz, filepath.FromSlash(doc))) {
			fuentes[fmt.Sprintf("%s#%d", doc, i+1)] = codigo
		}
	}
	for _, bot := range []string{"base", "aleatorio", "conservador"} {
		codigo, err := os.ReadFile(filepath.Join(raiz, "bots", bot, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		fuentes["bots/"+bot] = string(codigo)
	}
	if len(fuentes) < 5 {
		t.Fatalf("se encontraron %d fuentes, se esperaban al menos 5 (¿cambió el formato de los ejemplos?)", len(fuentes))
	}

	for nombre, codigo := range fuentes {
		t.Run(nombre, func(t *testing.T) {
			dir := t.TempDir()
			goMod := "module ejemplo.com/mibot\n\ngo 1.26.5\n\n" +
				"require github.com/amvz1704/pokerFight v0.0.0\n\n" +
				"replace github.com/amvz1704/pokerFight => " + filepath.ToSlash(raiz) + "\n"
			escribir(t, filepath.Join(dir, "go.mod"), goMod)
			escribir(t, filepath.Join(dir, "main.go"), codigo)

			cmd := exec.Command(goBin, "build", "-o", os.DevNull, ".")
			cmd.Dir = dir
			// GOWORK=off: un go.work del usuario no debe meter el módulo del
			// proyecto como si fuera local. GOFLAGS limpio por lo mismo.
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off")
			if salida, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("el bot no compila fuera del módulo del proyecto:\n%s", salida)
			}
		})
	}
}

// botsDeEjemplo devuelve los bloques ```go de un documento que son programas
// completos de bot (package main + botsdk).
func botsDeEjemplo(t *testing.T, ruta string) []string {
	t.Helper()
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	texto := strings.ReplaceAll(string(contenido), "\r\n", "\n")
	bloques := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllStringSubmatch(texto, -1)

	var bots []string
	for _, b := range bloques {
		if strings.Contains(b[1], "package main") && strings.Contains(b[1], "pkg/botsdk") {
			bots = append(bots, b[1])
		}
	}
	return bots
}

func escribir(t *testing.T, ruta, contenido string) {
	t.Helper()
	if err := os.WriteFile(ruta, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}
