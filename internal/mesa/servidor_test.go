package mesa

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// mesaFalsa implementa MesaInterface para probar el servidor sin depender de
// las tareas Mesa #1 y #2, que aún están en panic.
type mesaFalsa struct {
	sentados []string
	err      error
}

func (m *mesaFalsa) SentarJugador(id, nombre string, c ConexionMesaJugador) error {
	if m.err != nil {
		return m.err
	}
	m.sentados = append(m.sentados, id)
	return nil
}
func (m *mesaFalsa) LevantarJugador(id string) error                   { return nil }
func (m *mesaFalsa) Jugar(ctx context.Context) (ResumenPartida, error) { return ResumenPartida{}, nil }
func (m *mesaFalsa) Estado() protocolo.EstadoPublico                   { return protocolo.EstadoPublico{} }

func logSilencioso() *log.Logger { return log.New(io.Discard, "", 0) }

// --- ConexionPrueba --------------------------------------------------------

func TestConexionPruebaDevuelveAccionesEnOrden(t *testing.T) {
	cx := NuevaConexionPrueba("c-1",
		protocolo.Accion{Tipo: protocolo.Call},
		protocolo.Accion{Tipo: protocolo.Raise, Monto: 60},
	)

	if err := cx.EnviarMensaje(protocolo.MensajeMesa{Tipo: protocolo.MsgSolicitarAccion}, 100); err != nil {
		t.Fatalf("EnviarMensaje: %v", err)
	}

	a, err := cx.SolicitarAccion(100)
	if err != nil || a.Tipo != protocolo.Call {
		t.Fatalf("primera accion = %v, %v; se esperaba call", a, err)
	}
	a, err = cx.SolicitarAccion(100)
	if err != nil || a.Tipo != protocolo.Raise || a.Monto != 60 {
		t.Fatalf("segunda accion = %v, %v; se esperaba raise 60", a, err)
	}
	if _, err := cx.SolicitarAccion(100); !errors.Is(err, ErrSinAcciones) {
		t.Fatalf("se esperaba ErrSinAcciones, se obtuvo %v", err)
	}
	if n := len(cx.RecibidosDeTipo(protocolo.MsgSolicitarAccion)); n != 1 {
		t.Fatalf("historial = %d solicitudes, se esperaba 1", n)
	}
}

func TestConexionPruebaRespondeConFuncion(t *testing.T) {
	cx := NuevaConexionPruebaFunc("c-1", func(m protocolo.MensajeMesa) (protocolo.Accion, error) {
		if m.Estado != nil && m.Estado.ApuestaActual > 0 {
			return protocolo.Accion{Tipo: protocolo.Fold}, nil
		}
		return protocolo.Accion{Tipo: protocolo.Check}, nil
	})

	cx.EnviarMensaje(protocolo.MensajeMesa{
		Tipo:   protocolo.MsgSolicitarAccion,
		Estado: &protocolo.EstadoPublico{IDMano: "m-1", ApuestaActual: 20},
	}, 100)

	a, err := cx.SolicitarAccion(100)
	if err != nil || a.Tipo != protocolo.Fold {
		t.Fatalf("accion = %v, %v; se esperaba fold", a, err)
	}
}

func TestConexionPruebaSimulaTimeoutYCierre(t *testing.T) {
	cx := NuevaConexionPrueba("c-1", protocolo.Accion{Tipo: protocolo.Call})
	cx.RetrasoMs = 500

	if _, err := cx.SolicitarAccion(100); !errors.Is(err, ErrTiempoAgotado) {
		t.Fatalf("se esperaba ErrTiempoAgotado, se obtuvo %v", err)
	}
	if cx.Timeouts() != 1 {
		t.Fatalf("timeouts = %d, se esperaba 1", cx.Timeouts())
	}

	cx.Cerrar()
	if !cx.Cerrada() {
		t.Fatal("la conexion deberia estar cerrada")
	}
	if err := cx.EnviarMensaje(protocolo.MensajeMesa{}, 100); !errors.Is(err, ErrConexionCerrada) {
		t.Fatalf("se esperaba ErrConexionCerrada, se obtuvo %v", err)
	}
}

