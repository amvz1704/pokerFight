package mesa

import (
	"context"
	"testing"
	"time"
)

// TestJugarAplicaAccionSeguraAntePlazoVencido cubre el requisito funcional
// #5: si el bot no responde a tiempo, la mesa debe aplicar la accion segura
// (check/fold) en su lugar y seguir la partida sin trabarse.
func TestJugarAplicaAccionSeguraAntePlazoVencido(t *testing.T) {
	m := mesaDePrueba(t, 2, 300)
	m.CfgMesa.Timeout = 100 // muy corto a proposito

	cxLento := NuevaConexionPrueba("lento") // sin acciones programadas.
	cxLento.RetrasoMs = 1000                // mas lento que el timeout: cada SolicitarAccion vence.
	m.SentarJugador("lento", "lento", cxLento)
	m.SentarJugador("rapido", "rapido", NuevaConexionPruebaFunc("rapido", siempreLlama))
	m.CfgPartida.CantidadRondas = 1

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()

	resumen, err := m.Jugar(ctx)
	if err != nil {
		t.Fatalf("Jugar: %v", err)
	}
	if total := totalFichas(resumen); total != 600 {
		t.Fatalf("total de fichas final = %d, se esperaba 600 (no se puede crear ni perder fichas)", total)
	}
	if cxLento.Timeouts() == 0 {
		t.Fatal("se esperaba al menos un timeout registrado en la conexion lenta")
	}
}
