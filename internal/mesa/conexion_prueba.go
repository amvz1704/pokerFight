package mesa

import (
	"sync"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// ConexionPrueba es una implementación de ConexionMesaJugador que no toca la
// red: guarda en memoria lo que la mesa envía y responde con acciones
// programadas de antemano. Sirve para probar la lógica de mesa y crupier de
// forma determinista y para los bots "de mentira" de los tests.
//
// No está en un archivo _test.go a propósito: también la usan las pruebas de
// otros paquetes (crupier, casino) y las pruebas de humo.
type ConexionPrueba struct {
	IDJugador string

	// Responder, si no es nil, tiene prioridad sobre la cola de acciones. Recibe
	// el último mensaje que la mesa envió (normalmente el MsgSolicitarAccion) y
	// decide la jugada. Permite escribir bots de prueba con estrategia.
	Responder func(m protocolo.MensajeMesa) (protocolo.Accion, error)

	// RetrasoMs simula un bot lento. Si es mayor que el timeout de la solicitud,
	// SolicitarAccion devuelve ErrTiempoAgotado. No duerme de verdad: las
	// pruebas no deben tardar.
	RetrasoMs uint64

	// ErrAlEnviar, si no es nil, hace fallar todo EnviarMensaje. Sirve para
	// probar el camino de "jugador desconectado".
	ErrAlEnviar error

	mu        sync.Mutex
	recibidos []protocolo.MensajeMesa
	acciones  []protocolo.Accion
	cerrada   bool
	timeouts  uint64
}

// Verificación en tiempo de compilación de que cumple el contrato.
var _ ConexionMesaJugador = (*ConexionPrueba)(nil)

// NuevaConexionPrueba crea una conexión falsa que devolverá las acciones dadas
// en orden. Al agotarse, SolicitarAccion devuelve ErrSinAcciones y la mesa debe
// aplicar protocolo.AccionSegura.
func NuevaConexionPrueba(idJugador string, acciones ...protocolo.Accion) *ConexionPrueba {
	return &ConexionPrueba{
		IDJugador: idJugador,
		acciones:  append([]protocolo.Accion(nil), acciones...),
	}
}

// NuevaConexionPruebaFunc crea una conexión falsa que decide cada jugada con
// la función dada, en lugar de una cola fija.
func NuevaConexionPruebaFunc(idJugador string, responder func(m protocolo.MensajeMesa) (protocolo.Accion, error)) *ConexionPrueba {
	return &ConexionPrueba{IDJugador: idJugador, Responder: responder}
}

// EnviarMensaje guarda el mensaje en el historial. timeoutMs se ignora: no hay
// red que pueda bloquearse.
func (c *ConexionPrueba) EnviarMensaje(m protocolo.MensajeMesa, timeoutMs uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return ErrConexionCerrada
	}
	if c.ErrAlEnviar != nil {
		return c.ErrAlEnviar
	}
	c.recibidos = append(c.recibidos, m)
	return nil
}

// SolicitarAccion devuelve la siguiente acción programada.
func (c *ConexionPrueba) SolicitarAccion(timeoutMs uint64) (protocolo.Accion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return protocolo.Accion{}, ErrConexionCerrada
	}
	if timeoutMs > 0 && c.RetrasoMs > timeoutMs {
		c.timeouts++
		return protocolo.Accion{}, ErrTiempoAgotado
	}

	if c.Responder != nil {
		var ultimo protocolo.MensajeMesa
		if n := len(c.recibidos); n > 0 {
			ultimo = c.recibidos[n-1]
		}
		return c.Responder(ultimo)
	}

	if len(c.acciones) == 0 {
		return protocolo.Accion{}, ErrSinAcciones
	}
	a := c.acciones[0]
	c.acciones = c.acciones[1:]
	return a, nil
}

// Cerrar marca la conexión como cerrada. Es idempotente.
func (c *ConexionPrueba) Cerrar() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cerrada = true
	return nil
}

// --- Ayudas para las aserciones de los tests -------------------------------

// Cerrada indica si la mesa ya cerró esta conexión.
func (c *ConexionPrueba) Cerrada() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cerrada
}

// Timeouts es la cantidad de veces que se simuló una falta de respuesta.
func (c *ConexionPrueba) Timeouts() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timeouts
}

// Recibidos devuelve una copia de todos los mensajes que envió la mesa.
func (c *ConexionPrueba) Recibidos() []protocolo.MensajeMesa {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]protocolo.MensajeMesa(nil), c.recibidos...)
}

// RecibidosDeTipo filtra el historial por tipo de mensaje.
func (c *ConexionPrueba) RecibidosDeTipo(t protocolo.TipoMensajeMesa) []protocolo.MensajeMesa {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []protocolo.MensajeMesa
	for _, m := range c.recibidos {
		if m.Tipo == t {
			out = append(out, m)
		}
	}
	return out
}

// UltimoMensaje devuelve el último mensaje recibido y si existía alguno.
func (c *ConexionPrueba) UltimoMensaje() (protocolo.MensajeMesa, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recibidos) == 0 {
		return protocolo.MensajeMesa{}, false
	}
	return c.recibidos[len(c.recibidos)-1], true
}

// CartasRecibidas devuelve las cartas privadas del último MsgManoInicio. Es la
// forma de comprobar que la mesa nunca filtra cartas ajenas.
func (c *ConexionPrueba) CartasRecibidas() (protocolo.Mano, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.recibidos) - 1; i >= 0; i-- {
		if c.recibidos[i].Tipo == protocolo.MsgManoInicio && c.recibidos[i].Cartas != nil {
			return *c.recibidos[i].Cartas, true
		}
	}
	return protocolo.Mano{}, false
}

// ProgramarAcciones reemplaza la cola de acciones pendientes.
func (c *ConexionPrueba) ProgramarAcciones(acciones ...protocolo.Accion) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acciones = append([]protocolo.Accion(nil), acciones...)
}
