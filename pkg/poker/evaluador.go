// Package poker es la evaluación de manos de Texas Hold'em, expuesta como API
// pública para que los bots del torneo usen exactamente el mismo evaluador que
// el Crupier. Sin esto, cada participante tendría que reimplementarlo y
// arriesgarse a que su idea de "qué mano gana" no coincida con la de la mesa.
//
// Depende solo de internal/protocolo (los tipos de carta del contrato) y no
// sabe nada de red, mesa ni cuentas.
//
// Uso típico desde un bot:
//
//	ev := poker.Mejor(t.Mano, t.Estado.Comunitarias)
//	if ev.Categoria >= poker.Trio {
//	    return t.ApostarPozo(0.75)
//	}
package poker

import (
	"fmt"
	"sort"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Categoria es el tipo de jugada, de menor a mayor. Se puede comparar
// directamente: CartaAlta < Par < DoblePar < ...
type Categoria int

const (
	CartaAlta       Categoria = 0
	Par             Categoria = 1
	DoblePar        Categoria = 2
	Trio            Categoria = 3
	Escalera        Categoria = 4
	Color           Categoria = 5
	FullHouse       Categoria = 6
	Poker           Categoria = 7
	EscaleraDeColor Categoria = 8
)

var nombresCategoria = map[Categoria]string{
	CartaAlta:       "carta alta",
	Par:             "par",
	DoblePar:        "doble par",
	Trio:            "trio",
	Escalera:        "escalera",
	Color:           "color",
	FullHouse:       "full house",
	Poker:           "poker",
	EscaleraDeColor: "escalera de color",
}

// String devuelve el nombre de la categoría en español.
func (c Categoria) String() string {
	if n, ok := nombresCategoria[c]; ok {
		return n
	}
	return fmt.Sprintf("categoria(%d)", int(c))
}

// Evaluacion es el valor de una mano ya resuelta.
type Evaluacion struct {
	// Puntaje es un entero monótono: comparar dos manos es comparar dos
	// Puntaje. Igual Puntaje significa empate exacto y pozo dividido. No tiene
	// significado fuera de esa comparación, así que no lo interpretes ni lo
	// persistas: puede cambiar de escala entre versiones.
	Puntaje int
	// Categoria es el tipo de jugada. Es lo que conviene usar para decidir
	// (`>= poker.Trio`), no el Puntaje.
	Categoria Categoria
	// Descripcion es el nombre legible de la jugada, para logs y para el
	// mensaje mano_fin.
	Descripcion string
	// Mejores5 son las cinco cartas que forman la jugada.
	Mejores5 [5]protocolo.Carta
}

// ErrCartasInsuficientes se devuelve cuando hay menos de 5 cartas para armar
// una jugada.
var ErrCartasInsuficientes = fmt.Errorf("poker: hacen falta al menos 5 cartas para evaluar")

// Mejor devuelve la mejor jugada de 5 cartas entre las privadas y las
// comunitarias. Es la versión cómoda para bots: si todavía no hay 5 cartas
// (preflop, por ejemplo) devuelve una Evaluacion en cero, no un error.
func Mejor(privadas protocolo.Mano, comunitarias []protocolo.Carta) Evaluacion {
	ev, err := MejorDe(append(privadas[:], comunitarias...))
	if err != nil {
		return Evaluacion{}
	}
	return ev
}

// MejorDe devuelve la mejor jugada de 5 cartas entre las que se le pasen (5, 6
// o 7 en Hold'em). Devuelve ErrCartasInsuficientes si hay menos de 5.
func MejorDe(cartas []protocolo.Carta) (Evaluacion, error) {
	if len(cartas) < 5 {
		return Evaluacion{}, ErrCartasInsuficientes
	}

	mejor := Evaluacion{Puntaje: -1}
	// Se recorren las C(n,5) combinaciones sin materializarlas todas: con 7
	// cartas son 21, pero armar los slices igual costaba una asignación por
	// combinación en el camino caliente de cada showdown.
	combinar(cartas, func(c5 [5]protocolo.Carta) {
		ev := Evaluar5(c5)
		if ev.Puntaje > mejor.Puntaje {
			mejor = ev
		}
	})
	return mejor, nil
}

// combinar invoca fn con cada combinación de 5 cartas de las dadas.
func combinar(cartas []protocolo.Carta, fn func([5]protocolo.Carta)) {
	n := len(cartas)
	var c5 [5]protocolo.Carta
	var recorrer func(inicio, puestas int)
	recorrer = func(inicio, puestas int) {
		if puestas == 5 {
			fn(c5)
			return
		}
		// Poda: si no quedan suficientes cartas para llenar las 5, cortar.
		for i := inicio; i <= n-(5-puestas); i++ {
			c5[puestas] = cartas[i]
			recorrer(i+1, puestas+1)
		}
	}
	recorrer(0, 0)
}

// empaquetar compacta la jerarquía de la mano en un entero usando bit-packing,
// para que comparar manos sea comparar enteros.
//
// Estructura (bloques de 4 bits, porque el As vale 14 = 1110 en binario):
//
//	[Categoria] [Kicker 1] [Kicker 2] [Kicker 3] [Kicker 4] [Kicker 5]
//
// Ejemplo: full house de reyes (13) y ochos (8) ->
// empaquetar(FullHouse, 13, 8, 0, 0, 0) = (6<<20)|(13<<16)|(8<<12).
func empaquetar(categoria Categoria, k1, k2, k3, k4, k5 int) int {
	return (int(categoria) << 20) | (k1 << 16) | (k2 << 12) | (k3 << 8) | (k4 << 4) | k5
}

// Evaluar5 puntúa exactamente 5 cartas.
func Evaluar5(cartas [5]protocolo.Carta) Evaluacion {
	c := cartas
	// Ordenar de mayor a menor rango.
	sort.Slice(c[:], func(i, j int) bool { return c[i].Rango > c[j].Rango })

	esColor := true
	for i := 1; i < 5; i++ {
		if c[i].Palo != c[0].Palo {
			esColor = false
			break
		}
	}

	esEscalera := true
	for i := 1; i < 5; i++ {
		if c[i].Rango != c[i-1].Rango-1 {
			esEscalera = false
			break
		}
	}

	// Caso especial: la escalera A-2-3-4-5, donde el As cuenta como 1. Al
	// estar ordenado de mayor a menor, se ve como As, 5, 4, 3, 2.
	esEscaleraBaja := false
	if !esEscalera && c[0].Rango == protocolo.As && c[1].Rango == protocolo.Cinco &&
		c[2].Rango == protocolo.Cuatro && c[3].Rango == protocolo.Tres && c[4].Rango == protocolo.Dos {
		esEscalera = true
		esEscaleraBaja = true
	}

	// Frecuencias por rango, para identificar pares, tríos y póker. Se usa un
	// arreglo indexado por rango (2..14) en vez de un map: se llama una vez
	// por combinación de cada showdown y un map cuesta una asignación.
	var frecuencias [15]int
	for _, carta := range c {
		frecuencias[carta.Rango]++
	}

	var cuads, trios, pares, sueltas []int
	// De mayor a menor, así cada grupo ya sale ordenado y no hace falta
	// ordenarlo después.
	for rango := int(protocolo.As); rango >= int(protocolo.Dos); rango-- {
		switch frecuencias[rango] {
		case 4:
			cuads = append(cuads, rango)
		case 3:
			trios = append(trios, rango)
		case 2:
			pares = append(pares, rango)
		case 1:
			sueltas = append(sueltas, rango)
		}
	}

	evaluacion := func(cat Categoria, puntaje int, desc string) Evaluacion {
		return Evaluacion{Puntaje: puntaje, Categoria: cat, Descripcion: desc, Mejores5: c}
	}

	if esEscalera && esColor {
		alta := int(c[0].Rango)
		if esEscaleraBaja {
			alta = 5 // El 5 es la carta más alta real de esta escalera.
		}
		desc := "escalera de color"
		if alta == int(protocolo.As) {
			desc = "escalera real"
		}
		return evaluacion(EscaleraDeColor, empaquetar(EscaleraDeColor, alta, 0, 0, 0, 0), desc)
	}

	if len(cuads) == 1 {
		return evaluacion(Poker, empaquetar(Poker, cuads[0], sueltas[0], 0, 0, 0), "poker")
	}

	if len(trios) == 1 && len(pares) == 1 {
		return evaluacion(FullHouse, empaquetar(FullHouse, trios[0], pares[0], 0, 0, 0), "full house")
	}

	if esColor {
		return evaluacion(Color, empaquetar(Color,
			int(c[0].Rango), int(c[1].Rango), int(c[2].Rango), int(c[3].Rango), int(c[4].Rango)), "color")
	}

	if esEscalera {
		alta := int(c[0].Rango)
		if esEscaleraBaja {
			alta = 5
		}
		return evaluacion(Escalera, empaquetar(Escalera, alta, 0, 0, 0, 0), "escalera")
	}

	if len(trios) == 1 {
		return evaluacion(Trio, empaquetar(Trio, trios[0], sueltas[0], sueltas[1], 0, 0), "trio")
	}

	if len(pares) == 2 {
		return evaluacion(DoblePar, empaquetar(DoblePar, pares[0], pares[1], sueltas[0], 0, 0), "doble par")
	}

	if len(pares) == 1 {
		return evaluacion(Par, empaquetar(Par, pares[0], sueltas[0], sueltas[1], sueltas[2], 0), "par")
	}

	return evaluacion(CartaAlta, empaquetar(CartaAlta,
		int(c[0].Rango), int(c[1].Rango), int(c[2].Rango), int(c[3].Rango), int(c[4].Rango)), "carta alta")
}
