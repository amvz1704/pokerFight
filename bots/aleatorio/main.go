// Comando bot-aleatorio: bot de sparring que elige, entre las acciones
// válidas que le manda la mesa, una al azar. Cuando le toca apostar o subir,
// también elige un monto al azar dentro de lo que puede pagar. Sirve para
// probar la mesa de punta a punta (ver make torneo-local).
package main

import (
	"context"
	"flag"
	"log"
	"math/rand"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

type botAleatorio struct {
	nombre string
	id     string // Igual al token en modo abierto: sirve para ubicarse en estado.Jugadores.
}

func (b *botAleatorio) Nombre() string { return b.nombre }

func (b *botAleatorio) Decidir(ctx context.Context, estado protocolo.EstadoPublico, mias protocolo.Mano, validas []protocolo.TipoAccion) protocolo.Accion {
	if len(validas) == 0 {
		return protocolo.Accion{Tipo: protocolo.Fold}
	}

	tipo := validas[rand.Intn(len(validas))]
	if tipo != protocolo.Bet && tipo != protocolo.Raise {
		return protocolo.Accion{Tipo: tipo}
	}

	var saldo, apostado int64
	for _, j := range estado.Jugadores {
		if j.ID == b.id {
			saldo, apostado = j.Saldo, j.ApuestaRonda
			break
		}
	}
	techo := saldo + apostado // lo máximo a lo que puede llegar (all-in).
	minimo := estado.ApuestaActual + estado.SubidaMinima
	if minimo > techo {
		minimo = techo
	}
	monto := minimo
	if techo > minimo {
		monto += int64(rand.Intn(int(techo - minimo + 1)))
	}
	return protocolo.Accion{Tipo: tipo, Monto: monto}
}

func main() {
	addr := flag.String("addr", "localhost:9000", "dirección de la mesa")
	token := flag.String("token", "aleatorio-1", "token de sesión (en modo abierto, es el ID del jugador)")
	flag.Parse()

	bot := &botAleatorio{nombre: *token, id: *token}
	if err := botsdk.Correr(context.Background(), bot, botsdk.Opciones{Direccion: *addr, Token: *token}); err != nil {
		log.Fatalf("bot-aleatorio [%s]: %v", *token, err)
	}
}
