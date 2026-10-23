package mesa

import (
	"context"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// guion responde con las acciones dadas, en orden, y después juega
// siempreLlama. Sirve para fijar las primeras decisiones de una mano y dejar
// que el resto se resuelva solo hasta el showdown.
func guion(acciones ...protocolo.Accion) func(protocolo.MensajeMesa) (protocolo.Accion, error) {
	return func(m protocolo.MensajeMesa) (protocolo.Accion, error) {
		if len(acciones) == 0 {
			return siempreLlama(m)
		}
		a := acciones[0]
		acciones = acciones[1:]
		return a, nil
	}
}

// sentarConSaldos sienta a un jugador por silla, en orden, y le pisa el saldo
// inicial. La silla 0 arranca con el botón.
func sentarConSaldos(t *testing.T, m *Mesa, ids []string, saldos []uint64, conexiones []*ConexionPrueba) {
	t.Helper()
	for i, id := range ids {
		if err := m.SentarJugador(id, id, conexiones[i]); err != nil {
			t.Fatalf("SentarJugador(%s): %v", id, err)
		}
		m.Jugadores[i].Saldo = saldos[i]
	}
}

// solicitudesEn devuelve las solicitudes de acción que recibió una conexión
// durante la etapa dada.
func solicitudesEn(cx *ConexionPrueba, etapa protocolo.Etapa) []protocolo.MensajeMesa {
	var res []protocolo.MensajeMesa
	for _, msg := range cx.RecibidosDeTipo(protocolo.MsgSolicitarAccion) {
		if msg.Estado != nil && msg.Estado.Etapa == etapa {
			res = append(res, msg)
		}
	}
	return res
}

func contiene(validas []protocolo.TipoAccion, tipo protocolo.TipoAccion) bool {
	for _, v := range validas {
		if v == tipo {
			return true
		}
	}
	return false
}

func jugarUnaMano(t *testing.T, m *Mesa) ResumenPartida {
	t.Helper()
	m.CfgPartida.CantidadRondas = 1
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	resumen, err := m.Jugar(ctx)
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	return resumen
}

// A (botón, ciega chica) sube a 100. B (ciega grande, 150 fichas) va all-in a
// 150: 50 más, por debajo de la subida mínima de 80. Según docs/reglas.md A
// tiene que poder pagar esos 50 o retirarse, pero no volver a subir.
func TestAllInCortoObligaAPagarAlQueYaActuo(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	a := NuevaConexionPruebaFunc("a", guion(protocolo.Accion{Tipo: protocolo.Raise, Monto: 100}))
	b := NuevaConexionPruebaFunc("b", guion(protocolo.Accion{Tipo: protocolo.AllIn}))
	sentarConSaldos(t, m, []string{"a", "b"}, []uint64{1000, 150}, []*ConexionPrueba{a, b})

	resumen := jugarUnaMano(t, m)

	preflop := solicitudesEn(a, protocolo.PreFlop)
	if len(preflop) != 2 {
		t.Fatalf("A recibio %d solicitudes preflop, se esperaban 2 (subir, y despues responder al all-in corto)", len(preflop))
	}
	validas := preflop[1].AccionesValidas
	if !contiene(validas, protocolo.Call) || !contiene(validas, protocolo.Fold) {
		t.Errorf("ante el all-in corto A tiene que poder pagar o retirarse, acciones validas = %v", validas)
	}
	for _, prohibida := range []protocolo.TipoAccion{protocolo.Raise, protocolo.Bet, protocolo.AllIn} {
		if contiene(validas, prohibida) {
			t.Errorf("un all-in corto no reabre la accion, pero A puede declarar %q (validas = %v)", prohibida, validas)
		}
	}
	// A pago: en el pozo quedaron los 150 de cada uno, y alguien gano los 300.
	if total := totalFichas(resumen); total != 1150 {
		t.Fatalf("total de fichas final = %d, se esperaba 1150", total)
	}
	// Se mira SaldoFinal y no Neto: el neto se mide contra el stack inicial
	// de la mesa, y aca los saldos se pisaron a mano.
	for _, p := range resumen.Posiciones {
		if p.IDJugador == "b" && p.SaldoFinal != 0 && p.SaldoFinal != 150 && p.SaldoFinal != 300 {
			t.Errorf("B termino con %d fichas: con el all-in pagado solo puede ser 0, 300 o 150 (empate)", p.SaldoFinal)
		}
	}
}

// Una subida declarada donde solo se puede pagar o retirarse es una accion
// invalida: se aplica la accion segura (fold, porque hay apuesta pendiente) y
// se anota el timeout.
func TestSubirTrasUnAllInCortoEsInvalido(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	a := NuevaConexionPruebaFunc("a", guion(
		protocolo.Accion{Tipo: protocolo.Raise, Monto: 100},
		protocolo.Accion{Tipo: protocolo.Raise, Monto: 400},
	))
	b := NuevaConexionPruebaFunc("b", guion(protocolo.Accion{Tipo: protocolo.AllIn}))
	sentarConSaldos(t, m, []string{"a", "b"}, []uint64{1000, 150}, []*ConexionPrueba{a, b})

	resumen := jugarUnaMano(t, m)

	// Sin esto el resto del test podria pasar por azar: si a A no se le
	// pregunta, la subida a 400 se usa en otra calle y el resultado depende de
	// las cartas.
	if n := len(solicitudesEn(a, protocolo.PreFlop)); n != 2 {
		t.Fatalf("A recibio %d solicitudes preflop, se esperaban 2", n)
	}
	for _, p := range resumen.Posiciones {
		switch p.IDJugador {
		case "a":
			if p.Timeouts != 1 {
				t.Errorf("A tiene %d timeouts, se esperaba 1 por la subida invalida", p.Timeouts)
			}
			if p.SaldoFinal != 900 {
				t.Errorf("A termino con %d fichas, se esperaban 900 (se retiro tras subir a 100)", p.SaldoFinal)
			}
		case "b":
			if p.SaldoFinal != 250 {
				t.Errorf("B termino con %d fichas, se esperaban 250 (sus 150 mas los 100 de A)", p.SaldoFinal)
			}
		}
	}
}

// Dos all-in cortos que juntos suman una subida completa si reabren la accion
// para quien ya habia actuado (es la regla de la TDA, y la consecuencia
// natural de "subida completa" en docs/reglas.md).
func TestAllInCortosQueSumanUnaSubidaCompletaReabren(t *testing.T) {
	m := mesaDePrueba(t, 3, 1000)
	// Tres jugadores: silla 0 boton (abre preflop), 1 ciega chica, 2 grande.
	a := NuevaConexionPruebaFunc("a", guion(protocolo.Accion{Tipo: protocolo.Raise, Monto: 100})) // sube 80
	b := NuevaConexionPruebaFunc("b", guion(protocolo.Accion{Tipo: protocolo.AllIn}))             // a 150: +50
	c := NuevaConexionPruebaFunc("c", guion(protocolo.Accion{Tipo: protocolo.AllIn}))             // a 190: +40
	sentarConSaldos(t, m, []string{"a", "b", "c"}, []uint64{1000, 150, 190}, []*ConexionPrueba{a, b, c})

	jugarUnaMano(t, m)

	preflop := solicitudesEn(a, protocolo.PreFlop)
	if len(preflop) != 2 {
		t.Fatalf("A recibio %d solicitudes preflop, se esperaban 2", len(preflop))
	}
	if validas := preflop[1].AccionesValidas; !contiene(validas, protocolo.Raise) {
		t.Errorf("los all-in suman 90 sobre la apuesta de A (subida minima 80): A deberia poder resubir, validas = %v", validas)
	}
}

// Con el rival all-in y nada por pagar, no queda ninguna decision: la mesa no
// debe pedirle accion al unico que podria hablar en las calles siguientes.
func TestNoSePideAccionSinRivalQuePuedaResponder(t *testing.T) {
	m := mesaDePrueba(t, 2, 1000)
	a := NuevaConexionPruebaFunc("a", siempreLlama)
	b := NuevaConexionPruebaFunc("b", guion(protocolo.Accion{Tipo: protocolo.AllIn}))
	sentarConSaldos(t, m, []string{"a", "b"}, []uint64{1000, 150}, []*ConexionPrueba{a, b})

	jugarUnaMano(t, m)

	for _, etapa := range []protocolo.Etapa{protocolo.Flop, protocolo.Turn, protocolo.River} {
		if n := len(solicitudesEn(a, etapa)); n != 0 {
			t.Errorf("A recibio %d solicitudes en %s con B all-in y nada por pagar, se esperaban 0", n, etapa)
		}
	}
	if n := len(a.RecibidosDeTipo(protocolo.MsgManoFin)); n != 1 {
		t.Fatalf("A recibio %d mano_fin, se esperaba 1: la mano tiene que llegar igual al showdown", n)
	}
}
