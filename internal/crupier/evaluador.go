package crupier

import (
	"sort"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// empaquetar compacta la jerarquía de la mano en un entero de 32 bits usando "bit-packing".
// Permite comparar qué mano gana matemáticamente con un simple (Puntaje A > Puntaje B).
//
// Estructura de los 32 bits (en bloques de 4 bits, ya que el As vale 14 = 1110 en binario):
// [Categoría] [Kicker 1] [Kicker 2] [Kicker 3] [Kicker 4] [Kicker 5]
//
// Categorías: 8=Escalera Color, 7=Poker, 6=Full, 5=Color, 4=Escalera, 3=Trío, 2=Doble Par, 1=Par, 0=Alta.
//
// Ejemplo: Full House (Categoría 6) de Reyes (K=13) y Ochos (8)
// empaquetar(6, 13, 8, 0, 0, 0)
// Resultado: (6<<20) | (13<<16) | (8<<12) -> 0x06D8000
func empaquetar(categoria int, k1, k2, k3, k4, k5 int) int {
	return (categoria << 20) | (k1 << 16) | (k2 << 12) | (k3 << 8) | (k4 << 4) | k5
}

// evaluar5 recibe exactamente 5 cartas y devuelve el puntaje absoluto de la mano.
func evaluar5(cartas [5]protocolo.Carta) (int, string) {
	c := cartas
	// Ordenar de mayor a menor rango.
	sort.Slice(c[:], func(i, j int) bool {
		return c[i].Rango > c[j].Rango
	})

	// Comprobar color (flush).
	esColor := true
	for i := 1; i < 5; i++ {
		if c[i].Palo != c[0].Palo {
			esColor = false
			break
		}
	}

	// Comprobar escalera (straight).
	esEscalera := true
	for i := 1; i < 5; i++ {
		if c[i].Rango != c[i-1].Rango-1 {
			esEscalera = false
			break
		}
	}

	// Caso especial: escalera A-2-3-4-5.
	// Al estar ordenado, será As (14), 5, 4, 3, 2.
	esEscaleraBaja := false
	if !esEscalera && c[0].Rango == protocolo.As && c[1].Rango == protocolo.Cinco &&
		c[2].Rango == protocolo.Cuatro && c[3].Rango == protocolo.Tres && c[4].Rango == protocolo.Dos {
		esEscalera = true
		esEscaleraBaja = true
	}

	// Contar frecuencias para identificar pares, tríos, etc.
	counts := make(map[protocolo.Rango]int)
	for _, carta := range c {
		counts[carta.Rango]++
	}

	var cuads, trios, pares, sueltas []int
	for rango, cant := range counts {
		r := int(rango)
		switch cant {
		case 4:
			cuads = append(cuads, r)
		case 3:
			trios = append(trios, r)
		case 2:
			pares = append(pares, r)
		case 1:
			sueltas = append(sueltas, r)
		}
	}

	// Ordenar agrupaciones para tener el par/trío más alto primero (necesario en 7 cartas, y útil en 5).
	sort.Sort(sort.Reverse(sort.IntSlice(cuads)))
	sort.Sort(sort.Reverse(sort.IntSlice(trios)))
	sort.Sort(sort.Reverse(sort.IntSlice(pares)))
	sort.Sort(sort.Reverse(sort.IntSlice(sueltas)))

	// Asignar categoría y calcular puntaje.
	if esEscalera && esColor {
		alta := int(c[0].Rango)
		if esEscaleraBaja {
			alta = 5 // El 5 es la carta más alta real de esta escalera
		}
		if alta == 14 {
			return empaquetar(8, alta, 0, 0, 0, 0), "escalera real"
		}
		return empaquetar(8, alta, 0, 0, 0, 0), "escalera de color"
	}

	if len(cuads) == 1 {
		return empaquetar(7, cuads[0], sueltas[0], 0, 0, 0), "poker"
	}

	if len(trios) == 1 && len(pares) == 1 {
		return empaquetar(6, trios[0], pares[0], 0, 0, 0), "full house"
	}

	if esColor {
		return empaquetar(5, int(c[0].Rango), int(c[1].Rango), int(c[2].Rango), int(c[3].Rango), int(c[4].Rango)), "color"
	}

	if esEscalera {
		alta := int(c[0].Rango)
		if esEscaleraBaja {
			alta = 5
		}
		return empaquetar(4, alta, 0, 0, 0, 0), "escalera"
	}

	if len(trios) == 1 {
		return empaquetar(3, trios[0], sueltas[0], sueltas[1], 0, 0), "trio"
	}

	if len(pares) == 2 {
		return empaquetar(2, pares[0], pares[1], sueltas[0], 0, 0), "doble par"
	}

	if len(pares) == 1 {
		return empaquetar(1, pares[0], sueltas[0], sueltas[1], sueltas[2], 0), "par"
	}

	return empaquetar(0, int(c[0].Rango), int(c[1].Rango), int(c[2].Rango), int(c[3].Rango), int(c[4].Rango)), "carta alta"
}

// combinaciones5 genera todas las C(n, 5) combinaciones posibles de cartas.
func combinaciones5(cartas []protocolo.Carta) [][]protocolo.Carta {
	var result [][]protocolo.Carta
	n := len(cartas)
	if n < 5 {
		return nil
	}

	var aux func(inicio int, temp []protocolo.Carta)
	aux = func(inicio int, temp []protocolo.Carta) {
		if len(temp) == 5 {
			c := make([]protocolo.Carta, 5)
			copy(c, temp)
			result = append(result, c)
			return
		}
		for i := inicio; i < n; i++ {
			aux(i+1, append(temp, cartas[i]))
		}
	}
	aux(0, make([]protocolo.Carta, 0, 5))
	return result
}
