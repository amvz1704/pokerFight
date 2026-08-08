package mesa

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Es la interfaz que representa la conexión de un jugador con la mesa. Cualquier
// tipo de conexión a implementar (websocket, TCP, de prueba, etc.) debe implementar
// esta interfaz.
// Esta interfaz es utilizada por la mesa para enviar mensajes a los jugadores y
// recibir mensajes (acciones) de los jugadores.
// NOTA: Al ser una interfaz, no es necesario que las estructuras que la usen la llamen
// como puntero. Esto se debe a que en Go, las interfaces se manejan internamente como
// punteros. Por lo tanto, no habrán conexiones duplicadas ni problemas de memoria o red.
type ConexionMesaJugador interface {
	EnviarMensaje(m protocolo.MensajeMesa, timeoutMs uint64) error // La mesa envía un mensaje al jugador. Debe tener un timeout si no recibe confirmación de la respuesta. En caso no reciba confirmación, se retira al jugador de la mesa.
	SolicitarAccion(timeoutMs uint64) (protocolo.Accion, error)    // La mesa solicita una acción al jugador cuando sea su turno. Debe tener un timeout si no recibe confirmación de la respuesta. En caso no reciba confirmación, se opta por la acción más segura (check o fold) según el estado de la partida. En caso de error, se opta por la acción más segura (check o fold) según el estado de la partida.
	Cerrar() error                                                 // Cerrar la conexión con el jugador. Se llama cuando se termina la partida o el jugador se retira voluntariamente.
}

// Errores que puede devolver cualquier implementación de ConexionMesaJugador.
// La mesa los usa para decidir qué hacer: ante ErrTiempoAgotado aplica
// protocolo.AccionSegura; ante ErrConexionCerrada o ErrJugadorAbandono levanta
// al jugador de la mesa.
var (
	ErrConexionCerrada = errors.New("mesa: la conexión está cerrada")
	ErrTiempoAgotado   = errors.New("mesa: el jugador no respondió a tiempo")
	ErrJugadorAbandono = errors.New("mesa: el jugador abandonó la mesa")
	ErrVersionInvalida = errors.New("mesa: versión de protocolo incompatible")
	ErrSaludoInvalido  = errors.New("mesa: se esperaba un mensaje de saludo")
	ErrTokenInvalido   = errors.New("mesa: token inválido")
	ErrServidorCerrado = errors.New("mesa: el servidor está cerrado")
	ErrSinAcciones     = errors.New("mesa: la conexión de prueba no tiene más acciones programadas")
)

// timeoutMaximoMs acota el plazo para evitar desbordes al convertir a
// time.Duration. Son ~24 días, muy por encima de cualquier partida.
const timeoutMaximoMs = uint64(1) << 31

// plazo traduce un timeout en milisegundos a un instante absoluto para
// net.Conn.SetDeadline. Un timeout de 0 significa "sin plazo".
func plazo(timeoutMs uint64) time.Time {
	if timeoutMs == 0 {
		return time.Time{}
	}
	if timeoutMs > timeoutMaximoMs {
		timeoutMs = timeoutMaximoMs
	}
	return time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
}

// esTimeout indica si un error de red es un vencimiento de plazo.
func esTimeout(err error) bool {
	var nerr net.Error
	return errors.As(err, &nerr) && nerr.Timeout()
}

// --- Conexión real sobre TCP ----------------------------------------------

// ConexionTCP es la implementación real de ConexionMesaJugador: habla JSON
// Lines sobre una net.Conn (ver docs/protocolo.md). Es segura para uso
// concurrente: la mesa puede difundir un estado mientras otro jugador piensa.
type ConexionTCP struct {
	IDJugador string // Se completa tras el handshake. Vacío mientras no haya token validado.
	Nombre    string // Nombre a mostrar, devuelto por ValidarToken.

	conexion net.Conn
	codec    *protocolo.Codec

	mu       sync.Mutex
	idMano   string // Última mano anunciada. Sirve para descartar acciones tardías.
	cerrada  bool
	timeouts uint64 // Cuántas veces el jugador no respondió a tiempo.
}

