package mesa

import (
	"fmt"
	"testing"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Tests de lo que necesita un torneo por fases: ciegas que suben, fichas
// distintas por jugador y partidas que terminan antes del heads-up.

// crupierArmado reparte siempre las mismas cartas: la mano i va a la silla i.
// El resto (evaluar, repartir el pozo) lo hace el crupier real, así que lo que
// se prueba es la Mesa y no un doble de la evaluación.
type crupierArmado struct {
	crupier.Crupier
	manos []protocolo.Mano
}

func carta(r protocolo.Rango, p protocolo.Palo) protocolo.Carta {
	return protocolo.Carta{Rango: r, Palo: p}
}

func (c crupierArmado) RepartirPrivadas(n int) ([]protocolo.Mano, error) {
	return append([]protocolo.Mano(nil), c.manos[:n]...), nil
}

// RepartirComunitarias da una mesa sin escalera ni color para nadie, así que
// cada mano gana exactamente por su par.
func (c crupierArmado) RepartirComunitarias(e protocolo.Etapa) ([]protocolo.Carta, error) {
	switch e {
	case protocolo.Flop:
		return []protocolo.Carta{carta(protocolo.Dos, protocolo.Treboles), carta(protocolo.Siete, protocolo.Diamantes), carta(protocolo.Nueve, protocolo.Corazones)}, nil
	case protocolo.Turn:
		return []protocolo.Carta{carta(protocolo.Diez, protocolo.Picas)}, nil
	default:
		return []protocolo.Carta{carta(protocolo.Tres, protocolo.Treboles)}, nil
	}
}

// parejaDe arma una mano de dos cartas del mismo rango, de palos que no
// forman color con la mesa.
func parejaDe(r protocolo.Rango) protocolo.Mano {
	return protocolo.Mano{carta(r, protocolo.Treboles), carta(r, protocolo.Diamantes)}
}

// mesaArmada arma una mesa donde la silla 0 tiene la mejor mano, la 1 la
// segunda, y así. Con todos all-in, el reparto es calculable a mano.
func mesaArmada(t *testing.T, cfg ConfigPartida, sillas int) *Mesa {
	t.Helper()
	pares := []protocolo.Rango{protocolo.As, protocolo.Rey, protocolo.Reina, protocolo.Jota}
	var manos []protocolo.Mano
	for i := 0; i < sillas; i++ {
		manos = append(manos, parejaDe(pares[i]))
	}
	m, ok := NuevaMesa("armada",
		ConfigMesa{MaxJugadores: uint8(sillas), MinJugadores: 2, Timeout: 2000},
		cfg,
		crupierArmado{Crupier: crupier.Nuevo(), manos: manos},
	).(*Mesa)
	if !ok {
		t.Fatal("NuevaMesa no devolvio un *Mesa")
	}
	return m
}

// siempreAllIn va all-in apenas puede, y si no, paga o pasa.
func siempreAllIn(msg protocolo.MensajeMesa) (protocolo.Accion, error) {
	for _, tipo := range []protocolo.TipoAccion{protocolo.AllIn, protocolo.Call, protocolo.Check} {
		for _, v := range msg.AccionesValidas {
			if v == tipo {
				return protocolo.Accion{Tipo: tipo}, nil
			}
		}
	}
	return protocolo.Accion{Tipo: protocolo.Fold}, nil
}

// --- Ciegas que suben ---------------------------------------------------------

func TestLasCiegasSubenSegunElNumeroDeMano(t *testing.T) {
	m := mesaDePrueba(t, 2, 100000)
	m.CfgPartida.CantidadRondas = 4
	m.CfgPartida.Ciegas = func(n int64) (uint64, uint64) {
		if n <= 2 {
			return 10, 20
		}
		return 50, 100
	}

	a := NuevaConexionPruebaFunc("a", siempreLlama)
	b := NuevaConexionPruebaFunc("b", siempreLlama)
	m.SentarJugador("a", "a", a)
	m.SentarJugador("b", "b", b)

	if _, err := m.Jugar(contextoDePrueba(t)); err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	// La ciega grande que vio un bot en cada mano, tomada de lo que la mesa le
	// mando: es lo que un bot de verdad usa para decidir.
	vista := map[string]int64{}
	for _, msg := range append(a.RecibidosDeTipo(protocolo.MsgSolicitarAccion), b.RecibidosDeTipo(protocolo.MsgSolicitarAccion)...) {
		vista[msg.Estado.IDMano] = msg.Estado.CiegaGrande
	}
	esperado := map[int]int64{1: 20, 2: 20, 3: 100, 4: 100}
	for mano, grande := range esperado {
		id := fmt.Sprintf("%s-mano-%d", m.ID, mano)
		if vista[id] != grande {
			t.Errorf("mano %d: la ciega grande que vieron los bots fue %d, se esperaba %d", mano, vista[id], grande)
		}
	}
}

// TestUnaManoNoCambiaDeCiegasAMitadDeCamino: las ciegas se fijan al repartir.
// Si se consultaran en cada calle, una mano podria empezar con una ciega y
// seguir con otra, y la subida minima dejaria de tener sentido.
func TestUnaManoNoCambiaDeCiegasAMitadDeCamino(t *testing.T) {
	m := mesaDePrueba(t, 2, 100000)
	m.CfgPartida.CantidadRondas = 3

	llamadas := map[int64]int{}
	m.CfgPartida.Ciegas = func(n int64) (uint64, uint64) {
		llamadas[n]++
		return 10 * uint64(n), 20 * uint64(n)
	}
	m.SentarJugador("a", "a", NuevaConexionPruebaFunc("a", siempreLlama))
	m.SentarJugador("b", "b", NuevaConexionPruebaFunc("b", siempreLlama))
	if _, err := m.Jugar(contextoDePrueba(t)); err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	for mano := int64(1); mano <= 3; mano++ {
		if llamadas[mano] != 1 {
			t.Errorf("las ciegas de la mano %d se consultaron %d veces, se esperaba 1", mano, llamadas[mano])
		}
	}
}

func TestEstadoAntesDeEmpezarMuestraLasPrimerasCiegas(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	m.CfgPartida.Ciegas = func(int64) (uint64, uint64) { return 25, 50 }

	if estado := m.Estado(); estado.CiegaChica != 25 || estado.CiegaGrande != 50 {
		t.Fatalf("ciegas = %d/%d, se esperaban 25/50", estado.CiegaChica, estado.CiegaGrande)
	}
}

// --- Fichas distintas por jugador ----------------------------------------------

func TestCadaJugadorSeSientaConSuStack(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)
	m.CfgPartida.StackPorJugador = map[string]uint64{"corto": 300, "largo": 5000}

	m.SentarJugador("corto", "corto", NuevaConexionPrueba("corto"))
	m.SentarJugadorEnSilla("largo", "largo", 2, NuevaConexionPrueba("largo"))
	m.SentarJugador("normal", "normal", NuevaConexionPrueba("normal"))

	m.Mu.Lock()
	defer m.Mu.Unlock()
	for _, j := range m.Jugadores {
		var esperado uint64
		switch j.ID {
		case "corto":
			esperado = 300
		case "largo":
			esperado = 5000
		default:
			esperado = 1000 // No figura: usa StackInicial.
		}
		if j.Saldo != esperado {
			t.Errorf("%s se sento con %d, se esperaban %d", j.ID, j.Saldo, esperado)
		}
	}
}

