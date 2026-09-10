package mesa

import (
	"context"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// contextoDePrueba da un plazo generoso pero acotado: si una partida se
// trabara, el test falla en vez de colgar la suite entera.
func contextoDePrueba(t *testing.T) context.Context {
	t.Helper()
	ctx, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancelar)
	return ctx
}

// TestResumenCuentaLosTimeoutsPorJugador cubre lo que faltaba para que el
// ranking pudiera penalizar la lentitud: hasta ahora la conexion contaba los
// timeouts pero nunca llegaban al resumen de la partida, asi que el Casino
// siempre recibia 0.
func TestResumenCuentaLosTimeoutsPorJugador(t *testing.T) {
	m := mesaDePrueba(t, 2, 2000)
	m.CfgMesa.Timeout = 50
	m.CfgPartida.CantidadRondas = 3

	lento := NuevaConexionPrueba("lento")
	lento.RetrasoMs = 1000 // Siempre se pasa del plazo.
	m.SentarJugador("lento", "lento", lento)
	m.SentarJugador("rapido", "rapido", NuevaConexionPruebaFunc("rapido", siempreLlama))

	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	porID := map[string]ResumenJugador{}
	for _, p := range resumen.Posiciones {
		porID[p.IDJugador] = p
	}

	if porID["lento"].Timeouts == 0 {
		t.Error("el jugador que nunca respondio a tiempo deberia tener timeouts en el resumen")
	}
	if porID["rapido"].Timeouts != 0 {
		t.Errorf("el jugador que respondio siempre tiene %d timeouts", porID["rapido"].Timeouts)
	}
}

// TestResumenCuentaAccionesInvalidasComoTimeout: para el ranking da lo mismo
// no contestar que contestar cualquier cosa, porque la mesa hace lo mismo en
// los dos casos (aplicar la accion segura). Contarlo distinto le daria a un
// bot roto una ventaja sobre uno lento.
func TestResumenCuentaAccionesInvalidasComoTimeout(t *testing.T) {
	m := mesaDePrueba(t, 2, 2000)
	m.CfgPartida.CantidadRondas = 2

	// Un bot que siempre pide subir 1 ficha: nunca alcanza la subida minima,
	// asi que la mesa siempre le descarta la accion.
	invalido := NuevaConexionPruebaFunc("invalido", func(protocolo.MensajeMesa) (protocolo.Accion, error) {
		return protocolo.Accion{Tipo: protocolo.Raise, Monto: 1}, nil
	})
	m.SentarJugador("invalido", "invalido", invalido)
	m.SentarJugador("valido", "valido", NuevaConexionPruebaFunc("valido", siempreLlama))

	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	for _, p := range resumen.Posiciones {
		if p.IDJugador == "invalido" && p.Timeouts == 0 {
			t.Fatal("una accion invalida deberia contar igual que un timeout")
		}
	}
}

// TestJugadorQueSeSientaAMitadDeManoEsperaALaSiguiente es el caso que rompia
// la mesa en un torneo real: el servidor sienta a los bots desde la goroutine
// de cada conexion, asi que uno puede llegar con la mano ya repartida. Sin
// separar "sentado" de "en la mano", a ese jugador se le pedia accion en una
// mano en la que no tenia cartas ni fichas en el pozo.
func TestJugadorQueSeSientaAMitadDeManoEsperaALaSiguiente(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)
	m.CfgPartida.CantidadRondas = 2

	tarde := NuevaConexionPruebaFunc("tarde", siempreLlama)

	// El primer bot sienta al tercero en cuanto le piden su primera accion,
	// es decir con la mano ya en curso.
	sentado := false
	entrometido := NuevaConexionPruebaFunc("entrometido", func(msg protocolo.MensajeMesa) (protocolo.Accion, error) {
		if !sentado {
			sentado = true
			// SentarJugador toma Mu, y turnoJugador la suelta antes de pedir
			// la accion justamente para permitir esto.
			if err := m.SentarJugador("tarde", "tarde", tarde); err != nil {
				t.Errorf("SentarJugador a mitad de mano: %v", err)
			}
		}
		return siempreLlama(msg)
	})

	m.SentarJugador("entrometido", "entrometido", entrometido)
	m.SentarJugador("tranquilo", "tranquilo", NuevaConexionPruebaFunc("tranquilo", siempreLlama))

	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	if !sentado {
		t.Fatal("el tercer jugador nunca se sento: el test no probo nada")
	}

	// Las fichas tienen que cerrar: 2 jugadores x 1000 al empezar, mas 1000
	// del que se sumo despues.
	var total uint64
	porID := map[string]ResumenJugador{}
	for _, p := range resumen.Posiciones {
		total += p.SaldoFinal
		porID[p.IDJugador] = p
	}
	if total != 3000 {
		t.Fatalf("total de fichas = %d, se esperaban 3000: se crearon o perdieron fichas", total)
	}

	// El que llego tarde solo pudo jugar la segunda mano.
	if manos := porID["tarde"].ManosJugadas; manos != 1 {
		t.Errorf("el jugador que llego a mitad de mano jugo %d manos, se esperaba 1", manos)
	}
	// Y no se le pidio accion en la mano en la que no tenia cartas.
	for _, msg := range tarde.RecibidosDeTipo(protocolo.MsgSolicitarAccion) {
		if msg.Estado != nil && msg.Estado.IDMano == m.ID+"-mano-1" {
			t.Fatal("se le pidio accion en la mano 1, en la que no habia recibido cartas")
		}
	}
}