// Verificación en tiempo de compilación de que cumple el contrato.
var _ ConexionMesaJugador = (*ConexionTCP)(nil)

// NuevaConexionTCP envuelve una net.Conn ya establecida (la que devuelve
// Accept en el servidor, o Dial en un cliente).
func NuevaConexionTCP(c net.Conn) *ConexionTCP {
	return &ConexionTCP{
		conexion: c,
		codec:    protocolo.NuevoCodec(c, c),
	}
}

// ConectarTCP abre una conexión saliente hacia una mesa. Se usa en pruebas de
// humo y desde el lado del bot; la mesa como servidor usa NuevaConexionTCP.
func ConectarTCP(direccion string, timeoutMs uint64) (*ConexionTCP, error) {
	var (
		c   net.Conn
		err error
	)
	if timeoutMs == 0 {
		c, err = net.Dial("tcp", direccion)
	} else {
		c, err = net.DialTimeout("tcp", direccion, time.Until(plazo(timeoutMs)))
	}
	if err != nil {
		return nil, err
	}
	return NuevaConexionTCP(c), nil
}

// EnviarMensaje serializa el mensaje y lo escribe en la conexión. El timeout
// acota la escritura: si el jugador no lee (buffer lleno) el envío falla y la
// mesa puede levantarlo.
func (c *ConexionTCP) EnviarMensaje(m protocolo.MensajeMesa, timeoutMs uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return ErrConexionCerrada
	}
	if m.Version == "" {
		m.Version = protocolo.VersionProtocolo
	}
	// Recordamos la mano en curso para poder descartar acciones tardías.
	if m.Estado != nil && m.Estado.IDMano != "" {
		c.idMano = m.Estado.IDMano
	}

	if err := c.conexion.SetWriteDeadline(plazo(timeoutMs)); err != nil {
		return err
	}
	defer c.conexion.SetWriteDeadline(time.Time{})

	if err := c.codec.Enviar(m); err != nil {
		if esTimeout(err) {
			c.timeouts++
			return ErrTiempoAgotado
		}
		return err
	}
	return nil
}

// SolicitarAccion espera la respuesta del jugador a un MsgSolicitarAccion ya
// enviado. Descarta mensajes que no sean acciones y acciones de una mano
// anterior (llegadas tarde), siempre dentro del mismo plazo.
// Devuelve ErrTiempoAgotado si vence el plazo y ErrJugadorAbandono si el
// jugador se retira: en ambos casos la mesa debe aplicar protocolo.AccionSegura
// o levantar al jugador, según corresponda.
func (c *ConexionTCP) SolicitarAccion(timeoutMs uint64) (protocolo.Accion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return protocolo.Accion{}, ErrConexionCerrada
	}
	if err := c.conexion.SetReadDeadline(plazo(timeoutMs)); err != nil {
		return protocolo.Accion{}, err
	}
	defer c.conexion.SetReadDeadline(time.Time{})

	for {
		m, err := c.codec.RecibirMensajeBot()
		if err != nil {
			if esTimeout(err) {
				c.timeouts++
				return protocolo.Accion{}, ErrTiempoAgotado
			}
			return protocolo.Accion{}, err
		}

		switch m.Tipo {
		case protocolo.MsgAccion:
			// El bot hace eco del id_mano: si no coincide, la acción
			// corresponde a una mano ya cerrada y se ignora.
			if c.idMano != "" && m.IDMano != "" && m.IDMano != c.idMano {
				continue
			}
			if m.Accion == nil {
				return protocolo.Accion{}, protocolo.ErrAccionAusente
			}
			return *m.Accion, nil
		case protocolo.MsgAbandono:
			return protocolo.Accion{}, ErrJugadorAbandono
		default:
			// Saludos repetidos o basura: se ignoran, el plazo sigue corriendo.
			continue
		}
	}
}