// TestLaClasificacionOrdenaPorFichasNoPorGanancia: con stacks distintos, quien
// arranco con mucho y perdio un poco sigue estando por encima de quien arranco
// con poco y duplico. Ordenar por ganancia daria el orden al reves.
func TestLaClasificacionOrdenaPorFichasNoPorGanancia(t *testing.T) {
	m := mesaArmada(t, ConfigPartida{
		CiegaMenor: 10, CiegaMayor: 20, StackInicial: 100, CantidadRondas: 1,
		StackPorJugador: map[string]uint64{"chico": 100, "grande": 900},
	}, 2)

	// La silla 0 tiene la mejor mano: "chico" gana el pozo que logra cubrir.
	m.SentarJugador("chico", "chico", NuevaConexionPruebaFunc("chico", siempreAllIn))
	m.SentarJugador("grande", "grande", NuevaConexionPruebaFunc("grande", siempreAllIn))

	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	porID := map[string]ResumenJugador{}
	for _, p := range resumen.Posiciones {
		porID[p.IDJugador] = p
	}
	// chico gana 100 y queda en 200; grande pierde 100 y queda en 800.
	if porID["chico"].SaldoFinal != 200 || porID["grande"].SaldoFinal != 800 {
		t.Fatalf("saldos finales = %d y %d, se esperaban 200 y 800", porID["chico"].SaldoFinal, porID["grande"].SaldoFinal)
	}
	if porID["grande"].Posicion != 1 || porID["chico"].Posicion != 2 {
		t.Errorf("posiciones = grande %d, chico %d; se esperaba que quien tiene mas fichas sea el primero",
			porID["grande"].Posicion, porID["chico"].Posicion)
	}
	if porID["chico"].SaldoInicial != 100 || porID["grande"].SaldoInicial != 900 {
		t.Errorf("saldos iniciales = %d y %d, se esperaban 100 y 900", porID["chico"].SaldoInicial, porID["grande"].SaldoInicial)
	}
	if porID["chico"].Neto != 100 || porID["grande"].Neto != -100 {
		t.Errorf("netos = %d y %d, se esperaban +100 y -100: el neto se mide contra el stack propio",
			porID["chico"].Neto, porID["grande"].Neto)
	}
}

