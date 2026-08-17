package almacen

import (
	"path/filepath"
	"testing"
)

type registro struct {
	Nombre string `json:"nombre"`
	Valor  int    `json:"valor"`
}

func TestCargarSobreArchivoInexistenteNoFalla(t *testing.T) {
	a := NuevoJSON(filepath.Join(t.TempDir(), "no-existe.json"))
	var r registro
	if err := a.Cargar(&r); err != nil {
		t.Fatalf("Cargar sobre un archivo que no existe deberia devolver nil, dio: %v", err)
	}
	if r != (registro{}) {
		t.Fatalf("Cargar toco el valor aunque el archivo no existia: %+v", r)
	}
}

func TestGuardarYCargarRedondeaElValor(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "sub", "datos.json")
	a := NuevoJSON(ruta)

	original := registro{Nombre: "mesa-1", Valor: 42}
	if err := a.Guardar(&original); err != nil {
		t.Fatalf("Guardar: %v", err)
	}

	var leido registro
	if err := a.Cargar(&leido); err != nil {
		t.Fatalf("Cargar: %v", err)
	}
	if leido != original {
		t.Fatalf("Cargar = %+v, se esperaba %+v", leido, original)
	}
}

func TestGuardarSobreescribeElValorAnterior(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "datos.json")
	a := NuevoJSON(ruta)

	if err := a.Guardar(&registro{Nombre: "v1", Valor: 1}); err != nil {
		t.Fatalf("Guardar v1: %v", err)
	}
	if err := a.Guardar(&registro{Nombre: "v2", Valor: 2}); err != nil {
		t.Fatalf("Guardar v2: %v", err)
	}

	var leido registro
	if err := a.Cargar(&leido); err != nil {
		t.Fatalf("Cargar: %v", err)
	}
	if leido.Nombre != "v2" || leido.Valor != 2 {
		t.Fatalf("Cargar = %+v, se esperaba v2 (la ultima version guardada)", leido)
	}
}