// Cerrar cierra la conexión subyacente. Es idempotente.
func (c *ConexionTCP) Cerrar() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return nil
	}
	c.cerrada = true
	return c.conexion.Close()
}

// Direccion devuelve la dirección remota, útil para los logs del servidor.
func (c *ConexionTCP) Direccion() string {
	if c.conexion == nil || c.conexion.RemoteAddr() == nil {
		return ""
	}
	return c.conexion.RemoteAddr().String()
}

// Timeouts es la cantidad de veces que el jugador no respondió a tiempo
// (requisito de Estadisticas.Timeouts en docs/interfaces.md).
func (c *ConexionTCP) Timeouts() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timeouts
}

// recibirMensajeBot lee un mensaje del bot con plazo. Lo usa el handshake, que
// ocurre antes de que la conexión se entregue a la mesa.
func (c *ConexionTCP) recibirMensajeBot(timeoutMs uint64) (protocolo.MensajeBot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return protocolo.MensajeBot{}, ErrConexionCerrada
	}
	if err := c.conexion.SetReadDeadline(plazo(timeoutMs)); err != nil {
		return protocolo.MensajeBot{}, err
	}
	defer c.conexion.SetReadDeadline(time.Time{})

	m, err := c.codec.RecibirMensajeBot()
	if err != nil && esTimeout(err) {
		return m, ErrTiempoAgotado
	}
	return m, err
}

// EnviarMensajeBot escribe un mensaje del lado del bot. Solo se usa desde el
// cliente (pruebas de humo, bots de ejemplo); la mesa nunca lo llama.
func (c *ConexionTCP) EnviarMensajeBot(m protocolo.MensajeBot, timeoutMs uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return ErrConexionCerrada
	}
	if err := c.conexion.SetWriteDeadline(plazo(timeoutMs)); err != nil {
		return err
	}
	defer c.conexion.SetWriteDeadline(time.Time{})
	return c.codec.Enviar(m)
}

// RecibirMensajeMesa lee un mensaje de la mesa. Igual que EnviarMensajeBot,
// es la cara de cliente de la conexión.
func (c *ConexionTCP) RecibirMensajeMesa(timeoutMs uint64) (protocolo.MensajeMesa, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cerrada {
		return protocolo.MensajeMesa{}, ErrConexionCerrada
	}
	if err := c.conexion.SetReadDeadline(plazo(timeoutMs)); err != nil {
		return protocolo.MensajeMesa{}, err
	}
	defer c.conexion.SetReadDeadline(time.Time{})

	m, err := c.codec.RecibirMensajeMesa()
	if err != nil && esTimeout(err) {
		return m, ErrTiempoAgotado
	}
	return m, err
}

// ParConexionesMemoria devuelve dos ConexionTCP conectadas entre sí mediante
// net.Pipe: una para la mesa y otra para el bot. Sirve para probar el
// protocolo completo (codec, handshake, plazos) sin abrir puertos.
func ParConexionesMemoria() (mesa *ConexionTCP, bot *ConexionTCP) {
	a, b := net.Pipe()
	return NuevaConexionTCP(a), NuevaConexionTCP(b)
}

// --- Servidor TCP ----------------------------------------------------------

// ValidarToken traduce el token emitido por el Casino a una identidad. La mesa
// no importa el paquete casino: recibe esta función por inyección
// (docs/interfaces.md §4). Si es nil, el servidor trabaja en modo abierto y usa
// el token como identificador (solo para desarrollo).
type ValidarToken func(token string) (idJugador string, nombre string, err error)

// ConfigServidor define cómo escucha la mesa.
type ConfigServidor struct {
	Direccion          string // Dirección de escucha, por ejemplo ":9000". ":0" pide un puerto libre.
	TimeoutHandshakeMs uint64 // Plazo para que el bot envíe su saludo. 0 = sin plazo.
	MaxConexiones      int    // Máximo de conexiones aceptadas en simultáneo. 0 = sin límite.
	Log                *log.Logger
}