func TestConexionPruebaGuardaCartasPrivadas(t *testing.T) {
	cx := NuevaConexionPrueba("c-1")
	mano := protocolo.Mano{
		{Rango: protocolo.As, Palo: protocolo.Picas},
		{Rango: protocolo.Rey, Palo: protocolo.Picas},
	}
	cx.EnviarMensaje(protocolo.MensajeMesa{Tipo: protocolo.MsgManoInicio, Cartas: &mano}, 100)

	recibidas, ok := cx.CartasRecibidas()
	if !ok || recibidas != mano {
		t.Fatalf("cartas = %v (%v), se esperaba %v", recibidas, ok, mano)
	}
}

// --- ConexionTCP -----------------------------------------------------------

func TestConexionTCPIdaYVuelta(t *testing.T) {
	mesaCx, botCx := ParConexionesMemoria()
	defer mesaCx.Cerrar()
	defer botCx.Cerrar()

	// El bot lee la solicitud y responde con un raise.
	go func() {
		m, err := botCx.RecibirMensajeMesa(1000)
		if err != nil || m.Tipo != protocolo.MsgSolicitarAccion {
			return
		}
		botCx.EnviarMensajeBot(protocolo.MensajeBot{
			Tipo:   protocolo.MsgAccion,
			IDMano: m.Estado.IDMano,
			Accion: &protocolo.Accion{Tipo: protocolo.Raise, Monto: 60},
		}, 1000)
	}()

	err := mesaCx.EnviarMensaje(protocolo.MensajeMesa{
		Tipo:            protocolo.MsgSolicitarAccion,
		AccionesValidas: []protocolo.TipoAccion{protocolo.Fold, protocolo.Call, protocolo.Raise},
		Estado:          &protocolo.EstadoPublico{IDMano: "m-001", ApuestaActual: 20},
	}, 1000)
	if err != nil {
		t.Fatalf("EnviarMensaje: %v", err)
	}

	a, err := mesaCx.SolicitarAccion(1000)
	if err != nil {
		t.Fatalf("SolicitarAccion: %v", err)
	}
	if a.Tipo != protocolo.Raise || a.Monto != 60 {
		t.Fatalf("accion = %v, se esperaba raise 60", a)
	}
}

func TestConexionTCPDescartaAccionTardia(t *testing.T) {
	mesaCx, botCx := ParConexionesMemoria()
	defer mesaCx.Cerrar()
	defer botCx.Cerrar()

	go func() {
		if _, err := botCx.RecibirMensajeMesa(1000); err != nil {
			return
		}
		// Primero una accion de una mano ya cerrada, luego la valida.
		botCx.EnviarMensajeBot(protocolo.MensajeBot{
			Tipo:   protocolo.MsgAccion,
			IDMano: "m-000",
			Accion: &protocolo.Accion{Tipo: protocolo.AllIn},
		}, 1000)
		botCx.EnviarMensajeBot(protocolo.MensajeBot{
			Tipo:   protocolo.MsgAccion,
			IDMano: "m-001",
			Accion: &protocolo.Accion{Tipo: protocolo.Fold},
		}, 1000)
	}()

	mesaCx.EnviarMensaje(protocolo.MensajeMesa{
		Tipo:   protocolo.MsgSolicitarAccion,
		Estado: &protocolo.EstadoPublico{IDMano: "m-001"},
	}, 1000)

	a, err := mesaCx.SolicitarAccion(2000)
	if err != nil {
		t.Fatalf("SolicitarAccion: %v", err)
	}
	if a.Tipo != protocolo.Fold {
		t.Fatalf("accion = %v, se esperaba fold (la de m-000 debia descartarse)", a)
	}
}

