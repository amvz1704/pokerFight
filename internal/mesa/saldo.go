// Este archivo contiene la contabilidad de fichas dentro de una mano: mover
// fichas del saldo de un jugador al pozo, respetando su limite (all-in si no
// alcanza), y decidir que acciones puede declarar segun su saldo y la
// apuesta pendiente.
//
// Todas las funciones de este archivo requieren que quien las llame ya tenga
// tomado m.Mu.
package mesa

import "github.com/amvz1704/pokerFight/internal/protocolo"

// aportar mueve `monto` fichas adicionales (no el total de la ronda) del
// saldo del jugador al pozo de la mesa. Si el saldo no alcanza, aporta todo
// lo que tiene y lo marca AllIn. Devuelve lo efectivamente aportado.
func (m *Mesa) aportar(j *Jugador, monto uint64) uint64 {
	if monto > j.Saldo {
		monto = j.Saldo
	}
	j.Saldo -= monto
	j.ApuestaRonda += monto
	if j.Saldo == 0 {
		j.AllIn = true
	}
	if monto > 0 {
		m.Pozo.Apostar(j.ID, int64(monto))
	}
	return monto
}

// accionesValidas enumera las jugadas que un jugador puede declarar dado
// cuanto lleva apostado en la ronda (j.ApuestaRonda) contra la apuesta
// actual de la mesa. Solo se llama para jugadores que siguen en la mano y no
// estan all-in (los unicos a quienes tiene sentido pedirles accion).
func accionesValidas(j *Jugador, apuestaActual uint64) []protocolo.TipoAccion {
	validas := []protocolo.TipoAccion{protocolo.Fold}

	if apuestaActual <= j.ApuestaRonda {
		validas = append(validas, protocolo.Check)
		if j.Saldo > 0 {
			validas = append(validas, protocolo.Bet)
		}
	} else {
		validas = append(validas, protocolo.Call)
		if j.Saldo > apuestaActual-j.ApuestaRonda {
			validas = append(validas, protocolo.Raise)
		}
	}
	if j.Saldo > 0 {
		validas = append(validas, protocolo.AllIn)
	}
	return validas
}

// esAccionValida verifica una accion recibida de un bot contra las acciones
// permitidas y, para Bet/Raise, contra su saldo y la subida minima vigente.
// Requisito del protocolo (docs/interfaces.md): "la Mesa nunca confia en el
// bot". Una accion invalida se descarta y se aplica protocolo.AccionSegura.
func esAccionValida(j *Jugador, accion protocolo.Accion, apuestaActual, subidaMinima uint64) bool {
	permitida := false
	for _, t := range accionesValidas(j, apuestaActual) {
		if t == accion.Tipo {
			permitida = true
			break
		}
	}
	if !permitida {
		return false
	}

	switch accion.Tipo {
	case protocolo.Bet, protocolo.Raise:
		if accion.Monto <= 0 {
			return false
		}
		objetivo := uint64(accion.Monto)
		techo := j.Saldo + j.ApuestaRonda // lo maximo a lo que puede llegar (all-in).
		if objetivo > techo || objetivo <= apuestaActual {
			return false
		}
		// Debe cubrir al menos la subida minima, salvo que sea all-in por menos
		// (regla de docs/reglas.md: no reabre la accion, pero es valida).
		if objetivo != techo && objetivo-apuestaActual < subidaMinima {
			return false
		}
		return true
	default:
		return true
	}
}

// procesarAccion aplica una accion ya validada: mueve fichas, marca fold y
// actualiza la apuesta actual / subida minima de la mesa si corresponde.
// Devuelve si la accion reabre la ronda (obliga a los demas jugadores en
// mano a volver a actuar porque la apuesta subio en una subida completa).
func (m *Mesa) procesarAccion(j *Jugador, accion protocolo.Accion) (reabre bool) {
	switch accion.Tipo {
	case protocolo.Fold:
		j.Retirado = true
		m.Pozo.Fold(j.ID)

	case protocolo.Check:
		// Sin cambios.

	case protocolo.Call:
		pendiente := m.apuestaActual - j.ApuestaRonda
		m.aportar(j, pendiente)

	case protocolo.Bet, protocolo.Raise:
		objetivo := uint64(accion.Monto)
		incremento := objetivo - m.apuestaActual
		m.aportar(j, objetivo-j.ApuestaRonda)
		if incremento >= m.subidaMinima {
			m.subidaMinima = incremento
			reabre = true
		}
		m.apuestaActual = j.ApuestaRonda

	case protocolo.AllIn:
		previa := m.apuestaActual
		m.aportar(j, j.Saldo)
		if j.ApuestaRonda > previa {
			incremento := j.ApuestaRonda - previa
			if incremento >= m.subidaMinima {
				m.subidaMinima = incremento
				reabre = true
			}
			m.apuestaActual = j.ApuestaRonda
		}
	}
	return reabre
}
