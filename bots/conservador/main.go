// Comando bot-conservador: bot de sparring que nunca apuesta ni sube. Paga
// solo si lo pendiente por igualar es chico frente a la ciega grande, y en
// cualquier otro caso se retira. Pensado como el piso del ranking: cualquier
// bot medianamente agresivo debería poder ganarle.
package main

import (
	"context"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

// vecesCiegaTolerada es cuántas ciegas grandes está dispuesto a pagar el bot
// para seguir en la mano sin haber apostado él mismo.
const vecesCiegaTolerada = 3

type botConservador struct{}

func (botConservador) Nombre() string { return "conservador" }

func (botConservador) Decidir(ctx context.Context, t botsdk.Turno) protocolo.Accion {
	if t.Puede(protocolo.Check) {
		return t.Check()
	}
	if t.Puede(protocolo.Call) && t.PorIgualar() <= t.Estado.CiegaGrande*vecesCiegaTolerada {
		return t.Pagar()
	}
	return t.Fold()
}

func main() { botsdk.CorrerDesdeFlags(botConservador{}) }
