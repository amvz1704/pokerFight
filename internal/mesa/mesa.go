// Este paquete funciona como el guía de una partida. Aunque depende de protocolo
// y de crupier, no depende de los bots.
// Se encargará de manejar las ciegas, los turnos, los saldos y las conexiones de
// los jugadores. Asimismo, maneja el ritmo del juego, invocando al crupier cuando
// sea necesario.
package mesa

import (
	"context"
	"fmt"
	"sort"
	"sync"

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
	CartasPrivadas protocolo.Mano      // La mano (cartas privadas) del jugador. Nota: Este tipo de dato es una estructura, se debe inicializar con 0 antes de inciar la ronda para indicar que el jugador aún no tiene cartas.
	Activo         bool                // Verdadero si el jugador aún puede participar en el juego (sigue en el torneo, no se quedó sin fichas ni se retiró de la mesa)
	Retirado       bool                // Verdadero si el jugador hizo fold en la mano actual. Se reinicia a false al repartir cada mano nueva.
	AllIn          bool                // Verdadero si el jugador ha realizado un all in en la ronda actual. Si es verdadero el jugador no puede realizar más acciones en la ronda actual.
	Silla          int8                // Es la posición del jugador en la mesa, va del 0 a MaxJugadores - 1, en sentido horario. Si el jugador no está en la mesa (desconectado), es -1. La silla 0 es del dealer (jugador), la silla 1 de la ciega menor y la silla 2 de la ciega mayor (si hay tres o más jugadores).
	Conexion       ConexionMesaJugador // Es la conexion de la mesa con el jugador. Nota: Este tipo de dato es una interfaz.

	// EnMano indica si el jugador recibió cartas en la mano en curso. Un
	// jugador que se sienta a mitad de mano queda con EnMano=false y espera
	// a la siguiente: no se le pide acción ni participa del pozo. Se
	// recalcula al empezar cada mano.
	EnMano bool

	// Timeouts cuenta cuántas veces la Mesa tuvo que aplicar
	// protocolo.AccionSegura en lugar de la respuesta del bot (plazo vencido,
	// conexión rota o acción inválida) a lo largo de toda la partida. Viaja al
	// resumen final para que el Casino pueda descontar puntos
	// (docs/interfaces.md §2).
	Timeouts uint64

	// ManosJugadas cuenta las manos en las que recibió cartas. Sirve para las
	// estadísticas del torneo y para detectar bots que se conectan tarde.
	ManosJugadas uint64

	// NetoAcumulado son las fichas ganadas o perdidas a lo largo de toda la
	// partida. Solo lo usa el modo ConfigPartida.ReponerStack: sin reposición,
	// el neto es simplemente Saldo - StackInicial y este campo queda en 0.
	NetoAcumulado int64
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
	ActualJugadores uint8  // La cantidad de jugadores conectados actualmente. La mantiene la Mesa: no la escribas desde afuera.
	Timeout         uint64 // Tiempo máximo (en ms) que tiene un bot para realizar un movimiento

	// Observador, si no es nil, recibe cada mano terminada. Es el punto de
	// extensión para historiales de mano, replays o telemetría: la Mesa no
	// sabe qué se hace con eso (ver internal/arena/historial.go).
	Observador ObservadorMano
}

// ManoJugada es una mano ya terminada, con todo lo necesario para
// reconstruirla. Se pasa como estructura y no como lista de parámetros para
// poder agregarle campos sin romper a quien ya implemente ObservadorMano.
type ManoJugada struct {
	IDMesa    string
	IDMano    string
	Estado    protocolo.EstadoPublico // Incluye comunitarias, pozo e historial de acciones.
	Resultado protocolo.ResultadoMano

	// Privadas son las cartas de TODOS los que jugaron la mano, no solo las de
	// quienes llegaron al showdown (que son las de Resultado.Mostradas).
	//
	// Es información que jamás puede llegar a un bot. Existe para el historial
	// de auditoría: una mano que termina en fold no revela nada por protocolo,
	// y sin esto no habría forma de responder a un reclamo sobre esa mano.
	Privadas map[string]protocolo.Mano
}

// ObservadorMano recibe el cierre de cada mano jugada en la mesa. La Mesa lo
// invoca de forma síncrona y sin tener tomado Mu, así que una implementación
// lenta frena la partida: si hay que escribir a disco, hacerlo con buffer.
//
// Lo que recibe incluye las cartas privadas de todos los jugadores: un
// observador nunca debe reenviarlas a un bot.
type ObservadorMano interface {
	ManoTerminada(mano ManoJugada)
}

