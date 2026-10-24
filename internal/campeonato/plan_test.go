package campeonato

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// --- Reparto de mesas ---------------------------------------------------------

func TestRepartirMesasSegunLasBases(t *testing.T) {
	casos := []struct {
		nombre                  string
		jugadores, porMesa, fix int
		esperado                []int
	}{
		// "mesas de 5 jugadores (si la division no es exacta, algunas mesas tienen 6)".
		{"division exacta", 20, 5, 0, []int{5, 5, 5, 5}},
		{"algunas mesas llevan uno mas", 23, 5, 0, []int{6, 6, 6, 5}},
		{"una mesa de 6", 6, 5, 0, []int{6}},
		// 7 no se arma con mesas de 5 o 6: una sola seria de 7.
		{"7 no entra en una mesa", 7, 5, 0, []int{4, 3}},
		{"mesas de 4 de la final", 32, 4, 0, []int{4, 4, 4, 4, 4, 4, 4, 4}},
		// Cuando el resto no entra como "uno de mas", se arman mas mesas y
		// algunas quedan de uno menos, en vez de una mesa de 9.
		{"el resto no entra: mas mesas", 9, 5, 0, []int{5, 4}},
		{"el resto no entra, tres mesas", 14, 5, 0, []int{5, 5, 4}},
		{"menos jugadores que el tamano", 3, 5, 0, []int{3}},
		// "4 mesas de 2 o 4": la cantidad es fija, el tamano depende de cuantos lleguen.
		{"cuatro mesas con 8", 8, 0, 4, []int{2, 2, 2, 2}},
		{"cuatro mesas con 16", 16, 0, 4, []int{4, 4, 4, 4}},
		{"cuatro mesas con 18", 18, 0, 4, []int{5, 5, 4, 4}},
		{"mesa final", 4, 0, 1, []int{4}},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			tamanos, _, err := RepartirMesas(caso.jugadores, caso.porMesa, caso.fix)
			if err != nil {
				t.Fatalf("RepartirMesas: %v", err)
			}
			if !reflect.DeepEqual(tamanos, caso.esperado) {
				t.Fatalf("mesas = %v, se esperaban %v", tamanos, caso.esperado)
			}
		})
	}
}

// TestRepartirMesasSiempreArmaMesasValidas recorre todos los tamanos de
// torneo plausibles. Es donde se esconderia un caso borde: un resto que no
// entra, una mesa de un solo jugador, una mesa mas grande que el maximo.
func TestRepartirMesasSiempreArmaMesasValidas(t *testing.T) {
	for porMesa := 2; porMesa <= MaxJugadoresMesa-1; porMesa++ {
		for jugadores := 2; jugadores <= 200; jugadores++ {
			tamanos, _, err := RepartirMesas(jugadores, porMesa, 0)
			if err != nil {
				t.Fatalf("%d jugadores en mesas de %d: %v", jugadores, porMesa, err)
			}

			suma, minimo, maximo := 0, tamanos[0], tamanos[0]
			for _, tam := range tamanos {
				suma += tam
				if tam < minimo {
					minimo = tam
				}
				if tam > maximo {
					maximo = tam
				}
			}
			switch {
			case suma != jugadores:
				t.Fatalf("%d jugadores en mesas de %d: las mesas %v suman %d", jugadores, porMesa, tamanos, suma)
			case minimo < 2:
				t.Fatalf("%d jugadores en mesas de %d: mesa de %d en %v", jugadores, porMesa, minimo, tamanos)
			case maximo-minimo > 1:
				t.Fatalf("%d jugadores en mesas de %d: %v no es parejo", jugadores, porMesa, tamanos)
			case maximo > porMesa+1 && jugadores >= porMesa:
				t.Fatalf("%d jugadores en mesas de %d: %v tiene mesas de mas de %d", jugadores, porMesa, tamanos, porMesa+1)
			}
		}
	}
}

func TestRepartirMesasConMasMesasQueJugadoresLasRecorta(t *testing.T) {
	tamanos, aviso, err := RepartirMesas(7, 0, 4)
	if err != nil {
		t.Fatalf("RepartirMesas: %v", err)
	}
	if len(tamanos) != 3 {
		t.Fatalf("mesas = %v, se esperaban 3 (con 7 jugadores no entran 4 mesas de a 2)", tamanos)
	}
	if aviso == "" {
		t.Error("recortar la cantidad de mesas tiene que avisarse")
	}
}

