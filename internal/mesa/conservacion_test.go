package mesa

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// estrategiaAlAzar elige una accion valida al azar, con montos al azar para las
// subidas. Mezcla retiros, pagos, subidas y all-ins: es lo que arma pozos
// laterales de todas las formas posibles, que es donde una cuenta de fichas se
// rompe.
func estrategiaAlAzar(azar *rand.Rand) func(protocolo.MensajeMesa) (protocolo.Accion, error) {
	return func(msg protocolo.MensajeMesa) (protocolo.Accion, error) {
		validas := msg.AccionesValidas
		tipo := validas[azar.IntN(len(validas))]
		accion := protocolo.Accion{Tipo: tipo}
		if tipo == protocolo.Bet || tipo == protocolo.Raise {
			e := msg.Estado
			// Un total entre la subida minima y bastante por encima: unas veces valido,
			// otras pasado del saldo (la mesa lo descarta y juega la accion segura).
			minimo := e.ApuestaActual + e.SubidaMinima
			accion.Monto = minimo + azar.Int64N(3*e.CiegaGrande+1)*int64(1+azar.IntN(20))
		}
		return accion, nil
	}
}

// TestLasFichasSeConservanConPozosLateralesAlAzar juega miles de manos con
// mesas de 3 a 8 jugadores, stacks muy distintos y estrategias al azar, y
// verifica que el total de fichas no cambie nunca.
func TestLasFichasSeConservanConPozosLateralesAlAzar(t *testing.T) {
	for semilla := uint64(1); semilla <= 60; semilla++ {
		azar := rand.New(rand.NewPCG(semilla, semilla*7919))
		jugadores := 3 + azar.IntN(6)

		stacks := map[string]uint64{}
		var total uint64
		ids := make([]string, jugadores)
		for i := range ids {
			ids[i] = fmt.Sprintf("j%d", i)
			stacks[ids[i]] = uint64(100 + azar.IntN(3000))
			total += stacks[ids[i]]
		}

		m := NuevaMesa("estres",
			ConfigMesa{MaxJugadores: uint8(jugadores), MinJugadores: 2, Timeout: 1000},
			ConfigPartida{
				CiegaMenor: 10, CiegaMayor: 20, StackInicial: 1000, CantidadRondas: 25,
				StackPorJugador: stacks, HastaQueden: 1,
			},
			crupier.NuevoConSemilla(semilla),
		).(*Mesa)
		for _, id := range ids {
			if err := m.SentarJugador(id, id, NuevaConexionPruebaFunc(id, estrategiaAlAzar(azar))); err != nil {
				t.Fatalf("SentarJugador: %v", err)
			}
		}

		resumen, err := m.Jugar(contextoDePrueba(t))
		if err != nil {
			t.Fatalf("semilla %d: Jugar: %v", semilla, err)
		}
		var final uint64
		for _, p := range resumen.Posiciones {
			final += p.SaldoFinal
		}
		if final != total {
			t.Fatalf("semilla %d (%d jugadores, %d manos): arrancaron con %d fichas y terminaron con %d (se %s %d)",
				semilla, jugadores, resumen.CantidadRondasJugadas, total, final,
				map[bool]string{true: "perdieron", false: "crearon"}[final < total], max(total, final)-min(total, final))
		}
	}
}