// Configuración de la partida. Se define al crear la mesa y no cambia durante la partida.
type ConfigPartida struct {
	CiegaMenor     uint64 // La ciega menor de la partida
	CiegaMayor     uint64 // La ciega mayor de la partida, que debe ser el doble de la ciega menor.
	StackInicial   uint64 // La cantidad de fichas con las que inicia un jugador al entrar a la mesa
	CantidadRondas int64  // La cantidad de rondas a jugar. Si es -1, se juega hasta que quede un solo jugador Activo. En cualquier otro caso, debe ser mayor a 0.

	// ReponerStack devuelve a todos los jugadores al StackInicial antes de cada
	// mano, y lleva aparte cuánto ganó o perdió cada uno (Jugador.NetoAcumulado).
	//
	// Cambia qué mide la partida, y por eso existe:
	//
	//   - Sin reposición, la partida es un torneo: se juega hasta que alguien
	//     queda sin fichas, y CantidadRondas es apenas un techo que casi nunca
	//     se alcanza. Lo que se mide es quién quebró primero.
	//   - Con reposición, las manos son independientes y se juegan todas. Lo
	//     que se mide es cuántas fichas de ventaja saca un bot por mano, que es
	//     mucho menos ruidoso. Es el modo que usa el round-robin del torneo, y
	//     el mismo criterio de MIT Pokerbots.
	//
	// Exige CantidadRondas > 0: sin eliminación posible, una partida sin límite
	// de manos no termina nunca.
	ReponerStack bool
}

// ResumenJugador es la posición final de un jugador al terminar la partida.
type ResumenJugador struct {
	IDJugador    string // El identificador del jugador
	Nombre       string // El nombre a mostrar del jugador
	Posicion     int    // La posición final del jugador en la partida. 1 es el ganador y MaxJugadores es el último.
	SaldoFinal   uint64 // El saldo final del jugador al terminar la partida.
	SaldoInicial uint64 // Las fichas con las que arrancó cada mano.
	// Neto son las fichas ganadas (positivo) o perdidas (negativo) en toda la
	// partida. Es el número por el que se clasifica, y el que hay que mirar
	// en lugar de SaldoFinal: con ConfigPartida.ReponerStack, SaldoFinal es
	// solo el resultado de la última mano.
	Neto         int64
	Timeouts     uint64 // Cuántas veces se le aplicó la acción segura por no responder o responder algo inválido.
	ManosJugadas uint64 // En cuántas manos recibió cartas.
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
	// SentarJugador sienta a un jugador (bot) en la siguiente silla libre y le
	// da el StackInicial. Falla si la mesa está llena o si ese ID ya está
	// sentado (para volver a conectar a un jugador ya sentado, usar
	// ReconectarJugador).
	SentarJugador(idJugador string, nombreJugador string, c ConexionMesaJugador) error
	// SentarJugadorEnSilla sienta a un jugador en una silla concreta. Falla si
	// la silla no existe, ya está ocupada por otro, o el jugador ya está
	// sentado en otra.
	//
	// Existe porque "la primera silla libre" es una asignación por orden de
	// llegada, y el orden de llegada de unos bots que arrancan en paralelo es
	// una carrera. Quien organiza un torneo necesita decidir las posiciones:
	// jugar el mismo emparejamiento con las sillas intercambiadas no sirve de
	// nada si las sillas las reparte el azar de qué proceso conectó primero.
	SentarJugadorEnSilla(idJugador string, nombreJugador string, silla int, c ConexionMesaJugador) error
	// ReconectarJugador reemplaza la conexión de un jugador que ya está
	// sentado, sin tocar su saldo ni su silla. Es lo que permite que un bot que
	// se cayó vuelva a la misma partida. Falla si el jugador no está sentado.
	ReconectarJugador(idJugador string, c ConexionMesaJugador) error
	// LevantarJugador levanta a un jugador de la mesa y cierra su conexión. Si
	// el jugador no está sentado, devuelve un error.
	LevantarJugador(idJugador string) error
	// Jugar inicia la partida y corre las manos hasta que se cumpla la
	// condición de finalización (límite de rondas o un solo jugador con
	// fichas). Devuelve el resumen de la partida. Se puede cancelar por
	// contexto: en ese caso devuelve el resumen igual, junto al error.
	Jugar(ctx context.Context) (ResumenPartida, error)
	// Estado es la foto pública de la mesa, apta para enviar a un bot.
	Estado() protocolo.EstadoPublico
	// Sentados es cuántas sillas están ocupadas ahora mismo.
	Sentados() int
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

	// --- Estado de la mano en curso, expuesto vía Estado() ------------------
	idManoActual  string
	etapaActual   protocolo.Etapa
	comunitarias  []protocolo.Carta
	apuestaActual uint64 // Apuesta más alta de la ronda de apuestas actual.
	subidaMinima  uint64 // Incremento mínimo exigido para el próximo Raise.
	historial     []protocolo.AccionRegistrada

	// ordenEliminacion guarda los IDs en el orden en que se quedaron sin
	// fichas. Es lo que permite armar una clasificación de torneo correcta:
	// entre dos jugadores con 0 fichas, gana el puesto más alto el que
	// aguantó más (el último eliminado).
	ordenEliminacion []string

	// Mu protege todos los campos de arriba. Hace falta porque el Servidor
	// sienta y levanta jugadores desde la goroutine de cada conexión mientras
	// Jugar corre en otra, y porque Estado() se puede llamar en cualquier
	// momento desde afuera. Todo acceso debe tomarla, incluido el de Jugar,
	// Estado y ObtenerJugadoresActivos.
	Mu sync.Mutex
}