func TestConexionTCPTimeoutSiElBotNoResponde(t *testing.T) {
	mesaCx, botCx := ParConexionesMemoria()
	defer mesaCx.Cerrar()
	defer botCx.Cerrar()

	go botCx.RecibirMensajeMesa(1000) // Lee y se queda callado.

	mesaCx.EnviarMensaje(protocolo.MensajeMesa{
		Tipo:   protocolo.MsgSolicitarAccion,
		Estado: &protocolo.EstadoPublico{IDMano: "m-001", ApuestaActual: 20},
	}, 1000)

	inicio := time.Now()
	_, err := mesaCx.SolicitarAccion(150)
	if !errors.Is(err, ErrTiempoAgotado) {
		t.Fatalf("se esperaba ErrTiempoAgotado, se obtuvo %v", err)
	}
	if time.Since(inicio) < 100*time.Millisecond {
		t.Fatal("el plazo venció demasiado pronto")
	}
	if mesaCx.Timeouts() != 1 {
		t.Fatalf("timeouts = %d, se esperaba 1", mesaCx.Timeouts())
	}

	// La mesa aplica la accion segura: con apuesta pendiente, fold.
	if segura := protocolo.AccionSegura(20); segura.Tipo != protocolo.Fold {
		t.Fatalf("accion segura = %v, se esperaba fold", segura)
	}
}

func TestConexionTCPDetectaAbandono(t *testing.T) {
	mesaCx, botCx := ParConexionesMemoria()
	defer mesaCx.Cerrar()
	defer botCx.Cerrar()

	go botCx.EnviarMensajeBot(protocolo.MensajeBot{Tipo: protocolo.MsgAbandono}, 1000)

	if _, err := mesaCx.SolicitarAccion(1000); !errors.Is(err, ErrJugadorAbandono) {
		t.Fatalf("se esperaba ErrJugadorAbandono, se obtuvo %v", err)
	}
}

func TestConexionTCPCerradaRechazaOperaciones(t *testing.T) {
	mesaCx, botCx := ParConexionesMemoria()
	botCx.Cerrar()

	if err := mesaCx.Cerrar(); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	if err := mesaCx.Cerrar(); err != nil {
		t.Fatalf("Cerrar debe ser idempotente: %v", err)
	}
	if err := mesaCx.EnviarMensaje(protocolo.MensajeMesa{Tipo: protocolo.MsgEstado}, 100); !errors.Is(err, ErrConexionCerrada) {
		t.Fatalf("se esperaba ErrConexionCerrada, se obtuvo %v", err)
	}
	if _, err := mesaCx.SolicitarAccion(100); !errors.Is(err, ErrConexionCerrada) {
		t.Fatalf("se esperaba ErrConexionCerrada, se obtuvo %v", err)
	}
}

// --- Servidor --------------------------------------------------------------

// levantarServidor abre un servidor en un puerto libre y lo apaga al terminar.
func levantarServidor(t *testing.T, m MesaInterface, validar ValidarToken) *Servidor {
	t.Helper()

	s := NuevoServidor(ConfigServidor{
		Direccion:          "127.0.0.1:0",
		TimeoutHandshakeMs: 2000,
		Log:                logSilencioso(),
	}, m, validar)

	if err := s.Abrir(); err != nil {
		t.Fatalf("Abrir: %v", err)
	}

	ctx, cancelar := context.WithCancel(context.Background())
	listo := make(chan error, 1)
	go func() { listo <- s.Servir(ctx) }()

	t.Cleanup(func() {
		cancelar()
		s.Cerrar()
		select {
		case err := <-listo:
			if err != nil {
				t.Errorf("Servir devolvio %v, se esperaba nil en cierre ordenado", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("el servidor no se apago a tiempo")
		}
	})
	return s
}

func TestServidorHandshakeSientaAlJugador(t *testing.T) {
	m := &mesaFalsa{}
	validar := func(token string) (string, string, error) {
		if token != "token-bueno" {
			return "", "", ErrTokenInvalido
		}
		return "c-42", "BotAlpha", nil
	}
	s := levantarServidor(t, m, validar)

	bot, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("ConectarTCP: %v", err)
	}
	defer bot.Cerrar()

	err = bot.EnviarMensajeBot(protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: protocolo.VersionProtocolo,
		Token:   "token-bueno",
	}, 2000)
	if err != nil {
		t.Fatalf("saludo: %v", err)
	}

	resp, err := bot.RecibirMensajeMesa(2000)
	if err != nil {
		t.Fatalf("respuesta: %v", err)
	}
	if resp.Tipo != protocolo.MsgBienvenida {
		t.Fatalf("tipo = %q, se esperaba bienvenida (%s)", resp.Tipo, resp.Mensaje)
	}
	if resp.Version != protocolo.VersionProtocolo {
		t.Fatalf("version = %q, se esperaba %q", resp.Version, protocolo.VersionProtocolo)
	}

	esperarConexion(t, s, "c-42")
	if len(m.sentados) != 1 || m.sentados[0] != "c-42" {
		t.Fatalf("sentados = %v, se esperaba [c-42]", m.sentados)
	}
}

