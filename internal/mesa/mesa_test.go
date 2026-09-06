package mesa

import (
	"sync"
	"testing"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// mesaDePrueba arma una mesa vacía con la configuración dada.
func mesaDePrueba(t *testing.T, maxJugadores uint8, stackInicial uint64) *Mesa {
	t.Helper()

	m, ok := NuevaMesa("mesa-1",
		ConfigMesa{MaxJugadores: maxJugadores, MinJugadores: 2, Timeout: 2000},
		ConfigPartida{CiegaMenor: 10, CiegaMayor: 20, StackInicial: stackInicial, CantidadRondas: -1},
		crupier.Nuevo(),
	).(*Mesa)
	if !ok {
		t.Fatal("NuevaMesa no devolvio un *Mesa")
	}
	return m
}

// sentados cuenta las sillas ocupadas. Requiere Mu.
func sentados(m *Mesa) int {
	n := 0
	for _, j := range m.Jugadores {
		if j != nil {
			n++
		}
	}
	return n
}

func TestSentarJugadorAsignaSillasEnOrden(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)

	for _, id := range []string{"c-1", "c-2", "c-3"} {
		if err := m.SentarJugador(id, "bot "+id, NuevaConexionPrueba(id)); err != nil {
			t.Fatalf("SentarJugador(%s): %v", id, err)
		}
	}

	for i, j := range m.Jugadores {
		if j == nil {
			t.Fatalf("la silla %d quedo vacia", i)
		}
		if j.Silla != int8(i) {
			t.Errorf("%s en silla %d, se esperaba %d", j.ID, j.Silla, i)
		}
		if j.Saldo != 1000 {
			t.Errorf("%s con saldo %d, se esperaba el StackInicial 1000", j.ID, j.Saldo)
		}
		if !j.Activo {
			t.Errorf("%s deberia quedar activo al sentarse", j.ID)
		}
	}
}

func TestSentarJugadorRechazaMesaLlenaYDuplicados(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	m.SentarJugador("c-1", "uno", NuevaConexionPrueba("c-1"))
	m.SentarJugador("c-2", "dos", NuevaConexionPrueba("c-2"))

	if err := m.SentarJugador("c-3", "tres", NuevaConexionPrueba("c-3")); err == nil {
		t.Error("sentar en una mesa llena deberia fallar")
	}
	if err := m.SentarJugador("c-1", "uno", NuevaConexionPrueba("c-1")); err == nil {
		t.Error("sentar dos veces al mismo jugador deberia fallar")
	}
	if n := sentados(m); n != 2 {
		t.Fatalf("sentados = %d, la mesa no debio crecer", n)
	}
}

func TestLevantarJugadorLiberaLaSilla(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)
	cx := NuevaConexionPrueba("c-2")
	m.SentarJugador("c-1", "uno", NuevaConexionPrueba("c-1"))
	m.SentarJugador("c-2", "dos", cx)

	if err := m.LevantarJugador("c-2"); err != nil {
		t.Fatalf("LevantarJugador: %v", err)
	}
	if m.Jugadores[1] != nil {
		t.Error("la silla 1 debio quedar libre")
	}
	if !cx.Cerrada() {
		t.Error("LevantarJugador cierra la conexion en esta implementacion")
	}
	if err := m.LevantarJugador("c-2"); err == nil {
		t.Error("levantar dos veces deberia fallar")
	}
	if err := m.LevantarJugador("c-9"); err == nil {
		t.Error("levantar a un jugador inexistente deberia fallar")
	}

	// La silla liberada se reutiliza.
	if err := m.SentarJugador("c-3", "tres", NuevaConexionPrueba("c-3")); err != nil {
		t.Fatalf("SentarJugador: %v", err)
	}
	if m.Jugadores[1] == nil || m.Jugadores[1].ID != "c-3" {
		t.Fatalf("la silla 1 deberia reutilizarse para c-3, quedo %+v", m.Jugadores[1])
	}
}

// TestSentarJugadorConcurrente corre con -race: el Servidor sienta a cada bot
// desde la goroutine de su conexion, asi que Jugadores se toca en paralelo.
func TestSentarJugadorConcurrente(t *testing.T) {
	const sillas = 8
	m := mesaDePrueba(t, sillas, 1000)

	var espera sync.WaitGroup
	for i := 0; i < sillas*2; i++ { // El doble de intentos que de sillas.
		espera.Add(1)
		go func(n int) {
			defer espera.Done()
			id := string(rune('a' + n))
			m.SentarJugador(id, id, NuevaConexionPrueba(id))
		}(i)
	}
	espera.Wait()

	if n := sentados(m); n != sillas {
		t.Fatalf("sentados = %d, se esperaba %d (el limite de la mesa)", n, sillas)
	}
	vistos := map[string]bool{}
	for _, j := range m.Jugadores {
		if vistos[j.ID] {
			t.Fatalf("el jugador %s quedo sentado dos veces", j.ID)
		}
		vistos[j.ID] = true
	}
}

// TestServidorSientaEnMesaReal cierra el circuito: socket TCP -> handshake ->
// SentarJugador de la Mesa de verdad, sin dobles.
func TestServidorSientaEnMesaReal(t *testing.T) {
	m := mesaDePrueba(t, 4, 1000)
	validar := func(token string) (string, string, error) { return "c-42", "BotAlpha", nil }
	s := levantarServidor(t, m, validar)

	bot, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("ConectarTCP: %v", err)
	}
	defer bot.Cerrar()

	err = bot.EnviarMensajeBot(protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: protocolo.VersionProtocolo,
		Token:   "token",
	}, 2000)
	if err != nil {
		t.Fatalf("saludo: %v", err)
	}
	resp, err := bot.RecibirMensajeMesa(2000)
	if err != nil || resp.Tipo != protocolo.MsgBienvenida {
		t.Fatalf("respuesta = %v (%v), se esperaba bienvenida", resp.Tipo, err)
	}

	esperarConexion(t, s, "c-42")

	m.Mu.Lock()
	defer m.Mu.Unlock()
	j := m.Jugadores[0]
	if j == nil || j.ID != "c-42" || j.Nombre != "BotAlpha" || j.Saldo != 1000 {
		t.Fatalf("silla 0 = %+v, se esperaba c-42/BotAlpha con 1000", j)
	}
}
