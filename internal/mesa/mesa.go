// Este paquete funciona como el guía de una partida. Aunque depende de protocolo
// y de crupier, no depende de los bots.
// Se encargará de manejar las ciegas, los turnos, los saldos y las conexiones de
// los jugadores. Asimismo, maneja el ritmo del juego, invocando al crupier cuando
// sea necesario.
package mesa

import (
	"context"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Es la representación de un jugador en la mesa. Contiene los datos necesarios
// para que pueda jugar y ser identificado
type Jugador struct {
	ID             string              // El identificador del jugador
	Nombre         string              // El nombre a mostrar del jugador
	Saldo          uint64              // El saldo actual del jugador (mayor o igual a 0)
	ApuestaRonda   uint64              // La apuesta realizada en la ronda actual (mayor o igual a 0)
	CartasPrivadas protocolo.Mano      // La mano (cartas privadas) del jugador
	Activo         bool                // Verdadero si el jugador aún puede participar en el juego
	AllIn          bool                // Verdadero si el jugador ha realizado un all in en la ronda actual. Si es verdadero el jugador no puede realizar más acciones en la ronda actual.
	Silla          int8                // Es la posición del jugador en la mesa, va del 0 a MaxJugadores - 1, en sentido horario. Si el jugador no está en la mesa (desconectado), es -1. La silla 0 es del dealer (jugador), la silla 1 de la ciega menor y la silla 2 de la ciega mayor (si hay tres o más jugadores).
	Conexion       ConexionMesaJugador // Es la conexion de la mesa con el jugador.
}

// Configuración de la mesa. Se define al crear la mesa y no cambia durante la partida.
type ConfigMesa struct {
	MaxJugadores    uint8  // El juego tendría como máximo 8
	MinJugadores    uint8  // El juego tendría como mínimo 2
	ActualJugadores uint8  // La cantidad de jugadores conectados actualmente
	Timeout         uint64 // Tiempo máximo (en ms) que tiene un bot para realizar un movimiento
}

// Configuración de la partida. Se define al crear la mesa y no cambia durante la partida.
type ConfigPartida struct {
	CiegaMenor     uint64 // La ciega menor de la partida
	CiegaMayor     uint64 // La ciega mayor de la partida, que debe ser el doble de la ciega menor.
	StackInicial   uint64 // La cantidad de fichas con las que inicia un jugador al entrar a la mesa
	CantidadRondas int64  // La cantidad de rondas a jugar. Si es -1, se juega hasta que quede un solo jugador Activo. En cualquier otro caso, debe ser mayor a 0.
}

type ResumenJugador struct {
	IDJugador  string // El identificador del jugador
	Posicion   int    // La posición final del jugador en la partida. 1 es el ganador y MaxJugadores es el último.
	SaldoFinal uint64 // EL saldo final del jugador al terminar la partida.
}

// Es el resumen (reporte) del juego. Lo envía Mesa a Casino al finalizar una partida.
type ResumenPartida struct {
	IDMesa                string           // El identificador de la mesa
	CantidadRondasJugadas uint64           // La cantidad de rondas jugadas en la partida
	Posiciones            []ResumenJugador // Los datos de la posición final de cada jugador en la partida.
}

// Es la interfaz que representa a la mesa de juego y la hace independiente de la implementación de la mesa. Se usa para
// representar las funciones de una mesa y para facilitar la integración con Casino y con las pruebas.
type MesaInterface interface {
	SentarJugador(idJugador string, nombreJugador string, c ConexionMesaJugador) error // Sienta a un jugador (bot) a una mesa de juego. Si la mesa está llena, devuelve un error. Si el jugador ya está sentado y conectado, devuelve un error. Si el jugador está sentado pero desconectado, lo vuelve a conectar. Si el jugador no está sentado, lo sienta en la siguiente silla disponible.
	LevantarJugador(idJugador string) error                                            // Levanta a un jugador de la mesa de jugado, pero aún no lo desconecta. Es decir, puede permanecer como espectador. Si el jugador no está sentado, devuelve un error.
	Jugar(ctx context.Context) (ResumenPartida, error)                                 // Inicia la partida y corre las rondas hasta que se cumpla la condición de finalización. Devuelve el resumen de la partida o un error si la partida no pudo completarse. La partida puede ser cancelada mediante el contexto e igualmente devolver un resumen.
	Estado() protocolo.EstadoPublico
}

// Estructura que representa a la mesa de juego.
type Mesa struct {
	ID          string          // El identificador de la mesa
	CfgMesa     ConfigMesa      // Configuración de la mesa
	CfgPartida  ConfigPartida   // Configuración de la partida
	CrupierMesa crupier.Crupier // El crupier de la mesa. Contiene la lógica del juego.
	Jugadores   []*Jugador      // Lista de los jugadores sentados en la mesa. Modificable en tiempo real.
	Boton       uint8           // La posición del jugador que tiene el botón (dealer) en la mesa. Se actualiza en cada ronda.
	Pozo        *crupier.Pozo   // El pozo de la mesa. Se actualiza en cada ronda y etapa de la ronda.
}

// Función para crear una nueva mesa. Devuelve una instancia de MesaInterface (un puntero a Mesa).
func NuevaMesa(id string, cfgMesa ConfigMesa, cfgPartida ConfigPartida, cpr crupier.Crupier) MesaInterface {
	return &Mesa{
		ID:          id,         // ID definido por el casino, único para cada mesa.
		CfgMesa:     cfgMesa,    // Configuración definida antes de crear la mesa.
		CfgPartida:  cfgPartida, // Configuración de la partida definida antes de crear la mesa.
		CrupierMesa: cpr,        // Un crupier que tenga la lógica del juego. Permite modificaciones al poker clásico para futuras implementaciones.
		Jugadores:   nil,        // Nulo al inicio, se van agregando jugadores a medida que se sientan en la mesa.
		Boton:       0,          // Valor por defecto, se actualiza en cada ronda.
		Pozo:        nil,        // Nulo al inicio, se crea al iniciar la partida y se mantiene todo el juego.
	}
}

// TODO: Implmentar las funciones de MesaInterface para Mesa.
func (m *Mesa) SentarJugador(idJugador string, nombreJugador string, c ConexionMesaJugador) error {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #3")
}

func (m *Mesa) LevantarJugador(idJugador string) error {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #3")
}

func (m *Mesa) Jugar(ctx context.Context) (ResumenPartida, error) {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #1")
}

func (m *Mesa) Estado() protocolo.EstadoPublico {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #1")
}
