// Package botsdk es la librería pública para escribir bots en Go. Habla el
// protocolo Mesa <-> Bot (docs/protocolo.md) por TCP + JSON Lines,
// dependiendo únicamente de internal/protocolo — igual que la Mesa, pero sin
// importarla a ella (ver el mapa de dependencias en docs/interfaces.md §1).
//
// Lo mínimo que hay que escribir es un tipo con dos métodos:
//
//	type MiBot struct{}
//
//	func (MiBot) Nombre() string { return "mi-bot" }
//
//	func (MiBot) Decidir(ctx context.Context, t botsdk.Turno) protocolo.Accion {
//	    if t.Puede(protocolo.Check) {
//	        return t.Check()
//	    }
//	    return t.Fold()
//	}
//
//	func main() {
//	    botsdk.CorrerDesdeFlags(MiBot{})
//	}
//
// Todo lo demás (handshake, reconexión, eco del id_mano, saber cuál de los
// jugadores del estado sos vos) lo resuelve el SDK.
package botsdk

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// --- Contrato del bot ------------------------------------------------------

// Bot es la interfaz que cualquier bot escrito en Go debe implementar.
type Bot interface {
	// Nombre identifica al bot en los logs y en el historial de manos. Es
	// informativo: la identidad real ante la Mesa la fija el token.
	Nombre() string
	// Decidir devuelve la acción a jugar en el turno actual. Si tarda más que
	// t.Timeout, la Mesa aplica la acción segura (protocolo.AccionSegura) y
	// le anota un timeout al jugador, que resta puntos en el ranking.
	//
	// Devolver una acción inválida cuesta exactamente lo mismo que no
	// responder: la Mesa la descarta y aplica la acción segura. Los
	// constructores de Turno (t.Apostar, t.Pagar, ...) están para que eso no
	// pase por un error de aritmética.
	Decidir(ctx context.Context, t Turno) protocolo.Accion
}

// Inicializable lo puede implementar un bot que necesite preparar algo una vez
// antes de jugar: abrir un archivo, cargar un modelo, sembrar su generador de
// numeros al azar. Es opcional.
//
// El SDK lo llama una sola vez, **después de parsear los flags** y antes de
// conectarse. Ese orden importa: si tu bot define flags propios, sus valores
// recién existen en ese momento. No los leas en main antes de llamar a
// CorrerDesdeFlags, y no llames a flag.Parse por tu cuenta: el SDK todavía no
// definió -addr ni -token y el parseo va a fallar.
//
// Si Iniciar devuelve error, el bot no se conecta.
type Inicializable interface {
	Iniciar() error
}

// ObservaManos lo puede implementar un bot que quiera enterarse del principio
// y el final de cada mano, no solo de sus turnos. Es opcional: el SDK lo
// detecta con una aserción de tipo y lo llama solo si está.
type ObservaManos interface {
	// ManoInicio se llama al recibir las cartas privadas de una mano nueva.
	ManoInicio(estado protocolo.EstadoPublico, mias protocolo.Mano)
	// ManoFin se llama con el resultado de la mano: repartos, cartas
	// mostradas en el showdown y descripción de cada jugada.
	ManoFin(resultado protocolo.ResultadoMano)
}

// ObservaEstado lo puede implementar un bot que quiera ver cada actualización
// de estado, incluidas las de los turnos ajenos. Es opcional.
type ObservaEstado interface {
	EstadoActualizado(estado protocolo.EstadoPublico)
}

// --- Turno -----------------------------------------------------------------

