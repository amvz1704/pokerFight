// Este archivo contiene las funciones de orden de turno: quien tiene el
// boton, quien paga las ciegas y quien habla primero en cada ronda de
// apuestas. Ver docs/reglas.md ("Posiciones").
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
	n := uint8(len(m.Jugadores))
	if n == 0 {
		return 0, false
	}
	for i := uint8(1); i <= n; i++ {
		s := (desde + i) % n
		j := m.Jugadores[s]
		if j != nil && j.Activo {
			return s, true
		}
	}
	return 0, false
}

// primerEnManoDesdeBoton devuelve la primera silla, en sentido horario desde
// el boton, cuyo jugador sigue en la mano actual (no hizo fold). A diferencia
// de siguienteSillaOcupada, tambien descarta a quien ya se retiro: se usa
// para saber quien abre cada ronda de apuestas postflop.
func (m *Mesa) primerEnManoDesdeBoton() (silla uint8, ok bool) {
	n := uint8(len(m.Jugadores))
	if n == 0 {
		return 0, false
	}
	for i := uint8(1); i <= n; i++ {
		s := (m.Boton + i) % n
		j := m.Jugadores[s]
		if j != nil && j.Activo && !j.Retirado {
			return s, true
		}
	}
	return 0, false
}

// posicionesCiegas calcula las sillas de ciega chica y ciega grande a partir
// del boton actual:
//   - Heads-up (2 jugadores activos): el boton es la ciega chica.
//   - 3 o mas: la ciega chica esta a la izquierda del boton, la grande
//     despues de esta.
func (m *Mesa) posicionesCiegas(cantidadActivos int) (ciegaChica, ciegaGrande uint8) {
	if cantidadActivos == 2 {
		ciegaChica = m.Boton
		ciegaGrande, _ = m.siguienteSillaOcupada(m.Boton)
		return
	}
	ciegaChica, _ = m.siguienteSillaOcupada(m.Boton)
	ciegaGrande, _ = m.siguienteSillaOcupada(ciegaChica)
	return
}

// primerEnHablarPreflop devuelve quien abre la ronda preflop: en heads-up es
// el boton (ciega chica), que ya tiene su turno de nuevo tras postear;
// en 3+ es el siguiente jugador a la ciega grande.
func (m *Mesa) primerEnHablarPreflop(cantidadActivos int, ciegaChica, ciegaGrande uint8) uint8 {
	if cantidadActivos == 2 {
		return ciegaChica
	}
	silla, _ := m.siguienteSillaOcupada(ciegaGrande)
	return silla
}
