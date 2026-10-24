package campeonato

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/amvz1704/pokerFight/internal/mesa"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Lobby es el servidor único al que se conectan los bots del campeonato.
//
// Resuelve el problema del issue #9 ("1 mesa a la vez"): una mesa suelta escucha
// en un puerto y admite a todos los que lleguen, así que dos mesas necesitarían
// dos puertos y un protocolo para decidir quién va a cuál. Acá el bot se conecta
// UNA vez, con su token, y se queda conectado. Qué mesa le toca en cada ronda lo
// decide la arena, y el bot no se entera: sigue recibiendo mano_inicio,
// solicitar_accion y mano_fin como siempre. Es lo que piden las bases: "basta con
// conectarlo y dejarlo ejecutándose".
//
// Para no reescribir el handshake, el Lobby se hace pasar por una mesa
// (implementa mesa.MesaInterface) y se lo entrega a un mesa.Servidor. El Servidor
// valida la versión y el token, y llama a SentarJugador: acá eso significa
// "este jugador ya está en la sala". Las mesas de verdad se arman después y se
// sientan a los jugadores con Presencia.Asiento.
type Lobby struct {
	log      *log.Logger
	servidor *mesa.Servidor

	mu          sync.Mutex
	presencias  map[string]*Presencia // por alias
	aliasPorTok map[string]string     // token -> alias
}

var _ mesa.MesaInterface = (*Lobby)(nil)

// NuevoLobby arma el lobby para los inscritos. `alias` traduce el id de cada
// inscrito al nombre aleatorio con el que lo conocen las mesas.
//
// Los bots nunca ven su id de inscripción ni el de nadie: ni el nombre del
// equipo ni el del bot salen de la organización. Eso cumple lo que piden las
// bases ("la arena no revela a ningún bot [...] los nombres de los demás
// participantes, solo identificadores aleatorios") y, de paso, impide que dos
// bots del mismo equipo se reconozcan en una mesa.
func NuevoLobby(direccion string, inscritos Inscritos, alias map[string]string, handshakeMs uint64, registro *log.Logger) (*Lobby, error) {
	if registro == nil {
		registro = log.Default()
	}
	l := &Lobby{
		log:         registro,
		presencias:  make(map[string]*Presencia, len(inscritos)),
		aliasPorTok: make(map[string]string, len(inscritos)),
	}
	for _, in := range inscritos {
		a, ok := alias[in.ID]
		if !ok {
			return nil, fmt.Errorf("campeonato: el inscrito %q no tiene alias", in.ID)
		}
		l.presencias[a] = &Presencia{alias: a}
		l.aliasPorTok[in.Token] = a
	}

	l.servidor = mesa.NuevoServidor(mesa.ConfigServidor{
		Direccion:          direccion,
		TimeoutHandshakeMs: handshakeMs,
		// Cada inscrito puede estar registrado a la vez, más un margen para los
		// handshakes en curso.
		MaxConexiones: len(inscritos) + 8,
		Log:           registro,
		AlConectar:    l.alConectar,
	}, l, l.validarToken)
	return l, nil
}

// validarToken traduce el token del saludo a la identidad con la que el bot va a
// jugar: su alias, que hace de id y de nombre.
func (l *Lobby) validarToken(token string) (string, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	alias, ok := l.aliasPorTok[token]
	if !ok {
		return "", "", errors.New("credencial desconocida")
	}
	return alias, alias, nil
}

// Abrir reserva el puerto. Ver mesa.Servidor.Abrir.
func (l *Lobby) Abrir() error { return l.servidor.Abrir() }

// Servir acepta conexiones hasta que se cancele el contexto o se cierre.
func (l *Lobby) Servir(ctx context.Context) error { return l.servidor.Servir(ctx) }

// Direccion es la dirección real de escucha.
func (l *Lobby) Direccion() string { return l.servidor.Direccion() }

// Cerrar corta todas las conexiones. Los bots lo toman como el fin del
// campeonato y terminan solos.
func (l *Lobby) Cerrar() error { return l.servidor.Cerrar() }

// Presencia devuelve la presencia de un jugador, o nil si no existe.
func (l *Lobby) Presencia(alias string) *Presencia {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.presencias[alias]
}

// alConectar promueve la conexión nueva cuando el handshake terminó.
func (l *Lobby) alConectar(alias string) {
	p := l.Presencia(alias)
	if p == nil {
		return
	}
	if reemplazo := p.promover(); reemplazo {
		l.log.Printf("campeonato: %s se reconectó", alias)
	} else {
		l.log.Printf("campeonato: %s conectado", alias)
	}
}

// Conectados cuenta los jugadores con conexión viva.
func (l *Lobby) Conectados() int { return l.Sentados() }