// Turno es todo lo que el bot sabe en el momento de decidir. Se arma desde el
// mensaje solicitar_accion de la Mesa, más lo que el SDK viene guardando
// (cartas privadas e identidad propia).
type Turno struct {
	// IDJugador es tu identificador en esta mesa, tal como lo devolvió el
	// handshake. Es la clave para encontrarte en Estado.Jugadores: el token no
	// sirve, porque con Casino la Mesa usa el ID de cuenta.
	IDJugador string
	// Silla es tu posición en la mesa (0..MaxJugadores-1), o -1 si la Mesa no
	// la informó.
	Silla int
	// Yo es tu propia entrada del estado público, ya resuelta. Saldo, apuesta
	// de la ronda y demás salen de acá sin tener que recorrer Estado.Jugadores.
	Yo protocolo.JugadorPublico
	// Mano son tus dos cartas privadas de la mano en curso.
	Mano protocolo.Mano
	// Estado es la foto pública de la mesa (pozo, comunitarias, rivales,
	// historial de acciones de la mano).
	Estado protocolo.EstadoPublico
	// Validas son las únicas acciones que la Mesa va a aceptar en este turno.
	Validas []protocolo.TipoAccion
	// Timeout es el plazo que da la Mesa para responder. Pasado ese plazo la
	// respuesta se descarta.
	Timeout time.Duration
}

// Puede indica si una acción está entre las válidas de este turno.
func (t Turno) Puede(tipo protocolo.TipoAccion) bool {
	for _, v := range t.Validas {
		if v == tipo {
			return true
		}
	}
	return false
}

// PorIgualar son las fichas que te faltan poner para igualar la apuesta más
// alta de la ronda. Es 0 cuando podés pasar gratis.
func (t Turno) PorIgualar() int64 {
	pendiente := t.Estado.ApuestaActual - t.Yo.ApuestaRonda
	if pendiente < 0 {
		return 0
	}
	return pendiente
}

// Techo es el total máximo al que podés llevar tu apuesta en esta ronda: todo
// lo que ya pusiste más todo lo que te queda. Apostar exactamente el Techo es
// un all-in.
func (t Turno) Techo() int64 { return t.Yo.Saldo + t.Yo.ApuestaRonda }

// SubidaMinima es el total mínimo que la Mesa acepta como Bet o Raise válido.
// Puede ser mayor que tu Techo: en ese caso la única subida posible es el
// all-in (t.AllIn()), que la Mesa acepta aunque no llegue a la subida mínima.
func (t Turno) SubidaMinima() int64 {
	return t.Estado.ApuestaActual + t.Estado.SubidaMinima
}

// PozoSiPagas es cuánto habría en el pozo si igualás. Sirve para calcular pot
// odds sin sumar a mano.
func (t Turno) PozoSiPagas() int64 { return t.Estado.Pozo + t.PorIgualar() }

// Constructores de acciones. Existen para que el bot no arme structs a mano y
// se equivoque con el significado de Monto (es el TOTAL de la ronda, no el
// incremento; ver docs/protocolo.md).

// Fold se retira de la mano.
func (t Turno) Fold() protocolo.Accion { return protocolo.Accion{Tipo: protocolo.Fold} }

// Check pasa sin apostar. Solo es válido si PorIgualar() es 0.
func (t Turno) Check() protocolo.Accion { return protocolo.Accion{Tipo: protocolo.Check} }

// Pagar iguala la apuesta más alta de la ronda.
func (t Turno) Pagar() protocolo.Accion { return protocolo.Accion{Tipo: protocolo.Call} }

// AllIn apuesta todo el saldo restante.
func (t Turno) AllIn() protocolo.Accion { return protocolo.Accion{Tipo: protocolo.AllIn} }