// Servidor acepta conexiones TCP de bots, hace el handshake y sienta a los
// jugadores validados en la mesa.
type Servidor struct {
	cfg     ConfigServidor
	mesa    MesaInterface
	validar ValidarToken

	mu         sync.Mutex
	escucha    net.Listener
	conexiones map[string]*ConexionTCP
	cerrado    bool
	espera     sync.WaitGroup
}

// NuevoServidor construye el servidor. No abre el puerto: eso lo hace Abrir.
func NuevoServidor(cfg ConfigServidor, m MesaInterface, validar ValidarToken) *Servidor {
	if cfg.Log == nil {
		cfg.Log = log.Default()
	}
	return &Servidor{
		cfg:        cfg,
		mesa:       m,
		validar:    validar,
		conexiones: make(map[string]*ConexionTCP),
	}
}

// Abrir reserva el puerto sin empezar a aceptar. Separarlo de Servir permite
// conocer la dirección real (útil con ":0") antes de que llegue nadie, y evita
// carreras en las pruebas. Es idempotente.
func (s *Servidor) Abrir() error {
	// Se mantiene el lock durante el Listen: es una operación corta y así dos
	// llamadas concurrentes no reservan dos puertos distintos.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cerrado {
		return ErrServidorCerrado
	}
	if s.escucha != nil {
		return nil
	}

	escucha, err := net.Listen("tcp", s.cfg.Direccion)
	if err != nil {
		return err
	}
	s.escucha = escucha
	s.cfg.Log.Printf("mesa: escuchando en %s", escucha.Addr())
	return nil
}

// Escuchar abre el puerto y atiende conexiones hasta que se cancele el
// contexto o se llame a Cerrar. Bloquea. Devuelve nil en un cierre ordenado.
func (s *Servidor) Escuchar(ctx context.Context) error {
	if err := s.Abrir(); err != nil {
		return err
	}
	return s.Servir(ctx)
}

// Servir corre el bucle de aceptación sobre un puerto ya abierto por Abrir.
// Bloquea hasta que se cancele el contexto o se llame a Cerrar.
func (s *Servidor) Servir(ctx context.Context) error {
	if err := s.Abrir(); err != nil {
		return err
	}

	s.mu.Lock()
	escucha := s.escucha
	s.mu.Unlock()

	// El cierre del listener es lo que desbloquea Accept.
	hecho := make(chan struct{})
	defer close(hecho)
	go func() {
		select {
		case <-ctx.Done():
			s.Cerrar()
		case <-hecho:
		}
	}()

	for {
		conexion, err := escucha.Accept()
		if err != nil {
			s.mu.Lock()
			cerrado := s.cerrado
			s.mu.Unlock()
			if cerrado {
				s.espera.Wait()
				return nil
			}
			return err
		}

		if s.lleno() {
			s.rechazar(conexion, "mesa llena")
			continue
		}

		s.espera.Add(1)
		go func() {
			defer s.espera.Done()
			s.atender(conexion)
		}()
	}
}

// Direccion devuelve la dirección real de escucha. Es la forma de conocer el
// puerto cuando se configuró ":0".
func (s *Servidor) Direccion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.escucha == nil {
		return s.cfg.Direccion
	}
	return s.escucha.Addr().String()
}

// Cerrar deja de aceptar conexiones y cierra las que estén abiertas. Es
// idempotente y seguro de llamar desde otra goroutine.
func (s *Servidor) Cerrar() error {
	s.mu.Lock()
	if s.cerrado {
		s.mu.Unlock()
		return nil
	}
	s.cerrado = true
	escucha := s.escucha
	conexiones := make([]*ConexionTCP, 0, len(s.conexiones))
	for _, c := range s.conexiones {
		conexiones = append(conexiones, c)
	}
	s.conexiones = make(map[string]*ConexionTCP)
	s.mu.Unlock()

	var err error
	if escucha != nil {
		err = escucha.Close()
	}
	for _, c := range conexiones {
		c.Cerrar()
	}
	return err
}