// Verificación en tiempo de compilación de que cumple el contrato.
var _ MesaInterface = (*Mesa)(nil)

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
		return fmt.Errorf("mesa [ID: %s]: no ha sido correctamente inicializada (la lista de jugadores no existe)", m.ID)
	}
	// Inicializamos una variable para buscar la primera silla libre.
	sillaLibre := -1
	// Recorremos todas las sillas para evitar que se una un jugador 2 veces. También, para buscar la silla libre.
	for i, jugador := range m.Jugadores {
		// Si la silla no está vacia, revisamos que el jugador no se vaya a sentar 2 veces.
		if jugador != nil {
			if jugador.ID == idJugador {
				return fmt.Errorf("mesa [ID: %s]: el jugador %s [ID: %s] ya está sentado", m.ID, nombreJugador, idJugador)
			}
		} else if sillaLibre == -1 { // Si la silla está vacia, se guarda la primera silla libre que se encuentre.
			sillaLibre = i
		}
	}
	// Si no habían sillas libres
	if sillaLibre == -1 {
		return fmt.Errorf("mesa [ID: %s]: está llena, el jugador %s [ID: %s] no pudo sentarse", m.ID, nombreJugador, idJugador)
	}
	// Sentamos al jugador
	m.Jugadores[sillaLibre] = NuevoJugador(idJugador, nombreJugador, m.CfgPartida.StackInicial, int8(sillaLibre), c)
	m.CfgMesa.ActualJugadores = uint8(m.sentadosSinLock())
	return nil
}

// SentarJugadorEnSilla sienta a un jugador en la silla indicada. Ver el
// comentario de MesaInterface para por qué existe.
func (m *Mesa) SentarJugadorEnSilla(idJugador string, nombreJugador string, silla int, c ConexionMesaJugador) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	if silla < 0 || silla >= len(m.Jugadores) {
		return fmt.Errorf("mesa [ID: %s]: la silla %d no existe (la mesa tiene %d)", m.ID, silla, len(m.Jugadores))
	}
	for _, jugador := range m.Jugadores {
		if jugador != nil && jugador.ID == idJugador {
			return fmt.Errorf("mesa [ID: %s]: el jugador %s [ID: %s] ya está sentado", m.ID, nombreJugador, idJugador)
		}
	}
	if ocupante := m.Jugadores[silla]; ocupante != nil {
		return fmt.Errorf("mesa [ID: %s]: la silla %d ya está ocupada por [ID: %s]", m.ID, silla, ocupante.ID)
	}

	m.Jugadores[silla] = NuevoJugador(idJugador, nombreJugador, m.CfgPartida.StackInicial, int8(silla), c)
	m.CfgMesa.ActualJugadores = uint8(m.sentadosSinLock())
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
		return fmt.Errorf("mesa [ID: %s]: no se puede levantar al jugador [ID: %s] porque no se encuentra en la mesa", m.ID, idJugador)
	}
	// Se cierra la conexión del jugador con la mesa y se elimina el puntero
	m.Jugadores[posicionJugador].Conexion.Cerrar()
	m.Jugadores[posicionJugador] = nil
	m.CfgMesa.ActualJugadores = uint8(m.sentadosSinLock())
	return nil
}