// Apostar lleva tu apuesta de la ronda hasta `total` fichas. Elige sola entre
// bet (no había apuesta previa) y raise (sí la había), y recorta el total al
// rango que la Mesa acepta: nunca por debajo de la subida mínima, nunca por
// encima de tu techo. Si el total pedido llega o supera el techo, devuelve un
// all-in.
//
// Si en este turno no se puede subir en absoluto, cae a la mejor acción
// pasiva disponible (check si es gratis, call si no) en lugar de devolver algo
// que la Mesa va a descartar.
func (t Turno) Apostar(total int64) protocolo.Accion {
	tipo := protocolo.Raise
	if t.Estado.ApuestaActual <= t.Yo.ApuestaRonda {
		tipo = protocolo.Bet
	}
	if !t.Puede(tipo) {
		if t.Puede(protocolo.AllIn) && total >= t.Techo() {
			return t.AllIn()
		}
		if t.Puede(protocolo.Check) {
			return t.Check()
		}
		if t.Puede(protocolo.Call) {
			return t.Pagar()
		}
		return t.Fold()
	}

	techo := t.Techo()
	if total >= techo {
		// Llevar la apuesta al techo es exactamente un all-in. Se declara como
		// tal para que la Mesa lo trate con la regla de all-in corto (no
		// reabre la acción) en vez de rechazarlo por subida insuficiente.
		if t.Puede(protocolo.AllIn) {
			return t.AllIn()
		}
		total = techo
	}
	if minimo := t.SubidaMinima(); total < minimo {
		total = minimo
	}
	if total > techo {
		// La subida mínima no entra en el stack: la única subida legal es
		// el all-in.
		if t.Puede(protocolo.AllIn) {
			return t.AllIn()
		}
		if t.Puede(protocolo.Call) {
			return t.Pagar()
		}
		return t.Fold()
	}
	return protocolo.Accion{Tipo: tipo, Monto: total}
}

// ApostarPozo lleva la apuesta a una fracción del pozo (1.0 = pozo entero),
// pasando siempre por el ajuste de Apostar. Es azúcar sobre Apostar, que es
// donde está la lógica.
func (t Turno) ApostarPozo(fraccion float64) protocolo.Accion {
	objetivo := t.Estado.ApuestaActual + int64(float64(t.PozoSiPagas())*fraccion)
	return t.Apostar(objetivo)
}

// --- Ejecución -------------------------------------------------------------

// Opciones configura la conexión del bot con la mesa.
type Opciones struct {
	// Direccion es el host:puerto de la mesa, por ejemplo "localhost:9000".
	Direccion string
	// Token es el token emitido por el Casino ('casino login'). En modo
	// abierto (mesa levantada sin -casino-db) cualquier string sirve y se usa
	// directamente como identificador del jugador.
	Token string
	// Reintentos es cuántas veces reintentar la conexión inicial antes de
	// rendirse. Sirve porque la arena suele lanzar los bots en paralelo con la
	// mesa y el puerto puede tardar unos milisegundos en estar listo.
	// 0 usa el valor por defecto (reintentosPorDefecto).
	Reintentos int
	// EsperaReintento es cuánto esperar entre reintentos. 0 usa el valor por
	// defecto (esperaReintentoPorDefecto).
	EsperaReintento time.Duration
	// Log recibe los eventos del SDK. Si es nil, no se registra nada.
	Log *log.Logger
}

const (
	reintentosPorDefecto      = 20
	esperaReintentoPorDefecto = 100 * time.Millisecond
)

// ErrMesaCerro indica que la mesa cerró la conexión de forma limpia, que es
// como termina una partida normal. No es un fallo del bot.
var ErrMesaCerro = errors.New("botsdk: la mesa cerró la conexión")

// CorrerDesdeFlags es el atajo para un `func main` de bot: define los flags
// -addr y -token, los parsea y corre el bot hasta que la mesa cierre. Termina
// el proceso con código 1 si hubo un error real (no si la mesa simplemente
// cerró la partida).
//
// Si necesitás flags propios, definilos antes de llamar **pero no parsees**:
// CorrerDesdeFlags agrega -addr y -token (si no están ya definidos) y recién
// entonces llama a flag.Parse. Parsear vos mismo antes hace fallar el arranque,
// porque en ese momento -addr todavía no existe.
//
// Para usar el valor de un flag propio, implementá Inicializable: el SDK llama
// a Iniciar cuando los flags ya están parseados.
//
//	var semilla = flag.Uint64("semilla", 1, "semilla del generador")
//
//	func (b *MiBot) Iniciar() error {
//	    b.azar = rand.New(rand.NewPCG(*semilla, *semilla))
//	    return nil
//	}
func CorrerDesdeFlags(bot Bot) {
	if flag.Lookup("addr") == nil {
		flag.String("addr", "localhost:9000", "dirección de la mesa (host:puerto)")
	}
	if flag.Lookup("token") == nil {
		flag.String("token", "", "token de sesión ('casino login'). En modo abierto, es el ID del jugador")
	}
	if !flag.Parsed() {
		flag.Parse()
	}

	opciones := Opciones{
		Direccion: flag.Lookup("addr").Value.String(),
		Token:     flag.Lookup("token").Value.String(),
		Log:       log.New(os.Stderr, bot.Nombre()+": ", log.LstdFlags),
	}
	if opciones.Token == "" {
		opciones.Token = bot.Nombre()
	}

	err := Correr(context.Background(), bot, opciones)
	if err != nil && !errors.Is(err, ErrMesaCerro) {
		log.Fatalf("%s: %v", bot.Nombre(), err)
	}
}

