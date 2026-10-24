# El campeonato — torneo por fases de las bases

Es el motor del **Poker Fight de Bots** tal como lo describen las
[bases](#qué-dicen-las-bases-y-qué-hace-cada-parte): una clasificatoria de varias
rondas con mesas sorteadas de nuevo cada vez, y una final por eliminación hasta
una mesa final. Se configura y se sigue desde un **panel web**.

Este documento es para quien **organiza**. Si venís a competir, lo que buscás es
[`torneo.md`](torneo.md). Para un round-robin entre bots que ya se conocen, usá la
[arena](arena.md): son herramientas distintas (ver [Campeonato o arena](#campeonato-o-arena)).

- [Arranque rápido](#arranque-rápido)
- [Qué dicen las bases y qué hace cada parte](#qué-dicen-las-bases-y-qué-hace-cada-parte)
- [El panel](#el-panel)
- [Configuración](#configuración)
- [Cómo se arman las mesas](#cómo-se-arman-las-mesas)
- [Inscripción y credenciales](#inscripción-y-credenciales)
- [Cómo juegan los bots: una conexión para todo el torneo](#cómo-juegan-los-bots-una-conexión-para-todo-el-torneo)
- [Qué pasa cuando algo falla](#qué-pasa-cuando-algo-falla)
- [Un torneo en varios días](#un-torneo-en-varios-días)
- [Decisiones que las bases dejan abiertas](#decisiones-que-las-bases-dejan-abiertas)
- [Seguridad](#seguridad)
- [Campeonato o arena](#campeonato-o-arena)
- [API del panel](#api-del-panel)

---

## Arranque rápido

La forma más rápida de verlo funcionar es el demo: 24 bots de ejemplo que la
arena lanza sola, en una sola máquina. (`make build` deja los binarios en `./bin`;
los comandos de abajo son `./bin/campeonato`. O directamente: `make campeonato-demo`, que escribe todo en `campeonato-demo/` para no pisar una configuración real.)

```bash
make build
campeonato init -demo 24            # escribe campeonato.json e inscritos.json
campeonato servir                   # imprime la URL del panel
```

`servir` imprime algo así:

```
  Demo local
  24 bots inscritos (24 los lanza esta arena, el resto se conecta solo)
  Bots, conectarse a:  [::]:9000
  Panel:               http://127.0.0.1:8080/?clave=3f9a...

```

Abrí esa URL, miralo en **Configuración**, apretá **Modo automático** y seguí las
mesas en vivo.

Para un torneo de verdad:

```bash
campeonato init                                          # la configuración de las bases
campeonato inscribir -inscritos inscritos.json -csv equipos.csv   # da de alta y muestra los tokens
campeonato plan -jugadores 64                            # cuántas mesas tendría cada fase
campeonato servir -bots :9000 -panel 127.0.0.1:8080
```

Los bots de cada equipo se conectan a `host:9000` con el token que se les entregó.

---

## Qué dicen las bases y qué hace cada parte

| Las bases piden | Dónde está |
| --------------- | ---------- |
| Bots que se conectan por API y deciden solos | Protocolo de [`protocolo.md`](protocolo.md) |
| Cada bot empieza con 10 000 fichas | `fases[0].stack` |
| Mesas de 5; "si la división no es exacta, algunas tienen 6" | [`RepartirMesas`](#cómo-se-arman-las-mesas) |
| Rondas de [200] manos, ciegas que suben cada [X] manos, el que se queda sin fichas queda eliminado | `rondas`, `manos_por_ronda`, `ciegas` |
| Tras cada ronda se sortean las mesas de nuevo y cada bot conserva sus fichas | `Campeonato.JugarRonda` |
| Pasan a la final los 16 o 32 con más fichas | `clasifican` ([0 = automático](#decisiones-que-las-bases-dejan-abiertas)) |
| Final: mesas de 4, pasan los 2 últimos en pie | Fase `eliminatoria`, `avanzan_por_mesa: 2` |
| Luego 4 mesas de 2 o 4, pasa el último en pie | Fase con `mesas: 4`, `avanzan_por_mesa: 1` |
| Mesa final de 4 | Fase con `mesas: 1` |
| 10 segundos por jugada; si no responde, check o fold | `mesa.timeout_ms` |
| "Basta con conectarlo y dejarlo ejecutándose" | [Lobby](#cómo-juegan-los-bots-una-conexión-para-todo-el-torneo) |
| Si se desconecta, la arena juega por él hasta que vuelva | [Presencia](#qué-pasa-cuando-algo-falla) |
| Solo identificadores aleatorios; dos bots de un equipo pueden coincidir en una mesa | [Alias](#cómo-juegan-los-bots-una-conexión-para-todo-el-torneo) |
| Hasta 2 bots por equipo | [Inscripción](#inscripción-y-credenciales) |

Con los números de las bases, 64 inscritos dan **64 → 32 → 16 → 4 → 1**, y con 32
inscritos, **32 → 16 → 8 → 4 → 1** (con "4 mesas de 2" en la penúltima fase).
`campeonato plan` lo muestra sin jugar nada.

---

## El panel

Se abre con la URL que imprime `servir`. La clave de administración viaja en la URL
la primera vez y la página la saca de la barra de direcciones; después se guarda
solo en la pestaña.

**Encabezado.** Fase y ronda en curso, bots conectados contra bots esperados, y los
botones de control:

| Botón | Qué hace |
| ----- | -------- |
| **Jugar siguiente ronda** | Juega una ronda y se detiene. Es el modo para sesiones con horario: una ronda, revisás, la siguiente. |
| **Modo automático** | Sigue ronda tras ronda y de fase en fase hasta el final. |
| **Pausar** | En modo automático, detiene después de la ronda en juego. |
| **Cancelar ronda** | Corta la ronda en juego y la **descarta entera**: ningún resultado parcial se aplica. |

Si antes de jugar faltan bots por conectar, el panel avisa cuántos son: la arena va
a jugar por ellos con check o fold, como piden las bases, y van a ir perdiendo
fichas.

**Configuración.** Es donde se define *cuántas mesas y cuántos jugadores*:

- Cada fase es una fila: tipo, jugadores por mesa, **cantidad de mesas** (0 = se
  calcula), fichas, rondas, manos por ronda, cuántos pasan, ciegas.
- Debajo, **la vista previa**: cuántos entran, cuántas mesas y de qué tamaño, y
  cuántos pasan, fase por fase. Se recalcula mientras escribís.
- El campo **"Jugadores para la vista previa"** deja ver cómo quedaría con 64 aunque
  hoy haya 20 inscritos. No cambia la configuración.
- La vista previa **avisa** de lo que no cierra: una fase que no elimina a nadie,
  pedir más mesas de las que los jugadores permiten, una última fase que no deja un
  único campeón.
- **Se puede editar hasta jugar la primera ronda.** Después queda bloqueada: las
  fases ya jugadas dejarían de coincidir con las que dice la configuración. Si
  cancelás la primera ronda, vuelve a ser editable.

**Mesas en juego.** Una tarjeta por mesa: mano, calle, pozo, ciegas, cartas
comunitarias y cada asiento con sus fichas, su apuesta, si se retiró, si está
all-in y si el bot está conectado. **No hay cartas privadas**: ni el panel las ve
mientras se juega.

**Jugadores.** Todos los inscritos con su alias, equipo y bot (solo la organización
ve esa relación), estado, fichas, manos jugadas, timeouts y caídas. Primero quien
sigue o ganó, después los demás ordenados por hasta dónde llegaron.

**Rondas jugadas.** El historial de lo que ya pasó.

---

## Configuración

`campeonato.json`. Un campo con el nombre mal escrito **hace fallar la carga**: se
ignoraría en silencio y el torneo correría con otra configuración que la que se cree.

```json
{
  "nombre": "Poker Fight de Bots 2026",
  "semilla": null,
  "mesa": { "timeout_ms": 10000 },
  "salida": "campeonato",
  "guardar_historial": true,
  "fases": [
    {
      "nombre": "Clasificatoria", "tipo": "clasificatoria",
      "jugadores_por_mesa": 5, "mesas": 0, "stack": 10000,
      "rondas": 4, "manos_por_ronda": 200, "clasifican": 0,
      "ciegas": { "niveles": [50, 75, 100, 150, 200, 300, 400, 600, 800, 1200], "manos_por_nivel": 100 }
    },
    {
      "nombre": "Final: mesas de 4", "tipo": "eliminatoria",
      "jugadores_por_mesa": 4, "stack": 10000,
      "avanzan_por_mesa": 2, "max_manos": 600,
      "ciegas": { "niveles": [50, 75, 100, 150, 200, 300, 400, 600, 800, 1200], "manos_por_nivel": 30 }
    },
    {
      "nombre": "Final: 4 mesas", "tipo": "eliminatoria",
      "mesas": 4, "stack": 10000, "avanzan_por_mesa": 1, "max_manos": 600,
      "ciegas": { "niveles": [50, 75, 100, 150, 200, 300, 400, 600, 800, 1200], "manos_por_nivel": 30 }
    },
    {
      "nombre": "Mesa final", "tipo": "eliminatoria",
      "mesas": 1, "stack": 10000, "avanzan_por_mesa": 1, "max_manos": 800,
      "ciegas": { "niveles": [50, 75, 100, 150, 200, 300, 400, 600, 800, 1200], "manos_por_nivel": 30 }
    }
  ]
}
```

### Campos generales

| Campo | Qué hace |
| ----- | -------- |
| `nombre` | Identifica la edición. |
| `semilla` | Con valor, el campeonato es **reproducible**: el sorteo de mesas, los alias y el mazo de cada mesa salen de ella. Sin valor (lo que corresponde a un torneo real) todo usa aleatoriedad criptográfica y **nadie, ni la organización, puede predecir las cartas**. |
| `mesa.timeout_ms` | Plazo de cada jugada. Las bases fijan 10 000. |
| `salida` | Directorio del estado y del historial de manos. |
| `guardar_historial` | Escribe cada mano en `salida/manos/<mesa>.jsonl`, con **las cartas de todos**: es lo que permite resolver un reclamo. No lo publiques. |

### Campos de cada fase

| Campo | Aplica a | Qué hace |
| ----- | -------- | -------- |
| `tipo` | todas | `clasificatoria` o `eliminatoria` |
| `jugadores_por_mesa` | todas | Tamaño de mesa buscado (2 a 10). Si la división no es exacta, algunas llevan uno más. |
| `mesas` | todas | Si es mayor que 0, **fija la cantidad de mesas** y el tamaño sale de cuántos lleguen. Lo usan las fases definidas por cuántas mesas hay ("4 mesas de 2 o 4"). |
| `stack` | todas | Fichas con las que arrancan todos en la fase. `0` = conservan las que traen (solo a partir de la segunda fase; la primera necesita un valor). |
| `ciegas.niveles` | todas | Ciega chica de cada nivel; la grande es el doble. El último nivel se repite. |
| `ciegas.manos_por_nivel` | todas | Cuánto dura cada nivel. En una clasificatoria cuenta **sobre todas las rondas de la fase**: las ciegas siguen subiendo entre rondas, que es lo que fuerza las eliminaciones. |
| `rondas`, `manos_por_ronda` | clasificatoria | Cuántas veces se sortean las mesas y cuántas manos como máximo dura cada una. Una mesa termina antes si queda un solo jugador con fichas. |
| `clasifican` | clasificatoria | Cuántos pasan, los de más fichas. `0` = [automático](#decisiones-que-las-bases-dejan-abiertas). |
| `avanzan_por_mesa` | eliminatoria | Cuántos pasan de cada mesa: los últimos en pie. **La mesa termina cuando quedan exactamente esos.** |
| `max_manos` | eliminatoria | Tope de manos por mesa. Es una red de seguridad: dos bots que nunca apuestan podrían alargar una mesa mucho. Al llegar al tope pasan los que más fichas tengan. |

### Cómo se decide quién pasa

- **Clasificatoria:** se ordena por fichas. Desempates: menos timeouts (el bot más
  confiable) y, si sigue el empate, el alias (arbitrario pero estable). Quien se
  queda sin fichas deja de jugar y no vuelve en las rondas siguientes.
- **Eliminatoria:** pasan los primeros `avanzan_por_mesa` de cada mesa. Si dos
  jugadores quiebran en la misma mano, queda mejor el que **empezó la mano con más
  fichas**: sin esa regla el orden dependería de la silla.
- Si en una mesa `avanzan_por_mesa` es mayor o igual que su tamaño, pasan todos y
  **la mesa no se juega**.

---

## Cómo se arman las mesas

`RepartirMesas(jugadores, porMesa, mesas)`:

- Con `mesas` fijo, se reparte lo más parejo posible.
- Si no, se busca el tamaño pedido y los que sobran se suman **de a uno** a las
  mesas ("mesas de 5; algunas tienen 6").

| Jugadores | Mesas de 5 | Por qué |
| --------- | ---------- | ------- |
| 20 | 4 × 5 | Exacto. |
| 23 | 6, 6, 6, 5 | Sobran 3: tres mesas de 6. |
| 9 | 5, 4 | No se puede hacer **una mesa de 9**: se arman más mesas y alguna queda de uno menos. |
| 7 | 4, 3 | Idem. |

Siempre: ninguna mesa de menos de 2, ninguna de más de 10, y las mesas difieren
entre sí a lo sumo en uno. Lo verifica un test que recorre todos los tamaños de 2 a
200 jugadores.

En una clasificatoria las mesas se **sortean de nuevo en cada ronda con los que
siguen en pie**, así que a medida que quiebran bots las mesas se achican. El plan
muestra las del arranque.

El sorteo mezcla a los jugadores y corta; **el orden dentro de la mesa también es
el orden de las sillas**, así que nadie tiene una posición garantizada respecto del
botón.

---

## Inscripción y credenciales

`inscritos.json` tiene un token **secreto** por bot. Es lo único que identifica a
un bot ante la arena: quien lo conozca puede jugar en su lugar. El archivo se
guarda con permisos 0600 (**solo en Linux y macOS**: en Windows esos permisos no
existen, así que protegé la carpeta) y **no se sube a ningún repositorio** (está en
`.gitignore`).

```bash
# Un equipo
campeonato inscribir -equipo "Los Tahúres" -bot alfa
campeonato inscribir -equipo "Los Tahúres" -bot beta

# El export del formulario: columnas equipo,bot (el encabezado es opcional)
campeonato inscribir -csv equipos.csv
```

Imprime el token de cada bot **para entregárselo al equipo**. Las bases permiten
hasta 2 bots por equipo y la lista lo exige.

Las bases dicen que cada equipo recibe sus credenciales y un kit con la arena para
probar en local: el kit es este mismo repositorio. Para pruebas locales, un
inscrito puede traer `comando` y la arena lo lanza sola (es lo que hace el demo).

---

## Cómo juegan los bots: una conexión para todo el torneo

Una mesa suelta (`mesa`) escucha en un puerto y admite a quien llegue: dos mesas
necesitarían dos puertos y un protocolo para decidir quién va a cuál (es el
[issue #9](https://github.com/amvz1704/pokerFight/issues/9)). El campeonato lo
resuelve con un **lobby**:

```
                                       ┌───────────┐
   bot A ──┐                       ┌──▶│  mesa 1   │
   bot B ──┤   ┌──────────────┐    │   └───────────┘
   bot C ──┼──▶│    LOBBY     │────┤   ┌───────────┐
   bot D ──┤   │  un puerto   │    ├──▶│  mesa 2   │
   bot E ──┘   └──────────────┘    │   └───────────┘
                  token → alias    │   ┌───────────┐
                                   └──▶│  mesa 3   │
                                       └───────────┘
```

- **El bot se conecta una sola vez**, con su token, y se queda conectado. Qué mesa
  le toca en cada ronda lo decide la arena, y el bot no se entera: sigue recibiendo
  `mano_inicio`, `solicitar_accion` y `mano_fin` como siempre. El protocolo **no
  cambió**.
- El lobby reutiliza el handshake de `mesa.Servidor` (versión, token, bienvenida).
  Para eso se hace pasar por una mesa que no juega.
- **Alias.** Ni el equipo ni el nombre del bot salen de la organización: cada bot
  recibe un alias aleatorio (`j-a1b2c3`), que es su `id_jugador` y su nombre en
  todas las mesas. Cumple "la arena no revela [...] los nombres de los demás
  participantes, solo identificadores aleatorios" y hace que dos bots de un mismo
  equipo que coincidan en una mesa jueguen cada uno por su lado sin reconocerse. El
  alias es **estable durante todo el campeonato**: un bot puede aprender cómo juega
  un rival que se le cruza en varias rondas, aunque no sepa quién es.
- Un bot que no está en ninguna mesa (eliminado, o esperando) sigue conectado y no
  recibe nada.

---

## Qué pasa cuando algo falla

| Situación | Qué hace la arena |
| --------- | ----------------- |
| El bot no responde en `timeout_ms` | Juega check (o fold si hay que pagar) y le anota un timeout. |
| El bot responde algo inválido | Idem. Cuenta igual que no responder: la arena hace lo mismo en los dos casos. |
| El bot manda una línea que no es JSON | Idem, y **el bot sigue conectado**. Cortarlo lo sacaría del campeonato por un error de una sola jugada. |
| El bot se desconecta | La arena juega por él con la acción segura hasta que vuelva, y su silla sigue ocupada. Va perdiendo las ciegas. |
| El bot vuelve a conectarse con su token | Retoma **la misma silla con las mismas fichas**, en la mesa que esté jugando. |
| El bot no lee lo que se le manda | A los 2 s de no poder escribirle se lo da por desconectado. Esperar el plazo de jugada (10 s) en cada difusión de estado frenaría a los otros cuatro de la mesa. |
| Dos procesos usan el mismo token | El segundo en conectarse **reemplaza** al primero. |
| La mesa falla o se cancela la ronda | La ronda se descarta entera y se vuelve a jugar con un sorteo nuevo. Nunca se aplica un resultado parcial. |

Un bot que se cae **no se reconecta solo** (el SDK de Go termina): cada equipo tiene
que reconectar por su cuenta, con el mismo token. Está dicho en
[`torneo.md`](torneo.md#el-día-del-torneo).

---

## Un torneo en varios días

Las bases separan la clasificatoria (24 y 25 de noviembre) de la final (28). El
estado se guarda en `salida/estado.json` **después de cada ronda**; para seguir al
día siguiente alcanza con volver a levantar `campeonato servir` con los mismos
archivos.

Al retomar:

- Los jugadores conservan su **alias y sus fichas**, así que un bot que se
  reconecta es el mismo.
- La configuración que rige es la **congelada al jugar la primera ronda**, no la del
  archivo. Si alguien editó `campeonato.json` en el medio, se ignora (las fases ya
  jugadas dejarían de coincidir).
- La lista de inscritos no puede cambiar: sumar o sacar a alguien a mitad de
  campeonato cambiaría resultados ya jugados.

Entre la clasificatoria y la final los bots tienen que **volver a conectarse**: la
arena se apagó. Es lo normal en las "sesiones con horario fijo" de las bases.

---

## Decisiones que las bases dejan abiertas

Las bases son un borrador con valores entre corchetes. Esto es lo que se asumió; es
lo primero que hay que revisar antes de lanzar. Todo se cambia desde el panel o
`campeonato.json`.

| Pendiente en las bases | Se asumió | Dónde cambiarlo |
| ---------------------- | --------- | --------------- |
| `[200]` manos por ronda | 200 | `manos_por_ronda` |
| `[N]` rondas de clasificatoria | **4** | `rondas` |
| `[X]` manos por nivel de ciegas | 100 en la clasificatoria (= 2 niveles por ronda), 30 en la final | `ciegas.manos_por_nivel` |
| Ciegas iniciales y su escala | 50/100, 75/150, 100/200, 150/300, 200/400, 300/600, 400/800, 600/1200, 800/1600, 1200/2400 | `ciegas.niveles` |
| Dónde está el corte de "16 o 32 según la cantidad de participantes" | 32 con 64 o más inscritos, 16 con 32 a 63, y con menos, la mayor potencia de 2 que no pase de la mitad | `clasifican` (el automático es `ClasificanAuto`) |
| Con cuántas fichas entra cada fase final | **10 000 para todos** (reinicio). Las bases no dicen si conservan las de la clasificatoria. | `fases[i].stack` (0 = conservan) |
| Cuándo termina una mesa de la final | Cuando quedan los que pasan. Tope de seguridad: 600 a 800 manos | `max_manos` |
| Cómo se desempata el corte | Más fichas → menos timeouts → alias | (código: `cerrarClasificatoria`) |
| El alias, ¿cambia en cada ronda? | **No**: es estable en todo el campeonato | — |
| Premios, fechas, horarios | Fuera de alcance de la arena | — |

Una nota sobre las ciegas: con 10 000 fichas, ciegas que suben despacio y bots
prudentes, casi nadie quiebra en 4 rondas y el corte lo decide quién ganó fichas.
Con ciegas que suben rápido, la clasificatoria **elimina a casi todos** (en pruebas
con bots agresivos, de 20 bots quedó uno solo). `campeonato init -demo` usa una
escala suave por esa razón. Conviene probar la estructura con bots parecidos a los
que se esperan antes de fijarla.

---

## Seguridad

- **El panel controla el torneo entero.** Todo `/api` exige la clave de
  administración en el encabezado `X-Clave` (no vale por la URL: iría a los logs y al
  historial del navegador). La página en sí es estática y no lleva datos.
- **Por defecto el panel escucha solo en `127.0.0.1`.** Si lo abrís a la red,
  `servir` avisa: **no hay HTTPS**, así que la clave viaja en claro. Ponelo detrás
  de un proxy con TLS o una VPN.
- **Los tokens son credenciales.** `inscritos.json` va con permisos 0600 y fuera del
  repositorio. Quien tenga un token puede jugar como ese bot.
- **El lobby acepta a cualquiera que conozca un token y a nadie más.** No hay
  límite de intentos ni TLS en el puerto de los bots: para un torneo abierto a
  internet, poné un proxy TCP con TLS adelante.
- **La arena puede ejecutar código de terceros** si algún inscrito trae `comando`.
  Es para el demo y las pruebas; en el evento los bots corren en la máquina de cada
  equipo. Si lanzás bots ajenos, valen las advertencias de
  [`arena.md`](arena.md#seguridad).
- **El historial de manos tiene las cartas de todos.** Es para auditar. No lo
  expongas mientras se juega.

---

## Campeonato o arena

| | Arena (`arena`) | Campeonato (`campeonato`) |
| - | --------------- | ------------------------- |
| Para qué | Round-robin entre bots conocidos | Torneo por fases con eliminación |
| Quién decide los cruces | Un formato, **antes** de jugar | El resultado de la ronda anterior |
| Puertos | Uno por partida | **Uno solo** para todo el torneo |
| Bots | Los lanza la arena, o se conectan a cada mesa | Se conectan **una vez** y se mueven de mesa |
| Jugadores por mesa | 2 (o los que fije el formato) | 2 a 10, según la fase |
| Cuándo termina una partida | Por manos, o con reposición de stack | Por eliminación |
| Interfaz | Línea de comandos | Panel web + línea de comandos |
| Reproducible | Sí, con semilla | Sí, con semilla |

La arena sigue siendo la herramienta para que un equipo mida su bot contra los de
ejemplo en un round-robin. Comparten la Mesa, el Crupier y el historial de manos.

---

## API del panel

Todo exige `X-Clave: <clave>`. Los errores son `{"error": "..."}`.

| Método y ruta | Qué hace | Respuestas |
| ------------- | -------- | ---------- |
| `GET /api/estado` | Foto del campeonato: configuración, plan, jugadores, mesas, rondas | 200 |
| `POST /api/plan?jugadores=N` | Calcula el plan de la configuración del cuerpo, **sin guardarla** | 200, 400 |
| `PUT /api/config` | Guarda la configuración del cuerpo | 200, 400 (inválida), 409 (ya empezó) |
| `POST /api/ronda` | Juega una ronda en segundo plano | 202, 409 |
| `POST /api/auto` | Juega rondas hasta el final | 202, 409 |
| `POST /api/pausar` | Detiene el modo automático al terminar la ronda | 200 |
| `POST /api/cancelar` | Descarta la ronda en juego | 200 |

El cuerpo de `POST /api/plan` y `PUT /api/config` es la configuración JSON de
arriba; se rechazan campos desconocidos y cuerpos de más de 1 MB.

---

## Ver también

- [`torneo.md`](torneo.md) — la guía del participante, con el día del torneo.
- [`protocolo.md`](protocolo.md) — el contrato Mesa ↔ Bot (no cambió).
- [`interfaces.md`](interfaces.md#7-contratos-del-campeonato-internalcampeonato) — los contratos entre módulos.
- [`arena.md`](arena.md) — el round-robin.