// TestClasificacionRespetaElOrdenDeEliminacion: entre jugadores que terminaron
// sin fichas, va mas arriba el que aguanto mas. Sin esto, todos los eliminados
// empatan en 0 y el orden lo decide el azar del recorrido, que es exactamente
// lo que un ranking de torneo no puede hacer.
func TestClasificacionRespetaElOrdenDeEliminacion(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)

	// Se arma la situacion a mano en vez de jugarla: asi el test verifica el
	// criterio de ordenamiento y no depende de que caiga un reparto concreto.
	m.SentarJugador("sobreviviente", "sobreviviente", NuevaConexionPrueba("sobreviviente"))
	m.SentarJugador("primero-en-caer", "primero-en-caer", NuevaConexionPrueba("primero-en-caer"))
	m.SentarJugador("ultimo-en-caer", "ultimo-en-caer", NuevaConexionPrueba("ultimo-en-caer"))

	m.Mu.Lock()
	m.Jugadores[0].Saldo = 3000
	m.Jugadores[1].Saldo = 0
	m.Jugadores[1].Activo = false
	m.Jugadores[2].Saldo = 0
	m.Jugadores[2].Activo = false
	m.ordenEliminacion = []string{"primero-en-caer", "ultimo-en-caer"}
	m.Mu.Unlock()

	resumen := m.resumenFinal(ResumenPartida{IDMesa: m.ID})

	esperado := []string{"sobreviviente", "ultimo-en-caer", "primero-en-caer"}
	for i, id := range esperado {
		if resumen.Posiciones[i].IDJugador != id {
			t.Fatalf("puesto %d = %q, se esperaba %q. Clasificacion: %+v",
				i+1, resumen.Posiciones[i].IDJugador, id, resumen.Posiciones)
		}
	}
}

// TestReconectarJugadorConservaElSaldo: un bot que se cae y vuelve tiene que
// retomar su misma silla y sus mismas fichas. Antes solo existia
// SentarJugador, que rechaza duplicados, asi que un socket perdido dejaba al
// jugador fuera de la partida para siempre.
func TestReconectarJugadorConservaElSaldo(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	vieja := NuevaConexionPrueba("c-1")
	m.SentarJugador("c-1", "uno", vieja)

	m.Mu.Lock()
	m.Jugadores[0].Saldo = 777
	m.Mu.Unlock()

	nueva := NuevaConexionPrueba("c-1")
	if err := m.ReconectarJugador("c-1", nueva); err != nil {
		t.Fatalf("ReconectarJugador: %v", err)
	}

	m.Mu.Lock()
	j := m.Jugadores[0]
	saldo, conexion := uint64(0), ConexionMesaJugador(nil)
	if j != nil {
		saldo, conexion = j.Saldo, j.Conexion
	}
	m.Mu.Unlock()

	if j == nil || j.ID != "c-1" {
		t.Fatalf("la silla 0 quedo %+v", j)
	}
	if saldo != 777 {
		t.Errorf("saldo tras reconectar = %d, se esperaban 777", saldo)
	}
	if conexion != nueva {
		t.Error("la mesa siguio hablandole al socket viejo")
	}
	if !vieja.Cerrada() {
		t.Error("la conexion vieja deberia cerrarse: no pueden quedar dos sockets vivos para el mismo jugador")
	}
	if err := m.ReconectarJugador("c-9", NuevaConexionPrueba("c-9")); err == nil {
		t.Error("reconectar a alguien que no esta sentado deberia fallar")
	}
}

