package casino

import (
	"path/filepath"
	"testing"
)

// casinoDePrueba crea un Casino que persiste en un archivo temporal propio
// de cada test.
func casinoDePrueba(t *testing.T) Casino {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "casino.json")
	c, err := Nuevo(ruta)
	if err != nil {
		t.Fatalf("Nuevo: %v", err)
	}
	return c
}

func TestRegistrarRechazaUsuarioDuplicado(t *testing.T) {
	c := casinoDePrueba(t)

	cuenta, err := c.Registrar("Gandy")
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}
	if cuenta.ID == "" || cuenta.Usuario != "Gandy" {
		t.Fatalf("cuenta = %+v, inesperada", cuenta)
	}

	if _, err := c.Registrar("gandy"); err == nil {
		t.Error("registrar con distinta capitalizacion deberia fallar (mismo usuario)")
	}
	if _, err := c.Registrar("  Gandy  "); err == nil {
		t.Error("registrar con espacios de mas deberia fallar (mismo usuario)")
	}
	if _, err := c.Registrar(""); err == nil {
		t.Error("registrar con usuario vacio deberia fallar")
	}
}

func TestLoginYValidarTokenRedondeaLaIdentidad(t *testing.T) {
	c := casinoDePrueba(t)
	cuenta, err := c.Registrar("ezzzzzzno")
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	token, err := c.Login("ezzzzzzno")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("Login devolvio un token vacio")
	}

	id, nombre, err := c.ValidarToken(token)
	if err != nil {
		t.Fatalf("ValidarToken: %v", err)
	}
	if id != cuenta.ID || nombre != cuenta.Usuario {
		t.Fatalf("ValidarToken = (%q, %q), se esperaba (%q, %q)", id, nombre, cuenta.ID, cuenta.Usuario)
	}

	if _, _, err := c.ValidarToken("token-que-no-existe"); err == nil {
		t.Error("ValidarToken con un token inexistente deberia fallar")
	}
	if _, err := c.Login("usuario-que-no-existe"); err == nil {
		t.Error("Login de un usuario no registrado deberia fallar")
	}
}

func TestRegistrarBotYListarBots(t *testing.T) {
	c := casinoDePrueba(t)
	cuenta, _ := c.Registrar("jhntn")

	if _, err := c.RegistrarBot("cuenta-inexistente", "mi-bot", "0.0.1"); err == nil {
		t.Error("registrar un bot en una cuenta inexistente deberia fallar")
	}

	v1, err := c.RegistrarBot(cuenta.ID, "mi-bot", "0.0.1")
	if err != nil {
		t.Fatalf("RegistrarBot v1: %v", err)
	}
	v2, err := c.RegistrarBot(cuenta.ID, "mi-bot", "0.0.2")
	if err != nil {
		t.Fatalf("RegistrarBot v2: %v", err)
	}

	bots, err := c.ListarBots(cuenta.ID)
	if err != nil {
		t.Fatalf("ListarBots: %v", err)
	}
	if len(bots) != 2 || bots[0].ID != v1.ID || bots[1].ID != v2.ID {
		t.Fatalf("ListarBots = %+v, se esperaban v1 y v2 en orden", bots)
	}
}

func TestRegistrarResultadoActualizaRankingEnOrden(t *testing.T) {
	c := casinoDePrueba(t)
	primero, _ := c.Registrar("primero")
	segundo, _ := c.Registrar("segundo")
	tercero, _ := c.Registrar("tercero")

	err := c.RegistrarResultado(ResultadoPartida{
		IDMesa: "mesa-1",
		Posiciones: []ResultadoJugador{
			{IDCuenta: primero.ID, Posicion: 1, SaldoFinal: 3000},
			{IDCuenta: segundo.ID, Posicion: 2, SaldoFinal: 900, Timeouts: 1},
			{IDCuenta: tercero.ID, Posicion: 3, SaldoFinal: 0},
		},
	})
	if err != nil {
		t.Fatalf("RegistrarResultado: %v", err)
	}

	ranking, err := c.Ranking()
	if err != nil {
		t.Fatalf("Ranking: %v", err)
	}
	if len(ranking) != 3 {
		t.Fatalf("Ranking devolvio %d cuentas, se esperaban 3", len(ranking))
	}
	if ranking[0].IDCuenta != primero.ID {
		t.Fatalf("primer puesto del ranking = %s, se esperaba %s", ranking[0].IDCuenta, primero.ID)
	}
	if ranking[0].PartidasGanadas != 1 {
		t.Errorf("PartidasGanadas del ganador = %d, se esperaba 1", ranking[0].PartidasGanadas)
	}
	// El segundo tuvo un timeout: sus estadisticas deben reflejarlo aunque
	// haya terminado adelante del tercero en fichas.
	est, err := c.Estadisticas(segundo.ID)
	if err != nil {
		t.Fatalf("Estadisticas: %v", err)
	}
	if est.Timeouts != 1 {
		t.Errorf("Timeouts del segundo puesto = %d, se esperaba 1", est.Timeouts)
	}
	if ranking[0].Puntaje <= ranking[len(ranking)-1].Puntaje {
		t.Error("el ranking deberia venir ordenado de mayor a menor puntaje")
	}
}

func TestEstadisticasDeCuentaSinPartidasEsCero(t *testing.T) {
	c := casinoDePrueba(t)
	cuenta, _ := c.Registrar("novato")

	est, err := c.Estadisticas(cuenta.ID)
	if err != nil {
		t.Fatalf("Estadisticas: %v", err)
	}
	if est.PartidasJugadas != 0 || est.Puntaje != 0 {
		t.Fatalf("Estadisticas de una cuenta nueva = %+v, se esperaban todas en cero", est)
	}

	if _, err := c.Estadisticas("cuenta-inexistente"); err == nil {
		t.Error("Estadisticas de una cuenta inexistente deberia fallar")
	}
}

// TestPersisteEntreInstancias comprueba que un segundo Casino abierto sobre
// el mismo archivo ve exactamente lo que el primero guardó: es la garantia
// de que Guardar/Cargar (internal/almacen) realmente funcionan juntos.
func TestPersisteEntreInstancias(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "casino.json")

	c1, err := Nuevo(ruta)
	if err != nil {
		t.Fatalf("Nuevo (1): %v", err)
	}
	cuenta, err := c1.Registrar("persistente")
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}
	if _, err := c1.RegistrarBot(cuenta.ID, "bot-persistente", "1.0.0"); err != nil {
		t.Fatalf("RegistrarBot: %v", err)
	}
	token, err := c1.Login("persistente")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	c2, err := Nuevo(ruta)
	if err != nil {
		t.Fatalf("Nuevo (2): %v", err)
	}

	id, nombre, err := c2.ValidarToken(token)
	if err != nil {
		t.Fatalf("ValidarToken en la segunda instancia: %v", err)
	}
	if id != cuenta.ID || nombre != cuenta.Usuario {
		t.Fatalf("ValidarToken en la segunda instancia = (%q, %q), se esperaba (%q, %q)", id, nombre, cuenta.ID, cuenta.Usuario)
	}

	bots, err := c2.ListarBots(cuenta.ID)
	if err != nil || len(bots) != 1 || bots[0].Nombre != "bot-persistente" {
		t.Fatalf("ListarBots en la segunda instancia = %+v (%v), no coincide con lo guardado", bots, err)
	}
}
