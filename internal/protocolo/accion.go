// Package protocolo define los tipos y mensajes compartidos entre el
// Crupier, la Mesa y los Bots. Es la ÚNICA dependencia comun entre los tres
// modulos: nadie importa a otro modulo directamente, todos hablan protocolo.
package protocolo

// TipoAccion es la jugada que un bot puede declarar en su turno.
type TipoAccion string

const (
	Fold  TipoAccion = "fold"  // Se retira de la mano.
	Check TipoAccion = "check" // Pasa sin apostar (solo si no hay apuesta pendiente).
	Call  TipoAccion = "call"  // Iguala la apuesta pendiente.
	Bet   TipoAccion = "bet"   // Abre apuesta (solo si no hay apuesta pendiente).
	Raise TipoAccion = "raise" // Sube la apuesta pendiente.
	AllIn TipoAccion = "allin" // Apuesta todo el saldo restante.
)

// Accion es la respuesta de un bot ante una solicitud de turno.
// Monto se interpreta como el total al que el jugador lleva su apuesta en la
// ronda actual (no el incremento). Se ignora en Fold, Check y Call.
type Accion struct {
	Tipo  TipoAccion `json:"tipo"`
	Monto int64      `json:"monto,omitempty"`
}

// AccionSegura devuelve la accion por defecto cuando un bot no responde a
// tiempo o responde algo invalido: Check si es gratis, Fold en caso contrario.
// Requisito funcional: "el sistema debe decidir la opcion mas segura en caso
// de omision de acciones".
func AccionSegura(apuestaPendiente int64) Accion {
	if apuestaPendiente <= 0 {
		return Accion{Tipo: Check}
	}
	return Accion{Tipo: Fold}
}

// Etapa es la fase de reparto de cartas comunitarias de una mano.
type Etapa string

const (
	PreFlop  Etapa = "preflop" // 0 cartas comunitarias
	Flop     Etapa = "flop"    // 3 cartas comunitarias
	Turn     Etapa = "turn"    // 4 cartas comunitarias
	River    Etapa = "river"   // 5 cartas comunitarias
	Showdown Etapa = "showdown"
)

// CartasComunitarias indica cuantas cartas hay en la mesa en cada etapa.
var CartasComunitarias = map[Etapa]int{
	PreFlop: 0, Flop: 3, Turn: 4, River: 5, Showdown: 5,
}
