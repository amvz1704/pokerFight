# Cómo competir en pokerFight

Guía del participante. Si venís a organizar el torneo, lo que buscás es
[`arena.md`](arena.md).

Escribís un bot que juega Texas Hold'em No Limit heads-up. Compite contra todos
los demás, en las dos posiciones y con las mismas cartas. Gana el que saque más
fichas de ventaja.

- [En 5 minutos](#en-5-minutos)
- [Qué tenés que entregar](#qué-tenés-que-entregar)
- [Escribir un bot en Go](#escribir-un-bot-en-go)
- [Escribir un bot en otro lenguaje](#escribir-un-bot-en-otro-lenguaje)
- [Probar tu bot](#probar-tu-bot)
- [Reglas del torneo](#reglas-del-torneo)
- [Cómo se puntúa](#cómo-se-puntúa)
- [Los errores que más cuestan](#los-errores-que-más-cuestan)
- [Por dónde mejorar](#por-dónde-mejorar)

---

## En 5 minutos

```bash
git clone https://github.com/amvz1704/pokerFight.git
cd pokerFight
make build

# Corré un torneo de prueba entre los bots de ejemplo
./bin/arena init
./bin/arena correr -config torneo.json
```

Copiá [`bots/base/main.go`](../bots/base/main.go) (Go) o
[`bots/ejemplos/python/bot.py`](../bots/ejemplos/python/bot.py) (cualquier otro
lenguaje), cambiale la estrategia y agregate al `torneo.json` para medirte
contra los de ejemplo.

---

## Qué tenés que entregar

Dos cosas:

1. **Un comando que levante tu bot.** Puede ser un binario, `python mi_bot.py`,
   `node mi_bot.js`, lo que sea. La organización lo va a ejecutar con dos
   argumentos agregados al final:

   ```
   <tu comando> -addr <host:puerto> -token <tu-token>
   ```

   Tu bot **debe** aceptar esos dos flags. Es lo único que la arena le pasa.

2. **Todo lo necesario para que ese comando funcione** en la máquina del torneo:
   dependencias, instrucciones de build, versión de runtime. Si tu bot no
   arranca, tus enfrentamientos se anotan como no jugados y no puntúan.

Alternativa: si el torneo lo permite, podés correr tu bot en tu propia máquina y
conectarlo vos mismo. Preguntale a la organización la dirección y usá el token
que te den.

---

## Escribir un bot en Go

Usá [`pkg/botsdk`](../pkg/botsdk/botsdk.go). Resuelve el handshake, la
reconexión, el eco del `id_mano` y el problema de saber cuál de los jugadores
del estado sos vos.

Un bot completo:

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
	// poker.Mejor es el mismo evaluador que usa el crupier para decidir el
	// showdown: lo que acá te dice que tenés es exactamente lo que la mesa va
	// a puntuar.
	jugada := poker.Mejor(t.Mano, t.Estado.Comunitarias)

	if jugada.Categoria >= poker.Trio {
		return t.ApostarPozo(0.75) // Apuesta 3/4 del pozo.
	}
	if t.Puede(protocolo.Check) {
		return t.Check()
	}
	if t.PorIgualar() <= t.Estado.CiegaGrande {
		return t.Pagar()
	}
	return t.Fold()
}

func main() { botsdk.CorrerDesdeFlags(MiBot{}) }
```

`CorrerDesdeFlags` define `-addr` y `-token`, así que tu bot ya cumple el
contrato de la arena.

### La API de `Turno`

Todo lo que sabés al decidir:

| | |
| --- | --- |
| `t.Mano` | Tus dos cartas privadas. |
| `t.Yo` | Tu propia entrada del estado: saldo, apuesta de la ronda, silla. |
| `t.Estado` | La mesa: pozo, comunitarias, rivales, historial de acciones de la mano. |
| `t.Validas` | Las únicas acciones que la mesa va a aceptar ahora. |
| `t.Timeout` | Cuánto tiempo tenés. |

Consultas:

| | |
| --- | --- |
| `t.Puede(protocolo.Raise)` | ¿Está permitida esa acción? |
| `t.PorIgualar()` | Fichas que faltan para igualar. 0 si podés pasar gratis. |
| `t.Techo()` | Total máximo al que podés llevar tu apuesta: el all-in. |
| `t.SubidaMinima()` | Total mínimo que la mesa acepta como subida. |
| `t.PozoSiPagas()` | Cuánto habría en el pozo si igualás. Para pot odds. |

Acciones (usá estos constructores, no armes structs a mano):

| | |
| --- | --- |
| `t.Fold()` `t.Check()` `t.Pagar()` `t.AllIn()` | Las simples. |
| `t.Apostar(total)` | Lleva tu apuesta a `total`, ajustando al rango que la mesa acepta. Elige sola entre `bet` y `raise`, y convierte a all-in si corresponde. |
| `t.ApostarPozo(0.5)` | Media apuesta del pozo. Azúcar sobre `Apostar`. |

`Apostar` existe para que no pierdas manos por aritmética: recorta el total a la
subida mínima y a tu techo, y si en ese turno no se puede subir, cae a la mejor
acción pasiva disponible en lugar de devolver algo que la mesa va a descartar.

### Preparar algo antes de jugar

Si tu bot necesita cargar un archivo, inicializar un modelo o sembrar su
generador de números al azar, implementá `Iniciar`. El SDK lo llama una vez,
**después de parsear los flags** y antes de conectarse:

```go
var semilla = flag.Uint64("semilla", 1, "semilla del generador")

func (b *MiBot) Iniciar() error {
	b.azar = rand.New(rand.NewPCG(*semilla, *semilla))
	return nil
}
```

> Si definís flags propios, **no llames a `flag.Parse()` vos**: en ese momento el
> SDK todavía no definió `-addr` ni `-token`, y el parseo falla. Definí tus flags
> y dejá que `CorrerDesdeFlags` parsee; leé sus valores en `Iniciar`.

Ojo con la aleatoriedad: desde Go 1.22, el generador global de `math/rand`
arranca con una semilla distinta en cada proceso. Un bot que lo use no es
reproducible, y eso hace que el torneo tampoco lo sea.

### Enterarte de más cosas

Implementá estas interfaces opcionales y el SDK te llama solo:

```go
// Principio y fin de cada mano.
func (b *MiBot) ManoInicio(estado protocolo.EstadoPublico, mias protocolo.Mano) {}
func (b *MiBot) ManoFin(resultado protocolo.ResultadoMano) {
	// resultado.Mostradas trae las cartas del rival, si hubo showdown.
	// Es el mejor lugar para aprender a leerlo.
}

// Cada actualización de estado, incluidos los turnos ajenos.
func (b *MiBot) EstadoActualizado(estado protocolo.EstadoPublico) {}
```

### El evaluador

[`pkg/poker`](../pkg/poker/evaluador.go) es el mismo evaluador que usa el
crupier. Usalo: reimplementarlo es trabajo perdido y arriesgás que tu idea de
qué mano gana no coincida con la de la mesa.

```go
ev := poker.Mejor(t.Mano, t.Estado.Comunitarias)
ev.Categoria     // poker.Par, poker.Color, poker.FullHouse...
ev.Descripcion   // "doble par"
ev.Mejores5      // las 5 cartas que forman la jugada
ev.Puntaje       // entero monótono: comparar manos es comparar enteros
```

Comparalo por `Categoria` para decidir. `Puntaje` sirve para comparar dos manos
entre sí, pero no tiene significado fuera de esa comparación: no lo interpretes
ni lo persistas.

---

## Escribir un bot en otro lenguaje

El protocolo es TCP + JSON Lines, sin dependencias. La referencia completa está
en [`protocolo.md`](protocolo.md).

Empezá copiando un ejemplo:

- [`bots/ejemplos/python/bot.py`](../bots/ejemplos/python/bot.py) — 200 líneas, solo `socket` y `json`.
- [`bots/ejemplos/javascript/bot.js`](../bots/ejemplos/javascript/bot.js) — lo mismo con E/S asíncrona, solo el módulo `net`.

Los dos ya resuelven lo aburrido: el buffer de líneas, el handshake, encontrarte
en el estado, y una clase `Turno` con los mismos ayudantes que el SDK de Go. Vos
solo cambiás la función `decidir`.

El bucle mínimo es:

```
conectar
enviar   {"tipo":"saludo","version":"1.0.0","token":"..."}
recibir  bienvenida  → guardá id_jugador
repetir:
    recibir mensaje
    si mano_inicio       → guardá cartas
    si solicitar_accion  → enviá {"tipo":"accion","id_mano":..., "accion":{...}}
    si mano_fin, estado  → informativos, no respondas
    si el socket cierra  → la partida terminó
```

---

## Probar tu bot

**Contra los de ejemplo**, que es lo que importa. Agregate a `torneo.json`:

```json
{
  "id": "mi-bot", "nombre": "Mi Bot",
  "comando": ["python", "mi_bot.py"]
}
```

```bash
./bin/arena correr -config torneo.json -verboso
```

`-verboso` muestra el stdout y stderr de tu bot en la terminal: es donde vas a
ver por qué se cayó.

**Los tres rivales de referencia**, de más fácil a más difícil:

| Bot | Cómo juega | Qué significa perder contra él |
| --- | ---------- | ------------------------------ |
| `conservador` | Nunca apuesta. Paga barato o se retira. | Tu bot está regalando fichas. |
| `aleatorio` | Elige al azar entre las acciones válidas. | Tu bot está peor que el azar. |
| `base` | Chen preflop, jugada hecha postflop. | Normal al principio. Ganarle de forma consistente es el piso para competir. |

**A mano**, para depurar el protocolo:

```bash
./bin/mesa -addr :9000 -jugadores 2 -min-jugadores 2
# en otra terminal
./bin/bot-aleatorio -addr localhost:9000 -token rival
# en otra
python mi_bot.py -addr localhost:9000 -token yo
```

**El historial**, para entender qué pasó:

```bash
jq . resultados/manos/r001-mi-bot-vs-base.jsonl | less
```

Trae las cartas de todos, todas las acciones y los repartos de cada mano.

---

## Reglas del torneo

Las reglas de poker están en [`reglas.md`](reglas.md). Las del torneo:

1. **Formato:** round-robin heads-up. Cada par juega dos partidas intercambiando
   las posiciones, **con las mismas cartas**. Si le ganás a alguien en las dos,
   le ganaste jugando mejor.
2. **Un token por bot.** Dos bots no pueden compartir token: el segundo en
   conectarse se toma como una reconexión del primero.
3. **Tenés un plazo por turno** (`timeout_ms`, típicamente 2000 ms). Si te
   pasás, la mesa juega por vos la acción segura y te anota un timeout.
4. **Solo se juega a través del protocolo.** No leas los archivos del torneo, no
   te conectes a las mesas de otros, no toques los procesos de los demás. La
   arena no lo impide técnicamente; es una regla, y romperla es descalificación.
5. **Tu bot corre en la máquina del torneo** con recursos compartidos. Si
   consumís toda la CPU, les provocás timeouts a los demás.

---

## Cómo se puntúa

Por defecto, `neto`: **las fichas que ganás o perdés, sumadas sobre todos los
enfrentamientos**.

No se puntúa por partidas ganadas. Mide *cuánto* mejor jugás, no solo si ganás:
un bot que gana ajustado no queda igual que uno que arrasa.

Con `reponer_stack` activo (lo normal en round-robin), todos vuelven al stack
inicial antes de cada mano, así que se juegan las 200 manos completas y quedar
sin fichas en una mano no te elimina.

Cada timeout puede descontar puntos, según cómo lo configure la organización.

Desempates: puntos → fichas netas → menos timeouts → id.

---

## Los errores que más cuestan

Estos cuatro explican casi todos los bots que "no funcionan":

**1. `monto` es el TOTAL, no el incremento.**
Si ya pusiste 20, `apuesta_actual` es 40 y querés subir a 100, mandás
`monto: 100`, no `monto: 80`. En Go, usá `t.Apostar(100)` y no pensés más en
esto.

**2. Buscarte en `estado.jugadores` por el token.**
Tu identificador es el `id_jugador` que vino en la **bienvenida**. Con un Casino
detrás, no es el token. Si te equivocás acá, tu bot decide con el saldo de otro y
no falla nunca de forma visible: solo juega mal.

**3. Responder algo inválido "a ver si pasa".**
Cuesta exactamente lo mismo que no responder: la mesa descarta la acción, juega
la segura y te anota un timeout. No hay ninguna ventaja.

**4. Leer del socket sin buffer de líneas.**
Un `read` puede devolver media línea o dos y media. Acumulá hasta el `\n`.

Hay una tabla más completa en
[`protocolo.md`](protocolo.md#errores-frecuentes).

---

## Por dónde mejorar

El bot `base` es deliberadamente flojo. Lo que **no** hace, en orden aproximado
de cuánto rinde arreglarlo:

1. **Posición.** No mira si actúa primero o último. En heads-up eso es enorme:
   con el botón ves actuar al rival antes de decidir.
2. **Proyectos.** No distingue un color hecho de uno a una carta. Cuatro cartas
   del mismo palo en el flop valen mucho más que una carta alta, y `base` las
   trata igual.
3. **Leer al rival.** `estado.historial_acciones` trae todo lo que hizo el rival
   en la mano, y `mano_fin.mostradas` sus cartas cuando hubo showdown. `base`
   ignora las dos cosas. Un bot que ajusta según si el rival sube mucho o poco le
   gana a uno que juega siempre igual.
4. **Farolear.** `base` nunca apuesta sin mano. Contra un rival que se retira
   seguido, eso es plata regalada.
5. **Tamaño de apuesta.** `base` usa 3/4 del pozo siempre. Variar según la
   fuerza de la mano y la textura del flop es de lo que más rinde.

Y si tenés tiempo: `estado.historial_acciones` más el historial de manos del
torneo son suficientes para entrenar algo de verdad contra los bots de ejemplo,
antes de medirte con los demás.

---

## Ver también

- [`protocolo.md`](protocolo.md) — el contrato completo Mesa ↔ Bot.
- [`reglas.md`](reglas.md) — las reglas de poker que aplica la mesa.
- [`arena.md`](arena.md) — cómo se corre el torneo.
