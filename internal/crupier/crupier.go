package crupier

import (
	"fmt"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Participante asocia un ID de jugador a la mano recibida.
type Participante struct {
	ID   string
	Mano protocolo.Mano
}

// Evaluacion contiene el puntaje de una mano, permitiendo comparar
// quién gana la partida de forma sencilla (mayor puntaje gana).
type Evaluacion struct {
	Puntaje     int
	Descripcion string
	Mejores5    [5]protocolo.Carta
}

// Crupier administra el mazo y la evaluación de manos.
// No conoce la red, las cuentas ni administra el pozo de apuestas directamente.
type Crupier interface {
	NuevaMano(idMano string) error
	RepartirPrivadas(cantidadJugadores int) ([]protocolo.Mano, error)
	RepartirComunitarias(etapa protocolo.Etapa) ([]protocolo.Carta, error)
	Evaluar(privadas protocolo.Mano, comunitarias []protocolo.Carta) (Evaluacion, error)
	DecidirGanadores(participantes []Participante, comunitarias []protocolo.Carta, pozo *Pozo) (protocolo.ResultadoMano, error)
}

// motor es la implementación concreta de Crupier.
type motor struct {
	mazo   Mazo
	idMano string
}

// Nuevo crea una nueva instancia del crupier.
func Nuevo() Crupier {
	return &motor{
		mazo: NuevoMazo(),
	}
}

// NuevaMano prepara el mazo para una mano nueva.
func (m *motor) NuevaMano(idMano string) error {
	m.idMano = idMano
	m.mazo = NuevoMazo()
	return m.mazo.Barajar()
}

// RepartirPrivadas roba dos cartas por jugador en dos vueltas (estilo Texas Hold'em).
// Devuelve un slice con las manos en orden de silla (índice 0 = silla 0).
func (m *motor) RepartirPrivadas(cantidadJugadores int) ([]protocolo.Mano, error) {
	if cantidadJugadores <= 0 {
		return nil, fmt.Errorf("crupier: cantidad de jugadores debe ser mayor a 0")
	}
	manos := make([]protocolo.Mano, cantidadJugadores)

	// Vuelta 1: una carta a cada jugador en orden.
	for i := 0; i < cantidadJugadores; i++ {
		carta, err := m.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: repartir (vuelta 1, jugador %d): %w", i, err)
		}
		manos[i][0] = carta
	}

	// Vuelta 2: segunda carta a cada jugador en orden.
	for i := 0; i < cantidadJugadores; i++ {
		carta, err := m.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: repartir (vuelta 2, jugador %d): %w", i, err)
		}
		manos[i][1] = carta
	}

	return manos, nil
}

// RepartirComunitarias roba las cartas correspondientes a la etapa tras quemar una.
// - Flop: Quema 1, reparte 3.
// - Turn: Quema 1, reparte 1.
// - River: Quema 1, reparte 1.
func (m *motor) RepartirComunitarias(etapa protocolo.Etapa) ([]protocolo.Carta, error) {
	var cantidad int
	switch etapa {
	case protocolo.Flop:
		cantidad = 3
	case protocolo.Turn, protocolo.River:
		cantidad = 1
	default:
		return nil, fmt.Errorf("crupier: etapa %q no reparte comunitarias", etapa)
	}

	// Siempre se quema una carta antes de repartir en el flop, turn o river
	if err := m.mazo.Quemar(); err != nil {
		return nil, fmt.Errorf("crupier: error al quemar carta para %s: %w", etapa, err)
	}

	cartas := make([]protocolo.Carta, 0, cantidad)
	for i := 0; i < cantidad; i++ {
		carta, err := m.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: robar carta para %s: %w", etapa, err)
		}
		cartas = append(cartas, carta)
	}

	return cartas, nil
}

// Evaluar determina el valor de una mano de 5 cartas a partir de las 2 privadas y 5 comunitarias.
func (m *motor) Evaluar(privadas protocolo.Mano, comunitarias []protocolo.Carta) (Evaluacion, error) {
	todas := make([]protocolo.Carta, 0, 2+len(comunitarias))
	todas = append(todas, privadas[:]...)
	todas = append(todas, comunitarias...)

	combs := combinaciones5(todas)
	if len(combs) == 0 {
		return Evaluacion{}, fmt.Errorf("crupier: no hay suficientes cartas para evaluar")
	}

	maxPuntaje := -1
	var mejorDesc string
	var mejores5 [5]protocolo.Carta

	for _, comb := range combs {
		var c5 [5]protocolo.Carta
		copy(c5[:], comb)
		puntaje, desc := evaluar5(c5)
		if puntaje > maxPuntaje {
			maxPuntaje = puntaje
			mejorDesc = desc
			mejores5 = c5
		}
	}

	return Evaluacion{
		Puntaje:     maxPuntaje,
		Descripcion: mejorDesc,
		Mejores5:    mejores5,
	}, nil
}

// DecidirGanadores compara las manos de los participantes y distribuye el pozo.
func (m *motor) DecidirGanadores(participantes []Participante, comunitarias []protocolo.Carta, pozo *Pozo) (protocolo.ResultadoMano, error) {
	if pozo == nil {
		return protocolo.ResultadoMano{}, fmt.Errorf("crupier: pozo es nulo")
	}

	evaluaciones := make(map[string]Evaluacion)
	for _, p := range participantes {
		ev, err := m.Evaluar(p.Mano, comunitarias)
		if err != nil {
			return protocolo.ResultadoMano{}, err
		}
		evaluaciones[p.ID] = ev
	}

	subPozos := pozo.Descomponer()
	repartosMap := make(map[string]int64)

	for _, sp := range subPozos {
		if len(sp.Elegibles) == 0 {
			continue
		}

		maxPuntaje := -1
		for _, id := range sp.Elegibles {
			if ev, ok := evaluaciones[id]; ok {
				if ev.Puntaje > maxPuntaje {
					maxPuntaje = ev.Puntaje
				}
			}
		}

		var ganadores []string
		for _, p := range participantes {
			esElegible := false
			for _, eID := range sp.Elegibles {
				if eID == p.ID {
					esElegible = true
					break
				}
			}
			if esElegible && evaluaciones[p.ID].Puntaje == maxPuntaje {
				ganadores = append(ganadores, p.ID)
			}
		}

		if len(ganadores) == 0 {
			continue
		}

		premio := sp.Monto / int64(len(ganadores))
		resto := sp.Monto % int64(len(ganadores))

		for i, id := range ganadores {
			monto := premio
			if int64(i) < resto {
				monto++
			}
			repartosMap[id] += monto
		}
	}

	res := protocolo.ResultadoMano{
		IDMano:       m.idMano,
		Comunitarias: comunitarias,
		Repartos:     make([]protocolo.Reparto, 0, len(repartosMap)),
		Mostradas:    make(map[string]protocolo.Mano),
		Descripcion:  make(map[string]string),
	}

	for id, monto := range repartosMap {
		res.Repartos = append(res.Repartos, protocolo.Reparto{
			IDJugador: id,
			Monto:     monto,
		})
	}

	for _, p := range participantes {
		res.Mostradas[p.ID] = p.Mano
		res.Descripcion[p.ID] = evaluaciones[p.ID].Descripcion
	}

	return res, nil
}