// Correr conecta el bot a la mesa, hace el handshake y responde a cada
// solicitud de acción invocando Decidir, hasta que la mesa cierre la
// conexión, el bot se desconecte o el contexto se cancele. Bloquea.
//
// Devuelve ErrMesaCerro (envuelto) cuando la partida termina normalmente.
func Correr(ctx context.Context, bot Bot, opciones Opciones) error {
	// La inicialización va antes de conectar: si el bot no puede prepararse,
	// es mejor que no ocupe una silla que no va a poder jugar.
	if inicializable, ok := bot.(Inicializable); ok {
		if err := inicializable.Iniciar(); err != nil {
			return fmt.Errorf("botsdk: el bot no pudo inicializarse: %w", err)
		}
	}

	conexion, err := conectarConReintentos(ctx, opciones)
	if err != nil {
		return err
	}
	defer conexion.Close()

	// El cierre del socket es lo que desbloquea las lecturas si se cancela el
	// contexto: net.Conn no mira contextos por sí sola.
	listo := make(chan struct{})
	defer close(listo)
	go func() {
		select {
		case <-ctx.Done():
			conexion.Close()
		case <-listo:
		}
	}()

	codec := protocolo.NuevoCodec(conexion, conexion)
	sesion, err := saludar(codec, opciones)
	if err != nil {
		return err
	}
	registrar(opciones, "conectado a %s como %s (silla %d)", opciones.Direccion, sesion.id, sesion.silla)

	return atenderMesa(ctx, bot, codec, sesion, opciones)
}

// sesion es la identidad que la mesa nos asignó en el handshake.
type sesion struct {
	id    string
	silla int
}

func conectarConReintentos(ctx context.Context, opciones Opciones) (net.Conn, error) {
	reintentos := opciones.Reintentos
	if reintentos <= 0 {
		reintentos = reintentosPorDefecto
	}
	espera := opciones.EsperaReintento
	if espera <= 0 {
		espera = esperaReintentoPorDefecto
	}

	var dialer net.Dialer
	var ultimo error
	for intento := 0; intento < reintentos; intento++ {
		conexion, err := dialer.DialContext(ctx, "tcp", opciones.Direccion)
		if err == nil {
			return conexion, nil
		}
		ultimo = err

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(espera):
		}
	}
	return nil, fmt.Errorf("botsdk: no se pudo conectar a %s tras %d intentos: %w", opciones.Direccion, reintentos, ultimo)
}

// saludar manda el saludo y espera la bienvenida, de la que sale la identidad
// del bot en esta mesa.
func saludar(codec *protocolo.Codec, opciones Opciones) (sesion, error) {
	saludo := protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: protocolo.VersionProtocolo,
		Token:   opciones.Token,
	}
	if err := codec.Enviar(saludo); err != nil {
		return sesion{}, fmt.Errorf("botsdk: error al enviar el saludo: %w", err)
	}

	bienvenida, err := codec.RecibirMensajeMesa()
	if err != nil {
		return sesion{}, fmt.Errorf("botsdk: error al recibir la bienvenida: %w", err)
	}
	switch bienvenida.Tipo {
	case protocolo.MsgError:
		return sesion{}, fmt.Errorf("botsdk: la mesa rechazó la conexión: %s", bienvenida.Mensaje)
	case protocolo.MsgBienvenida:
		// Ok, seguimos.
	default:
		return sesion{}, fmt.Errorf("botsdk: se esperaba bienvenida, llegó %q", bienvenida.Tipo)
	}

	s := sesion{id: bienvenida.IDJugador, silla: bienvenida.Silla}
	if s.id == "" {
		// Una mesa que no informa la identidad solo puede estar en modo
		// abierto, donde el token es el ID. Se deja anotado en vez de fallar
		// para no romper contra una mesa vieja.
		s.id = opciones.Token
		s.silla = -1
		registrar(opciones, "la mesa no informó id_jugador; se asume que el token es el ID (modo abierto)")
	}
	return s, nil
}

