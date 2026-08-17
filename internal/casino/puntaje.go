// Este archivo contiene la fórmula de puntos y el ranking. Ver casino.go
// para los tipos y la interfaz Casino.
package casino

import (
	"fmt"
	"sort"
)

// puntosPorPuesto y puntosPorTimeout son los pesos de la fórmula de puntaje.
// No hay una especificación previa de la fórmula en docs/ más allá del
// requisito del README ("cada timeout resta puntos en el ranking"), así que
// se documenta la elegida acá: por cada rival que terminó por debajo tuyo en
// una partida sumás puntosPorPuesto, y por cada vez que se te aplicó la
// acción segura por no responder a tiempo restás puntosPorTimeout. Es
// deliberadamente simple; si el equipo define una fórmula distinta, este es
// el único lugar que hay que tocar.
const (
	puntosPorPuesto  = 10
	puntosPorTimeout = 2
)

// puntosPorResultado calcula cuánto suma o resta a una cuenta el resultado
// de una sola partida.
func puntosPorResultado(posicion, totalJugadores int, timeouts uint64) int64 {
	var puntos int64
	if totalJugadores > 1 && posicion >= 1 {
		superados := totalJugadores - posicion
		puntos += int64(superados) * puntosPorPuesto
	}
	puntos -= int64(timeouts) * puntosPorTimeout
	return puntos
}

// RegistrarResultado suma el resultado de una partida ya jugada a las
// estadísticas históricas de cada cuenta involucrada. Cuentas que todavía no
// tenían estadísticas (por ejemplo, se registraron después de que el Casino
// arrancó desde un almacén viejo) arrancan de cero.
func (m *motor) RegistrarResultado(resultado ResultadoPartida) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := len(resultado.Posiciones)
	respaldo := make(map[string]Estadisticas, total)

	for _, rj := range resultado.Posiciones {
		est, existe := m.datos.Estadisticas[rj.IDCuenta]
		if !existe {
			est = Estadisticas{IDCuenta: rj.IDCuenta}
		}
		respaldo[rj.IDCuenta] = est

		est.PartidasJugadas++
		if rj.Posicion == 1 {
			est.PartidasGanadas++
		}
		est.Timeouts += rj.Timeouts
		est.Puntaje += puntosPorResultado(rj.Posicion, total, rj.Timeouts)

		m.datos.Estadisticas[rj.IDCuenta] = est
	}

	if err := m.guardarSinLock(); err != nil {
		for id, est := range respaldo {
			m.datos.Estadisticas[id] = est
		}
		return fmt.Errorf("casino: no se pudo guardar el resultado de la partida %s: %w", resultado.IDMesa, err)
	}
	return nil
}

// Estadisticas devuelve las estadísticas acumuladas de una cuenta. Una
// cuenta que todavía no jugó ninguna partida devuelve estadísticas en cero,
// no un error.
func (m *motor) Estadisticas(idCuenta string) (Estadisticas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, existe := m.datos.Cuentas[idCuenta]; !existe {
		return Estadisticas{}, fmt.Errorf("casino: no existe la cuenta %q", idCuenta)
	}
	est, existe := m.datos.Estadisticas[idCuenta]
	if !existe {
		return Estadisticas{IDCuenta: idCuenta}, nil
	}
	return est, nil
}

// Ranking devuelve las estadísticas de todas las cuentas, ordenadas de mayor
// a menor puntaje. A igual puntaje, ordena por menos partidas jugadas
// primero (más eficiente subiendo el ranking) y, si eso tampoco desempata,
// por ID de cuenta para que el orden sea determinista.
func (m *motor) Ranking() ([]Estadisticas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Estadisticas, 0, len(m.datos.Estadisticas))
	for _, est := range m.datos.Estadisticas {
		out = append(out, est)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Puntaje != out[j].Puntaje {
			return out[i].Puntaje > out[j].Puntaje
		}
		if out[i].PartidasJugadas != out[j].PartidasJugadas {
			return out[i].PartidasJugadas < out[j].PartidasJugadas
		}
		return out[i].IDCuenta < out[j].IDCuenta
	})
	return out, nil
}