// VistaPresencias lista el estado de conexión de cada jugador, por alias.
func (l *Lobby) VistaPresencias() []VistaPresencia {
	l.mu.Lock()
	lista := make([]*Presencia, 0, len(l.presencias))
	for _, p := range l.presencias {
		lista = append(lista, p)
	}
	l.mu.Unlock()

	vista := make([]VistaPresencia, 0, len(lista))
	for _, p := range lista {
		vista = append(vista, p.vista())
	}
	sort.Slice(vista, func(i, j int) bool { return vista[i].Alias < vista[j].Alias })
	return vista
}

// --- mesa.MesaInterface: lo que usa el Servidor para el handshake -------------

// SentarJugador registra la conexión de un jugador que ya pasó la validación.
//
// Siempre acepta a un jugador conocido, aunque ya tenga una conexión viva: un
// bot que se cae y vuelve se presenta con el mismo token antes de que el lobby
// se entere de que la conexión vieja murió (nadie lee un socket ocioso). La
// credencial es lo que garantiza que es el mismo bot, así que la nueva conexión
// reemplaza a la vieja.
func (l *Lobby) SentarJugador(id, _ string, c mesa.ConexionMesaJugador) error {
	p := l.Presencia(id)
	if p == nil {
		return fmt.Errorf("jugador %q desconocido", id)
	}
	cx, ok := c.(*mesa.ConexionTCP)
	if !ok {
		return fmt.Errorf("el lobby solo acepta conexiones TCP")
	}
	p.preparar(cx)
	return nil
}

// SentarJugadorEnSilla no tiene sentido en el lobby: las sillas son de las mesas.
func (l *Lobby) SentarJugadorEnSilla(id, nombre string, _ int, c mesa.ConexionMesaJugador) error {
	return l.SentarJugador(id, nombre, c)
}

// ReconectarJugador es igual a sentar: ver SentarJugador.
func (l *Lobby) ReconectarJugador(id string, c mesa.ConexionMesaJugador) error {
	return l.SentarJugador(id, id, c)
}

// LevantarJugador corta la conexión de un jugador.
func (l *Lobby) LevantarJugador(id string) error {
	p := l.Presencia(id)
	if p == nil {
		return fmt.Errorf("jugador %q desconocido", id)
	}
	p.desconectar()
	return nil
}

// Jugar no existe: el lobby no juega, solo conecta.
func (l *Lobby) Jugar(context.Context) (mesa.ResumenPartida, error) {
	return mesa.ResumenPartida{}, errors.New("el lobby no juega: las partidas las corren las mesas del campeonato")
}

// Estado devuelve a los jugadores que hay en la sala. La silla es -1: todavía
// no tienen mesa. Es lo que usa el Servidor para armar la bienvenida.
func (l *Lobby) Estado() protocolo.EstadoPublico {
	vista := l.VistaPresencias()
	jugadores := make([]protocolo.JugadorPublico, 0, len(vista))
	for _, v := range vista {
		jugadores = append(jugadores, protocolo.JugadorPublico{
			ID: v.Alias, Nombre: v.Alias, EnTorneo: true, PosicionSilla: -1,
		})
	}
	return protocolo.EstadoPublico{Jugadores: jugadores}
}

// Sentados cuenta los jugadores con conexión viva.
func (l *Lobby) Sentados() int {
	l.mu.Lock()
	lista := make([]*Presencia, 0, len(l.presencias))
	for _, p := range l.presencias {
		lista = append(lista, p)
	}
	l.mu.Unlock()

	n := 0
	for _, p := range lista {
		if p.Conectado() {
			n++
		}
	}
	return n
}

// --- Presencia ------------------------------------------------------------------

// maxEscrituraMs es lo máximo que la arena espera para escribirle a un bot.
//
// Escribir un mensaje a un socket sano tarda microsegundos. Si tarda más de un
// par de segundos es porque el bot no está leyendo y el buffer se llenó. Con el
// plazo de jugada de las bases (10 s) esperar eso en cada difusión de estado
// frenaría la mesa entera: un bot colgado le costaría diez segundos a los otros
// cuatro en cada acción. Pasado este plazo se lo da por desconectado.
const maxEscrituraMs = 2000

// Presencia es un jugador dentro del lobby. Es también el puente entre el
// jugador y las mesas: implementa mesa.ConexionMesaJugador, y a la mesa le da lo
// mismo qué socket hay detrás. Si el bot se cae a mitad de una ronda, la
// presencia pasa a responder "conexión cerrada" y la mesa juega por él la acción
// segura (check o fold); si vuelve, el mismo asiento sigue con el socket nuevo.
type Presencia struct {
	alias string

	mu             sync.Mutex
	cx             *mesa.ConexionTCP // Conexión viva, o nil.
	pendiente      *mesa.ConexionTCP // Conexión nueva que todavía no recibió su bienvenida.
	desde          time.Time
	ultimoContacto time.Time
	caidas         int
}

var _ mesa.ConexionMesaJugador = (*Presencia)(nil)

