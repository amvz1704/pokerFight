package arena

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// configDePrueba arma un torneo minimo con participantes sin comando (bots
// remotos), que es lo que hace falta para probar emparejamientos y
// clasificacion sin lanzar procesos.
func configDePrueba(t *testing.T, ids ...string) ConfigTorneo {
	t.Helper()

	cfg := ConfigTorneo{
		Nombre:                 "prueba",
		ManosPorEnfrentamiento: 10,
		IdaYVuelta:             true,
		Semilla:                42,
	}
	for _, id := range ids {
		cfg.Participantes = append(cfg.Participantes, Participante{ID: id, Nombre: id})
	}
	if err := cfg.Validar(); err != nil {
		t.Fatalf("Validar: %v", err)
	}
	return cfg
}

// --- Configuracion ---------------------------------------------------------

func TestValidarRellenaValoresPorDefecto(t *testing.T) {
	cfg := ConfigTorneo{Participantes: []Participante{{ID: "a"}, {ID: "b"}}}
	if err := cfg.Validar(); err != nil {
		t.Fatalf("Validar: %v", err)
	}

	if cfg.Formato != NombreRoundRobin {
		t.Errorf("formato por defecto = %q, se esperaba %q", cfg.Formato, NombreRoundRobin)
	}
	if cfg.Puntuacion != NombrePuntuacionNeto {
		t.Errorf("puntuacion por defecto = %q, se esperaba %q", cfg.Puntuacion, NombrePuntuacionNeto)
	}
	if cfg.Mesa.CiegaGrande != 2*cfg.Mesa.CiegaChica {
		t.Errorf("ciegas por defecto = %d/%d, la grande deberia ser el doble", cfg.Mesa.CiegaChica, cfg.Mesa.CiegaGrande)
	}
	if cfg.Mesa.Stack == 0 || cfg.Mesa.TimeoutMs == 0 {
		t.Errorf("faltaron valores por defecto de mesa: %+v", cfg.Mesa)
	}
}

func TestValidarRechazaConfiguracionesRotas(t *testing.T) {
	casos := []struct {
		nombre string
		cfg    ConfigTorneo
	}{
		{"un solo participante", ConfigTorneo{Participantes: []Participante{{ID: "a"}}}},
		{"id repetido", ConfigTorneo{Participantes: []Participante{{ID: "a"}, {ID: "a"}}}},
		{"participante sin id", ConfigTorneo{Participantes: []Participante{{ID: "a"}, {Nombre: "sin id"}}}},
		{"token compartido", ConfigTorneo{Participantes: []Participante{
			{ID: "a", Token: "mismo"}, {ID: "b", Token: "mismo"},
		}}},
		{"ciega grande menor que la chica", ConfigTorneo{
			Participantes: []Participante{{ID: "a"}, {ID: "b"}},
			Mesa:          ConfigMesa{CiegaChica: 100, CiegaGrande: 50},
		}},
		{"stack que no paga una ciega", ConfigTorneo{
			Participantes: []Participante{{ID: "a"}, {ID: "b"}},
			Mesa:          ConfigMesa{CiegaChica: 10, CiegaGrande: 20, Stack: 5},
		}},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			cfg := caso.cfg
			if err := cfg.Validar(); err == nil {
				t.Fatal("se esperaba un error de validacion")
			}
		})
	}
}

// TestCargarConfigRechazaCamposDesconocidos: un campo mal escrito en el JSON
// se ignoraria en silencio y el torneo correria con otra configuracion que la
// que su autor cree.
func TestCargarConfigRechazaCamposDesconocidos(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "torneo.json")
	contenido := `{"nombre":"x","manos_por_enfrentamento":100,
		"participantes":[{"id":"a"},{"id":"b"}]}`
	if err := os.WriteFile(ruta, []byte(contenido), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := CargarConfig(ruta); err == nil {
		t.Fatal("un campo con el nombre mal escrito deberia hacer fallar la carga")
	}
}

func TestConfigEjemploEsValida(t *testing.T) {
	cfg := ConfigEjemplo()
	if err := cfg.Validar(); err != nil {
		t.Fatalf("la config de ejemplo que escribe `arena init` no valida: %v", err)
	}
	if _, err := BuscarFormato(cfg.Formato, cfg); err != nil {
		t.Fatalf("la config de ejemplo usa un formato inexistente: %v", err)
	}
	if _, err := BuscarPuntuador(cfg.Puntuacion, cfg); err != nil {
		t.Fatalf("la config de ejemplo usa una puntuacion inexistente: %v", err)
	}
}

