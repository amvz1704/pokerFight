package crupier

import (
	"fmt"
	"sort"
)

// SubPozo es un pozo (principal o lateral) con su monto y los jugadores
// que pueden ganarlo.
type SubPozo struct {
	Monto     int64
	Elegibles []string // IDs de jugadores activos que compiten por este sub-pozo
}

// Pozo administra las apuestas de una mano, incluyendo pozos laterales por all-in.
type Pozo struct {
	contribuciones map[string]int64  // total acumulado por jugador durante la mano
	activos        map[string]bool   // jugadores que aun no han hecho fold
}

// NuevoPozo crea un pozo vacio para la lista de jugadores dada.
func NuevoPozo(jugadores []string) *Pozo {
	p := &Pozo{
		contribuciones: make(map[string]int64, len(jugadores)),
		activos:        make(map[string]bool, len(jugadores)),
	}
	for _, id := range jugadores {
		p.activos[id] = true
	}
	return p
}

// Apostar registra monto fichas adicionales del jugador al pozo.
func (p *Pozo) Apostar(idJugador string, monto int64) error {
	if monto < 0 {
		return fmt.Errorf("crupier: apuesta negativa de %q: %d", idJugador, monto)
	}
	p.contribuciones[idJugador] += monto
	return nil
}

// Fold retira al jugador de los elegibles. Sus fichas ya aportadas permanecen.
func (p *Pozo) Fold(idJugador string) {
	delete(p.activos, idJugador)
}

// Total devuelve la suma de todas las fichas comprometidas al pozo.
func (p *Pozo) Total() int64 {
	var total int64
	for _, v := range p.contribuciones {
		total += v
	}
	return total
}

// Descomponer divide el pozo en sub-pozos segun los topes de contribucion.
// Cada nivel unico de contribucion genera un sub-pozo distinto, lo que permite
// calcular correctamente los pozos laterales cuando hay jugadores all-in.
//
// Los jugadores que hicieron fold contribuyen fichas al pozo pero no aparecen
// en Elegibles de ningun sub-pozo.
func (p *Pozo) Descomponer() []SubPozo {
	if len(p.contribuciones) == 0 {
		return nil
	}

	// Recolectar niveles unicos de contribucion.
	seen := make(map[int64]bool, len(p.contribuciones))
	var niveles []int64
	for _, total := range p.contribuciones {
		if total > 0 && !seen[total] {
			seen[total] = true
			niveles = append(niveles, total)
		}
	}
	sort.Slice(niveles, func(i, j int) bool { return niveles[i] < niveles[j] })

	var subPozos []SubPozo
	var anterior int64
	for _, tope := range niveles {
		tramo := tope - anterior

		// Fichas en este tramo: cada jugador aporta min(su_total - anterior, tramo).
		var fichas int64
		for _, contrib := range p.contribuciones {
			aporte := contrib - anterior
			if aporte > tramo {
				aporte = tramo
			}
			if aporte < 0 {
				aporte = 0
			}
			fichas += aporte
		}

		// Elegibles: activos cuya contribucion total cubre este tope.
		var elegibles []string
		for id := range p.activos {
			if p.contribuciones[id] >= tope {
				elegibles = append(elegibles, id)
			}
		}
		sort.Strings(elegibles) // orden determinista

		if fichas > 0 {
			subPozos = append(subPozos, SubPozo{Monto: fichas, Elegibles: elegibles})
		}
		anterior = tope
	}
	return subPozos
}

// Reiniciar limpia el pozo para una nueva mano con la lista de jugadores dada.
func (p *Pozo) Reiniciar(jugadores []string) {
	p.contribuciones = make(map[string]int64, len(jugadores))
	p.activos = make(map[string]bool, len(jugadores))
	for _, id := range jugadores {
		p.activos[id] = true
	}
}