func TestServidorRechazaVersionDistinta(t *testing.T) {
	s := levantarServidor(t, &mesaFalsa{}, nil)

	bot, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("ConectarTCP: %v", err)
	}
	defer bot.Cerrar()

	bot.EnviarMensajeBot(protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: "99.0.0",
		Token:   "cualquiera",
	}, 2000)

	resp, err := bot.RecibirMensajeMesa(2000)
	if err != nil {
		t.Fatalf("respuesta: %v", err)
	}
	if resp.Tipo != protocolo.MsgError {
		t.Fatalf("tipo = %q, se esperaba error", resp.Tipo)
	}
}

func TestServidorRechazaTokenInvalido(t *testing.T) {
	validar := func(token string) (string, string, error) { return "", "", ErrTokenInvalido }
	s := levantarServidor(t, &mesaFalsa{}, validar)

	bot, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("ConectarTCP: %v", err)
	}
	defer bot.Cerrar()

	bot.EnviarMensajeBot(protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: protocolo.VersionProtocolo,
		Token:   "falso",
	}, 2000)

	resp, err := bot.RecibirMensajeMesa(2000)
	if err != nil {
		t.Fatalf("respuesta: %v", err)
	}
	if resp.Tipo != protocolo.MsgError {
		t.Fatalf("tipo = %q, se esperaba error", resp.Tipo)
	}
	if len(s.Conexiones()) != 0 {
		t.Fatalf("no deberia registrarse ninguna conexion, hay %d", len(s.Conexiones()))
	}
}

func TestServidorSobreviveAlPanicDeLaMesa(t *testing.T) {
	s := levantarServidor(t, &mesaQuePanica{}, nil)

	// Primer bot: la mesa entra en panic al sentarlo.
	bot1, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("ConectarTCP: %v", err)
	}
	bot1.EnviarMensajeBot(protocolo.MensajeBot{
		Tipo: protocolo.MsgSaludo, Version: protocolo.VersionProtocolo, Token: "c-1",
	}, 2000)
	bot1.RecibirMensajeMesa(2000)
	bot1.Cerrar()

	// El servidor debe seguir aceptando.
	bot2, err := ConectarTCP(s.Direccion(), 2000)
	if err != nil {
		t.Fatalf("el servidor murio con el panic: %v", err)
	}
	bot2.Cerrar()
}

type mesaQuePanica struct{ mesaFalsa }

func (*mesaQuePanica) SentarJugador(id, nombre string, c ConexionMesaJugador) error {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #3")
}

// esperarConexion espera a que el servidor registre al jugador, porque el
// handshake termina en otra goroutine.
func esperarConexion(t *testing.T, s *Servidor, id string) {
	t.Helper()
	limite := time.Now().Add(2 * time.Second)
	for time.Now().Before(limite) {
		if _, ok := s.Conexiones()[id]; ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("el jugador %s nunca fue registrado", id)
}