// Conexiones devuelve las conexiones actualmente registradas, por id de jugador.
func (s *Servidor) Conexiones() map[string]*ConexionTCP {
	s.mu.Lock()
	defer s.mu.Unlock()
	copia := make(map[string]*ConexionTCP, len(s.conexiones))
	for id, c := range s.conexiones {
		copia[id] = c
	}
	return copia
}

func (s *Servidor) lleno() bool {
	if s.cfg.MaxConexiones <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conexiones) >= s.cfg.MaxConexiones
}

// atender corre en su propia goroutine por conexión: handshake y, si todo va
// bien, entrega el jugador a la mesa.
func (s *Servidor) atender(conexion net.Conn) {
	cx := NuevaConexionTCP(conexion)

	// La mesa todavía tiene tareas sin implementar (ver docs/interfaces.md).
	// Un panic en SentarJugador no debe tumbar al servidor entero.
	defer func() {
		if r := recover(); r != nil {
			s.cfg.Log.Printf("mesa: pánico atendiendo a %s: %v", cx.Direccion(), r)
			cx.Cerrar()
		}
	}()

	id, nombre, err := s.saludar(cx)
	if err != nil {
		s.cfg.Log.Printf("mesa: handshake fallido con %s: %v", cx.Direccion(), err)
		s.rechazarConexion(cx, err.Error())
		return
	}

	cx.IDJugador = id
	cx.Nombre = nombre

	if err := s.mesa.SentarJugador(id, nombre, cx); err != nil {
		s.cfg.Log.Printf("mesa: no se pudo sentar a %s: %v", id, err)
		s.rechazarConexion(cx, err.Error())
		return
	}

	s.registrar(id, cx)
	s.cfg.Log.Printf("mesa: jugador %s (%s) conectado desde %s", id, nombre, cx.Direccion())
}

// saludar ejecuta el handshake: espera el saludo, verifica versión y token, y
// responde con la bienvenida.
func (s *Servidor) saludar(cx *ConexionTCP) (idJugador string, nombre string, err error) {
	m, err := cx.recibirMensajeBot(s.cfg.TimeoutHandshakeMs)
	if err != nil {
		return "", "", err
	}
	if m.Tipo != protocolo.MsgSaludo {
		return "", "", ErrSaludoInvalido
	}
	if m.Version != protocolo.VersionProtocolo {
		return "", "", ErrVersionInvalida
	}

	if s.validar != nil {
		idJugador, nombre, err = s.validar(m.Token)
		if err != nil {
			return "", "", err
		}
	} else {
		// Modo abierto (desarrollo): el token es el identificador.
		idJugador, nombre = m.Token, m.Token
	}
	if idJugador == "" {
		return "", "", ErrTokenInvalido
	}

	bienvenida := protocolo.MensajeMesa{
		Tipo:    protocolo.MsgBienvenida,
		Version: protocolo.VersionProtocolo,
		Mensaje: "bienvenido " + nombre,
	}
	if err := cx.EnviarMensaje(bienvenida, s.cfg.TimeoutHandshakeMs); err != nil {
		return "", "", err
	}
	return idJugador, nombre, nil
}

func (s *Servidor) registrar(id string, cx *ConexionTCP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cerrado {
		cx.Cerrar()
		return
	}
	if anterior, ok := s.conexiones[id]; ok && anterior != cx {
		anterior.Cerrar() // Reconexión: se descarta el socket viejo.
	}
	s.conexiones[id] = cx
}

// rechazarConexion avisa del motivo y cierra.
func (s *Servidor) rechazarConexion(cx *ConexionTCP, motivo string) {
	cx.EnviarMensaje(protocolo.MensajeMesa{
		Tipo:    protocolo.MsgError,
		Version: protocolo.VersionProtocolo,
		Mensaje: motivo,
	}, s.cfg.TimeoutHandshakeMs)
	cx.Cerrar()
}

func (s *Servidor) rechazar(conexion net.Conn, motivo string) {
	s.rechazarConexion(NuevaConexionTCP(conexion), motivo)
}