// ReconectarJugador reemplaza la conexión de un jugador que ya está sentado.
// Conserva su saldo, su silla y su estado en la mano en curso: para la partida
// es el mismo jugador, solo que ahora se le habla por otro socket.
//
// Existe separada de SentarJugador a propósito. Sentar dos veces al mismo ID
// es un error (dos bots con el mismo token intentando ocupar dos sillas);
// reconectar es una operación distinta y explícita, que además cierra el
// socket viejo para que no queden dos conexiones vivas contra el mismo
// jugador. Ver docs/protocolo.md, sección "Reconexión".
func (m *Mesa) ReconectarJugador(idJugador string, c ConexionMesaJugador) error {
	m.Mu.Lock()
	var anterior ConexionMesaJugador
	encontrado := false
	for _, jugador := range m.Jugadores {
		if jugador == nil || jugador.ID != idJugador {
			continue
		}
		if jugador.Conexion != c {
			anterior = jugador.Conexion
		}
		jugador.Conexion = c
		encontrado = true
		break
	}
	m.Mu.Unlock()

	if !encontrado {
		return fmt.Errorf("mesa [ID: %s]: el jugador [ID: %s] no está sentado, no hay nada que reconectar", m.ID, idJugador)
	}

	// El socket viejo se cierra fuera de Mu. Cerrar una ConexionTCP toma el
	// mutex de esa conexión, que puede estar tomado por un SolicitarAccion en
	// curso: hacerlo con Mu tomada frenaría la mesa entera durante todo el
	// plazo que el bot tenga para pensar.
	if anterior != nil {
		anterior.Cerrar()
	}
	return nil
}

// Sentados devuelve cuántas sillas están ocupadas. Es lo que mira quien
// orquesta la mesa para saber si ya llegó el mínimo de jugadores.
func (m *Mesa) Sentados() int {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.sentadosSinLock()
}

// sentadosSinLock requiere Mu tomado.
func (m *Mesa) sentadosSinLock() int {
	n := 0
	for _, j := range m.Jugadores {
		if j != nil {
			n++
		}
	}
	return n
}

// Esta función es el bucle que dará inicio a la partida en la mesa.
func (m *Mesa) Jugar(ctx context.Context) (ResumenPartida, error) {
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
			return m.resumenFinal(resumen), fmt.Errorf("mesa [ID: %s]: la partida fue cancelada: %w", m.ID, ctx.Err())
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

		if err := m.jugarMano(ctx, jugadoresActivos, rondaActual+1); err != nil {
			return m.resumenFinal(resumen), err
		}

		// Preparar la siguiente ronda: el botón pasa al siguiente jugador que
		// sigue en el torneo (salta sillas vacías o de jugadores eliminados).
		m.Mu.Lock()
		if siguiente, ok := m.siguienteSillaOcupada(m.Boton); ok {
			m.Boton = siguiente
		}
		m.Mu.Unlock()

		rondaActual++
		resumen.CantidadRondasJugadas = uint64(rondaActual)
	}
	return m.resumenFinal(resumen), nil
}

