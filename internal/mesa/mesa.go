// Este paquete funciona como el guía de una partida. Aunque depende de protocolo
// y de crupier, no depende de los bots.
// Se encargará de manejar las ciegas, los turnos, los saldos y las conexiones de
// los jugadores. Asimismo, maneja el ritmo del juego, invocando al crupier cuando
// sea necesario.
package mesa

import (
	"context"
	"fmt"

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

func NuevoJugador(id string, nombre string, saldo uint64, silla int8, c ConexionMesaJugador) *Jugador {
	return &Jugador{
		ID:             id,
		Nombre:         nombre,
		Saldo:          saldo,
		ApuestaRonda:   0,
		CartasPrivadas: protocolo.Mano{}, // Se intancian las cartas como 2 cartas vacías. Se asignarán al iniciar una ronda.
		Activo:         true,             // Inica activo, se desactiva si se retira o pierde todas sus fichas.
		AllIn:          false,            // Inicia sin all in, se activa si realiza un all in en la ronda.
		Silla:          silla,            // Su posición en el arreglo de jugadores de la mesa.
		Conexion:       c,
	}
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
		ID:          id,                                     // ID definido por el casino, único para cada mesa.
		CfgMesa:     cfgMesa,                                // Configuración definida antes de crear la mesa.
		CfgPartida:  cfgPartida,                             // Configuración de la partida definida antes de crear la mesa.
		CrupierMesa: cpr,                                    // Un crupier que tenga la lógica del juego. Permite modificaciones al poker clásico para futuras implementaciones.
		Jugadores:   make([]*Jugador, cfgMesa.MaxJugadores), // Vacío al inicio, se agregan jugadores a medida que se sientan en la mesa.
		Boton:       0,                                      // Valor por defecto, se actualiza en cada ronda.
		Pozo:        nil,                                    // Nulo al inicio, se crea al iniciar la partida y se mantiene todo el juego.
	}
}

// SentarJugador sienta a un jugador (bot) a la mesa de juego. Si no hay lista de jugadores disponible, devuelve error. Si la mesa está
// llena, devuelve error. Si el jugador ya estaba sentado, devuelve error.
func (m *Mesa) SentarJugador(idJugador string, nombreJugador string, c ConexionMesaJugador) error {
	// Para evitar desreferenciar un puntero nulo
	if m.Jugadores == nil {
		return fmt.Errorf("Error: La mesa [ID: %s] no ha sido correctamente inicializada (La lista de jugadores no existe).\n", m.ID)
	}
	// Inicializamos una variable para buscar la primera silla libre.
	sillaLibre := -1
	// Recorremos todas las sillas para evitar que se una un jugador 2 veces. También, para buscar la silla libre.
	for i, jugador := range m.Jugadores {
		// Si la silla no está vacia, revisamos que el jugador no se vaya a sentar 2 veces.
		if jugador != nil {
			if jugador.ID == idJugador {
				return fmt.Errorf("Error: El jugador %s [ID: %s] ya está sentado en la mesa [ID: %s].\n", nombreJugador, idJugador, m.ID)
			}
		} else if sillaLibre == -1 { // Si la silla está vacia, se guarda la primera silla libre que se encuentre.
			sillaLibre = i
		}
	}
	// Si no habían sillas libres
	if sillaLibre == -1 {
		return fmt.Errorf("Error: La mesa [ID: %s] está llena. El jugador %s [ID: %s] no pudo sentarse.\n", m.ID, nombreJugador, idJugador)
	}
	// Sentamos al jugador
	m.Jugadores[sillaLibre] = NuevoJugador(idJugador, nombreJugador, m.CfgPartida.StackInicial, int8(sillaLibre), c)
	fmt.Printf("Mesa [ID: %s]: El jugador %s [ID: %s] se ha sentado en la silla %d.\n", m.ID, nombreJugador, idJugador, sillaLibre)
	return nil
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
