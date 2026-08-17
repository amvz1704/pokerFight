package mesa

import (
	"context"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// siempreLlama responde check si es gratis, o call si hay que igualar. Nunca
// sube ni se retira: sirve para correr manos completas hasta el showdown de
// forma determinista.
func siempreLlama(m protocolo.MensajeMesa) (protocolo.Accion, error) {
	for _, t := range m.AccionesValidas {
		if t == protocolo.Check {
			return protocolo.Accion{Tipo: protocolo.Check}, nil
		}
	}
	for _, t := range m.AccionesValidas {
		if t == protocolo.Call {
			return protocolo.Accion{Tipo: protocolo.Call}, nil
		}
	}
	return protocolo.Accion{Tipo: protocolo.Fold}, nil
}

// totalFichas suma el saldo final de todos los jugadores del resumen: sirve
// para comprobar que Jugar nunca crea ni destruye fichas.
func totalFichas(resumen ResumenPartida) uint64 {
	var total uint64
	for _, p := range resumen.Posiciones {
		total += p.SaldoFinal
	}
	return total
}

func TestJugarManoLlegaAShowdownYReparteElPozo(t *testing.T) {
	m := mesaDePrueba(t, 3, 300)
	ids := []string{"c-1", "c-2", "c-3"}
	var conexiones []*ConexionPrueba
	for _, id := range ids {
		cx := NuevaConexionPruebaFunc(id, siempreLlama)
		conexiones = append(conexiones, cx)
		if err := m.SentarJugador(id, id, cx); err != nil {
			t.Fatalf("SentarJugador(%s): %v", id, err)
		}
	}
	m.CfgPartida.CantidadRondas = 3

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()

	resumen, err := m.Jugar(ctx)
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	if resumen.CantidadRondasJugadas != 3 {
		t.Fatalf("se jugaron %d rondas, se esperaban 3", resumen.CantidadRondasJugadas)
	}
	if total := totalFichas(resumen); total != 300*3 {
		t.Fatalf("total de fichas final = %d, se esperaba %d (no se puede crear ni perder fichas)", total, 300*3)
	}

	for _, cx := range conexiones {
		if _, ok := cx.CartasRecibidas(); !ok {
			t.Errorf("%s nunca recibio sus cartas privadas", cx.IDJugador)
		}
		if n := len(cx.RecibidosDeTipo(protocolo.MsgManoFin)); n != 3 {
			t.Errorf("%s recibio %d mano_fin, se esperaban 3", cx.IDJugador, n)
		}
	}
}

// TestJugarManoGanaPorFoldSinShowdown cubre el caso en el que todos los
// demas se retiran antes del flop: la mesa debe repartir el pozo sin
// pedirle al Crupier que evalue manos (no habria comunitarias todavia).
func TestJugarManoGanaPorFoldSinShowdown(t *testing.T) {
	m := mesaDePrueba(t, 2, 300)

	seRetira := func(protocolo.MensajeMesa) (protocolo.Accion, error) {
		return protocolo.Accion{Tipo: protocolo.Fold}, nil
	}
	m.SentarJugador("c-1", "c-1", NuevaConexionPruebaFunc("c-1", seRetira))
	m.SentarJugador("c-2", "c-2", NuevaConexionPruebaFunc("c-2", siempreLlama))
	m.CfgPartida.CantidadRondas = 1

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()

	resumen, err := m.Jugar(ctx)
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	if total := totalFichas(resumen); total != 600 {
		t.Fatalf("total de fichas final = %d, se esperaba 600", total)
	}
}

// TestJugarTerminaCuandoQuedaUnSoloJugadorActivo cubre el corte por
// eliminacion: con fichas cortas frente a las ciegas, alguien queda en 0 y
// el torneo debe terminar ahi aunque CantidadRondas sea -1 (sin limite).
func TestJugarTerminaCuandoQuedaUnSoloJugadorActivo(t *testing.T) {
	m := mesaDePrueba(t, 2, 20)
	m.CfgPartida.CantidadRondas = -1

	m.SentarJugador("a", "a", NuevaConexionPruebaFunc("a", siempreLlama))
	m.SentarJugador("b", "b", NuevaConexionPruebaFunc("b", siempreLlama))

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()

	resumen, err := m.Jugar(ctx)
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	if resumen.CantidadRondasJugadas < 1 {
		t.Fatal("se esperaba al menos una ronda jugada")
	}
	if total := totalFichas(resumen); total != 40 {
		t.Fatalf("total de fichas final = %d, se esperaba 40", total)
	}
}