// TestSaldoInicialViajaEnElResumen: la arena calcula el neto de cada
// enfrentamiento restando el stack inicial, asi que tiene que estar en el
// resumen.
func TestSaldoInicialViajaEnElResumen(t *testing.T) {
	m := mesaDePrueba(t, 2, 500)
	m.CfgPartida.CantidadRondas = 1
	m.SentarJugador("a", "a", NuevaConexionPruebaFunc("a", siempreLlama))
	m.SentarJugador("b", "b", NuevaConexionPruebaFunc("b", siempreLlama))

	resumen, err := m.Jugar(contextoDePrueba(t))
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	for _, p := range resumen.Posiciones {
		if p.SaldoInicial != 500 {
			t.Errorf("%s tiene SaldoInicial %d, se esperaban 500", p.IDJugador, p.SaldoInicial)
		}
		if p.Nombre == "" {
			t.Errorf("%s no trae nombre en el resumen", p.IDJugador)
		}
	}
}

// TestObservadorRecibeCadaMano cubre el enganche del que cuelga el historial
// de manos de la arena.
func TestObservadorRecibeCadaMano(t *testing.T) {
	espia := &observadorEspia{}
	m := mesaDePrueba(t, 2, 2000)
	m.CfgMesa.Observador = espia
	m.CfgPartida.CantidadRondas = 3

	m.SentarJugador("a", "a", NuevaConexionPruebaFunc("a", siempreLlama))
	m.SentarJugador("b", "b", NuevaConexionPruebaFunc("b", siempreLlama))

	if _, err := m.Jugar(contextoDePrueba(t)); err != nil {
		t.Fatalf("Jugar: %v", err)
	}

	if len(espia.manos) != 3 {
		t.Fatalf("el observador recibio %d manos, se esperaban 3", len(espia.manos))
	}
	for i, mano := range espia.manos {
		if mano.IDMano == "" {
			t.Errorf("la mano %d llego sin id", i)
		}
		if len(mano.Resultado.Repartos) == 0 {
			t.Errorf("la mano %d llego sin repartos", i)
		}
		// El historial tiene que traer las cartas de los dos, aunque uno se
		// haya retirado antes del showdown: es lo unico que hace auditable
		// una mano terminada en fold.
		if len(mano.Privadas) != 2 {
			t.Errorf("la mano %d trajo %d manos privadas, se esperaban 2", i, len(mano.Privadas))
		}
	}
}

type observadorEspia struct {
	manos []ManoJugada
}

func (o *observadorEspia) ManoTerminada(mano ManoJugada) {
	o.manos = append(o.manos, mano)
}

// TestEstadoDistingueEnTorneoDeEnMano: un bot necesita las dos cosas, y antes
// el estado publico solo tenia un campo `activo` que mezclaba ambas.
func TestEstadoDistingueEnTorneoDeEnMano(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)
	m.SentarJugador("a", "a", NuevaConexionPrueba("a"))
	m.SentarJugador("b", "b", NuevaConexionPrueba("b"))

	m.Mu.Lock()
	m.Jugadores[0].EnMano = true
	m.Jugadores[0].Retirado = true // Hizo fold: sigue en el torneo, no en la mano.
	m.Jugadores[1].EnMano = true
	m.Mu.Unlock()

	estado := m.Estado()
	porID := map[string]protocolo.JugadorPublico{}
	for _, j := range estado.Jugadores {
		porID[j.ID] = j
	}

	if porID["a"].Activo {
		t.Error("el jugador que hizo fold no deberia figurar activo en la mano")
	}
	if !porID["a"].EnTorneo {
		t.Error("hacer fold no saca a nadie del torneo")
	}
	if !porID["b"].Activo || !porID["b"].EnTorneo {
		t.Errorf("el jugador que sigue en la mano deberia estar activo y en torneo: %+v", porID["b"])
	}
}

