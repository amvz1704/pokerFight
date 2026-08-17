// Comando bot-conservador: bot de sparring que nunca apuesta ni sube. Paga
// solo si lo pendiente por igualar es chico frente a la ciega grande, y en
// cualquier otro caso se retira. Pensado como el piso del ranking: cualquier
// bot medianamente agresivo debería poder ganarle.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

// vecesCiegaTolerada es cuántas ciegas grandes está dispuesto a pagar el bot
// para seguir en la mano sin haber apostado él mismo.
const vecesCiegaTolerada = 3

type botConservador struct {
	nombre string
	id     string
}

func (b *botConservador) Nombre() string { return b.nombre }

func (b *botConservador) Decidir(ctx context.Context, estado protocolo.EstadoPublico, mias protocolo.Mano, validas []protocolo.TipoAccion) protocolo.Accion {
	tiene := func(t protocolo.TipoAccion) bool {
		for _, v := range validas {
			if v == t {
				return true
			}
		}
		return false
	}

	if tiene(protocolo.Check) {
		return protocolo.Accion{Tipo: protocolo.Check}
	}

	if tiene(protocolo.Call) {
		var apostado int64
		for _, j := range estado.Jugadores {
			if j.ID == b.id {
				apostado = j.ApuestaRonda
				break
			}
		}
		pendiente := estado.ApuestaActual - apostado
		if pendiente <= estado.CiegaGrande*vecesCiegaTolerada {
			return protocolo.Accion{Tipo: protocolo.Call}
		}
	}

	return protocolo.Accion{Tipo: protocolo.Fold}
}

func main() {
	addr := flag.String("addr", "localhost:9000", "dirección de la mesa")
	token := flag.String("token", "conservador-1", "token de sesión (en modo abierto, es el ID del jugador)")
	flag.Parse()

	bot := &botConservador{nombre: *token, id: *token}
	if err := botsdk.Correr(context.Background(), bot, botsdk.Opciones{Direccion: *addr, Token: *token}); err != nil {
		log.Fatalf("bot-conservador [%s]: %v", *token, err)
	}
}
