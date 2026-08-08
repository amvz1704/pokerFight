// Este paquete funciona como el guía de una partida. Aunque depende de protocolo
// y de crupier, no depende de los bots.
// Se encargará de manejar las ciegas, los turnos, los saldos y las conexiones de
// los jugadores. Asimismo, maneja el ritmo del juego, invocando al crupier cuando
// sea necesario.
package mesa

import (
	"context"
	"fmt"
	"sync"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

type JugadorInterface interface {
	AsignarCartasPrivadas(mano protocolo.Mano)
	SolicitarApuesta()
}

// Es la representación de un jugador en la mesa. Contiene los datos necesarios
// para que pueda jugar y ser identificado
type Jugador struct {
	ID             string              // El identificador del jugador
	Nombre         string              // El nombre a mostrar del jugador
	Saldo          uint64              // El saldo actual del jugador (mayor o igual a 0)
	ApuestaRonda   uint64              // La apuesta realizada en la ronda actual (mayor o igual a 0)
	CartasPrivadas protocolo.Mano      // La mano (cartas privadas) del jugador. Nota: Este tipo de dato es una estructura, se debe inicializar con 0 antes de inciar la ronda para indicar que el jugador aún no tiene cartas.
	Activo         bool                // Verdadero si el jugador aún puede participar en el juego
	AllIn          bool                // Verdadero si el jugador ha realizado un all in en la ronda actual. Si es verdadero el jugador no puede realizar más acciones en la ronda actual.
	Silla          int8                // Es la posición del jugador en la mesa, va del 0 a MaxJugadores - 1, en sentido horario. Si el jugador no está en la mesa (desconectado), es -1. La silla 0 es del dealer (jugador), la silla 1 de la ciega menor y la silla 2 de la ciega mayor (si hay tres o más jugadores).
	Conexion       ConexionMesaJugador // Es la conexion de la mesa con el jugador. Nota: Este tipo de dato es una interfaz.
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
	LevantarJugador(idJugador string) error                                            // Levanta a un jugador de la mesa y lo desconecta. Si el jugador no está sentado, devuelve un error.
	Jugar(ctx context.Context) (ResumenPartida, error)                                 // Inicia la partida y corre las rondas hasta que se cumpla la condición de finalización. Devuelve el resumen de la partida o un error si la partida no pudo completarse. La partida puede ser cancelada mediante el contexto e igualmente devolver un resumen.
	Estado() protocolo.EstadoPublico
}

// Estructura que representa a la mesa de juego.
type Mesa struct {
	ID          string          // El identificador de la mesa
	CfgMesa     ConfigMesa      // Configuración de la mesa
	CfgPartida  ConfigPartida   // Configuración de la partida
	CrupierMesa crupier.Crupier // El crupier de la mesa. Contiene la lógica del juego.
	Jugadores   []*Jugador      // Lista de los jugadores sentados en la mesa. Modificable en tiempo real. Protegida por Mu.
	Boton       uint8           // La posición del jugador que tiene el botón (dealer) en la mesa. Se actualiza en cada ronda.
	Pozo        *crupier.Pozo

	// Mu protege a Jugadores. Hace falta porque el Servidor sienta y levanta
	// jugadores desde la goroutine de cada conexión mientras Jugar corre en
	// otra. Todo acceso a Jugadores debe tomarla, incluido el de Jugar,
	// Estado y ObtenerJugadoresActivos.
	Mu sync.Mutex
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
		Pozo:        nil,
	}
}

// SentarJugador sienta a un jugador (bot) a la mesa de juego. Si no hay lista de jugadores disponible, devuelve error. Si la mesa está
// llena, devuelve error. Si el jugador ya estaba sentado, devuelve error.
// TODO: Si es posible, usar un map o reducir la complejidad a O(log(N))
func (m *Mesa) SentarJugador(idJugador string, nombreJugador string, c ConexionMesaJugador) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()

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

// TODO: Igualmente, buscar la forma de reducir la complejidad a O(log(N))
func (m *Mesa) LevantarJugador(idJugador string) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	// Variable para realizar la búsqueda
	posicionJugador := -1
	// Se recorre la lista de jugadores en busca del ID del jugador a levantar
	for i, jugador := range m.Jugadores {
		if jugador != nil {
			if jugador.ID == idJugador {
				posicionJugador = i
				break
			}
		}
	}
	// En caso de que el jugador no se encuentre en la mesa
	if posicionJugador == -1 {
		return fmt.Errorf("Error: No se puede levantar al jugador [ID: %s] de la mesa [ID: %s] porque el jugador no se encuentra en la mesa.\n", idJugador, m.ID)
	}
	// Se cierra la conexión del jugador con la mesa y se elimina el puntero
	m.Jugadores[posicionJugador].Conexion.Cerrar()
	m.Jugadores[posicionJugador] = nil
	return nil
}

