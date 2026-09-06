// Package almacen persiste datos en disco. Es deliberadamente genérico: no
// conoce el modelo de dominio de quien lo usa (hoy, Casino). La idea es que
// el día que se cambie a SQLite (ver README, "Estructura del repositorio"),
// el paquete casino no tenga que cambiar cómo lo usa, solo qué implementación
// de este paquete recibe.
package almacen

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// JSON persiste un único valor serializable en un archivo JSON. Es segura
// para uso concurrente.
type JSON struct {
	ruta string
	mu   sync.Mutex
}

// NuevoJSON construye un almacén sobre el archivo en ruta. No lo crea ni lo
// toca todavía: eso lo hace la primera llamada a Guardar. Cargar sobre un
// archivo que no existe no es un error (primer uso).
func NuevoJSON(ruta string) *JSON {
	return &JSON{ruta: ruta}
}

// Cargar deserializa el archivo en v. Si el archivo no existe o está vacío,
// deja v tal cual está y no devuelve error.
func (a *JSON) Cargar(v any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	datos, err := os.ReadFile(a.ruta)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(datos) == 0 {
		return nil
	}
	return json.Unmarshal(datos, v)
}

// Guardar serializa v y lo escribe de forma atómica (archivo temporal +
// rename) para no dejar el archivo a medio escribir si el proceso se cae en
// el medio.
func (a *JSON) Guardar(v any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if dir := filepath.Dir(a.ruta); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	datos, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	tmp := a.ruta + ".tmp"
	if err := os.WriteFile(tmp, datos, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.ruta)
}
