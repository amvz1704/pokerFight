// Comando bot-aleatorio: bot de sparring que elige, entre las acciones
// válidas que le manda la mesa, una al azar. Cuando le toca apostar o subir,
// también elige un monto al azar dentro de lo que puede pagar.
//
// No pretende jugar bien: existe para que la mesa reciba todas las acciones
// posibles (incluidos all-ins y subidas raras) y se vea si algo se rompe. Es
// el rival contra el que conviene probar un bot nuevo antes del torneo.
package main

import (
	"context"
	"flag"
	"math/rand/v2"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

// semilla se declara a nivel de paquete porque CorrerDesdeFlags es quien parsea:
// su valor recién existe cuando el SDK llama a Iniciar.
var semilla = flag.Uint64("semilla", 1, "semilla del generador. Cambiala para que el bot juegue distinto")

type botAleatorio struct {
	azar *rand.Rand
}

// Iniciar implementa botsdk.Inicializable. El bot lleva su propia fuente
// sembrada en vez de usar la global de math/rand, que desde Go 1.22 arranca con
// una semilla distinta en cada proceso: sin esto, un torneo que incluya a este
// bot no sería reproducible, y la prueba de humo del repositorio daría un
// resultado distinto en cada corrida.
func (b *botAleatorio) Iniciar() error {
	b.azar = rand.New(rand.NewPCG(*semilla, *semilla^0x9E3779B97F4A7C15))
	return nil
}

func (*botAleatorio) Nombre() string { return "aleatorio" }

func (b *botAleatorio) Decidir(ctx context.Context, t botsdk.Turno) protocolo.Accion {
	if len(t.Validas) == 0 {
		return t.Fold()
	}

	switch tipo := t.Validas[b.azar.IntN(len(t.Validas))]; tipo {
	case protocolo.Bet, protocolo.Raise:
		// Un total al azar entre la subida mínima y el all-in. Apostar se
		// encarga de recortarlo a lo que la mesa acepta.
		minimo, techo := t.SubidaMinima(), t.Techo()
		if techo <= minimo {
			return t.Apostar(techo)
		}
		return t.Apostar(minimo + b.azar.Int64N(techo-minimo+1))
	default:
		return protocolo.Accion{Tipo: tipo}
	}
}

func main() { botsdk.CorrerDesdeFlags(&botAleatorio{}) }
