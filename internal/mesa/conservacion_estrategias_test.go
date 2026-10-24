package mesa

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

func tiene(v []protocolo.TipoAccion, t protocolo.TipoAccion) bool {
	for _, x := range v {
		if x == t {
			return true
		}
	}
	return false
}

// Las tres estrategias de los bots del campeonato de prueba.
var estrategias = []func(protocolo.MensajeMesa, protocolo.Mano) protocolo.Accion{
	func(m protocolo.MensajeMesa, _ protocolo.Mano) protocolo.Accion { // pasivo
		if tiene(m.AccionesValidas, protocolo.Check) {
			return protocolo.Accion{Tipo: protocolo.Check}
		}
		return protocolo.Accion{Tipo: protocolo.Call}
	},
	func(m protocolo.MensajeMesa, mano protocolo.Mano) protocolo.Accion { // all-in con par
		if mano[0].Rango == mano[1].Rango && tiene(m.AccionesValidas, protocolo.AllIn) {
			return protocolo.Accion{Tipo: protocolo.AllIn}
		}
		if tiene(m.AccionesValidas, protocolo.Check) {
			return protocolo.Accion{Tipo: protocolo.Check}
		}
		return protocolo.Accion{Tipo: protocolo.Fold}
	},
	func(m protocolo.MensajeMesa, _ protocolo.Mano) protocolo.Accion { // timido
		if tiene(m.AccionesValidas, protocolo.Check) {
			return protocolo.Accion{Tipo: protocolo.Check}
		}
		return protocolo.Accion{Tipo: protocolo.Fold}
	},
}

// jugadorConEstrategia es un bot sin red que recuerda sus cartas. No se usa
// ConexionPrueba porque su Responder corre con el mutex de la conexion tomado,
// y pedirle ahi las cartas a la misma conexion es un deadlock.
type jugadorConEstrategia struct {
	est    func(protocolo.MensajeMesa, protocolo.Mano) protocolo.Accion
	mano   protocolo.Mano
	ultima protocolo.MensajeMesa
}

func (j *jugadorConEstrategia) EnviarMensaje(m protocolo.MensajeMesa, _ uint64) error {
	if m.Tipo == protocolo.MsgManoInicio && m.Cartas != nil {
		j.mano = *m.Cartas
	}
	if m.Tipo == protocolo.MsgSolicitarAccion {
		j.ultima = m
	}
	return nil
}
func (j *jugadorConEstrategia) SolicitarAccion(uint64) (protocolo.Accion, error) {
	return j.est(j.ultima, j.mano), nil
}
func (j *jugadorConEstrategia) Cerrar() error { return nil }

// TestFichasSeConservanConEstrategiasDelCampeonato juega cientos de partidas con
// las estrategias que usan los bots de los torneos (pasivo, all-in con par,
// timido), ciegas que suben y stacks muy distintos, y verifica que el total de
// fichas no cambie nunca.
//
// Existe por un bug real: cuando alguien se retiraba despues de aportar mas de lo
// que ponia cualquier rival en mano (la ciega grande contra un all-in corto), ese
// excedente quedaba sin dueno y desaparecia. El estres con jugadores al azar no
// lo veia; hacen falta ciegas altas y all-ins cortos a la vez. La semilla 1494 lo
// reproducia. Con 3000 semillas tarda unos 7 segundos; en la suite se corren 600.
func TestFichasSeConservanConEstrategiasDelCampeonato(t *testing.T) {
	for semilla := uint64(1); semilla <= 600; semilla++ {
		azar := rand.New(rand.NewPCG(semilla, 99))
		n := 2 + azar.IntN(7)
		stacks := map[string]uint64{}
		var total uint64
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("j%d", i)
			stacks[ids[i]] = uint64(50 + azar.IntN(4000))
			total += stacks[ids[i]]
		}
		m := NuevaMesa("e", ConfigMesa{MaxJugadores: uint8(n), MinJugadores: 2, Timeout: 1000},
			ConfigPartida{CiegaMenor: 10, CiegaMayor: 20, StackInicial: 1000, CantidadRondas: 40, StackPorJugador: stacks, HastaQueden: 1,
				Ciegas: func(h int64) (uint64, uint64) { c := uint64(10) << uint(h/8); return c, 2 * c }},
			crupier.NuevoConSemilla(semilla)).(*Mesa)
		for _, id := range ids {
			m.SentarJugador(id, id, &jugadorConEstrategia{est: estrategias[azar.IntN(len(estrategias))]})
		}
		resumen, err := m.Jugar(contextoDePrueba(t))
		if err != nil {
			t.Fatalf("semilla %d: %v", semilla, err)
		}
		var final uint64
		for _, p := range resumen.Posiciones {
			final += p.SaldoFinal
		}
		if final != total {
			t.Fatalf("semilla %d (%d jugadores, %d manos): %d -> %d fichas (diferencia %d)", semilla, n, resumen.CantidadRondasJugadas, total, final, int64(final)-int64(total))
		}
	}
}