func TestGuardarYCargarConfigDaLoMismo(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "torneo.json")
	original := ConfigEjemplo()
	if err := original.Guardar(ruta); err != nil {
		t.Fatalf("Guardar: %v", err)
	}

	recargada, err := CargarConfig(ruta)
	if err != nil {
		t.Fatalf("CargarConfig: %v", err)
	}
	if recargada.Nombre != original.Nombre ||
		recargada.Semilla != original.Semilla ||
		recargada.ManosPorEnfrentamiento != original.ManosPorEnfrentamiento ||
		len(recargada.Participantes) != len(original.Participantes) {
		t.Fatalf("la config no sobrevivio la ida y vuelta:\n%+v\n%+v", original, recargada)
	}
}

// --- Formato round-robin ---------------------------------------------------

func TestRoundRobinEmparejaATodosContraTodos(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "c", "d")
	cfg.IdaYVuelta = false

	enfrentamientos, err := RoundRobin{JugadoresPorMesa: 2}.Emparejamientos(cfg)
	if err != nil {
		t.Fatalf("Emparejamientos: %v", err)
	}

	// C(4,2) = 6 pares.
	if len(enfrentamientos) != 6 {
		t.Fatalf("se armaron %d enfrentamientos, se esperaban 6", len(enfrentamientos))
	}

	vistos := map[string]bool{}
	for _, e := range enfrentamientos {
		if len(e.Participantes) != 2 {
			t.Fatalf("%s tiene %d jugadores, se esperaban 2", e.ID, len(e.Participantes))
		}
		clave := e.Participantes[0] + "|" + e.Participantes[1]
		if vistos[clave] {
			t.Fatalf("el emparejamiento %s se repitio", clave)
		}
		vistos[clave] = true
	}
}

// TestIdaYVuelaCompartenSemillaYRotanSillas es la propiedad que hace justo al
// torneo: los dos bots de un par juegan las mismas cartas en las dos
// posiciones.
func TestIdaYVueltaCompartenSemillaYRotanSillas(t *testing.T) {
	cfg := configDePrueba(t, "a", "b")

	enfrentamientos, err := RoundRobin{JugadoresPorMesa: 2}.Emparejamientos(cfg)
	if err != nil {
		t.Fatalf("Emparejamientos: %v", err)
	}
	if len(enfrentamientos) != 2 {
		t.Fatalf("con ida y vuelta se esperaban 2 enfrentamientos, hay %d", len(enfrentamientos))
	}

	ida, vuelta := enfrentamientos[0], enfrentamientos[1]
	if ida.Semilla != vuelta.Semilla {
		t.Fatalf("la ida (%d) y la vuelta (%d) deberian compartir semilla", ida.Semilla, vuelta.Semilla)
	}
	if ida.ID == vuelta.ID {
		t.Fatal("la ida y la vuelta deberian tener IDs distintos")
	}
	if ida.Participantes[0] == vuelta.Participantes[0] {
		t.Fatalf("las sillas no se intercambiaron: %v y %v", ida.Participantes, vuelta.Participantes)
	}
}

// TestSemillasNoDependenDelRestoDelTorneo: agregar un participante nuevo no
// tiene que cambiar las cartas de los emparejamientos que ya existian. Si
// dependieran de un contador, sumar a alguien al archivo de configuracion
// haria irreproducible todo lo anterior.
func TestSemillasNoDependenDelRestoDelTorneo(t *testing.T) {
	dos := configDePrueba(t, "a", "b")
	tres := configDePrueba(t, "a", "b", "c")

	semillaDeAB := func(cfg ConfigTorneo) uint64 {
		enfrentamientos, err := RoundRobin{JugadoresPorMesa: 2}.Emparejamientos(cfg)
		if err != nil {
			t.Fatalf("Emparejamientos: %v", err)
		}
		for _, e := range enfrentamientos {
			if len(e.Participantes) == 2 &&
				((e.Participantes[0] == "a" && e.Participantes[1] == "b") ||
					(e.Participantes[0] == "b" && e.Participantes[1] == "a")) {
				return e.Semilla
			}
		}
		t.Fatal("no aparecio el emparejamiento a vs b")
		return 0
	}

	if semillaDeAB(dos) != semillaDeAB(tres) {
		t.Fatal("la semilla de a vs b cambio al agregar un tercer participante")
	}
}

func TestRoundRobinNecesitaSuficientesParticipantes(t *testing.T) {
	cfg := configDePrueba(t, "a", "b")
	if _, err := (RoundRobin{JugadoresPorMesa: 6}).Emparejamientos(cfg); err == nil {
		t.Fatal("mesas de 6 con 2 participantes deberia fallar")
	}
}

