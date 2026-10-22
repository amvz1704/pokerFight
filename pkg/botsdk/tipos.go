// Este archivo re-exporta los tipos del protocolo que un bot necesita
// nombrar.
//
// Existe porque internal/protocolo no es importable desde fuera del módulo
// (lo impone el compilador de Go), y los bots de los participantes viven fuera
// del módulo. Sin estos alias, un bot externo no podría ni escribir la firma
// de Decidir, que devuelve una Accion: el tipo existiría pero no tendría
// nombre que se pueda importar.
//
// Son alias (=), no tipos nuevos: botsdk.Accion y protocolo.Accion son
// exactamente el mismo tipo, así que no hay conversión que hacer en ningún
// lado. Si se agrega al protocolo algo que un bot tenga que nombrar, se agrega
// también acá.
package botsdk

import "github.com/amvz1704/pokerFight/internal/protocolo"

// --- Acciones ---------------------------------------------------------------

// Accion es la respuesta del bot en su turno. Monto es el TOTAL al que se
// lleva la apuesta en la ronda, no el incremento (docs/protocolo.md).
type Accion = protocolo.Accion

// TipoAccion es la jugada declarada: fold, check, call, bet, raise o allin.
type TipoAccion = protocolo.TipoAccion

const (
	Fold  = protocolo.Fold
	Check = protocolo.Check
	Call  = protocolo.Call
	Bet   = protocolo.Bet
	Raise = protocolo.Raise
	AllIn = protocolo.AllIn
)

// --- Estado de la mesa ------------------------------------------------------

type (
	EstadoPublico    = protocolo.EstadoPublico
	JugadorPublico   = protocolo.JugadorPublico
	AccionRegistrada = protocolo.AccionRegistrada
	ResultadoMano    = protocolo.ResultadoMano
	Reparto          = protocolo.Reparto
)

// Etapa es la calle de la mano en curso.
type Etapa = protocolo.Etapa

const (
	PreFlop  = protocolo.PreFlop
	Flop     = protocolo.Flop
	Turn     = protocolo.Turn
	River    = protocolo.River
	Showdown = protocolo.Showdown
)

// --- Cartas -----------------------------------------------------------------

type (
	Carta = protocolo.Carta
	Mano  = protocolo.Mano
	Rango = protocolo.Rango
	Palo  = protocolo.Palo
)

const (
	Dos    = protocolo.Dos
	Tres   = protocolo.Tres
	Cuatro = protocolo.Cuatro
	Cinco  = protocolo.Cinco
	Seis   = protocolo.Seis
	Siete  = protocolo.Siete
	Ocho   = protocolo.Ocho
	Nueve  = protocolo.Nueve
	Diez   = protocolo.Diez
	Jota   = protocolo.Jota
	Reina  = protocolo.Reina
	Rey    = protocolo.Rey
	As     = protocolo.As
)

const (
	Treboles  = protocolo.Treboles
	Diamantes = protocolo.Diamantes
	Corazones = protocolo.Corazones
	Picas     = protocolo.Picas
)
