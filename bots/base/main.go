// Comando bot-base: el bot de referencia del torneo. No es fuerte, pero juega
// poker de verdad: mide la mano antes de decidir, apuesta cuando tiene algo y
// se retira cuando no.
//
// Está pensado como punto de partida para un participante: es corto, usa solo
// la API pública (pkg/botsdk y pkg/poker) y cada decisión está explicada. Si
// tu bot no le gana a este de forma consistente, todavía no está listo para el
// torneo.
//
// La estrategia:
//
//   - Preflop se puntúa la mano con la fórmula de Chen (un estándar viejo y
//     simple para decidir con qué manos entrar).
//   - Postflop se mira la jugada ya armada con pkg/poker, el mismo evaluador
//     que usa el crupier.
//
// Lo que NO hace, y es justamente por dónde se lo puede mejorar: no considera
// la posición, no lee el historial de acciones del rival, no hace proyectos
// (no distingue un color hecho de uno a una carta) y no farolea nunca.
package main

import (
	"context"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
	"github.com/amvz1704/pokerFight/pkg/poker"
)

type botBase struct{}

func (botBase) Nombre() string { return "base" }

func (b botBase) Decidir(ctx context.Context, t botsdk.Turno) protocolo.Accion {
	if len(t.Estado.Comunitarias) < 3 {
		return b.preflop(t)
	}
	return b.postflop(t)
}

// --- Preflop ---------------------------------------------------------------

// Umbrales de la puntuación de Chen. La escala va de -1 (72o, la peor) a 20
// (AA). Los cortes son deliberadamente conservadores: un bot que entra con
// todo pierde plata contra cualquiera que no lo haga.
const (
	chenParaSubir = 9.0 // Pares medios para arriba, AK, AQ...
	chenParaPagar = 5.0 // Manos jugables: conectores del mismo palo, ases chicos.
)

func (botBase) preflop(t botsdk.Turno) protocolo.Accion {
	puntaje := puntuacionChen(t.Mano)

	switch {
	case puntaje >= chenParaSubir:
		// Mano fuerte: se sube a tres ciegas grandes. Si ya hay una apuesta
		// grande sobre la mesa, Apostar la recorta a lo que la mesa acepte.
		return t.Apostar(t.Estado.CiegaGrande * 3)

	case puntaje >= chenParaPagar:
		if t.Puede(protocolo.Check) {
			return t.Check()
		}
		// Se paga solo mientras siga barato: con una mano jugable pero no
		// fuerte, meter un tercio del stack preflop es la forma más rápida de
		// perderlo.
		if t.Puede(protocolo.Call) && t.PorIgualar() <= t.Estado.CiegaGrande*3 {
			return t.Pagar()
		}
		return t.Fold()

	default:
		// Mano mala: solo se sigue si es gratis.
		if t.Puede(protocolo.Check) {
			return t.Check()
		}
		return t.Fold()
	}
}

// puntuacionChen implementa la fórmula de Bill Chen para manos iniciales:
//
//  1. Se toma la carta más alta: A=10, K=8, Q=7, J=6, y el resto vale la mitad
//     de su rango.
//  2. Si es par, se duplica ese valor, con un mínimo de 5.
//  3. +2 si son del mismo palo.
//  4. Se resta según el hueco entre las cartas: 1 hueco -1, 2 huecos -2,
//     3 huecos -4, 4 o más -5.
//  5. +1 si las dos cartas son menores que Q y el hueco es 0 o 1 (una
//     conectora chica puede armar escalera por los dos lados).
func puntuacionChen(mano protocolo.Mano) float64 {
	alta, baja := mano[0].Rango, mano[1].Rango
	if baja > alta {
		alta, baja = baja, alta
	}

	puntaje := valorChen(alta)

	if alta == baja {
		puntaje *= 2
		if puntaje < 5 {
			puntaje = 5
		}
	}

	if mano[0].Palo == mano[1].Palo {
		puntaje += 2
	}

	hueco := int(alta) - int(baja) - 1
	switch {
	case hueco <= 0:
		// Conectoras o par: sin descuento.
	case hueco == 1:
		puntaje -= 1
	case hueco == 2:
		puntaje -= 2
	case hueco == 3:
		puntaje -= 4
	default:
		puntaje -= 5
	}

	if hueco <= 1 && alta < protocolo.Reina && alta != baja {
		puntaje += 1
	}

	return puntaje
}

// valorChen es el valor de la carta más alta en la fórmula de Chen.
func valorChen(r protocolo.Rango) float64 {
	switch r {
	case protocolo.As:
		return 10
	case protocolo.Rey:
		return 8
	case protocolo.Reina:
		return 7
	case protocolo.Jota:
		return 6
	default:
		return float64(r) / 2
	}
}

// --- Postflop --------------------------------------------------------------

func (botBase) postflop(t botsdk.Turno) protocolo.Accion {
	// El mismo evaluador que usa el crupier para decidir el showdown: lo que
	// este bot cree que tiene es exactamente lo que la mesa va a puntuar.
	jugada := poker.Mejor(t.Mano, t.Estado.Comunitarias)

	switch {
	case jugada.Categoria >= poker.Trio:
		// Mano grande: se apuesta fuerte y se paga cualquier subida.
		if t.Puede(protocolo.Bet) || t.Puede(protocolo.Raise) {
			return t.ApostarPozo(0.75)
		}
		if t.Puede(protocolo.Call) {
			return t.Pagar()
		}
		return t.Check()

	case jugada.Categoria >= poker.Par:
		// Mano mediana: se apuesta chico para cobrarle a manos peores, pero no
		// se paga una apuesta grande.
		if t.Puede(protocolo.Check) && t.Puede(protocolo.Bet) {
			return t.ApostarPozo(0.4)
		}
		if t.Puede(protocolo.Call) && t.PorIgualar()*2 <= t.Estado.Pozo {
			return t.Pagar()
		}
		if t.Puede(protocolo.Check) {
			return t.Check()
		}
		return t.Fold()

	default:
		// Carta alta: no hay nada que defender.
		if t.Puede(protocolo.Check) {
			return t.Check()
		}
		return t.Fold()
	}
}

func main() { botsdk.CorrerDesdeFlags(botBase{}) }