func TestMesaUnicaSientaATodosJuntos(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "c")

	enfrentamientos, err := MesaUnica{}.Emparejamientos(cfg)
	if err != nil {
		t.Fatalf("Emparejamientos: %v", err)
	}
	if len(enfrentamientos) != 1 {
		t.Fatalf("mesa-unica armo %d enfrentamientos, se esperaba 1", len(enfrentamientos))
	}
	if len(enfrentamientos[0].Participantes) != 3 {
		t.Fatalf("se sentaron %d jugadores, se esperaban 3", len(enfrentamientos[0].Participantes))
	}
}

func TestMesaUnicaRechazaMasDeOchoJugadores(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "c", "d", "e", "f", "g", "h", "i")
	if _, err := (MesaUnica{}).Emparejamientos(cfg); err == nil {
		t.Fatal("mesa-unica con 9 jugadores deberia fallar")
	}
}

// --- Registro de formatos y puntuaciones -----------------------------------

func TestRegistroDevuelveLoRegistrado(t *testing.T) {
	cfg := configDePrueba(t, "a", "b")

	for _, nombre := range FormatosDisponibles() {
		f, err := BuscarFormato(nombre, cfg)
		if err != nil {
			t.Fatalf("BuscarFormato(%q): %v", nombre, err)
		}
		if f.Nombre() != nombre {
			t.Errorf("el formato registrado como %q se llama %q", nombre, f.Nombre())
		}
	}
	for _, nombre := range PuntuadoresDisponibles() {
		p, err := BuscarPuntuador(nombre, cfg)
		if err != nil {
			t.Fatalf("BuscarPuntuador(%q): %v", nombre, err)
		}
		if p.Nombre() != nombre {
			t.Errorf("el puntuador registrado como %q se llama %q", nombre, p.Nombre())
		}
	}
}

func TestBuscarFormatoInexistenteFalla(t *testing.T) {
	cfg := configDePrueba(t, "a", "b")
	if _, err := BuscarFormato("no-existe", cfg); err == nil {
		t.Fatal("buscar un formato inexistente deberia fallar")
	}
	if _, err := BuscarPuntuador("no-existe", cfg); err == nil {
		t.Fatal("buscar una puntuacion inexistente deberia fallar")
	}
}

// --- Clasificacion ---------------------------------------------------------

func resultadoDe(id string, a, b string, netoA, netoB int64) ResultadoEnfrentamiento {
	posicionA, posicionB := 1, 2
	if netoB > netoA {
		posicionA, posicionB = 2, 1
	}
	return ResultadoEnfrentamiento{
		Enfrentamiento: Enfrentamiento{ID: id, Participantes: []string{a, b}},
		Puestos: []Puesto{
			{IDParticipante: a, Nombre: a, Posicion: posicionA, Neto: netoA, ManosJugadas: 10},
			{IDParticipante: b, Nombre: b, Posicion: posicionB, Neto: netoB, ManosJugadas: 10},
		},
	}
}

func TestClasificarOrdenaPorPuntos(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "c")
	resultados := []ResultadoEnfrentamiento{
		resultadoDe("1", "a", "b", 100, -100),
		resultadoDe("2", "a", "c", 300, -300),
		resultadoDe("3", "b", "c", 50, -50),
	}

	clasificacion := Clasificar(cfg, PuntuacionNeto{}, resultados)
	if len(clasificacion.Filas) != 3 {
		t.Fatalf("la tabla tiene %d filas, se esperaban 3", len(clasificacion.Filas))
	}

	esperado := []string{"a", "b", "c"} // a: +400, b: -50, c: -350
	for i, id := range esperado {
		if clasificacion.Filas[i].IDParticipante != id {
			t.Fatalf("puesto %d = %q, se esperaba %q. Tabla:\n%s",
				i+1, clasificacion.Filas[i].IDParticipante, id, clasificacion.Tabla())
		}
		if clasificacion.Filas[i].Posicion != i+1 {
			t.Errorf("la fila %d dice posicion %d", i, clasificacion.Filas[i].Posicion)
		}
	}

	if clasificacion.Filas[0].Ganados != 2 || clasificacion.Filas[0].Jugados != 2 {
		t.Errorf("a jugo %d y gano %d, se esperaban 2 y 2",
			clasificacion.Filas[0].Jugados, clasificacion.Filas[0].Ganados)
	}
}

