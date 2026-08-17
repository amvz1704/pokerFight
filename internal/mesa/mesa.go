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
	Activo         bool                // Verdadero si el jugador aún puede participar en el juego (sigue en el torneo, no se quedó sin fichas ni se retiró de la mesa)
	Retirado       bool                // Verdadero si el jugador hizo fold en la mano actual. Se reinicia a false al repartir cada mano nueva.
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

	// --- Estado de la mano en curso, expuesto vía Estado() ------------------
	idManoActual  string
	etapaActual   protocolo.Etapa
	comunitarias  []protocolo.Carta
	apuestaActual uint64 // Apuesta más alta de la ronda de apuestas actual.
	subidaMinima  uint64 // Incremento mínimo exigido para el próximo Raise.
	historial     []protocolo.AccionRegistrada

	// Mu protege todos los campos de arriba. Hace falta porque el Servidor
	// sienta y levanta jugadores desde la goroutine de cada conexión mientras
	// Jugar corre en otra, y porque Estado() se puede llamar en cualquier
	// momento desde afuera. Todo acceso debe tomarla, incluido el de Jugar,
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
			return m.resumenFinal(resumen), fmt.Errorf("Mesa [ID: %s]: La partida ha sido cancelada por un evento externo.\n", m.ID)
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
func (m *Mesa) jugarMano(ctx context.Context, jugadoresActivos []*Jugador, numeroMano int64) error {
	idMano := fmt.Sprintf("Mesa:%s-Ronda:%d", m.ID, numeroMano)

	if err := m.CrupierMesa.NuevaMano(idMano); err != nil {
		return fmt.Errorf("Error en Mesa [ID: %s]: El crupier no pudo iniciar la Mano [ID: %s].\n%w\n", m.ID, idMano, err)
	}

	idsActivos := ObtenerIdsDeJugadores(jugadoresActivos)

	m.Mu.Lock()
	m.Pozo = crupier.NuevoPozo(idsActivos)
	m.idManoActual = idMano
	m.etapaActual = protocolo.PreFlop
	m.comunitarias = nil
	m.historial = nil
	for _, j := range jugadoresActivos {
		j.Retirado = false
		j.AllIn = false
		j.ApuestaRonda = 0
	}
	m.Mu.Unlock()

	manos, err := m.CrupierMesa.RepartirPrivadas(len(idsActivos))
	if err != nil {
		return fmt.Errorf("Mesa [ID: %s]: Error al repartir las cartas privadas. %w\n", m.ID, err)
	}

	m.Mu.Lock()
	for i, jugador := range jugadoresActivos {
		jugador.CartasPrivadas = manos[i]
	}

	sillaChica, sillaGrande := m.posicionesCiegas(len(jugadoresActivos))
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
			return fmt.Errorf("Mesa [ID: %s]: Error al repartir comunitarias de %s. %w\n", m.ID, etapa, err)
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
	var participantes []crupier.Participante
	for _, j := range jugadoresActivos {
		if j.Activo && !j.Retirado {
			participantes = append(participantes, crupier.Participante{ID: j.ID, Mano: j.CartasPrivadas})
		}
	}
	comunitarias := append([]protocolo.Carta(nil), m.comunitarias...)
	pozo := m.Pozo
	m.Mu.Unlock()

	var resultado protocolo.ResultadoMano
	if len(participantes) == 1 {
		// Todos los demás se retiraron: se gana el pozo sin showdown. No se
		// evalúan manos porque puede no haber comunitarias repartidas (fold
		// preflop).
		resultado = protocolo.ResultadoMano{
			IDMano:       idMano,
			Comunitarias: comunitarias,
			Repartos:     []protocolo.Reparto{{IDJugador: participantes[0].ID, Monto: pozo.Total()}},
		}
	} else {
		var err error
		resultado, err = m.CrupierMesa.DecidirGanadores(participantes, comunitarias, pozo)
		if err != nil {
			return fmt.Errorf("Mesa [ID: %s]: Error al decidir ganadores de la Mano [ID: %s]. %w\n", m.ID, idMano, err)
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
	// quedar de espectador, LevantarJugador es una acción explícita).
	for _, j := range jugadoresActivos {
		if j.Saldo == 0 {
			j.Activo = false
		}
	}
	m.Mu.Unlock()

	m.enviarManoFin(jugadoresActivos, resultado)
	return nil
}

// resumenFinal arma la clasificación final ordenando a todos los jugadores
// sentados por saldo descendente.
func (m *Mesa) resumenFinal(resumen ResumenPartida) ResumenPartida {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	ordenados := make([]*Jugador, 0, len(m.Jugadores))
	for _, j := range m.Jugadores {
		if j != nil {
			ordenados = append(ordenados, j)
		}
	}
	sort.Slice(ordenados, func(a, b int) bool { return ordenados[a].Saldo > ordenados[b].Saldo })

	resumen.Posiciones = make([]ResumenJugador, 0, len(ordenados))
	for i, j := range ordenados {
		resumen.Posiciones = append(resumen.Posiciones, ResumenJugador{
			IDJugador:  j.ID,
			Posicion:   i + 1,
			SaldoFinal: j.Saldo,
		})
	}
	return resumen
}

// contarEnMano cuenta cuántos jugadores siguen en la mano actual (no
// hicieron fold). Requiere Mu tomado.
func (m *Mesa) contarEnMano() int {
	n := 0
	for _, j := range m.Jugadores {
		if j != nil && j.Activo && !j.Retirado {
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
		if j != nil && j.Activo && !j.Retirado && !j.AllIn {
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
		debeActuar := pendientes[silla] && j != nil && j.Activo && !j.Retirado && !j.AllIn
		m.Mu.Unlock()

		if !debeActuar {
			delete(pendientes, silla)
			siguiente, ok := m.siguienteSillaOcupadaConLock(silla)
			if !ok {
				return nil
			}
			silla = siguiente
			continue
		}

		delete(pendientes, silla)
		m.turnoJugador(j, etapa, pendientes)

		siguiente, ok := m.siguienteSillaOcupadaConLock(silla)
		if !ok {
			return nil
		}
		silla = siguiente
	}
	return nil
}

// siguienteSillaOcupadaConLock es siguienteSillaOcupada tomando Mu, para
// usarla desde rondaApuestas sin mantener el lock durante toda la ronda.
func (m *Mesa) siguienteSillaOcupadaConLock(desde uint8) (uint8, bool) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.siguienteSillaOcupada(desde)
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

	accion := protocolo.AccionSegura(pendiente)
	if err := j.Conexion.EnviarMensaje(solicitud, timeout); err == nil {
		if recibida, err := j.Conexion.SolicitarAccion(timeout); err == nil {
			if esAccionValida(j, recibida, apuestaActual, subidaMinima) {
				accion = recibida
			}
		} else if err == ErrJugadorAbandono {
			accion = protocolo.Accion{Tipo: protocolo.Fold}
		}
		// Cualquier otro error (timeout, conexión cerrada) deja la acción
		// segura ya calculada arriba.
	}

	m.Mu.Lock()
	apuestaAntes := m.apuestaActual
	reabre := m.procesarAccion(j, accion)
	m.historial = append(m.historial, protocolo.AccionRegistrada{IDJugador: j.ID, Etapa: etapa, Accion: accion})
	if reabre && m.apuestaActual > apuestaAntes {
		for s, jj := range m.Jugadores {
			if uint8(s) == silla {
				continue
			}
			if jj != nil && jj.Activo && !jj.Retirado && !jj.AllIn {
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
			Activo:        !j.Retirado,
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