// TestIdaYVueltaRepartenLasMismasCartas es la garantia de justicia del torneo,
// probada de punta a punta: dos partidas con la misma semilla reparten a cada
// SILLA exactamente las mismas cartas, mano por mano. Como la arena juega cada
// emparejamiento dos veces intercambiando las sillas, eso significa que los dos
// bots reciben las mismas manos, una vez cada uno.
//
// Si esto se rompe (por ejemplo, porque alguna decision de los bots empieza a
// consumir cartas del mazo), el round-robin deja de medir habilidad y pasa a
// medir suerte, sin que nada mas falle de forma visible.
func TestIdaYVueltaRepartenLasMismasCartas(t *testing.T) {
	const semilla = 4242

	// Se juegan dos partidas con la misma semilla y bots que juegan distinto,
	// para comprobar que el reparto no depende de lo que hagan.
	repartos := func(agresivo bool) []map[int]protocolo.Mano {
		m := NuevaMesa("m",
			ConfigMesa{MaxJugadores: 2, MinJugadores: 2, Timeout: 1000},
			ConfigPartida{CiegaMenor: 10, CiegaMayor: 20, StackInicial: 2000, CantidadRondas: 5, ReponerStack: true},
			crupier.NuevoConSemilla(semilla),
		).(*Mesa)

		responder := siempreLlama
		if agresivo {
			responder = func(msg protocolo.MensajeMesa) (protocolo.Accion, error) {
				for _, v := range msg.AccionesValidas {
					if v == protocolo.Fold {
						return protocolo.Accion{Tipo: protocolo.Fold}, nil
					}
				}
				return siempreLlama(msg)
			}
		}
		m.SentarJugador("s0", "s0", NuevaConexionPruebaFunc("s0", responder))
		m.SentarJugador("s1", "s1", NuevaConexionPruebaFunc("s1", siempreLlama))

		espia := &espiaPrivadas{}
		m.CfgMesa.Observador = espia
		if _, err := m.Jugar(contextoDePrueba(t)); err != nil {
			t.Fatalf("Jugar: %v", err)
		}
		return espia.porSilla
	}

	pasivos, agresivos := repartos(false), repartos(true)

	if len(pasivos) != 5 || len(agresivos) != 5 {
		t.Fatalf("se jugaron %d y %d manos, se esperaban 5 en las dos partidas", len(pasivos), len(agresivos))
	}
	for i := range pasivos {
		for silla, mano := range pasivos[i] {
			if otra, ok := agresivos[i][silla]; !ok || otra != mano {
				t.Fatalf("la mano %d de la silla %d cambio entre partidas con la misma semilla: %v vs %v",
					i+1, silla, mano, otra)
			}
		}
	}
}

// espiaPrivadas anota las cartas privadas de cada mano, indexadas por silla.
type espiaPrivadas struct {
	porSilla []map[int]protocolo.Mano
}

func (e *espiaPrivadas) ManoTerminada(mano ManoJugada) {
	sillaDe := map[string]int{}
	for _, j := range mano.Estado.Jugadores {
		sillaDe[j.ID] = j.PosicionSilla
	}
	deLaMano := map[int]protocolo.Mano{}
	for id, cartas := range mano.Privadas {
		deLaMano[sillaDe[id]] = cartas
	}
	e.porSilla = append(e.porSilla, deLaMano)
}

// TestSentarJugadorEnSillaFijaLasPosiciones cubre la asignacion explicita de
// sillas. Sin ella, la silla la reparte el orden de conexion: entre bots que la
// arena lanza en paralelo eso es una carrera, y la ida y la vuelta de un
// emparejamiento pueden terminar con la misma disposicion y ser, en la
// practica, la misma partida jugada dos veces.
func TestSentarJugadorEnSillaFijaLasPosiciones(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)

	// Se sientan en orden inverso al de las sillas, que es justo lo que hace
	// un bot que conecta antes que otro.
	if err := m.SentarJugadorEnSilla("segundo", "segundo", 2, NuevaConexionPrueba("segundo")); err != nil {
		t.Fatalf("SentarJugadorEnSilla(2): %v", err)
	}
	if err := m.SentarJugadorEnSilla("primero", "primero", 0, NuevaConexionPrueba("primero")); err != nil {
		t.Fatalf("SentarJugadorEnSilla(0): %v", err)
	}

	m.Mu.Lock()
	silla0, silla1, silla2 := m.Jugadores[0], m.Jugadores[1], m.Jugadores[2]
	m.Mu.Unlock()

	if silla0 == nil || silla0.ID != "primero" {
		t.Errorf("la silla 0 quedo %+v, se esperaba primero", silla0)
	}
	if silla1 != nil {
		t.Errorf("la silla 1 deberia seguir libre, quedo %+v", silla1)
	}
	if silla2 == nil || silla2.ID != "segundo" {
		t.Errorf("la silla 2 quedo %+v, se esperaba segundo", silla2)
	}
}

func TestSentarJugadorEnSillaRechazaSillasInvalidas(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	m.SentarJugadorEnSilla("a", "a", 0, NuevaConexionPrueba("a"))

	casos := []struct {
		nombre string
		id     string
		silla  int
	}{
		{"silla negativa", "b", -1},
		{"silla fuera de rango", "b", 5},
		{"silla ocupada", "b", 0},
		{"jugador ya sentado en otra silla", "a", 1},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			if err := m.SentarJugadorEnSilla(caso.id, caso.id, caso.silla, NuevaConexionPrueba(caso.id)); err == nil {
				t.Fatal("se esperaba un error")
			}
		})
	}
}