func TestRepartirMesasRechazaLoImposible(t *testing.T) {
	if _, _, err := RepartirMesas(1, 5, 0); err == nil {
		t.Error("un solo jugador no puede armar mesas")
	}
	if _, _, err := RepartirMesas(40, 0, 3); err == nil {
		t.Error("40 jugadores en 3 mesas dan mesas de 14: pasan el maximo")
	}
	if _, _, err := RepartirMesas(20, 1, 0); err == nil {
		t.Error("mesas de 1 jugador no existen")
	}
}

func TestDescribirMesas(t *testing.T) {
	casos := map[string][]int{
		"1 mesa de 4":                      {4},
		"8 mesas de 4":                     {4, 4, 4, 4, 4, 4, 4, 4},
		"4 mesas: 2 de 6 y 2 de 5":         {6, 6, 5, 5},
		"5 mesas: 1 de 6, 3 de 5 y 1 de 4": {6, 5, 5, 5, 4},
		"":                                 nil,
	}
	for esperado, tamanos := range casos {
		if got := DescribirMesas(tamanos); got != esperado {
			t.Errorf("DescribirMesas(%v) = %q, se esperaba %q", tamanos, got, esperado)
		}
	}
}

func TestClasificanAuto(t *testing.T) {
	casos := map[int]int{
		200: 32, 64: 32,
		63: 16, 32: 16,
		31: 8, 16: 8,
		12: 4, 8: 4,
		4: 2, 3: 2, 2: 2,
	}
	for inscritos, esperado := range casos {
		if got := ClasificanAuto(inscritos); got != esperado {
			t.Errorf("ClasificanAuto(%d) = %d, se esperaba %d", inscritos, got, esperado)
		}
	}
}

// --- Plan de las bases --------------------------------------------------------

// TestPlanConLasBasesParaSesentaYCuatroBots sigue el torneo de punta a punta
// con los numeros de las bases: 64 -> 32 -> 16 -> 4 -> 1.
func TestPlanConLasBasesParaSesentaYCuatroBots(t *testing.T) {
	planes, err := Plan(ConfigBases(), 64)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(planes) != 4 {
		t.Fatalf("el plan tiene %d fases, se esperaban 4", len(planes))
	}

	esperado := []struct {
		entran, salen int
		mesas         string
	}{
		{64, 32, "12 mesas: 4 de 6 y 8 de 5"},
		{32, 16, "8 mesas de 4"},
		{16, 4, "4 mesas de 4"},
		{4, 1, "1 mesa de 4"},
	}
	for i, e := range esperado {
		p := planes[i]
		if p.Entran != e.entran || p.Salen != e.salen || p.MesasTexto != e.mesas {
			t.Errorf("fase %d (%s): entran %d, salen %d, %q; se esperaba %d, %d, %q",
				i+1, p.Nombre, p.Entran, p.Salen, p.MesasTexto, e.entran, e.salen, e.mesas)
		}
		if len(p.Avisos) != 0 {
			t.Errorf("fase %d: no se esperaban avisos y hay %v", i+1, p.Avisos)
		}
	}
}

// TestPlanConTreintaYDosBotsUsaMesasDeDos: con 16 clasificados, las bases dicen
// "4 mesas de 2 o 4": aca son cuatro mesas de 2.
func TestPlanConTreintaYDosBotsUsaMesasDeDos(t *testing.T) {
	planes, err := Plan(ConfigBases(), 32)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if planes[0].Salen != 16 {
		t.Fatalf("con 32 inscritos deberian pasar 16, pasan %d", planes[0].Salen)
	}
	// 16 -> mesas de 4 -> pasan 2 por mesa -> 8 -> 4 mesas de 2 -> 4.
	if planes[1].Entran != 16 || planes[1].Salen != 8 {
		t.Errorf("segunda fase: entran %d, salen %d; se esperaba 16 y 8", planes[1].Entran, planes[1].Salen)
	}
	if planes[2].MesasTexto != "4 mesas de 2" {
		t.Errorf("tercera fase = %q, se esperaba \"4 mesas de 2\"", planes[2].MesasTexto)
	}
}

func TestPlanNoFallaConPocosJugadoresPeroLoAvisa(t *testing.T) {
	planes, err := Plan(ConfigBases(), 3)
	if err != nil {
		t.Fatalf("Plan con pocos jugadores no deberia ser un error: %v", err)
	}
	avisos := 0
	for _, p := range planes {
		avisos += len(p.Avisos)
	}
	if avisos == 0 {
		t.Fatal("con 3 jugadores el plan tiene que advertir que las fases no se sostienen")
	}
}