// TestClasificarNoOcultaLosQueNoJugaron: si el bot de alguien nunca arranco,
// eso tiene que verse en el resultado publicado.
func TestClasificarNoOcultaLosQueNoJugaron(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "roto")
	resultados := []ResultadoEnfrentamiento{
		resultadoDe("1", "a", "b", 100, -100),
		{
			Enfrentamiento: Enfrentamiento{ID: "2", Participantes: []string{"a", "roto"}},
			Error:          "no se pudo lanzar el bot",
		},
		{
			Enfrentamiento: Enfrentamiento{ID: "3", Participantes: []string{"b", "roto"}},
			Error:          "no se pudo lanzar el bot",
		},
	}

	clasificacion := Clasificar(cfg, PuntuacionNeto{}, resultados)

	if len(clasificacion.Incidencias) != 2 {
		t.Fatalf("se registraron %d incidencias, se esperaban 2", len(clasificacion.Incidencias))
	}

	var roto *FilaClasificacion
	for i := range clasificacion.Filas {
		if clasificacion.Filas[i].IDParticipante == "roto" {
			roto = &clasificacion.Filas[i]
		}
	}
	if roto == nil {
		t.Fatal("el participante cuyo bot no arranco desaparecio de la tabla")
	}
	if roto.NoJugados != 2 {
		t.Errorf("no_jugados = %d, se esperaban 2", roto.NoJugados)
	}
	if roto.Jugados != 0 {
		t.Errorf("jugados = %d, se esperaba 0", roto.Jugados)
	}
}

func TestClasificarEsDeterministaConEmpateTotal(t *testing.T) {
	cfg := configDePrueba(t, "a", "b", "c")
	resultados := []ResultadoEnfrentamiento{
		resultadoDe("1", "a", "b", 0, 0),
		resultadoDe("2", "a", "c", 0, 0),
		resultadoDe("3", "b", "c", 0, 0),
	}

	primera := Clasificar(cfg, PuntuacionNeto{}, resultados)
	for i := 0; i < 10; i++ {
		otra := Clasificar(cfg, PuntuacionNeto{}, resultados)
		for j := range primera.Filas {
			if primera.Filas[j].IDParticipante != otra.Filas[j].IDParticipante {
				t.Fatalf("la tabla cambio entre corridas en la fila %d: %q vs %q",
					j, primera.Filas[j].IDParticipante, otra.Filas[j].IDParticipante)
			}
		}
	}
}

func TestPuntuacionesDanLoEsperado(t *testing.T) {
	r := resultadoDe("1", "a", "b", 250, -250)
	ganador, perdedor := r.Puestos[0], r.Puestos[1]
	ganador.Timeouts = 3

	if p := (PuntuacionNeto{}).Puntos(r, ganador); p != 250 {
		t.Errorf("neto sin penalizacion = %v, se esperaba 250", p)
	}
	if p := (PuntuacionNeto{PenalizacionTimeout: 10}).Puntos(r, ganador); p != 220 {
		t.Errorf("neto con 3 timeouts a 10 = %v, se esperaba 220", p)
	}
	if p := (PuntuacionVictorias{}).Puntos(r, ganador); p != 1 {
		t.Errorf("victorias del ganador = %v, se esperaba 1", p)
	}
	if p := (PuntuacionVictorias{}).Puntos(r, perdedor); p != 0 {
		t.Errorf("victorias del perdedor = %v, se esperaba 0", p)
	}
	if p := (PuntuacionPuestos{PuntosPorRivalSuperado: 10}).Puntos(r, ganador); p != 10 {
		t.Errorf("puestos del ganador de 2 = %v, se esperaba 10", p)
	}
}

// --- Torneo completo -------------------------------------------------------

// TestTorneoNecesitaDosParticipantes cubre el camino corto de Correr.
func TestTorneoNecesitaDosParticipantes(t *testing.T) {
	torneo := &Torneo{
		Config:  ConfigTorneo{Participantes: []Participante{{ID: "solo"}}},
		Formato: RoundRobin{JugadoresPorMesa: 2},
		Puntua:  PuntuacionNeto{},
	}
	if _, _, err := torneo.Correr(context.Background()); err == nil {
		t.Fatal("un torneo de un solo participante deberia fallar")
	}
}

// TestSanearProtegeLasRutas: los IDs vienen de un archivo de configuracion y
// con ellos se arma el nombre del historial de manos.
func TestSanearProtegeLasRutas(t *testing.T) {
	casos := map[string]string{
		"normal-1":       "normal-1",
		"../../etc/paso": "______etc_paso",
		"con espacio":    "con_espacio",
		"":               "_",
	}
	for entrada, esperado := range casos {
		if salida := sanear(entrada); salida != esperado {
			t.Errorf("sanear(%q) = %q, se esperaba %q", entrada, salida, esperado)
		}
	}
}