// jugarMano corre una mano completa: ciegas, preflop, flop, turn, river y
// reparto del pozo. No avanza el botón: eso lo hace el llamador (Jugar).
//
// jugadoresActivos es la foto de quiénes siguen en el torneo al empezar la
// mano. Esa foto es la que manda: si alguien se sienta mientras la mano corre,
// queda con EnMano=false y espera a la siguiente. Sin eso, un bot que se
// conecta a mitad de mano recibiría turno sin tener cartas ni fichas en el pozo.
func (m *Mesa) jugarMano(ctx context.Context, jugadoresActivos []*Jugador, numeroMano int64) error {
	idMano := fmt.Sprintf("%s-mano-%d", m.ID, numeroMano)

	if err := m.CrupierMesa.NuevaMano(idMano); err != nil {
		return fmt.Errorf("mesa [ID: %s]: el crupier no pudo iniciar la mano [ID: %s]: %w", m.ID, idMano, err)
	}

	idsActivos := ObtenerIdsDeJugadores(jugadoresActivos)

	m.Mu.Lock()
	m.Pozo = crupier.NuevoPozo(idsActivos)
	m.idManoActual = idMano
	m.etapaActual = protocolo.PreFlop
	m.comunitarias = nil
	m.historial = nil
	// Solo los de la foto entran a la mano. El resto (sillas vacías o
	// jugadores recién sentados) quedan fuera hasta la próxima.
	for _, j := range m.Jugadores {
		if j != nil {
			j.EnMano = false
		}
	}
	for _, j := range jugadoresActivos {
		j.Retirado = false
		j.AllIn = false
		j.ApuestaRonda = 0
		j.EnMano = true
		j.ManosJugadas++
		if m.CfgPartida.ReponerStack {
			// Se anota lo que quedó de la mano anterior y se vuelve al stack
			// inicial: cada mano arranca en igualdad de condiciones.
			j.NetoAcumulado += int64(j.Saldo) - int64(m.CfgPartida.StackInicial)
			j.Saldo = m.CfgPartida.StackInicial
		}
	}
	m.Mu.Unlock()

	manos, err := m.CrupierMesa.RepartirPrivadas(len(idsActivos))
	if err != nil {
		return fmt.Errorf("mesa [ID: %s]: error al repartir las cartas privadas: %w", m.ID, err)
	}

	m.Mu.Lock()
	for i, jugador := range jugadoresActivos {
		jugador.CartasPrivadas = manos[i]
	}

	sillaChica, sillaGrande, okCiegas := m.posicionesCiegas(len(jugadoresActivos))
	if !okCiegas {
		m.Mu.Unlock()
		return fmt.Errorf("mesa [ID: %s]: no se pudieron determinar las ciegas de la mano [ID: %s]", m.ID, idMano)
	}
	m.aportar(m.Jugadores[sillaChica], m.CfgPartida.CiegaMenor)
	m.aportar(m.Jugadores[sillaGrande], m.CfgPartida.CiegaMayor)
	m.apuestaActual = m.CfgPartida.CiegaMayor
	m.subidaMinima = m.CfgPartida.CiegaMayor
	primerEnHablar := m.primerEnHablarPreflop(len(jugadoresActivos), sillaChica, sillaGrande)
	m.Mu.Unlock()

	m.enviarManoInicio(jugadoresActivos)

	if err := m.rondaApuestas(ctx, protocolo.PreFlop, primerEnHablar); err != nil {
		return err
	}

	for _, etapa := range []protocolo.Etapa{protocolo.Flop, protocolo.Turn, protocolo.River} {
		m.Mu.Lock()
		enMano := m.contarEnMano()
		m.Mu.Unlock()
		if enMano <= 1 {
			break
		}

		cartas, err := m.CrupierMesa.RepartirComunitarias(etapa)
		if err != nil {
			return fmt.Errorf("mesa [ID: %s]: error al repartir comunitarias de %s: %w", m.ID, etapa, err)
		}

		m.Mu.Lock()
		m.comunitarias = append(m.comunitarias, cartas...)
		m.etapaActual = etapa
		for _, j := range jugadoresActivos {
			j.ApuestaRonda = 0
		}
		m.apuestaActual = 0
		m.subidaMinima = m.CfgPartida.CiegaMayor
		primerPost, hayQuienActue := m.primerEnManoDesdeBoton()
		m.Mu.Unlock()

		m.difundirEstado(-1)

		if !hayQuienActue {
			continue
		}
		if err := m.rondaApuestas(ctx, etapa, primerPost); err != nil {
			return err
		}
	}

	return m.repartirPozo(jugadoresActivos, idMano)
}

