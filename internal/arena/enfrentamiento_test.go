package arena

import (
	"context"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/mesa"
	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

// botPasivo pasa si es gratis y paga si no. Alcanza para que una partida
// corra entera sin depender de la estrategia.
type botPasivo struct{}

func (botPasivo) Nombre() string { return "pasivo" }

func (botPasivo) Decidir(ctx context.Context, t botsdk.Turno) botsdk.Accion {
	if t.Puede(botsdk.Check) {
		return t.Check()
	}
	return t.Pagar()
}

// lanzadorEnProceso corre cada bot como goroutine con botsdk, en lugar de
// como subproceso: prueba el camino completo (TCP, handshake, protocolo) sin
// tener que compilar binarios.
type lanzadorEnProceso struct {
	// antes, si no es nil, corre antes de arrancar cada bot.
	antes func(p Participante, direccion string)

	mu       sync.Mutex
	sesiones []*sesionEnProceso
}

func (l *lanzadorEnProceso) Lanzar(ctx context.Context, p Participante, direccion string) (Sesion, error) {
	if l.antes != nil {
		l.antes(p, direccion)
	}
	s := &sesionEnProceso{id: p.ID, fin: make(chan struct{})}
	go func() {
		defer close(s.fin)
		// Sin el ctx del enfrentamiento a propósito, igual que LanzadorProceso:
		// el bot tiene que terminar porque la mesa le cierra la conexión.
		botsdk.Correr(context.Background(), botPasivo{}, botsdk.Opciones{Direccion: direccion, Token: p.TokenEfectivo()})
	}()
	l.mu.Lock()
	l.sesiones = append(l.sesiones, s)
	l.mu.Unlock()
	return s, nil
}

// sesionEnProceso anota si, cuando la arena la quiso detener, el bot terminó
// solo dentro del plazo o hubo que abandonarlo (el equivalente a matarlo).
type sesionEnProceso struct {
	id  string
	fin chan struct{}

	mu          sync.Mutex
	detenida    bool
	terminoSolo bool
}

func (s *sesionEnProceso) Detener() error {
	select {
	case <-s.fin:
		s.mu.Lock()
		s.terminoSolo = true
		s.mu.Unlock()
	case <-time.After(time.Second):
	}
	s.mu.Lock()
	s.detenida = true
	s.mu.Unlock()
	return nil
}

func torneoEnProceso(t *testing.T, l *lanzadorEnProceso, ids ...string) *Torneo {
	t.Helper()
	cfg := configDePrueba(t, ids...)
	cfg.ManosPorEnfrentamiento = 5
	cfg.IdaYVuelta = false
	for i := range cfg.Participantes {
		// Con comando, la arena lo trata como un bot que lanza ella (y le da
		// token de sesión). El lanzador de prueba no lo ejecuta.
		cfg.Participantes[i].Comando = []string{"en-proceso"}
	}
	formato, err := BuscarFormato(cfg.Formato, cfg)
	if err != nil {
		t.Fatal(err)
	}
	puntuador, err := BuscarPuntuador(cfg.Puntuacion, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Torneo{
		Config:   cfg,
		Formato:  formato,
		Puntua:   puntuador,
		Lanzador: l,
		Log:      log.New(io.Discard, "", 0),
	}
}

func correrTorneo(t *testing.T, torneo *Torneo) []ResultadoEnfrentamiento {
	t.Helper()
	ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelar()
	_, resultados, err := torneo.Correr(ctx)
	if err != nil {
		t.Fatalf("Correr: %v", err)
	}
	for _, r := range resultados {
		if !r.Jugado() {
			t.Fatalf("el enfrentamiento %s no se jugo: %s", r.ID, r.Error)
		}
	}
	return resultados
}

// Al terminar la partida, la arena tiene que cerrarles la conexión a los bots
// ANTES de detenerlos. Si los detiene primero, espera todo el plazo de cierre
// a un bot que sigue conectado, y después lo mata: se pierde tiempo en cada
// enfrentamiento y el participante ve su bot terminado a la fuerza.
func TestLosBotsTerminanSolosAlTerminarLaPartida(t *testing.T) {
	l := &lanzadorEnProceso{}
	correrTorneo(t, torneoEnProceso(t, l, "a", "b"))

	if len(l.sesiones) != 2 {
		t.Fatalf("se lanzaron %d bots, se esperaban 2", len(l.sesiones))
	}
	for _, s := range l.sesiones {
		s.mu.Lock()
		detenida, terminoSolo := s.detenida, s.terminoSolo
		s.mu.Unlock()
		if !detenida {
			t.Errorf("la sesion de %s nunca se detuvo", s.id)
		}
		if !terminoSolo {
			t.Errorf("el bot de %s seguia conectado cuando la arena lo quiso detener", s.id)
		}
	}
}

// En modo abierto los IDs de los participantes son públicos (están en el
// archivo del torneo y en el estado de la mesa). Si sirvieran de token, un bot
// podría presentarse con el ID del rival y, como la mesa acepta reconexiones,
// quedarse con su asiento.
func TestUnBotNoPuedeSuplantarAlRival(t *testing.T) {
	var (
		mu        sync.Mutex
		respuesta protocolo.MensajeMesa
		intento   bool
	)
	l := &lanzadorEnProceso{antes: func(p Participante, direccion string) {
		if p.ID != "b" {
			return
		}
		// b, antes de conectarse como sí mismo, intenta entrar como a.
		cx, err := mesa.ConectarTCP(direccion, 2000)
		if err != nil {
			t.Errorf("ConectarTCP: %v", err)
			return
		}
		defer cx.Cerrar()
		cx.EnviarMensajeBot(protocolo.MensajeBot{Tipo: protocolo.MsgSaludo, Version: protocolo.VersionProtocolo, Token: "a"}, 2000)
		resp, err := cx.RecibirMensajeMesa(2000)
		if err != nil {
			t.Errorf("respuesta al intento de suplantacion: %v", err)
			return
		}
		mu.Lock()
		respuesta, intento = resp, true
		mu.Unlock()
	}}

	correrTorneo(t, torneoEnProceso(t, l, "a", "b"))

	mu.Lock()
	defer mu.Unlock()
	if !intento {
		t.Fatal("el intento de suplantacion no llego a hacerse")
	}
	if respuesta.Tipo != protocolo.MsgError {
		t.Fatalf("la mesa respondio %q al bot que se presento con el ID del rival, se esperaba error", respuesta.Tipo)
	}
}