// VistaPresencia es lo que se muestra de un jugador en el panel.
type VistaPresencia struct {
	Alias          string    `json:"alias"`
	Conectado      bool      `json:"conectado"`
	Desde          time.Time `json:"desde,omitempty"`
	UltimoContacto time.Time `json:"ultimo_contacto,omitempty"`
	Caidas         int       `json:"caidas"`
}

func (p *Presencia) vista() VistaPresencia {
	p.mu.Lock()
	defer p.mu.Unlock()
	return VistaPresencia{
		Alias: p.alias, Conectado: p.cx != nil,
		Desde: p.desde, UltimoContacto: p.ultimoContacto, Caidas: p.caidas,
	}
}

// Conectado indica si hay una conexión que no se sabe caída. Un bot que se cae
// sin avisar sigue figurando conectado hasta el primer mensaje que falle.
func (p *Presencia) Conectado() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cx != nil
}

func (p *Presencia) actual() *mesa.ConexionTCP {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cx
}

// preparar guarda una conexión nueva sin ponerla en uso todavía. Ver promover.
func (p *Presencia) preparar(cx *mesa.ConexionTCP) {
	p.mu.Lock()
	anterior := p.pendiente
	p.pendiente = cx
	p.mu.Unlock()
	if anterior != nil && anterior != cx {
		anterior.Cerrar()
	}
}

// promover pone en uso la conexión preparada, una vez que ya recibió su
// bienvenida. Hasta ese momento una mesa que empezara a jugar le mandaría
// mensajes de juego antes que la bienvenida, y el bot esperaría otra cosa.
// Devuelve true si reemplazó a una conexión anterior.
func (p *Presencia) promover() (reemplazo bool) {
	p.mu.Lock()
	nueva := p.pendiente
	p.pendiente = nil
	if nueva == nil {
		p.mu.Unlock()
		return false
	}
	vieja := p.cx
	p.cx = nueva
	p.desde = time.Now()
	p.ultimoContacto = p.desde
	p.mu.Unlock()

	// Se cierra fuera del lock: cerrar una ConexionTCP toma su propio mutex, que
	// puede estar tomado por una lectura en curso.
	if vieja != nil && vieja != nueva {
		vieja.Cerrar()
	}
	return vieja != nil
}

// desconectar corta la conexión actual.
func (p *Presencia) desconectar() {
	if cx := p.actual(); cx != nil {
		p.caida(cx)
	}
}

// caida da por muerta una conexión y la cierra, si todavía es la actual.
func (p *Presencia) caida(cx *mesa.ConexionTCP) {
	p.mu.Lock()
	vigente := p.cx == cx
	if vigente {
		p.cx = nil
		p.caidas++
	}
	p.mu.Unlock()
	cx.Cerrar()
}

func (p *Presencia) tocar() {
	p.mu.Lock()
	p.ultimoContacto = time.Now()
	p.mu.Unlock()
}

// EnviarMensaje implementa mesa.ConexionMesaJugador.
func (p *Presencia) EnviarMensaje(m protocolo.MensajeMesa, timeoutMs uint64) error {
	cx := p.actual()
	if cx == nil {
		return mesa.ErrConexionCerrada
	}
	if timeoutMs == 0 || timeoutMs > maxEscrituraMs {
		timeoutMs = maxEscrituraMs
	}
	if err := cx.EnviarMensaje(m, timeoutMs); err != nil {
		p.caida(cx)
		return err
	}
	return nil
}

// SolicitarAccion implementa mesa.ConexionMesaJugador.
func (p *Presencia) SolicitarAccion(timeoutMs uint64) (protocolo.Accion, error) {
	cx := p.actual()
	if cx == nil {
		return protocolo.Accion{}, mesa.ErrConexionCerrada
	}

	accion, err := cx.SolicitarAccion(timeoutMs)
	switch {
	case err == nil:
		p.tocar()
	case errors.Is(err, mesa.ErrTiempoAgotado), errors.Is(err, protocolo.ErrAccionAusente), esErrorDeFormato(err):
		// El bot sigue ahí: tardó, o mandó algo ilegible. La mesa juega la
		// acción segura y la conexión queda en pie. Cortarle el socket por una
		// línea mal formada lo dejaría fuera del campeonato sin vuelta atrás
		// (el SDK no reintenta), por un error que podía ser de una sola jugada.
	default:
		// Error de lectura (el socket se cerró), o el bot avisó que se retira.
		p.caida(cx)
	}
	return accion, err
}

// Cerrar no hace nada: la mesa que termina no puede cortar al bot, que sigue
// en el campeonato y va a jugar en otra mesa. Cortar conexiones es cosa del
// lobby (desconectar, Cerrar).
func (p *Presencia) Cerrar() error { return nil }

// esErrorDeFormato distingue una línea que no es JSON válido de un error de red.
func esErrorDeFormato(err error) bool {
	var sintaxis *json.SyntaxError
	var tipo *json.UnmarshalTypeError
	return errors.As(err, &sintaxis) || errors.As(err, &tipo)
}