// repartirPozo llega al showdown (o al cierre por fold de todos menos uno),
// pide al Crupier que decida ganadores y reparte las fichas.
func (m *Mesa) repartirPozo(jugadoresActivos []*Jugador, idMano string) error {
	m.Mu.Lock()
	m.etapaActual = protocolo.Showdown
	// Los participantes van en orden de acción, empezando por la izquierda del
	// botón. El Crupier usa ese orden para decidir a quién le toca la ficha
	// sobrante cuando un pozo no divide exacto (docs/reglas.md, "Reglas de
	// apuesta"), sin necesidad de saber dónde está el botón.
	participantes := make([]crupier.Participante, 0, len(jugadoresActivos))
	sillas := uint8(len(m.Jugadores))
	for i := uint8(1); i <= sillas; i++ {
		j := m.Jugadores[(m.Boton+i)%sillas]
		if j != nil && j.EnMano && !j.Retirado {
			participantes = append(participantes, crupier.Participante{ID: j.ID, Mano: j.CartasPrivadas})
		}
	}
	comunitarias := append([]protocolo.Carta(nil), m.comunitarias...)
	pozo := m.Pozo
	m.Mu.Unlock()

	var resultado protocolo.ResultadoMano
	switch {
	case len(participantes) == 0:
		// No debería pasar: rondaApuestas corta en cuanto queda uno solo en
		// mano. Si pasara (por ejemplo, un fold aplicado fuera de turno por un
		// bug futuro), no se pierden fichas en silencio: se devuelve el error
		// en vez de dejar el pozo sin dueño.
		return fmt.Errorf("mesa [ID: %s]: la mano [ID: %s] terminó sin ningún jugador en pie", m.ID, idMano)

	case len(participantes) == 1:
		// Todos los demás se retiraron: se gana el pozo sin showdown. No se
		// evalúan manos porque puede no haber comunitarias repartidas (fold
		// preflop).
		resultado = protocolo.ResultadoMano{
			IDMano:       idMano,
			Comunitarias: comunitarias,
			Repartos:     []protocolo.Reparto{{IDJugador: participantes[0].ID, Monto: pozo.Total()}},
		}

	default:
		var err error
		resultado, err = m.CrupierMesa.DecidirGanadores(participantes, comunitarias, pozo)
		if err != nil {
			return fmt.Errorf("mesa [ID: %s]: error al decidir ganadores de la mano [ID: %s]: %w", m.ID, idMano, err)
		}
	}

	m.Mu.Lock()
	for _, r := range resultado.Repartos {
		for _, j := range jugadoresActivos {
			if j.ID == r.IDJugador && r.Monto > 0 {
				j.Saldo += uint64(r.Monto)
			}
		}
	}
	// Quien se queda sin fichas sale del torneo (pero no se desconecta: puede
	// quedar de espectador, LevantarJugador es una acción explícita). Se
	// anota el orden de eliminación para que la clasificación final pueda
	// desempatar entre los que terminaron en 0.
	// Con reposición no hay eliminación: el stack vuelve al inicial en la mano
	// siguiente, así que quedarse en 0 es el resultado de una mano, no el fin
	// de la partida.
	if !m.CfgPartida.ReponerStack {
		for _, j := range jugadoresActivos {
			if j.Saldo == 0 && j.Activo {
				j.Activo = false
				m.ordenEliminacion = append(m.ordenEliminacion, j.ID)
			}
		}
	}
	estado := m.estadoSinLock()
	observador := m.CfgMesa.Observador
	var privadas map[string]protocolo.Mano
	if observador != nil {
		privadas = make(map[string]protocolo.Mano, len(jugadoresActivos))
		for _, j := range jugadoresActivos {
			if j.EnMano {
				privadas[j.ID] = j.CartasPrivadas
			}
		}
	}
	m.Mu.Unlock()

	m.enviarManoFin(jugadoresActivos, resultado)
	if observador != nil {
		observador.ManoTerminada(ManoJugada{
			IDMesa:    m.ID,
			IDMano:    idMano,
			Estado:    estado,
			Resultado: resultado,
			Privadas:  privadas,
		})
	}
	return nil
}

// resumenFinal arma la clasificación final del torneo. El criterio no es solo
// el saldo: entre dos jugadores que terminaron en 0 fichas, va más arriba el
// que sobrevivió más tiempo (el último eliminado). Sin eso, todos los
// eliminados empatarían en 0 y el orden lo decidiría el azar del recorrido,
// que es exactamente lo que un ranking de torneo no puede hacer.
func (m *Mesa) resumenFinal(resumen ResumenPartida) ResumenPartida {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	// Puesto de eliminación: 0 = nunca eliminado, 1 = primero en caer.
	eliminadoEn := make(map[string]int, len(m.ordenEliminacion))
	for i, id := range m.ordenEliminacion {
		eliminadoEn[id] = i + 1
	}

	ordenados := make([]*Jugador, 0, len(m.Jugadores))
	for _, j := range m.Jugadores {
		if j != nil {
			ordenados = append(ordenados, j)
		}
	}
	sort.SliceStable(ordenados, func(a, b int) bool {
		ja, jb := ordenados[a], ordenados[b]
		netoA, netoB := m.netoSinLock(ja), m.netoSinLock(jb)
		if netoA != netoB {
			return netoA > netoB
		}
		// Mismo resultado (típicamente 0 fichas, todos eliminados): el que cayó
		// más tarde va primero.
		return eliminadoEn[ja.ID] > eliminadoEn[jb.ID]
	})

	resumen.Posiciones = make([]ResumenJugador, 0, len(ordenados))
	for i, j := range ordenados {
		resumen.Posiciones = append(resumen.Posiciones, ResumenJugador{
			IDJugador:    j.ID,
			Nombre:       j.Nombre,
			Posicion:     i + 1,
			SaldoFinal:   j.Saldo,
			SaldoInicial: m.CfgPartida.StackInicial,
			Neto:         m.netoSinLock(j),
			Timeouts:     j.Timeouts,
			ManosJugadas: j.ManosJugadas,
		})
	}
	return resumen
}

// netoSinLock son las fichas que ganó o perdió un jugador en toda la partida.
// Con reposición se lleva acumulado mano a mano; sin ella, es simplemente la
// diferencia contra el stack con el que se sentó. Requiere Mu tomado.
func (m *Mesa) netoSinLock(j *Jugador) int64 {
	if m.CfgPartida.ReponerStack {
		// La mano en curso todavía no se sumó al acumulado (eso pasa al
		// empezar la siguiente), así que se agrega acá.
		return j.NetoAcumulado + int64(j.Saldo) - int64(m.CfgPartida.StackInicial)
	}
	return int64(j.Saldo) - int64(m.CfgPartida.StackInicial)
}