// Esta función es el bucle que dará inicio a la partida en la mesa.
func (m *Mesa) Jugar(ctx context.Context) (ResumenPartida, error) {
	// Mensaje de inicio
	fmt.Printf("Mesa [ID: %s]: Iniciando juego.\n", m.ID)

	// Variable de resumen de la partida
	resumen := ResumenPartida{
		IDMesa: m.ID,
	}

	rondaActual := int64(0)

	// Loop de la partida
	for {
		// Vemos si hay alguna señal por procesar
		select {
		case <-ctx.Done():
			return resumen, fmt.Errorf("Mesa [ID: %s]: La partida ha sido cancelada por un evento externo.\n", m.ID)
		default:
			// Seguir la partida
		}

		// Si la partida debe finalizar por la cantidad de rondas
		if m.CfgPartida.CantidadRondas != -1 && rondaActual >= m.CfgPartida.CantidadRondas {
			break
		}
		// Obtener los jugadores activos
		m.Mu.Lock()
		jugadoresActivos := m.ObtenerJugadoresActivos()
		m.Mu.Unlock()

		// Si solo queda un jugador activo, es el ganador
		if len(jugadoresActivos) <= 1 {
			break
		}

		// Preparamos la mano a repartir a los jugadores
		idMano := fmt.Sprintf("Mesa:%s-Ronda:%d", m.ID, rondaActual+1)

		// Obtenemos la mano y verificamos que no hayan errores de creación
		if err := m.CrupierMesa.NuevaMano(idMano); err != nil {
			return resumen, fmt.Errorf("Error en Mesa [ID: %s]: El crupier no pudo iniciar la Mano [ID: %s].\n%w\n", m.ID, idMano, err)
		}

		// Obtenemos los IDs de los jugadores activos y creamos el pozo
		idsActivos := ObtenerIdsDeJugadores(jugadoresActivos)
		m.Pozo = crupier.NuevoPozo(idsActivos)

		// Repartimos las cartas y cobramos las ciegas
		manos, err := m.CrupierMesa.RepartirPrivadas(len(idsActivos))
		if err != nil {
			return resumen, fmt.Errorf("Mesa [ID: %s]: Error al repartir las cartas privadas. %w\n", m.ID, err)
		}

		m.Mu.Lock()
		for i, jugador := range jugadoresActivos {
			jugador.CartasPrivadas = manos[i]
		}
		m.Mu.Unlock()

		// Apuestas comunitarias (Pre-Flop, Flop, Turn, River)
		// TODO: Here

		// Repartición del pozo
		// TODO: Here

		// Preparar la siguiente ronda
		m.Boton = (m.Boton + 1) % m.CfgMesa.MaxJugadores // NOTA: ¿Qué pasa si la silla está vacia?
		rondaActual++

		resumen.CantidadRondasJugadas = uint64(rondaActual)
	}
	return ResumenPartida{}, nil
}

func (m *Mesa) Estado() protocolo.EstadoPublico {
	panic("no implementado: ver docs/interfaces.md, tarea Mesa #1")
}

// ObtenerJugadoresActivos requiere que quien la llame ya tenga tomado Mu.
func (m *Mesa) ObtenerJugadoresActivos() []*Jugador {
	// Creamos el slice con 0 elementos pero capacidad para N jugadores.
	activos := make([]*Jugador, 0, len(m.Jugadores))

	// Recorremos la lista para buscar los jugadores activos
	for _, jugador := range m.Jugadores {
		if jugador != nil && jugador.Activo {
			activos = append(activos, jugador)
		}
	}

	return activos
}

// Función para obtener el ID de un array de jugadores
func ObtenerIdsDeJugadores(jugadores []*Jugador) []string {
	if len(jugadores) == 0 {
		return nil
	}

	ids := make([]string, 0, len(jugadores))

	for _, jugador := range jugadores {
		if jugador != nil {
			ids = append(ids, jugador.ID)
		}
	}

	return ids
}
