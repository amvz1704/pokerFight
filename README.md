# pokerFight

Arena de torneos de bots de poker. Texas Hold'em No Limit, escrito en Go, con un
protocolo abierto para que cualquiera conecte su bot en el lenguaje que quiera.

Nacio del ocio de un grupo de desarrolladores jugando poker en Discord y de la
mala experiencia con los bots de Poker Night. La diferencia con las alternativas
actuales es el sistema de competicion entre bots hechos por los propios
jugadores.

```
$ ./bin/arena correr -config torneo.json

arena: torneo "torneo-2026", formato round-robin, 3 participantes, 6 enfrentamientos
arena: [1/6] r001-alice-vs-bob — Alice +12350, Bob -12350
...
#    participante          fichas  jugados  ganados perdidos       fichas  timeouts
-----------------------------------------------------------------------------------
1    Alice                  16940        4        4        0       +16940         0
2    Carol                  -6508        4        0        4        -6508         0
3    Bob                   -10432        4        2        2       -10432         0
```

---

## Tabla de contenidos

- [Arranque rapido](#arranque-rapido)
- [Que es cada cosa](#que-es-cada-cosa)
- [Arquitectura](#arquitectura)
- [Estructura del repositorio](#estructura-del-repositorio)
- [Escribir un bot](#escribir-un-bot)
- [Correr un torneo](#correr-un-torneo)
- [Requisitos](#requisitos)
- [Documentacion](#documentacion)
- [Reglas del equipo](#reglas-del-equipo)
- [Estado](#estado)
- [Equipo](#equipo)

---

## Arranque rapido

```bash
git clone https://github.com/amvz1704/pokerFight.git
cd pokerFight

make ayuda          # lista los comandos disponibles
make build          # compila todo en ./bin
make torneo-local   # torneo de humo con los 3 bots de ejemplo
```

`make torneo-local` compila, escribe un `torneo.json` de ejemplo y juega un
round-robin completo. Si eso funciona, todo funciona.

---

## Que es cada cosa

| Binario | Para que |
| ------- | -------- |
| `arena` | **El torneo.** Arma los cruces, corre las partidas, produce la clasificacion. |
| `mesa` | Una mesa suelta, para conectarle bots a mano y depurar. |
| `casino` | Cuentas, tokens, bots versionados y ranking historico. |
| `bot-*` | Bots de ejemplo y sparring. |

| Libreria publica | Para que |
| ---------------- | -------- |
| `pkg/botsdk` | Escribir un bot en Go sin implementar el protocolo. |
| `pkg/poker` | El mismo evaluador de manos que usa el crupier. |

---

## Arquitectura

Cuatro modulos que se comunican solo a traves del paquete `protocolo`.

```txt
                        ┌──────────────┐
                        │    ARENA     │  emparejamientos, clasificacion
                        │              │  una mesa por enfrentamiento
                        └──────┬───────┘
                               │
        ┌──────────────┐  tokens de sesion   ┌──────────────┐
        │    CASINO    │───────────────────▶│     MESA     │
        │              │                     │              │
        │ cuentas      │◀───────────────────│ saldos       │
        │ bots (vers.) │  resultados/stats   │ turnos       │
        │ ranking      │                     │ ciegas       │
        └──────────────┘                     │ conexiones   │
                                             └──────┬───────┘
                                                    │
                              cartas / ganador      │
                                             ┌──────▼───────┐
                                             │   CRUPIER    │
                                             │ mazo         │
                                             │ reparto      │
                                             │ pozo         │
                                             │ evaluador    │
                                             └──────────────┘
                                                    ▲
                            TCP + JSON Lines        │
        ┌──────────────┐                            │
        │     BOTS     │────────────────────────────┘
        │ (cualquier   │
        │  lenguaje)   │
        └──────────────┘
```

| Modulo | Responsabilidad | No hace |
| ------ | --------------- | ------- |
| **Crupier** | Baraja, reparte, administra el pozo, decide la mano ganadora | No conoce la red ni las cuentas |
| **Mesa** | Saldos, turnos, ciegas, validacion de acciones, conexiones | No evalua manos ni guarda cuentas |
| **Casino** | Cuentas, sesiones, bots y su versionado, estadisticas, ranking | No juega |
| **Arena** | Emparejamientos, ejecucion de partidas, clasificacion, historiales | No conoce reglas de poker ni cuentas |

El diseño es asi a proposito: el Crupier y la Mesa se pueden testear enteros sin
abrir un socket, y ni la Mesa ni la Arena importan el Casino (reciben
`ValidarToken` por inyeccion). El contrato completo esta en
[`docs/interfaces.md`](docs/interfaces.md).

---

## Estructura del repositorio

```txt
pokerFight/
├── cmd/                        # Binarios (punto de entrada, sin logica)
│   ├── arena/                  # Runner de torneos
│   ├── mesa/                   # Servidor de una mesa suelta
│   └── casino/                 # CLI de cuentas, bots y ranking
│
├── internal/                   # Codigo privado del proyecto
│   ├── protocolo/              # Contrato compartido (no importa a nadie)
│   │   ├── carta.go            # Carta, Rango, Palo, Mano
│   │   ├── accion.go           # Accion, Etapa, AccionSegura
│   │   ├── mensajes.go         # MensajeMesa, MensajeBot, EstadoPublico
│   │   └── codec.go            # Transporte JSON Lines
│   │
│   ├── crupier/
│   │   ├── crupier.go          # Interfaz Crupier + DecidirGanadores
│   │   ├── baraja.go           # Mazo, barajado criptografico o sembrado
│   │   └── pozo.go             # Pozo principal y pozos laterales
│   │
│   ├── mesa/
│   │   ├── mesa.go             # Interfaz Mesa, Config, Jugador, bucle de juego
│   │   ├── turnos.go           # Boton, ciegas, orden de accion
│   │   ├── saldo.go            # Contabilidad de fichas, validacion de acciones
│   │   ├── servidor.go         # Servidor TCP, handshake, asignacion de sillas
│   │   └── conexion_prueba.go  # Conexion sin red, para tests
│   │
│   ├── arena/
│   │   ├── arena.go            # Tipos base y el runner del torneo
│   │   ├── formato.go          # Emparejamientos. Extensible por registro
│   │   ├── puntuacion.go       # Formulas de puntaje. Extensible por registro
│   │   ├── lanzador.go         # Como se levanta un bot: proceso o remoto
│   │   ├── clasificacion.go    # Agregacion en la tabla final
│   │   ├── historial.go        # Historial de manos en JSON Lines
│   │   └── config.go           # El torneo como archivo JSON
│   │
│   ├── casino/
│   │   ├── casino.go           # Interfaz Casino, Cuenta, Bot, Estadisticas
│   │   ├── cuentas.go          # Registro, login, tokens
│   │   ├── bots.go             # Alta y versionado de bots
│   │   └── puntaje.go          # Formula de puntos y ranking historico
│   │
│   └── almacen/                # Persistencia (JSON hoy, SQLite despues)
│
├── pkg/                        # LO UNICO PUBLICO: lo usan los bots
│   ├── botsdk/                 # Libreria para escribir bots en Go
│   └── poker/                  # Evaluador de manos (el mismo que el crupier)
│
├── bots/
│   ├── aleatorio/              # Sparring: acciones al azar
│   ├── conservador/            # Sparring: el piso del ranking
│   ├── base/                   # Bot de referencia. El punto de partida
│   └── ejemplos/
│       ├── python/             # Bot completo, sin dependencias
│       └── javascript/         # Idem, con E/S asincrona
│
├── docs/
│   ├── protocolo.md            # Contrato Mesa <-> Bot. Para participantes
│   ├── torneo.md               # Como competir. Para participantes
│   ├── arena.md                # Como correr y extender un torneo
│   ├── interfaces.md           # Contratos entre modulos. Documento primario
│   ├── reglas.md               # Reglas de poker implementadas
│   └── prompts/                # Documentacion obligatoria de prompts de IA
│
├── .github/workflows/ci.yml    # fmt + vet + build + test (con race)
├── Makefile
├── CONTRIBUTING.md
└── README.md
```

Convencion: `internal/` es codigo que nadie fuera del repo puede importar (lo
impone el compilador de Go). `pkg/` es lo unico publico, porque es lo que van a
usar los participantes del torneo.

---

## Escribir un bot

**En Go**, implementando una sola interfaz:

```go
package main

import (
	"context"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
	"github.com/amvz1704/pokerFight/pkg/poker"
)

type MiBot struct{}

func (MiBot) Nombre() string { return "mi-bot" }

func (MiBot) Decidir(ctx context.Context, t botsdk.Turno) protocolo.Accion {
	// El mismo evaluador que usa el crupier para el showdown.
	if poker.Mejor(t.Mano, t.Estado.Comunitarias).Categoria >= poker.Trio {
		return t.ApostarPozo(0.75)
	}
	if t.Puede(protocolo.Check) {
		return t.Check()
	}
	return t.Fold()
}

func main() { botsdk.CorrerDesdeFlags(MiBot{}) }
```

El SDK resuelve el handshake, la reconexion, el eco del `id_mano` y el calculo de
montos validos.

**En cualquier otro lenguaje**: abri un socket TCP y hablá JSON Lines. Hay bots
completos y comentados en
[`bots/ejemplos/python`](bots/ejemplos/python/bot.py) y
[`bots/ejemplos/javascript`](bots/ejemplos/javascript/bot.js), y la referencia en
[`docs/protocolo.md`](docs/protocolo.md).

La guia completa del participante esta en [`docs/torneo.md`](docs/torneo.md).

### El protocolo, en 5 lineas

```txt
bot  → mesa   {"tipo":"saludo","version":"1.0.0","token":"..."}
mesa → bot    {"tipo":"bienvenida","id_jugador":"c-42","silla":0}
mesa → bot    {"tipo":"mano_inicio","cartas":[...],"estado":{...}}
mesa → bot    {"tipo":"solicitar_accion","acciones_validas":["fold","call","raise"],"timeout_ms":2000,"estado":{...}}
bot  → mesa   {"tipo":"accion","id_mano":"m-001","accion":{"tipo":"raise","monto":60}}
mesa → bot    {"tipo":"mano_fin","resultado":{...}}
```

Garantias:

- La Mesa nunca envia cartas privadas ajenas.
- La Mesa nunca confia en el bot: valida cada accion contra las validas, el
  saldo y la subida minima.
- Si el bot no responde a tiempo **o responde algo invalido**, se aplica la
  **accion segura**: `check` si no hay apuesta pendiente, `fold` si la hay. Cada
  aplicacion resta puntos en el ranking.
- `Accion.Monto` es el **total** al que se lleva la apuesta en la ronda, no el
  incremento.

---

## Correr un torneo

```bash
./bin/arena init                          # escribe un torneo.json de ejemplo
./bin/arena validar -config torneo.json   # revisa la config y lista los cruces
./bin/arena correr  -config torneo.json   # juega el torneo
```

Un torneo **es** su archivo JSON: eso es lo que se versiona y lo que hace
reproducible una edicion.

Lo que hace la arena por vos:

- **Round-robin heads-up**: todos contra todos, en las dos posiciones.
- **Mismas cartas en la ida y en la vuelta**: los dos bots de un cruce reciben
  exactamente las mismas manos, una vez cada uno. Si le ganaste en las dos, le
  ganaste jugando mejor. Es la reduccion de varianza de MIT Pokerbots.
- **Reproducible**: fijar la semilla repite el torneo entero, carta por carta.
- **Aislado de fallos**: un bot que no arranca no aborta el torneo, se anota como
  incidencia y se sigue.
- **Auditable**: historial completo de cada mano, con las cartas de todos —
  incluidas las de quienes se retiraron sin llegar al showdown.
- **Extensible**: formatos y puntuaciones se registran por nombre y se eligen
  desde el archivo de configuracion, sin tocar el runner.

Referencia completa: [`docs/arena.md`](docs/arena.md).

---

## Requisitos

- Go 1.26.5 o superior
- `make` (opcional pero recomendado)
- Sin dependencias externas: todo con la libreria estandar

Para `make test` con el detector de carreras hace falta un compilador de C
(`-race` requiere cgo). En Windows sin uno, usá `go test ./...` a secas; CI corre
la version con `-race`.

---

## Documentacion

| Documento | Para quien | Contenido |
| --------- | ---------- | --------- |
| [`docs/torneo.md`](docs/torneo.md) | **Participantes** | Como escribir tu bot y competir |
| [`docs/protocolo.md`](docs/protocolo.md) | **Participantes** | El contrato Mesa ↔ Bot, completo |
| [`docs/reglas.md`](docs/reglas.md) | Participantes | Reglas de poker implementadas |
| [`docs/arena.md`](docs/arena.md) | Organizadores | Correr y extender un torneo |
| [`docs/interfaces.md`](docs/interfaces.md) | Equipo | Contratos entre modulos. **Documento primario** |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Equipo | Commits, ramas, checklist de PR |
| [`docs/prompts/`](docs/prompts/) | Equipo | Prompts de IA usados en el proyecto |

---

## Reglas del equipo

1. Documentacion obligatoria de funcionalidades.
2. **Documentacion mandatoria de interfaces** (mesa-crupier-bots). Es la
   prioridad numero uno: si el codigo y `docs/interfaces.md` no coinciden, el PR
   se rechaza.
3. Clean code obligatorio.
4. Si se usa IA, se documenta el prompt en `docs/prompts/` con la plantilla.
5. Commits descriptivos. Si no, se rechaza el pull request.
6. Testing por versiones.
7. Comentarios, por favor. I beg you.

Detalle completo en [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## Estado

### Especificaciones funcionales

| # | Especificacion | Modulo | Estado |
| --- | -------------- | ------ | ------ |
| 1 | Conectarse por internet | Mesa | ✅ |
| 2 | Registrarse y mantener sesion durante un juego | Casino | ✅ |
| 3 | Conectar su bot a la mesa | Mesa + Casino | ✅ |
| 4 | Registrar y guardar estadisticas de los bots | Casino | ✅ |
| 5 | Decidir la opcion segura ante omision (check/fold) | Protocolo | ✅ |
| 6 | Repartir cartas a cada usuario | Crupier | ✅ |
| 7 | Contabilidad de apuestas | Mesa | ✅ |
| 8 | Repartir cartas comunes | Crupier | ✅ |
| 9 | Turnos y apuestas individuales (SB / BB / D) | Mesa | ✅ |
| 10 | Decidir el ganador de la partida | Crupier | ✅ |
| 11 | Correr un torneo entre bots | Arena | ✅ |

### Limitaciones conocidas

- **No hay sandbox para los bots de terceros.** La arena ejecuta codigo de
  participantes con los permisos del usuario que la corre. Ver
  [`docs/arena.md`](docs/arena.md#seguridad).
- **El Casino no tiene autenticacion real**: `Login` no pide contraseña.
- **La arena no soporta formatos con rondas dependientes** (eliminacion directa,
  suizo de verdad): los emparejamientos se calculan todos antes de jugar.
- **`LevantarJugador` cierra la conexion**: no hay modo espectador.

La lista completa esta en
[`docs/interfaces.md`](docs/interfaces.md#7-estado-del-proyecto).

---

## Equipo

| Persona | Rol | Modulo |
| ------- | --- | ------ |
| Lucas | Documentacion explicita — bastion del proyecto | Mesa |
| Enzo | Experiencia acreditada en Balatro | Crupier |
| Gandy | — | Crupier |
| Alvaro | PM | Mesa |
| Jhntn | DevSecMLFin-LLMOps (*pray god, you gonna need it*) | Casino |

Precedentes y consultores: [@ezzzzzzno](https://github.com/) (poker),
[@lucats](https://github.com/) (apuestas), MIT PokerBots.