// contarEnMano cuenta cuántos jugadores siguen en la mano actual (recibieron
// cartas y no hicieron fold). Requiere Mu tomado.
func (m *Mesa) contarEnMano() int {
	n := 0
	for _, j := range m.Jugadores {
		if j != nil && j.EnMano && !j.Retirado {
			n++
		}
	}
	return n
}

// rondaApuestas ejecuta una ronda de apuestas de la etapa dada, empezando en
// primerEnHablar y recorriendo las sillas en sentido horario. Sigue hasta que
// todos los jugadores en mano y no all-in igualaron la apuesta actual (o se
// retiraron), o hasta que quede uno solo en mano.
func (m *Mesa) rondaApuestas(ctx context.Context, etapa protocolo.Etapa, primerEnHablar uint8) error {
	m.Mu.Lock()
	pendientes := make(map[uint8]bool)
	for silla, j := range m.Jugadores {
		if j != nil && j.EnMano && !j.Retirado && !j.AllIn {
			pendientes[uint8(silla)] = true
		}
	}
	m.Mu.Unlock()

	silla := primerEnHablar
	for len(pendientes) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		m.Mu.Lock()
		if m.contarEnMano() <= 1 {
			m.Mu.Unlock()
			return nil
		}
		j := m.Jugadores[silla]
		debeActuar := pendientes[silla] && j != nil && j.EnMano && !j.Retirado && !j.AllIn
		m.Mu.Unlock()

		if !debeActuar {
			delete(pendientes, silla)
			siguiente, ok := m.siguienteSillaEnManoConLock(silla)
			if !ok {
				return nil
			}
			silla = siguiente
			continue
		}

		delete(pendientes, silla)
		m.turnoJugador(j, etapa, pendientes)

		siguiente, ok := m.siguienteSillaEnManoConLock(silla)
		if !ok {
			return nil
		}
		silla = siguiente
	}
	return nil
}

// siguienteSillaEnManoConLock es siguienteSillaEnMano tomando Mu, para usarla
// desde rondaApuestas sin mantener el lock durante toda la ronda.
func (m *Mesa) siguienteSillaEnManoConLock(desde uint8) (uint8, bool) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.siguienteSillaEnMano(desde)
}

// turnoJugador le pide una acción a j, la valida y la aplica. Si reabre la
// ronda (subida completa), agrega de vuelta a pendientes a los demás
// jugadores en mano que no están all-in.
func (m *Mesa) turnoJugador(j *Jugador, etapa protocolo.Etapa, pendientes map[uint8]bool) {
	m.Mu.Lock()
	pendiente := int64(m.apuestaActual) - int64(j.ApuestaRonda)
	validas := accionesValidas(j, m.apuestaActual)
	estado := m.estadoSinLock()
	apuestaActual, subidaMinima, timeout := m.apuestaActual, m.subidaMinima, m.CfgMesa.Timeout
	silla := uint8(j.Silla)
	m.Mu.Unlock()

	solicitud := protocolo.MensajeMesa{
		Tipo:            protocolo.MsgSolicitarAccion,
		Version:         protocolo.VersionProtocolo,
		Estado:          &estado,
		AccionesValidas: validas,
		TimeoutMs:       int(timeout),
	}

	// Punto de partida: la acción segura. Solo se reemplaza si el bot
	// responde a tiempo con algo que la Mesa considere válido; cualquier otro
	// camino (plazo vencido, conexión rota, acción inválida) cuenta como
	// timeout y se lo anota al jugador para el ranking.
	accion := protocolo.AccionSegura(pendiente)
	aplicoSegura := true
	if err := j.Conexion.EnviarMensaje(solicitud, timeout); err == nil {
		if recibida, err := j.Conexion.SolicitarAccion(timeout); err == nil {
			if esAccionValida(j, recibida, apuestaActual, subidaMinima) {
				accion = recibida
				aplicoSegura = false
			}
		} else if err == ErrJugadorAbandono {
			accion = protocolo.Accion{Tipo: protocolo.Fold}
		}
		// Cualquier otro error (timeout, conexión cerrada) deja la acción
		// segura ya calculada arriba.
	}

	m.Mu.Lock()
	if aplicoSegura {
		j.Timeouts++
	}
	apuestaAntes := m.apuestaActual
	reabre := m.procesarAccion(j, accion)
	m.historial = append(m.historial, protocolo.AccionRegistrada{IDJugador: j.ID, Etapa: etapa, Accion: accion})
	if reabre && m.apuestaActual > apuestaAntes {
		for s, jj := range m.Jugadores {
			if uint8(s) == silla {
				continue
			}
			if jj != nil && jj.EnMano && !jj.Retirado && !jj.AllIn {
				pendientes[uint8(s)] = true
			}
		}
	}
	m.Mu.Unlock()

	m.difundirEstado(int(silla))
}

