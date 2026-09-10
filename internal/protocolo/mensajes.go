package protocolo

// VersionProtocolo se envia en el saludo y en cada mensaje de la Mesa. Si el
// saludo del bot no la trae exacta, la Mesa rechaza la conexion.
//
// Politica de versionado (semver sobre el contrato, no sobre el codigo):
//   - PATCH: correcciones que no cambian ningun campo (docs, textos).
//   - MINOR: campos nuevos opcionales. Un bot viejo los ignora y sigue
//     jugando, pero igual debe declarar la version nueva en el saludo.
//   - MAYOR: cualquier cambio que rompa a un bot existente.
//
// Ver docs/protocolo.md, que es la referencia normativa de este contrato.
const VersionProtocolo = "1.0.0"

// --- Vista publica del estado ---------------------------------------------

// JugadorPublico es lo que un bot puede ver de sus rivales. Nunca incluye
// cartas privadas: eso lo garantiza la Mesa al construir el mensaje.
type JugadorPublico struct {
	ID           string `json:"id"`
	Nombre       string `json:"nombre"`
	Saldo        int64  `json:"saldo"`         // Fichas restantes.
	ApuestaRonda int64  `json:"apuesta_ronda"` // Apostado en la ronda actual.

	// Activo es "sigue en la mano en curso": false en cuanto hace fold.
	// Vuelve a true al repartirse la mano siguiente.
	Activo bool `json:"activo"`
	// EnTorneo es "sigue en la partida": false cuando se quedo sin fichas y
	// quedo eliminado. Un jugador con EnTorneo=false no vuelve a recibir
	// cartas. Se distingue de Activo a proposito: un bot necesita las dos
	// cosas (a quien le puedo ganar la mano vs. cuantos rivales quedan).
	EnTorneo bool `json:"en_torneo"`

	AllIn         bool `json:"allin"`          // true si ya no puede apostar mas en esta mano.
	EnMano        bool `json:"en_mano"`        // true si recibio cartas en la mano en curso.
	PosicionSilla int  `json:"posicion_silla"` // 0..MaxJugadores-1, sentido horario.
}

// EstadoPublico es la foto de la mesa que se envia a un bot en su turno.
type EstadoPublico struct {
	IDMano            string             `json:"id_mano"`
	Etapa             Etapa              `json:"etapa"`
	Comunitarias      []Carta            `json:"comunitarias"`
	Pozo              int64              `json:"pozo"`
	ApuestaActual     int64              `json:"apuesta_actual"` // Apuesta mas alta de la ronda.
	SubidaMinima      int64              `json:"subida_minima"`  // Incremento minimo para Raise.
	CiegaChica        int64              `json:"ciega_chica"`
	CiegaGrande       int64              `json:"ciega_grande"`
	PosicionBoton     int                `json:"posicion_boton"`
	Jugadores         []JugadorPublico   `json:"jugadores"`
	HistorialAcciones []AccionRegistrada `json:"historial_acciones"`
}

// AccionRegistrada es una accion ya ejecutada, para que los bots puedan
// razonar sobre el desarrollo de la mano.
type AccionRegistrada struct {
	IDJugador string `json:"id_jugador"`
	Etapa     Etapa  `json:"etapa"`
	Accion    Accion `json:"accion"`
}

// --- Mensajes Mesa -> Bot -------------------------------------------------

// TipoMensajeMesa identifica el contenido de un MensajeMesa.
type TipoMensajeMesa string

const (
	MsgBienvenida      TipoMensajeMesa = "bienvenida"
	MsgManoInicio      TipoMensajeMesa = "mano_inicio" // Entrega cartas privadas.
	MsgEstado          TipoMensajeMesa = "estado"      // Actualizacion sin turno.
	MsgSolicitarAccion TipoMensajeMesa = "solicitar_accion"
	MsgManoFin         TipoMensajeMesa = "mano_fin"
	MsgError           TipoMensajeMesa = "error"
)

// MensajeMesa es el sobre de todo lo que la Mesa envia al bot.
// Solo uno de los campos opcionales viene poblado, segun Tipo.
type MensajeMesa struct {
	Tipo            TipoMensajeMesa `json:"tipo"`
	Version         string          `json:"version"`
	Estado          *EstadoPublico  `json:"estado,omitempty"`
	Cartas          *Mano           `json:"cartas,omitempty"`           // Solo en MsgManoInicio.
	AccionesValidas []TipoAccion    `json:"acciones_validas,omitempty"` // Solo en MsgSolicitarAccion.
	TimeoutMs       int             `json:"timeout_ms,omitempty"`       // Plazo para responder.
	Resultado       *ResultadoMano  `json:"resultado,omitempty"`        // Solo en MsgManoFin.
	Mensaje         string          `json:"mensaje,omitempty"`          // Texto libre / error.

	// Identidad del destinatario. Se envia en MsgBienvenida y es la unica
	// forma que tiene un bot de saber cual de los EstadoPublico.Jugadores es
	// el mismo: el token no sirve como identificador salvo en modo abierto,
	// porque con Casino la Mesa usa el ID de cuenta que devolvio ValidarToken.
	IDJugador string `json:"id_jugador,omitempty"` // Solo en MsgBienvenida.
	Silla     int    `json:"silla,omitempty"`      // Solo en MsgBienvenida. 0..MaxJugadores-1.
}

// --- Mensajes Bot -> Mesa -------------------------------------------------

// TipoMensajeBot identifica el contenido de un MensajeBot.
type TipoMensajeBot string

const (
	MsgSaludo   TipoMensajeBot = "saludo"   // Handshake + token de sesion.
	MsgAccion   TipoMensajeBot = "accion"   // Respuesta a MsgSolicitarAccion.
	MsgAbandono TipoMensajeBot = "abandono" // Se retira de la mesa.
)

// MensajeBot es el sobre de todo lo que el bot envia a la Mesa.
type MensajeBot struct {
	Tipo    TipoMensajeBot `json:"tipo"`
	Version string         `json:"version,omitempty"`
	Token   string         `json:"token,omitempty"`   // Emitido por el Casino.
	IDMano  string         `json:"id_mano,omitempty"` // Ecos para evitar acciones tardias.
	Accion  *Accion        `json:"accion,omitempty"`
}

// --- Resultado ------------------------------------------------------------

// Reparto indica cuanto se lleva un jugador de un pozo concreto.
type Reparto struct {
	IDJugador string `json:"id_jugador"`
	Monto     int64  `json:"monto"`
}

// ResultadoMano es el cierre de una mano, emitido por el Crupier y
// distribuido por la Mesa.
type ResultadoMano struct {
	IDMano       string            `json:"id_mano"`
	Comunitarias []Carta           `json:"comunitarias"`
	Repartos     []Reparto         `json:"repartos"`
	Mostradas    map[string]Mano   `json:"mostradas,omitempty"`   // Cartas reveladas en showdown.
	Descripcion  map[string]string `json:"descripcion,omitempty"` // "color", "par de reyes"...
}

// ErrAccionAusente se devuelve cuando un bot responde sin accion.
var ErrAccionAusente = errorAccionAusente{}

type errorAccionAusente struct{}

func (errorAccionAusente) Error() string { return "protocolo: mensaje sin accion" }