// atenderMesa es el bucle principal: lee mensajes hasta que la conexión se
// cierre y responde a los que piden acción.
func atenderMesa(ctx context.Context, bot Bot, codec *protocolo.Codec, ses sesion, opciones Opciones) error {
	observaManos, _ := bot.(ObservaManos)
	observaEstado, _ := bot.(ObservaEstado)

	var mano protocolo.Mano

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := codec.RecibirMensajeMesa()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return fmt.Errorf("%w", ErrMesaCerro)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("botsdk: error de lectura: %w", err)
		}

		switch msg.Tipo {
		case protocolo.MsgManoInicio:
			if msg.Cartas != nil {
				mano = *msg.Cartas
			}
			if observaManos != nil {
				observaManos.ManoInicio(estadoDe(msg), mano)
			}

		case protocolo.MsgSolicitarAccion:
			estado := estadoDe(msg)
			turno := Turno{
				IDJugador: ses.id,
				Silla:     ses.silla,
				Yo:        buscarJugador(estado, ses.id),
				Mano:      mano,
				Estado:    estado,
				Validas:   msg.AccionesValidas,
				Timeout:   time.Duration(msg.TimeoutMs) * time.Millisecond,
			}

			accion := decidirConPlazo(ctx, bot, turno)
			respuesta := protocolo.MensajeBot{
				Tipo:    protocolo.MsgAccion,
				Version: protocolo.VersionProtocolo,
				IDMano:  estado.IDMano,
				Accion:  &accion,
			}
			if err := codec.Enviar(respuesta); err != nil {
				return fmt.Errorf("botsdk: error al enviar la acción: %w", err)
			}

		case protocolo.MsgEstado:
			if observaEstado != nil {
				observaEstado.EstadoActualizado(estadoDe(msg))
			}

		case protocolo.MsgManoFin:
			if observaManos != nil && msg.Resultado != nil {
				observaManos.ManoFin(*msg.Resultado)
			}

		case protocolo.MsgError:
			return fmt.Errorf("botsdk: error de la mesa: %s", msg.Mensaje)
		}
	}
}

// decidirConPlazo llama a Decidir con un contexto que vence junto al plazo de
// la mesa. No puede interrumpir un Decidir que ignore el contexto (Go no mata
// goroutines), pero le da al bot la forma correcta de enterarse de que se le
// acaba el tiempo: `select { case <-ctx.Done(): ... }`.
func decidirConPlazo(ctx context.Context, bot Bot, t Turno) protocolo.Accion {
	if t.Timeout <= 0 {
		return bot.Decidir(ctx, t)
	}
	ctxTurno, cancelar := context.WithTimeout(ctx, t.Timeout)
	defer cancelar()
	return bot.Decidir(ctxTurno, t)
}

func estadoDe(msg protocolo.MensajeMesa) protocolo.EstadoPublico {
	if msg.Estado == nil {
		return protocolo.EstadoPublico{}
	}
	return *msg.Estado
}

// buscarJugador ubica al bot dentro del estado público. Devuelve el cero de
// JugadorPublico si no aparece (por ejemplo, un estado de una mano en la que
// el bot todavía no está sentado).
func buscarJugador(estado protocolo.EstadoPublico, id string) protocolo.JugadorPublico {
	for _, j := range estado.Jugadores {
		if j.ID == id {
			return j
		}
	}
	return protocolo.JugadorPublico{ID: id}
}

func registrar(opciones Opciones, formato string, args ...any) {
	if opciones.Log == nil {
		return
	}
	opciones.Log.Printf(formato, args...)
}
