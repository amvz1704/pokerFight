package crupier

import (
	"fmt"
	"sort"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/poker"
)

// Participante asocia un ID de jugador a la mano recibida.
type Participante struct {
	ID   string
	Mano protocolo.Mano
}

// Evaluacion contiene el puntaje de una mano, permitiendo comparar
// quién gana la partida de forma sencilla (mayor puntaje gana).
//
// Es un alias de poker.Evaluacion, no un tipo aparte: el evaluador vive en
// pkg/poker para que los bots del torneo puedan usar exactamente el mismo que
// la mesa (internal/ no es importable desde fuera del módulo). El alias
// mantiene `crupier.Evaluacion` como nombre válido dentro del proyecto.
type Evaluacion = poker.Evaluacion

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
	fuente Fuente
	idMano string
}

// Nuevo crea un crupier con barajado criptográfico. Es el que se usa en una
// partida real: nadie, ni siquiera quien levantó la mesa, puede predecir el
// mazo.
func Nuevo() Crupier { return NuevoCon(FuenteCriptografica()) }

// NuevoConSemilla crea un crupier determinista: dos crupiers con la misma
// semilla reparten exactamente las mismas cartas, mano por mano.
//
// Es lo que le permite a la arena jugar un emparejamiento en las dos
// posiciones con los mismos repartos y quedarse solo con la diferencia de
// habilidad (ver internal/arena). No usarlo donde los jugadores puedan
// conocer la semilla.
func NuevoConSemilla(semilla uint64) Crupier { return NuevoCon(FuenteSembrada(semilla)) }

// NuevoCon crea un crupier con la fuente de aleatoriedad dada.
func NuevoCon(fuente Fuente) Crupier {
	if fuente == nil {
		fuente = FuenteCriptografica()
	}
	return &motor{
		mazo:   NuevoMazoCon(fuente),
		fuente: fuente,
	}
}

// NuevaMano prepara el mazo para una mano nueva. El mazo se rearma completo y
// se baraja de cero: no quedan restos de la mano anterior.
func (m *motor) NuevaMano(idMano string) error {
	m.idMano = idMano
	m.mazo = NuevoMazoCon(m.fuente)
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

// Evaluar determina el valor de la mejor jugada de 5 cartas a partir de las 2
// privadas y las comunitarias que haya. Delega en pkg/poker: la lógica vive
// ahí para que los bots puedan importarla.
func (m *motor) Evaluar(privadas protocolo.Mano, comunitarias []protocolo.Carta) (Evaluacion, error) {
	todas := make([]protocolo.Carta, 0, 2+len(comunitarias))
	todas = append(todas, privadas[:]...)
	todas = append(todas, comunitarias...)

	ev, err := poker.MejorDe(todas)
	if err != nil {
		return Evaluacion{}, fmt.Errorf("crupier: no se pudo evaluar la mano: %w", err)
	}
	return ev, nil
}

// DecidirGanadores compara las manos de los participantes y distribuye el
// pozo, incluidos los pozos laterales que arma Pozo.Descomponer.
//
// El orden de `participantes` es significativo y lo fija la Mesa: es el orden
// de acción, empezando por el primer jugador a la izquierda del botón. De ahí
// sale, sin que el Crupier tenga que saber dónde está el botón, la regla de
// docs/reglas.md sobre el reparto de un pozo que no divide exacto: la ficha
// sobrante va al primero de esa lista entre los ganadores.
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

	// Se recorre `participantes` y no el map: iterar un map en Go da un orden
	// distinto en cada corrida, y el resultado de la mano termina en el
	// historial y en el mensaje mano_fin. Dos réplicas de la misma partida
	// tienen que producir exactamente el mismo JSON.
	for _, p := range participantes {
		if monto, ok := repartosMap[p.ID]; ok {
			res.Repartos = append(res.Repartos, protocolo.Reparto{IDJugador: p.ID, Monto: monto})
			delete(repartosMap, p.ID)
		}
	}
	// Red de seguridad: si quedó algún ganador que no estaba en participantes
	// (no debería), se agrega ordenado por ID para no perder fichas ni
	// determinismo.
	if len(repartosMap) > 0 {
		sobrantes := make([]string, 0, len(repartosMap))
		for id := range repartosMap {
			sobrantes = append(sobrantes, id)
		}
		sort.Strings(sobrantes)
		for _, id := range sobrantes {
			res.Repartos = append(res.Repartos, protocolo.Reparto{IDJugador: id, Monto: repartosMap[id]})
		}
	}

	for _, p := range participantes {
		res.Mostradas[p.ID] = p.Mano
		res.Descripcion[p.ID] = evaluaciones[p.ID].Descripcion
	}

	return res, nil
}
