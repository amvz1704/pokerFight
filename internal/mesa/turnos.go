// Este archivo contiene las funciones de orden de turno: quien tiene el
// boton, quien paga las ciegas y quien habla primero en cada ronda de
// apuestas. Ver docs/reglas.md ("Posiciones").
//
// Hay dos nociones de "esta jugando" y no son intercambiables:
//
//   - Jugador.Activo   -> sigue en el torneo (le quedan fichas).
//   - Jugador.EnMano   -> recibio cartas en la mano en curso.
//
// Entre una y otra hay una ventana real: un bot que se conecta a mitad de
// mano queda Activo pero con EnMano=false hasta la mano siguiente. Las
// funciones de reparto de ciegas y de orden de turno usan EnMano; solo el
// avance del boton entre manos usa Activo.
//
// Todas las funciones de este archivo requieren que quien las llame ya tenga
// tomado m.Mu, igual que ObtenerJugadoresActivos en mesa.go.
package mesa

// siguienteSillaOcupada busca, a partir de (sin incluir) la silla `desde`, la
// siguiente silla con un jugador que sigue en el torneo (Jugador.Activo),
// recorriendo en sentido horario y dando la vuelta si hace falta. Ignora si
// el jugador hizo fold o esta all-in en la mano actual: eso es estado de la
// mano, no del torneo.
// Devuelve ok=false solo si no hay ningun jugador en el torneo (no deberia
// pasar mientras Jugar seguio adelante, que ya corta con 0 o 1 activos).
func (m *Mesa) siguienteSillaOcupada(desde uint8) (silla uint8, ok bool) {
	return m.siguienteSillaSi(desde, func(j *Jugador) bool { return j.Activo })
}

// siguienteSillaEnMano es el equivalente restringido a los jugadores que
// recibieron cartas en la mano en curso. Es la que usa el recorrido de las
// rondas de apuestas: un jugador recien sentado esta Activo pero no debe
// recibir turno hasta la mano siguiente.
func (m *Mesa) siguienteSillaEnMano(desde uint8) (silla uint8, ok bool) {
	return m.siguienteSillaSi(desde, func(j *Jugador) bool { return j.EnMano })
}

// siguienteSillaSi recorre las sillas en sentido horario desde (sin incluir)
// `desde` y devuelve la primera cuyo ocupante cumple el predicado.
func (m *Mesa) siguienteSillaSi(desde uint8, cumple func(*Jugador) bool) (silla uint8, ok bool) {
	n := uint8(len(m.Jugadores))
	if n == 0 {
		return 0, false
	}
	for i := uint8(1); i <= n; i++ {
		s := (desde + i) % n
		j := m.Jugadores[s]
		if j != nil && cumple(j) {
			return s, true
		}
	}
	return 0, false
}

// primerEnManoDesdeBoton devuelve la primera silla, en sentido horario desde
// el boton, cuyo jugador sigue en la mano actual (recibio cartas y no hizo
// fold). Se usa para saber quien abre cada ronda de apuestas postflop.
func (m *Mesa) primerEnManoDesdeBoton() (silla uint8, ok bool) {
	return m.siguienteSillaSi(m.Boton, func(j *Jugador) bool { return j.EnMano && !j.Retirado })
}

// posicionesCiegas calcula las sillas de ciega chica y ciega grande a partir
// del boton actual:
//   - Heads-up (2 jugadores en mano): el boton es la ciega chica.
//   - 3 o mas: la ciega chica esta a la izquierda del boton, la grande
//     despues de esta.
//
// Devuelve ok=false si no hay suficientes jugadores en mano como para cobrar
// las dos ciegas. El llamador debe abortar la mano en ese caso en vez de
// indexar Jugadores a ciegas: el boton puede apuntar a una silla que quedo
// vacia (LevantarJugador) o a un jugador ya eliminado.
func (m *Mesa) posicionesCiegas(cantidadEnMano int) (ciegaChica, ciegaGrande uint8, ok bool) {
	if cantidadEnMano < 2 {
		return 0, 0, false
	}

	if cantidadEnMano == 2 {
		// Heads-up: el boton paga la ciega chica, pero solo si el boton es de
		// verdad una silla en juego. Si no lo es (silla liberada o jugador
		// eliminado en la mano anterior), se corre a la siguiente que si lo sea.
		ciegaChica = m.Boton
		if j := m.Jugadores[ciegaChica]; j == nil || !j.EnMano {
			if ciegaChica, ok = m.siguienteSillaEnMano(m.Boton); !ok {
				return 0, 0, false
			}
		}
		ciegaGrande, ok = m.siguienteSillaEnMano(ciegaChica)
		return ciegaChica, ciegaGrande, ok
	}

	if ciegaChica, ok = m.siguienteSillaEnMano(m.Boton); !ok {
		return 0, 0, false
	}
	ciegaGrande, ok = m.siguienteSillaEnMano(ciegaChica)
	return ciegaChica, ciegaGrande, ok
}

// primerEnHablarPreflop devuelve quien abre la ronda preflop: en heads-up es
// el boton (ciega chica), que ya tiene su turno de nuevo tras postear;
// en 3+ es el siguiente jugador a la ciega grande.
func (m *Mesa) primerEnHablarPreflop(cantidadEnMano int, ciegaChica, ciegaGrande uint8) uint8 {
	if cantidadEnMano == 2 {
		return ciegaChica
	}
	silla, ok := m.siguienteSillaEnMano(ciegaGrande)
	if !ok {
		return ciegaGrande
	}
	return silla
}
