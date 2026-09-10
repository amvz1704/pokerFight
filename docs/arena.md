# La arena — correr y extender un torneo

La arena es el motor de torneos: arma los emparejamientos, levanta una mesa por
partida, conecta a los bots, junta los resultados y produce la clasificación.
Es el equivalente al *engine* de MIT Pokerbots.

Este documento es para quien **organiza** el torneo. Si venís a competir, lo que
buscás es [`torneo.md`](torneo.md).

- [Arranque rápido](#arranque-rápido)
- [El archivo de torneo](#el-archivo-de-torneo)
- [Formatos](#formatos)
- [Puntuación](#puntuación)
- [Reproducibilidad](#reproducibilidad)
- [Bots locales y bots remotos](#bots-locales-y-bots-remotos)
- [Qué escribe la arena](#qué-escribe-la-arena)
- [Integración con el Casino](#integración-con-el-casino)
- [Seguridad](#seguridad)
- [Extender la arena](#extender-la-arena)

---

## Arranque rápido

```bash
make build
./bin/arena init                        # escribe un torneo.json de ejemplo
./bin/arena validar -config torneo.json # revisa la config y lista los cruces
./bin/arena correr  -config torneo.json # juega el torneo
```

Salida típica:

```
arena: torneo "torneo-local", formato round-robin, 3 participantes, 6 enfrentamientos
arena: [1/6] r001-aleatorio-vs-base — Bot Base +12350, Bot Aleatorio -12350
...
#    participante          fichas  jugados  ganados perdidos       fichas  timeouts
-----------------------------------------------------------------------------------
1    Bot Base               16940        4        4        0       +16940         0
2    Bot Conservador        -6508        4        0        4        -6508         0
3    Bot Aleatorio         -10432        4        2        2       -10432         0
```

Otros comandos:

```bash
./bin/arena formatos                    # lista formatos y puntuaciones disponibles
./bin/arena correr -config t.json -verboso   # muestra el stderr de los bots
```

---

## El archivo de torneo

Un torneo **es** su archivo JSON. Eso es lo que se versiona en git, lo que se le
comparte a los participantes y lo que hace reproducible una edición.

```json
{
  "nombre": "torneo-2026",
  "formato": "round-robin",
  "puntuacion": "neto",

  "manos_por_enfrentamiento": 200,
  "ida_y_vuelta": true,
  "reponer_stack": true,
  "semilla": 20260906,

  "paralelismo": 4,
  "penalizacion_timeout": 0,
  "guardar_historial": true,
  "salida": "resultados",

  "mesa": {
    "ciega_chica": 10,
    "ciega_grande": 20,
    "stack": 4000,
    "timeout_ms": 2000,
    "espera_jugadores_ms": 15000
  },

  "participantes": [
    { "id": "alice", "nombre": "Alice", "comando": ["./bin/bot-alice"] },
    { "id": "bob",   "nombre": "Bob",   "comando": ["python", "bots/bob/bot.py"] },
    { "id": "carol", "nombre": "Carol", "token": "tok-abc123" }
  ]
}
```

> Un campo con el nombre mal escrito **hace fallar la carga**. Es a propósito:
> un `manos_por_enfrentamento` con una letra de menos se ignoraría en silencio y
> el torneo correría con otra configuración que la que su autor cree.

### Campos

| Campo | Por defecto | Qué hace |
| ----- | ----------- | -------- |
| `nombre` | `"torneo"` | Identifica la edición. Aparece en los reportes. |
| `formato` | `"round-robin"` | Ver [Formatos](#formatos). |
| `puntuacion` | `"neto"` | Ver [Puntuación](#puntuación). |
| `manos_por_enfrentamiento` | `0` (sin límite) | Cuántas manos dura cada partida. |
| `ida_y_vuelta` | `false` | Juega cada cruce dos veces con las sillas intercambiadas. |
| `reponer_stack` | `false` | Devuelve a todos al stack inicial antes de cada mano. |
| `semilla` | `0` | Base de la que se derivan las semillas de cada partida. |
| `paralelismo` | `1` | Cuántas partidas se juegan a la vez. |
| `penalizacion_timeout` | `0` | Cuánto descuenta cada timeout, en las unidades del puntuador. |
| `guardar_historial` | `false` | Escribe el historial de manos en JSON Lines. |
| `salida` | `"resultados"` | Directorio donde se escribe todo. |
| `mesa.ciega_chica` | `10` | |
| `mesa.ciega_grande` | `2 × chica` | Tiene que ser mayor que la chica. |
| `mesa.stack` | `200 × grande` | Fichas iniciales por jugador. |
| `mesa.timeout_ms` | `2000` | Plazo del bot para responder su turno. |
| `mesa.espera_jugadores_ms` | `15000` | Cuánto se espera a que se conecten los bots. |

### Participantes

| Campo | Obligatorio | Qué hace |
| ----- | ----------- | -------- |
| `id` | sí | Único y estable. Es la clave de la clasificación. |
| `nombre` | no | Nombre a mostrar. Por defecto, el `id`. |
| `token` | no | Lo que el bot manda en el saludo. Por defecto, el `id`. |
| `comando` | no | Programa a ejecutar, ya partido en argumentos. Sin él, el bot es [remoto](#bots-locales-y-bots-remotos). |
| `directorio` | no | Directorio de trabajo del comando. |
| `entorno` | no | Variables de entorno extra, en formato `"CLAVE=valor"`. |

La arena le agrega `-addr <host:puerto> -token <token>` al final del `comando`.

### Los tres parámetros que definen la calidad del torneo

**`manos_por_enfrentamiento` + `reponer_stack`.** Van juntos, y su combinación
decide qué mide el torneo:

- **Sin `reponer_stack`**, la partida termina en cuanto alguien se queda sin
  fichas. `manos_por_enfrentamiento` pasa a ser un techo que casi nunca se
  alcanza (en la práctica, 30 o 40 manos de las 200 configuradas) y el resultado
  se satura en ±stack. Lo que se mide es **quién quebró primero**.
- **Con `reponer_stack`**, cada mano arranca en igualdad y se juegan todas. Lo
  que se mide es **cuántas fichas de ventaja saca un bot por mano**, que es
  mucho menos ruidoso.

Para un round-robin, usá `reponer_stack: true` y al menos 200 manos. Con menos,
estás midiendo suerte. Para `mesa-unica` dejalo en `false`: ahí la gracia es
justamente eliminar jugadores.

**`mesa.timeout_ms`.** Define qué clase de bot puede competir. Subilo si querés
dejar lugar a bots que piensan de verdad; bajalo si querés castigar la lentitud.
Ojo con subir `paralelismo` al mismo tiempo: si las partidas se pisan por CPU,
empiezan a aparecer timeouts que no son culpa de nadie.

---

## Formatos

`./bin/arena formatos` lista los registrados.

### `round-robin`

Cada participante contra cada otro, heads-up. Es el formato del torneo: todos
juegan la misma cantidad de manos contra los mismos rivales, así que no depende
de la suerte del sorteo.

Con `ida_y_vuelta` (recomendado), cada par juega **dos partidas intercambiando
las sillas y con la misma semilla**: los dos bots reciben exactamente las mismas
cartas en las mismas posiciones, una vez cada uno. Si A le gana a B en las dos,
le ganó jugando mejor, no porque le tocaran mejores manos. Es la misma reducción
de varianza que usa MIT Pokerbots.

Con N participantes son `N×(N-1)/2` cruces, el doble con ida y vuelta. Con 20
inscritos y 200 manos son 76.000 manos: subí `paralelismo`.

### `mesa-unica`

Todos en una sola mesa (máximo 8), hasta que quede uno. Es el formato "sit and
go": más espectacular y mucho más ruidoso, porque el resultado depende de una
sola partida. Sirve para una final o una exhibición, no para medir a quién
programó mejor.

---

## Puntuación

| Nombre | Unidad | Qué suma |
| ------ | ------ | -------- |
| `neto` | fichas | Las fichas ganadas o perdidas en cada partida. **Por defecto.** |
| `puestos` | puntos | 10 por cada rival que quedó por debajo. |
| `victorias` | victorias | 1 por partida ganada, 0 por el resto. |

`neto` mide *cuánto* mejor jugaste, no solo si ganaste: un bot que gana ajustado
no queda igual que uno que arrasa. Es la que corresponde a un round-robin.

`puestos` es la fórmula que ya usa el ranking histórico del Casino
([`internal/casino/puntaje.go`](../internal/casino/puntaje.go)) y tiene sentido
en `mesa-unica`, donde el neto en fichas depende de una sola partida.

`victorias` es la más fácil de explicar y la más ruidosa. Está para exhibiciones.

`penalizacion_timeout` descuenta de cualquiera de las tres. Podés dejarlo en 0:
un bot lento ya se castiga solo, porque foldear cuando había que pagar cuesta
fichas de verdad.

**Desempates**, en orden: puntos → fichas netas → menos timeouts → id. La tabla
es determinista incluso con empate total.

---

## Reproducibilidad

Fijar `semilla` hace el torneo entero repetible: **la misma configuración
produce exactamente las mismas cartas**.

Cómo funciona:

- La semilla de cada partida se deriva de `semilla` y de **quiénes juegan**, no
  de un contador. Agregar un participante nuevo al archivo no cambia las cartas
  de los cruces que ya existían.
- Cada partida usa un crupier sembrado con esa semilla. La secuencia de barajados
  no depende de lo que hagan los bots, así que la ida y la vuelta de un cruce
  reparten idéntico.
- Los repartos de pozo se emiten en orden de acción, no en el orden en que Go
  recorre un map: dos réplicas de la misma partida producen el mismo JSON.
- Las sillas las fija el enfrentamiento, **no el orden en que los bots logren
  conectarse**. Sin eso, la ida y la vuelta podrían terminar con la misma
  disposición y ser, en la práctica, la misma partida jugada dos veces.

Lo que **no** es reproducible: un bot que use aleatoriedad sin semilla propia, o
que dependa del reloj. Eso es responsabilidad del participante.

Una mesa suelta (`./bin/mesa`) usa barajado criptográfico y no es reproducible,
que es lo correcto para jugar de verdad.

---

## Bots locales y bots remotos

**Locales** (con `comando`): la arena ejecuta el bot como subproceso y le pasa
`-addr` y `-token`. Es el modelo de MIT Pokerbots. Todo pasa en una máquina y el
torneo corre solo.

**Remotos** (sin `comando`): el participante corre su bot donde quiera y lo
conecta él mismo con su token. La arena abre el puerto y espera.

Los dos modos se pueden mezclar en el mismo torneo. Para bots remotos hay que
levantar la arena con `-host 0.0.0.0` (por defecto escucha solo en `127.0.0.1`,
para no exponer el torneo a la red sin querer) y avisarle a cada participante la
dirección. Si un bot remoto no se conecta dentro de `espera_jugadores_ms`, su
enfrentamiento se anota como no jugado.

Un enfrentamiento que falla **no aborta el torneo**: se anota con su motivo en
las incidencias y se sigue. Un bot roto no puede arruinarle la competencia al
resto.

---

## Qué escribe la arena

En el directorio `salida`:

| Archivo | Contenido |
| ------- | --------- |
| `clasificacion.txt` | La tabla final, en texto. Para pegar en un mensaje. |
| `clasificacion.json` | La misma tabla, más las incidencias. |
| `enfrentamientos.jsonl` | Una línea JSON por partida: puestos, netos, timeouts, duración. |
| `torneo.json` | La configuración **efectiva**, con los valores por defecto ya resueltos. |
| `manos/<enfrentamiento>.jsonl` | Historial de manos, si `guardar_historial` está activo. |

El historial trae, por cada mano: las comunitarias, el pozo, el estado de cada
jugador, **todas** las acciones, los repartos y **las cartas privadas de todos
los que jugaron la mano** — no solo las de quienes llegaron al showdown. Es lo
único que permite responder a un reclamo sobre una mano que terminó en fold.

```bash
# ¿Cuántas manos ganó cada uno en un cruce?
jq -r '.repartos[].id_jugador' resultados/manos/r001-alice-vs-bob.jsonl | sort | uniq -c

# El neto de cada participante, ordenado
jq -r '.puestos[] | "\(.nombre)\t\(.neto)"' resultados/enfrentamientos.jsonl \
  | awk -F'\t' '{s[$1]+=$2} END {for (n in s) print s[n], n}' | sort -rn
```

---

## Integración con el Casino

Sin `-casino-db`, la arena corre en **modo abierto**: el token de cada
participante es directamente su identificador. Es lo que se quiere para probar
en local.

Con `-casino-db casino.json`:

```bash
./bin/casino registrar -usuario alice
./bin/casino login -usuario alice          # imprime el token
./bin/arena correr -config torneo.json -casino-db casino.json
./bin/casino ranking
```

Los tokens se validan contra cuentas reales y, al terminar, el resultado del
torneo se suma al ranking histórico. El Casino ve el torneo entero como una sola
"partida", con la posición de la clasificación final.

Para que el ranking asocie bien, el `id` de cada participante en `torneo.json`
tiene que ser el ID de cuenta que devolvió `casino registrar`.

---

## Seguridad

**La arena ejecuta código de terceros con los permisos del usuario que la corre.
No hay sandbox.** Un bot participante puede leer el disco, abrir la red y —si
encuentra el archivo— leer los historiales de manos de sus rivales.

Para un torneo con participantes que no son del equipo:

- Corré la arena dentro de un contenedor descartable o una VM.
- Usá un usuario sin privilegios, con acceso solo al directorio del torneo.
- No dejes el directorio `salida` accesible para los procesos de los bots
  mientras el torneo corre.
- Limitá CPU y memoria por proceso (cgroups, `ulimit`, límites del contenedor):
  la arena impone un plazo por turno, pero no impide que un bot consuma toda la
  máquina y le provoque timeouts a los demás.

Para bots remotos, la arena escucha en `127.0.0.1` salvo que le pases `-host`.
No hay TLS ni límite de intentos en el handshake: no la expongas a internet sin
un proxy adelante.

---

## Extender la arena

Los dos puntos de extensión son **Formato** (qué se juega contra qué) y
**Puntuador** (cómo se suma). Se registran por nombre y el archivo de
configuración los elige por ese nombre, así que agregar uno nuevo no obliga a
tocar ni el runner ni el CLI.

### Un formato nuevo

```go
package arena

// SistemaSuizo empareja en cada ronda a los que van parejos en la tabla.
type SistemaSuizo struct{ Rondas int }

func (SistemaSuizo) Nombre() string { return "suizo" }

func (f SistemaSuizo) Emparejamientos(cfg ConfigTorneo) ([]Enfrentamiento, error) {
    // Devolvé todos los enfrentamientos, cada uno con su ID, su semilla y su
    // límite de manos. Usá semillaDe(cfg.Semilla, ids) para que la semilla no
    // dependa del resto del torneo.
    return enfrentamientos, nil
}

func init() {
    RegistrarFormato("suizo", func(cfg ConfigTorneo) (Formato, error) {
        return SistemaSuizo{Rondas: 5}, nil
    })
}
```

`Emparejamientos` se llama **una sola vez, antes de que se juegue nada**. La
arena no soporta hoy formatos donde la ronda 2 dependa del resultado de la ronda
1 (eliminación directa, suizo de verdad). El camino para agregarlos es un método
opcional nuevo en la interfaz, no cambiar `Emparejamientos`.

### Una puntuación nueva

```go
type PuntuacionPorCiegaGrande struct{ CiegaGrande float64 }

func (PuntuacionPorCiegaGrande) Nombre() string { return "bb-por-100" }
func (PuntuacionPorCiegaGrande) Unidad() string { return "bb/100" }

func (p PuntuacionPorCiegaGrande) Puntos(r ResultadoEnfrentamiento, puesto Puesto) float64 {
    if puesto.ManosJugadas == 0 {
        return 0
    }
    return float64(puesto.Neto) / p.CiegaGrande / float64(puesto.ManosJugadas) * 100
}

func init() {
    RegistrarPuntuador("bb-por-100", func(cfg ConfigTorneo) (Puntuador, error) {
        return PuntuacionPorCiegaGrande{CiegaGrande: float64(cfg.Mesa.CiegaGrande)}, nil
    })
}
```

`Puntos` **tiene que ser una función pura de `(resultado, puesto)`**. La
clasificación se arma sumando, y una fórmula que dependa de estado acumulado
haría que el orden en que terminan los enfrentamientos —que el paralelismo
vuelve impredecible— cambie la tabla final.

### Una forma nueva de lanzar bots

```go
// LanzadorDocker corre cada bot en su propio contenedor.
type LanzadorDocker struct{ Imagen string }

func (l LanzadorDocker) Lanzar(ctx context.Context, p Participante, direccion string) (Sesion, error) {
    // Devolvé una Sesion cuyo Detener() limpie el contenedor.
}
```

Se le pasa al `Torneo` en el campo `Lanzador`. Devolver error significa que el
bot no pudo **arrancar**: el enfrentamiento se da por no jugado. No devuelvas
error porque el bot juegue mal o se caiga después — eso es parte del juego y lo
resuelve la Mesa con la acción segura.

### Un historial nuevo

`Historial` entrega un `mesa.ObservadorMano` por enfrentamiento. Implementalo
para mandar las manos a una base de datos, a un visor en vivo o a donde quieras.

Lo que recibe **incluye las cartas privadas de todos los jugadores**: un
observador nunca debe reenviarlas a un bot.

---

## Ver también

- [`torneo.md`](torneo.md) — la guía del participante.
- [`protocolo.md`](protocolo.md) — el contrato Mesa ↔ Bot.
- [`interfaces.md`](interfaces.md) — los contratos entre módulos.