func TestPlanAvisaQueLaUltimaFaseNoDejaCampeon(t *testing.T) {
	cfg := ConfigBases()
	cfg.Fases = cfg.Fases[:2] // Termina en mesas de 4 donde pasan 2.
	planes, err := Plan(cfg, 32)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	ultima := planes[len(planes)-1]
	if len(ultima.Avisos) == 0 || !strings.Contains(strings.Join(ultima.Avisos, " "), "campe") {
		t.Fatalf("avisos de la ultima fase = %v, se esperaba uno sobre el campeon", ultima.Avisos)
	}
}

func TestPlanAvisaCuandoUnaFaseNoEliminaANadie(t *testing.T) {
	cfg := ConfigBases()
	cfg.Fases = []Fase{cfg.Fases[1]}
	cfg.Fases[0].AvanzanPorMesa = 4 // En mesas de 4 pasan los 4.
	planes, err := Plan(cfg, 16)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(planes[0].Avisos) == 0 {
		t.Fatal("una fase donde pasan todos tiene que avisarse")
	}
}

// --- Ciegas -------------------------------------------------------------------

func TestLasCiegasSubenCadaNivel(t *testing.T) {
	c := Ciegas{Niveles: []uint64{50, 100, 200}, ManosPorNivel: 10}
	casos := []struct {
		mano          int64
		chica, grande uint64
	}{
		{1, 50, 100}, {10, 50, 100},
		{11, 100, 200}, {20, 100, 200},
		{21, 200, 400},
		{9999, 200, 400}, // El ultimo nivel se repite.
	}
	for _, caso := range casos {
		chica, grande := c.Nivel(caso.mano)
		if chica != caso.chica || grande != caso.grande {
			t.Errorf("mano %d: ciegas %d/%d, se esperaban %d/%d", caso.mano, chica, grande, caso.chica, caso.grande)
		}
	}
}

// --- Configuracion ------------------------------------------------------------

func TestLaConfiguracionDeLasBasesEsValida(t *testing.T) {
	cfg := ConfigBases()
	if err := cfg.Validar(); err != nil {
		t.Fatalf("ConfigBases no valida: %v", err)
	}
}

func TestValidarRechazaConfiguracionesRotas(t *testing.T) {
	casos := map[string]func(*Config){
		"sin fases":                 func(c *Config) { c.Fases = nil },
		"tipo desconocido":          func(c *Config) { c.Fases[0].Tipo = "suiza" },
		"mesa de un jugador":        func(c *Config) { c.Fases[0].JugadoresPorMesa = 1 },
		"mesa de once":              func(c *Config) { c.Fases[0].JugadoresPorMesa = 11 },
		"primera fase sin stack":    func(c *Config) { c.Fases[0].Stack = 0 },
		"ciegas sin niveles":        func(c *Config) { c.Fases[0].Ciegas.Niveles = nil },
		"ciega en cero":             func(c *Config) { c.Fases[0].Ciegas.Niveles = []uint64{0} },
		"clasificatoria sin rondas": func(c *Config) { c.Fases[0].Rondas = 0 },
		"clasificatoria sin manos":  func(c *Config) { c.Fases[0].ManosPorRonda = 0 },
		"eliminatoria sin avanzan":  func(c *Config) { c.Fases[1].AvanzanPorMesa = 0 },
		"mesas negativas":           func(c *Config) { c.Fases[1].Mesas = -1 },
	}
	for nombre, romper := range casos {
		t.Run(nombre, func(t *testing.T) {
			cfg := ConfigBases()
			romper(&cfg)
			if err := cfg.Validar(); err == nil {
				t.Fatal("se esperaba un error de validacion")
			}
		})
	}
}

func TestLaConfiguracionRechazaCamposDesconocidos(t *testing.T) {
	var cfg Config
	err := cfg.desdeJSON([]byte(`{"nombre":"x","fases":[{"nombre":"f","tipo":"eliminatoria","jugadores_por_mesa":4,
		"avanzan_por_mesa":1,"stack":100,"ciegas":{"niveles":[10]},"avanzan_x_mesa":2}]}`))
	if err == nil {
		t.Fatal("un campo con el nombre mal escrito deberia hacer fallar la carga")
	}
}

func TestLaConfiguracionSobreviveIdaYVuelta(t *testing.T) {
	semilla := uint64(7)
	original := ConfigBases()
	original.Semilla = &semilla

	ruta := t.TempDir() + "/campeonato.json"
	if err := original.Guardar(ruta); err != nil {
		t.Fatalf("Guardar: %v", err)
	}
	recargada, err := CargarConfig(ruta)
	if err != nil {
		t.Fatalf("CargarConfig: %v", err)
	}
	if !reflect.DeepEqual(original, recargada) {
		t.Fatalf("la configuracion cambio en la ida y vuelta:\n%+v\n%+v", original, recargada)
	}
}

