# pokerFight

Servicio de poker por linea de comandos para correr **torneos de bots**.
Texas Hold'em No Limit, escrito en Go, con un protocolo abierto para que
cualquiera conecte su propio bot a la mesa.

Nacio del ocio de un grupo de desarrolladores jugando poker en Discord y de
la mala experiencia con los bots de Poker Night. La diferencia con las
alternativas actuales es el sistema de competicion entre bots hechos por los
propios jugadores.

---

## Tabla de contenidos

- [Arquitectura](#arquitectura)
- [Estructura del repositorio](#estructura-del-repositorio)
- [Requisitos](#requisitos)
- [Arranque rapido](#arranque-rapido)
- [Escribir un bot](#escribir-un-bot)
- [Protocolo](#protocolo)
- [Alcance y estado](#alcance-y-estado)
- [Reglas del equipo](#reglas-del-equipo)
- [Equipo y fechas](#equipo-y-fechas)
- [Documentacion](#documentacion)

---

## Arquitectura

Tres modulos independientes que se comunican solo a traves del paquete
`protocolo`. Ninguno importa al otro directamente.

```txt
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
| -------- | ----------------- | --------- |
| **Crupier** | Baraja, reparte, administra el pozo, decide la mano ganadora | No conoce la red ni las cuentas |
| **Mesa** | Saldos, turnos, ciegas, validacion de acciones, conexiones | No evalua manos ni guarda cuentas |
| **Casino** | Cuentas, sesiones, bots y su versionado, estadisticas, ranking | No juega |

El diseño es asi a proposito: el Crupier y la Mesa se pueden testear enteros
sin abrir un socket, y el Casino se puede desarrollar en paralelo sin
depender de que la Mesa este lista.

## Estructura del repositorio

```txt
pokerFight/
├── cmd/                        # Binarios (punto de entrada, sin logica)
│   ├── mesa/                   # Servidor de mesa
│   ├── casino/                 # CLI de cuentas, bots y ranking
│   └── bot/                    # Cliente CLI para jugar a mano
│
├── internal/                   # Codigo privado del proyecto
│   ├── protocolo/              # Contrato compartido (no importa a nadie)
│   │   ├── carta.go            # Carta, Rango, Palo, Mano
│   │   ├── accion.go           # Accion, Etapa, AccionSegura
│   │   ├── mensajes.go         # MensajeMesa, MensajeBot, EstadoPublico
│   │   └── codec.go            # Transporte JSON Lines
│   │
│   ├── crupier/
│   │   ├── crupier.go          # Interfaz Crupier + Evaluacion
│   │   ├── baraja.go           # Mazo, Barajar, Robar, Quemar
│   │   ├── pozo.go             # Pozo principal y pozos laterales
│   │   └── evaluador.go        # Puntuacion de manos
│   │
│   ├── mesa/
│   │   ├── mesa.go             # Interfaz Mesa, Config, Jugador, Conexion
│   │   ├── turnos.go           # Boton, ciegas, orden de accion
│   │   ├── saldo.go            # Contabilidad de fichas
│   │   └── servidor.go         # Servidor TCP
│   │
│   ├── casino/
│   │   ├── casino.go           # Interfaz Casino, Cuenta, Bot, Estadisticas
│   │   ├── cuentas.go          # Registro, login, tokens
│   │   ├── bots.go             # Alta y versionado de bots
│   │   └── puntaje.go          # Formula de puntos y ranking
│   │
│   └── almacen/                # Persistencia (JSON hoy, SQLite despues)
│
├── pkg/botsdk/                 # Libreria publica para escribir bots en Go
│
├── bots/                       # Bots de ejemplo / sparring
│   ├── aleatorio/
│   └── conservador/
│
├── docs/
│   ├── interfaces.md           # Documento primario del proyecto
│   ├── protocolo.md            # Mensajes crudos con ejemplos
│   ├── reglas.md               # Reglas de poker implementadas
│   └── prompts/                # Documentacion obligatoria de prompts de IA
│
├── scripts/torneo-local.sh     # Prueba de humo: mesa + 2 bots
├── testdata/                   # Fixtures de tests
├── .github/workflows/ci.yml    # fmt + vet + build + test
├── Makefile
├── CONTRIBUTING.md
└── README.md
```

Convencion: `internal/` es codigo que nadie fuera del repo puede importar
(lo impone el compilador de Go). `pkg/botsdk` es lo unico publico, porque es
lo que los participantes del torneo van a usar.

## Requisitos

- Go 1.26.5
- `make` (opcional pero recomendado)
- Sin dependencias externas: todo con la libreria estandar

## Arranque rapido

```bash
git clone https://github.com/amvz1704/pokerFight.git
cd pokerFight

make ayuda       # lista los comandos disponibles
make build       # compila todo en ./bin
make test        # tests con race detector
make lint        # gofmt + go vet
```

Levantar una mesa y conectarle dos bots de ejemplo:

```bash
make torneo-local
```

O a mano:

```bash
./bin/mesa -addr :9000 -jugadores 6 -ciega-grande 20 -stack 1000
./bin/bot-aleatorio -addr localhost:9000 -token demo-1
```

Crear una cuenta y obtener un token:

```bash
./bin/casino registrar -usuario ezzzzzzno
./bin/casino login -usuario ezzzzzzno
./bin/casino ranking
```

## Escribir un bot

En Go, implementando una sola interfaz:

```go
package main

import (
    "context"

    "github.com/amvz1704/pokerFight/internal/protocolo"
    "github.com/amvz1704/pokerFight/pkg/botsdk"
)

type MiBot struct{}

func (MiBot) Nombre() string { return "mi-bot" }

func (MiBot) Decidir(
    ctx context.Context,
    estado protocolo.EstadoPublico,
    mias protocolo.Mano,
    validas []protocolo.TipoAccion,
) protocolo.Accion {
    // Tu logica aca. Si tardas mas que el timeout, la mesa hace check o fold.
    return protocolo.Accion{Tipo: protocolo.Call}
}

func main() {
    botsdk.Correr(context.Background(), MiBot{}, botsdk.Opciones{
        Direccion: "localhost:9000",
        Token:     "tu-token",
    })
}
```

En **cualquier otro lenguaje**: abri un socket TCP y hablá JSON Lines. Ver
[`docs/protocolo.md`](docs/protocolo.md).

## Protocolo

TCP con un objeto JSON por linea. Elegido para que sea agnostico del lenguaje
y depurable con `nc`.

```txt
bot  → mesa   {"tipo":"saludo","version":"1.0.0","token":"..."}
mesa → bot    {"tipo":"mano_inicio","cartas":[...],"estado":{...}}
mesa → bot    {"tipo":"solicitar_accion","acciones_validas":["fold","call","raise"],"timeout_ms":2000,"estado":{...}}
bot  → mesa   {"tipo":"accion","id_mano":"m-001","accion":{"tipo":"raise","monto":60}}
mesa → bot    {"tipo":"mano_fin","resultado":{...}}
```

Garantias del protocolo:

- La Mesa nunca envia cartas privadas ajenas.
- La Mesa nunca confia en el bot: valida cada accion contra las validas y el saldo.
- Si el bot no responde a tiempo o responde algo invalido, se aplica la
  **accion segura**: `check` si no hay apuesta pendiente, `fold` si la hay.
  Cada timeout resta puntos en el ranking.
- `Accion.Monto` es el **total** al que se lleva la apuesta en la ronda, no el
  incremento.

El contrato completo esta en [`docs/interfaces.md`](docs/interfaces.md), que
es el documento primario del proyecto.

## Alcance y estado

Los `panic("no implementado")` del codigo apuntan a la tarea correspondiente
en `docs/interfaces.md`. Nada de eso es codigo muerto: es el andamiaje con
las interfaces ya cerradas.

### Crupier

- [x] Mazo y barajado criptografico
- [ ] Entrega de cartas iniciales — 4h
- [ ] Mesa inicial y cartas posteriores — 2h
- [ ] Administracion del pozo (incluye pozos laterales) — 3h
- [ ] Decision de mano ganadora — 4h

### Mesa

- [ ] Administracion de saldo — 4h
- [ ] Puntaje general — 3h
- [ ] Conexiones — 5h

### Casino

- [ ] Administracion de cuentas — 3h
- [ ] Administracion de bots por cuenta (versionamiento) — 3h
- [ ] Puntaje general de cuenta — 2h

### Bots

- [x] Bot aleatorio (sparring)
- [x] Bot conservador (piso del ranking)

### Especificaciones funcionales

| # | Especificacion | Modulo | Estado |
| --- | ---------------- | -------- | -------- |
| 1 | Conectarse por internet | Mesa | ⏳ |
| 2 | Registrarse y mantener sesion durante un juego | Casino | ⏳ |
| 3 | Conectar su bot a la mesa | Mesa + Casino | ⏳ |
| 4 | Registrar y guardar estadisticas de los bots | Casino | ⏳ |
| 5 | Decidir la opcion segura ante omision (check/fold) | Protocolo | ✅ |
| 6 | Repartir cartas a cada usuario | Crupier | ⏳ |
| 7 | Contabilidad de apuestas | Mesa | ⏳ |
| 8 | Repartir cartas comunes | Crupier | ⏳ |
| 9 | Turnos y apuestas individuales (SB / BB / D) | Mesa | ⏳ |
| 10 | Decidir el ganador de la partida | Crupier | ⏳ |

## Reglas del equipo

1. Documentacion obligatoria de funcionalidades.
2. **Documentacion mandatoria de interfaces** (mesa-crupier-bots). Es la
   prioridad numero uno: si el codigo y `docs/interfaces.md` no coinciden, el
   PR se rechaza.
3. Clean code obligatorio.
4. Si se usa IA, se documenta el prompt en `docs/prompts/` con la plantilla.
5. Commits descriptivos. Si no, se rechaza el pull request.
6. Testing por versiones.
7. Comentarios, por favor. I beg you.

Detalle completo en [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Equipo y fechas

| Persona | Rol | Modulo |
| --------- | ----- | -------- |
| Lucas | Documentacion explicita — bastion del proyecto | Mesa |
| Enzo | Experiencia acreditada en Balatro | Crupier |
| Gandy | — | Crupier |
| Alvaro | PM | Mesa |
| Jhntn | DevSecMLFin-LLMOps (*pray god, you gonna need it*) | Casino |

Precedentes y consultores: [@ezzzzzzno](https://github.com/) (poker),
[@lucats](https://github.com/) (apuestas), MIT PokerBots.

| Entregable | Fecha |
| ------------ | ------- |
| Mesa (interfaces definidas si o si) | 01/08/26 |
| Crupier | 02/08/26 |
| Casino | 02/08/26 |
| **Deadline** | **02/08/26** |

Las interfaces de la Mesa se congelan el 01/08 aunque la implementacion se
aplace: el resto del equipo depende de ellas para avanzar en paralelo.

## Documentacion

| Documento | Contenido |
| ----------- | ----------- |
| [`docs/interfaces.md`](docs/interfaces.md) | Contratos entre modulos. **Documento primario.** |
| [`docs/protocolo.md`](docs/protocolo.md) | Mensajes crudos con ejemplos |
| [`docs/reglas.md`](docs/reglas.md) | Reglas de poker implementadas |
| [`docs/prompts/`](docs/prompts/) | Prompts de IA usados en el proyecto |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Commits, ramas, checklist de PR |