// --- Partidas que terminan antes del heads-up ------------------------------------

// quebrantosSimultaneos es el escenario de los dos ultimos en pie: cuatro
// jugadores, todos all-in, y con la mano mejor en la silla 0. Se calcula a mano:
//
//	pozo a 100 (4 aportes):  gana la silla 0 -> 400
//	pozo a 200 (3 aportes):  gana la silla 1 -> 300
//	pozo a 300 (2 aportes):  gana la silla 1 -> 200
//	exceso de la silla 1:    vuelve a la silla 1 -> 200
//
// Quedan la silla 0 con 400 y la silla 1 con 700. Las sillas 2 y 3 quiebran en la
// misma mano: la 2 empezo con 300 y la 3 con 200.
func quebrantosSimultaneos(t *testing.T, hastaQueden int, manos int64) ResumenPartida {
	t.Helper()
	m := mesaArmada(t, ConfigPartida{
		CiegaMenor: 10, CiegaMayor: 20, StackInicial: 100, CantidadRondas: manos, HastaQueden: hastaQueden,
		StackPorJugador: map[string]uint64{"s0": 100, "s1": 500, "s2": 300, "s3": 200},
	}, 4)
	for _, id := range []string{"s0", "s1", "s2", "s3"} {
		if err := m.SentarJugador(id, id, NuevaConexionPruebaFunc(id, siempreAllIn)); err != nil {
			t.Fatalf("SentarJugador(%s): %v", id, err)
		}
	}
	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	return resumen
}

func TestLaPartidaTerminaCuandoQuedanLosQuePasan(t *testing.T) {
	resumen := quebrantosSimultaneos(t, 2, 5)

	if resumen.CantidadRondasJugadas != 1 {
		t.Fatalf("se jugaron %d manos, se esperaba 1: con dos en pie la partida tenia que terminar", resumen.CantidadRondasJugadas)
	}
	saldos := map[string]uint64{}
	for _, p := range resumen.Posiciones {
		saldos[p.IDJugador] = p.SaldoFinal
	}
	if saldos["s0"] != 400 || saldos["s1"] != 700 || saldos["s2"] != 0 || saldos["s3"] != 0 {
		t.Fatalf("saldos = %v, se esperaban s0=400 s1=700 s2=0 s3=0", saldos)
	}
}

// TestSinHastaQuedenLaPartidaSigueHastaElHeadsUp es el control del anterior: con
// el comportamiento de siempre la partida no se corta con dos en pie. Sin este
// caso, el test de arriba pasaria igual si la partida terminara por cualquier
// otro motivo.
func TestSinHastaQuedenLaPartidaSigueHastaElHeadsUp(t *testing.T) {
	resumen := quebrantosSimultaneos(t, 0, 5)

	if resumen.CantidadRondasJugadas <= 1 {
		t.Fatalf("se jugo %d mano, con dos jugadores en pie y sin HastaQueden la partida tenia que seguir", resumen.CantidadRondasJugadas)
	}
}

// TestQuienEmpezoConMasFichasQuedaMejorEntreLosQueQuiebranJuntos: la silla 2
// (300 al empezar) y la silla 3 (200) quiebran en la misma mano. El orden del
// recorrido pondria primero a la silla 2, asi que sin el desempate quedaria peor
// clasificada que la 3 siendo la que mas tenia.
func TestQuienEmpezoConMasFichasQuedaMejorEntreLosQueQuiebranJuntos(t *testing.T) {
	resumen := quebrantosSimultaneos(t, 2, 5)

	orden := make([]string, 0, 4)
	for _, p := range resumen.Posiciones {
		orden = append(orden, p.IDJugador)
	}
	esperado := []string{"s1", "s0", "s2", "s3"}
	for i := range esperado {
		if orden[i] != esperado[i] {
			t.Fatalf("clasificacion = %v, se esperaba %v", orden, esperado)
		}
	}
}

func TestLasFichasSeConservanEnLaPartidaPorFases(t *testing.T) {
	resumen := quebrantosSimultaneos(t, 2, 5)

	var total uint64
	for _, p := range resumen.Posiciones {
		total += p.SaldoFinal
	}
	if total != 1100 {
		t.Fatalf("total de fichas = %d, se esperaban 1100", total)
	}
}