// --- Sorteo -------------------------------------------------------------------

func idsDe(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("j-%02d", i)
	}
	return ids
}

func TestElSorteoUsaATodosUnaSolaVez(t *testing.T) {
	ids := idsDe(23)
	tamanos := []int{6, 6, 6, 5}
	mesas, err := Sortear(ids, tamanos, NuevoAzar(nil))
	if err != nil {
		t.Fatalf("Sortear: %v", err)
	}

	vistos := map[string]int{}
	for i, mesa := range mesas {
		if len(mesa) != tamanos[i] {
			t.Errorf("mesa %d tiene %d jugadores, se esperaban %d", i, len(mesa), tamanos[i])
		}
		for _, id := range mesa {
			vistos[id]++
		}
	}
	for _, id := range ids {
		if vistos[id] != 1 {
			t.Errorf("%s aparece %d veces en el sorteo, se esperaba 1", id, vistos[id])
		}
	}
}

func TestElSorteoNoModificaLaEntrada(t *testing.T) {
	ids := idsDe(10)
	copia := append([]string(nil), ids...)
	if _, err := Sortear(ids, []int{5, 5}, NuevoAzar(nil)); err != nil {
		t.Fatalf("Sortear: %v", err)
	}
	if !reflect.DeepEqual(ids, copia) {
		t.Fatal("Sortear modifico la lista de entrada")
	}
}

func TestElSorteoConSemillaSeRepiteYCambiaConElContexto(t *testing.T) {
	semilla := uint64(99)
	ids := idsDe(20)
	tamanos := []int{5, 5, 5, 5}

	sortear := func(sal ...int) [][]string {
		mesas, err := Sortear(ids, tamanos, NuevoAzar(&semilla, sal...))
		if err != nil {
			t.Fatalf("Sortear: %v", err)
		}
		return mesas
	}

	if !reflect.DeepEqual(sortear(0, 1), sortear(0, 1)) {
		t.Error("el mismo sorteo con la misma semilla tiene que repetirse")
	}
	// Dos rondas del mismo campeonato no pueden dar la misma permutacion: el
	// sorteo "de nuevo en cada ronda" no sortearia nada.
	if reflect.DeepEqual(sortear(0, 1), sortear(0, 2)) {
		t.Error("dos rondas distintas dieron el mismo sorteo")
	}
	if reflect.DeepEqual(sortear(0, 1), sortear(1, 1)) {
		t.Error("dos fases distintas dieron el mismo sorteo")
	}
}

func TestElSorteoSinSemillaNoSeRepite(t *testing.T) {
	ids := idsDe(30)
	a, _ := Sortear(ids, []int{15, 15}, NuevoAzar(nil))
	b, _ := Sortear(ids, []int{15, 15}, NuevoAzar(nil))
	if reflect.DeepEqual(a, b) {
		t.Fatal("dos sorteos criptograficos dieron exactamente lo mismo")
	}
}

func TestElSorteoRechazaMesasQueNoSuman(t *testing.T) {
	if _, err := Sortear(idsDe(10), []int{5, 4}, NuevoAzar(nil)); err == nil {
		t.Fatal("mesas que no suman los jugadores tienen que fallar")
	}
}

// TestElSorteoEsParejo: ningun jugador deberia caer sistematicamente en la
// primera mesa. No es una prueba estadistica fina, solo atrapa un sorteo roto
// (por ejemplo, uno que no mezcle).
func TestElSorteoEsParejo(t *testing.T) {
	ids := idsDe(10)
	cuenta := map[string]int{}
	const vueltas = 4000
	semilla := uint64(1)
	for i := 0; i < vueltas; i++ {
		mesas, _ := Sortear(ids, []int{5, 5}, NuevoAzar(&semilla, i))
		for _, id := range mesas[0] {
			cuenta[id]++
		}
	}
	claves := make([]string, 0, len(cuenta))
	for k := range cuenta {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	for _, id := range claves {
		// Esperado: la mitad de las veces, con holgura amplia.
		if cuenta[id] < vueltas*40/100 || cuenta[id] > vueltas*60/100 {
			t.Errorf("%s cayo en la primera mesa %d de %d veces: el sorteo no es parejo", id, cuenta[id], vueltas)
		}
	}
}