// enviarManoInicio manda a cada jugador sus cartas privadas junto al estado
// inicial de la mano. Los errores de envío se ignoran: si un bot está
// desconectado, su turno se resolverá con la acción segura.
func (m *Mesa) enviarManoInicio(jugadores []*Jugador) {
	m.Mu.Lock()
	estado := m.estadoSinLock()
	timeout := m.CfgMesa.Timeout
	m.Mu.Unlock()

	for _, j := range jugadores {
		mano := j.CartasPrivadas
		msg := protocolo.MensajeMesa{
			Tipo:    protocolo.MsgManoInicio,
			Version: protocolo.VersionProtocolo,
			Cartas:  &mano,
			Estado:  &estado,
		}
		_ = j.Conexion.EnviarMensaje(msg, timeout)
	}
}

// enviarManoFin manda a cada jugador el resultado de la mano (repartos,
// cartas mostradas, descripciones).
func (m *Mesa) enviarManoFin(jugadores []*Jugador, resultado protocolo.ResultadoMano) {
	timeout := m.CfgMesa.Timeout
	msg := protocolo.MensajeMesa{
		Tipo:      protocolo.MsgManoFin,
		Version:   protocolo.VersionProtocolo,
		Resultado: &resultado,
	}
	for _, j := range jugadores {
		_ = j.Conexion.EnviarMensaje(msg, timeout)
	}
}

// difundirEstado manda un estado actualizado a todos los jugadores sentados,
// menos al de exceptoSilla (quien acaba de actuar ya conoce el resultado de
// su propia acción). Pasar -1 para avisar a todos, por ejemplo al abrir una
// calle nueva.
func (m *Mesa) difundirEstado(exceptoSilla int) {
	m.Mu.Lock()
	estado := m.estadoSinLock()
	jugadores := make([]*Jugador, len(m.Jugadores))
	copy(jugadores, m.Jugadores)
	timeout := m.CfgMesa.Timeout
	m.Mu.Unlock()

	msg := protocolo.MensajeMesa{Tipo: protocolo.MsgEstado, Version: protocolo.VersionProtocolo, Estado: &estado}
	for silla, j := range jugadores {
		if j == nil || silla == exceptoSilla {
			continue
		}
		_ = j.Conexion.EnviarMensaje(msg, timeout)
	}
}

// estadoSinLock arma la vista pública de la mesa a partir del estado interno.
// Requiere Mu tomado.
func (m *Mesa) estadoSinLock() protocolo.EstadoPublico {
	jugadoresPublicos := make([]protocolo.JugadorPublico, 0, len(m.Jugadores))
	for _, j := range m.Jugadores {
		if j == nil {
			continue
		}
		jugadoresPublicos = append(jugadoresPublicos, protocolo.JugadorPublico{
			ID:            j.ID,
			Nombre:        j.Nombre,
			Saldo:         int64(j.Saldo),
			ApuestaRonda:  int64(j.ApuestaRonda),
			Activo:        j.EnMano && !j.Retirado,
			EnTorneo:      j.Activo,
			EnMano:        j.EnMano,
			AllIn:         j.AllIn,
			PosicionSilla: int(j.Silla),
		})
	}

	var pozoTotal int64
	if m.Pozo != nil {
		pozoTotal = m.Pozo.Total()
	}

	return protocolo.EstadoPublico{
		IDMano:            m.idManoActual,
		Etapa:             m.etapaActual,
		Comunitarias:      append([]protocolo.Carta(nil), m.comunitarias...),
		Pozo:              pozoTotal,
		ApuestaActual:     int64(m.apuestaActual),
		SubidaMinima:      int64(m.subidaMinima),
		CiegaChica:        int64(m.CfgPartida.CiegaMenor),
		CiegaGrande:       int64(m.CfgPartida.CiegaMayor),
		PosicionBoton:     int(m.Boton),
		Jugadores:         jugadoresPublicos,
		HistorialAcciones: append([]protocolo.AccionRegistrada(nil), m.historial...),
	}
}

// Estado devuelve una foto de la mesa apta para enviar a un bot.
func (m *Mesa) Estado() protocolo.EstadoPublico {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.estadoSinLock()
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
