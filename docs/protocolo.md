# Protocolo Mesa ↔ Bot — v1.0.0

Referencia normativa del contrato entre la Mesa y un bot. Si el código y este
documento difieren, es un bug: abrí un issue.

Esto es todo lo que hace falta para escribir un bot en **cualquier lenguaje**.
No hace falta leer el código de la Mesa.

- [Transporte](#transporte)
- [Versionado](#versionado)
- [Ciclo de vida de una conexión](#ciclo-de-vida-de-una-conexión)
- [Mensajes Mesa → Bot](#mensajes-mesa--bot)
- [Mensajes Bot → Mesa](#mensajes-bot--mesa)
- [Tipos comunes](#tipos-comunes)
- [Acciones](#acciones)
- [Garantías de la Mesa](#garantías-de-la-mesa)
- [Errores frecuentes](#errores-frecuentes)
- [Reconexión](#reconexión)
- [Probar a mano](#probar-a-mano)

---

## Transporte

TCP, **un objeto JSON por línea, terminado en `\n`** (JSON Lines), en las dos
direcciones. Sin longitud previa, sin framing binario, sin compresión.

Se eligió así para que el protocolo sea depurable con `nc` y para que
implementarlo en un lenguaje nuevo no requiera ninguna librería.

Consecuencia práctica: **un `read` del socket puede devolver media línea o dos
y media.** Hay que acumular en un buffer y cortar por `\n`. Los ejemplos en
[`bots/ejemplos/`](../bots/ejemplos/) lo hacen bien; copiá de ahí.

La codificación es UTF-8. Los campos no listados acá pueden aparecer en
versiones futuras (ver [Versionado](#versionado)): **ignorá los que no
conozcas**, no falles.

---

## Versionado

`version` es semver sobre el **contrato**, no sobre el código del proyecto.

| Cambio | Sube | Qué le pasa a tu bot |
| ------ | ---- | -------------------- |
| Correcciones de texto o documentación | PATCH | Nada. |
| Campos nuevos opcionales | MINOR | Nada, si ignorás los campos desconocidos. |
| Cualquier cosa que rompa un bot existente | MAYOR | Hay que actualizarlo. |

El bot **debe** mandar la versión exacta en el saludo. Si no coincide con la de
la Mesa, la conexión se rechaza con un mensaje `error`. Es deliberado: preferimos
que un bot desactualizado falle al conectar y no a mitad del torneo.

La versión actual es **`1.0.0`**.

---

## Ciclo de vida de una conexión

```
bot                                              mesa
 |                                                 |
 |------------------ saludo ---------------------->|   handshake
 |<---------------- bienvenida --------------------|   (trae tu id y tu silla)
 |                                                 |
 |<---------------- mano_inicio -------------------|   tus 2 cartas privadas
 |<-------------- solicitar_accion ----------------|   es tu turno
 |------------------ accion ---------------------->|
 |<------------------ estado ----------------------|   actuó un rival
 |<-------------- solicitar_accion ----------------|   tu turno otra vez
 |------------------ accion ---------------------->|
 |<----------------- mano_fin ---------------------|   repartos y showdown
 |                                                 |
 |             (se repite por cada mano)           |
 |                                                 |
 |<--------------- cierre del socket --------------|   la partida terminó
```

`mano_inicio` → (`solicitar_accion` → `accion`)* → `mano_fin`, con mensajes
`estado` intercalados en cualquier momento.

**Solo `solicitar_accion` espera respuesta.** Todo lo demás es informativo: si
respondés a un `estado`, la Mesa lo descarta.

El cierre del socket por parte de la Mesa es el fin normal de la partida, no un
error.

---

## Mensajes Mesa → Bot

Todos traen `tipo` y `version`. Los demás campos dependen del `tipo`.

### `bienvenida`

Cierra el handshake. **Leelo: trae tu identidad.**

```json
{"tipo":"bienvenida","version":"1.0.0","id_jugador":"c-42","silla":0,"mensaje":"bienvenido BotAlpha"}
```

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `id_jugador` | string | **Tu identificador en esta mesa.** Es la clave para encontrarte en `estado.jugadores`. |
| `silla` | int | Tu posición, `0..MaxJugadores-1`. `-1` si la Mesa no la informa. |
| `mensaje` | string | Texto libre, informativo. |

> **`id_jugador` no es tu token.** Cuando la mesa corre contra un Casino, tu
> identificador es el ID de cuenta que el Casino devolvió al validar tu token, y
> es lo que aparece en `estado.jugadores[].id`. Si asumís que el token es tu ID,
> tu bot no va a poder encontrarse a sí mismo en el estado y va a decidir con el
> saldo de otro. Es el error más caro que se puede cometer con esta API.

### `mano_inicio`

Empieza una mano. Trae tus dos cartas privadas.

```json
{"tipo":"mano_inicio","version":"1.0.0","cartas":[{"rango":14,"palo":3},{"rango":13,"palo":3}],"estado":{"...":"..."}}
```

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `cartas` | array de 2 [Carta](#carta) | **Solo las tuyas.** Nunca llegan las de un rival. |
| `estado` | [EstadoPublico](#estadopublico) | Foto de la mesa al empezar la mano. |

### `solicitar_accion`

Es tu turno. **Este mensaje sí espera respuesta**, dentro de `timeout_ms`.

```json
{"tipo":"solicitar_accion","version":"1.0.0","acciones_validas":["fold","call","raise","allin"],"timeout_ms":2000,"estado":{"...":"..."}}
```

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `acciones_validas` | array de string | Las **únicas** acciones que la Mesa va a aceptar ahora. |
| `timeout_ms` | int | Plazo para responder, en milisegundos. |
| `estado` | [EstadoPublico](#estadopublico) | Foto de la mesa. |

### `estado`

Actualización informativa tras la acción de un rival o al abrir una calle
nueva. No hay que responder.

```json
{"tipo":"estado","version":"1.0.0","estado":{"...":"..."}}
```

### `mano_fin`

Cierre de la mano.

```json
{"tipo":"mano_fin","version":"1.0.0","resultado":{
  "id_mano":"mesa-1-mano-7",
  "comunitarias":[{"rango":2,"palo":0},{"rango":7,"palo":1},{"rango":14,"palo":2},{"rango":9,"palo":3},{"rango":4,"palo":0}],
  "repartos":[{"id_jugador":"c-42","monto":180}],
  "mostradas":{"c-42":[{"rango":14,"palo":3},{"rango":13,"palo":3}]},
  "descripcion":{"c-42":"par de ases"}
}}
```

| Campo de `resultado` | Tipo | Significado |
| -------------------- | ---- | ----------- |
| `id_mano` | string | La mano que cerró. |
| `comunitarias` | array de [Carta](#carta) | Las cartas de la mesa. Puede estar vacío si la mano terminó preflop. |
| `repartos` | array de `{id_jugador, monto}` | Cuánto se llevó cada uno. Los `monto` suman el pozo entero. |
| `mostradas` | objeto `id → [Carta,Carta]` | Cartas reveladas en el showdown. **Ausente si la mano terminó sin showdown.** |
| `descripcion` | objeto `id → string` | Nombre de la jugada de cada uno: `"color"`, `"par de ases"`... |

`mostradas` es la única fuente legítima de información sobre las cartas de un
rival, y solo aparece cuando hubo showdown. Es el mejor lugar para que tu bot
aprenda a leer al rival.

### `error`

Algo salió mal. Después de un `error` la Mesa normalmente cierra la conexión.

```json
{"tipo":"error","version":"1.0.0","mensaje":"mesa: versión de protocolo incompatible"}
```

---

## Mensajes Bot → Mesa

### `saludo`

Primer mensaje. Obligatorio.

```json
{"tipo":"saludo","version":"1.0.0","token":"tok-a1b2c3..."}
```

| Campo | Tipo | Obligatorio | Significado |
| ----- | ---- | ----------- | ----------- |
| `tipo` | string | sí | `"saludo"` |
| `version` | string | sí | Tiene que ser exactamente la versión de la Mesa. |
| `token` | string | sí | El de `casino login`. En modo abierto, cualquier string: se usa como tu identificador. |

### `accion`

Respuesta a `solicitar_accion`.

```json
{"tipo":"accion","version":"1.0.0","id_mano":"mesa-1-mano-7","accion":{"tipo":"raise","monto":60}}
```

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `id_mano` | string | Eco de `estado.id_mano`. La Mesa descarta acciones cuyo `id_mano` no coincida con la mano en curso: es lo que evita que una respuesta que llegó tarde se aplique a la mano siguiente. Mandalo siempre. |
| `accion` | [Accion](#acciones) | La jugada. |

### `abandono`

Te retirás de la mesa. La Mesa lo trata como `fold` y te levanta.

```json
{"tipo":"abandono"}
```

---

## Tipos comunes

### Carta

```json
{"rango": 14, "palo": 3}
```

| Campo | Valores |
| ----- | ------- |
| `rango` | `2`..`14`. `11`=J, `12`=Q, `13`=K, `14`=A. |
| `palo` | `0`=tréboles (c), `1`=diamantes (d), `2`=corazones (h), `3`=picas (s). |

Notación legible que se usa en logs y documentación: `As` (as de picas), `Th`
(diez de corazones), `2c` (dos de tréboles).

El as vale 14 **salvo** en la escalera A-2-3-4-5, donde cuenta como 1 (ver
[`reglas.md`](reglas.md)).

### EstadoPublico

La foto de la mesa. Nunca contiene cartas privadas ajenas.

```json
{
  "id_mano": "mesa-1-mano-7",
  "etapa": "flop",
  "comunitarias": [{"rango":2,"palo":0},{"rango":7,"palo":1},{"rango":14,"palo":2}],
  "pozo": 120,
  "apuesta_actual": 40,
  "subida_minima": 20,
  "ciega_chica": 10,
  "ciega_grande": 20,
  "posicion_boton": 0,
  "jugadores": [
    {"id":"c-42","nombre":"BotAlpha","saldo":960,"apuesta_ronda":40,"activo":true,"en_torneo":true,"allin":false,"en_mano":true,"posicion_silla":0}
  ],
  "historial_acciones": [
    {"id_jugador":"c-42","etapa":"preflop","accion":{"tipo":"raise","monto":40}}
  ]
}
```

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `id_mano` | string | Identificador de la mano. Se devuelve en `accion.id_mano`. |
| `etapa` | string | `"preflop"`, `"flop"`, `"turn"`, `"river"`, `"showdown"`. |
| `comunitarias` | array de [Carta](#carta) | 0, 3, 4 o 5 cartas según la etapa. |
| `pozo` | int | Total acumulado en la mano, incluidos los pozos laterales. |
| `apuesta_actual` | int | La apuesta más alta de **esta ronda**. |
| `subida_minima` | int | Incremento mínimo para subir. Ver [Acciones](#acciones). |
| `ciega_chica`, `ciega_grande` | int | Las ciegas de la partida. |
| `posicion_boton` | int | Silla del botón (dealer). |
| `jugadores` | array de [JugadorPublico](#jugadorpublico) | Todos los sentados, incluido vos. |
| `historial_acciones` | array | Las acciones de **esta mano**, en orden. Se vacía en cada mano nueva. |

### JugadorPublico

| Campo | Tipo | Significado |
| ----- | ---- | ----------- |
| `id` | string | Comparalo con tu `id_jugador` de la bienvenida para encontrarte. |
| `nombre` | string | Nombre a mostrar. |
| `saldo` | int | Fichas que le quedan (sin contar lo ya apostado en la ronda). |
| `apuesta_ronda` | int | Lo que puso **en la ronda de apuestas actual**. |
| `activo` | bool | Sigue **en la mano** (no hizo fold). |
| `en_torneo` | bool | Sigue **en la partida** (le quedan fichas). |
| `en_mano` | bool | Recibió cartas en la mano en curso. |
| `allin` | bool | Ya no puede apostar más en esta mano. |
| `posicion_silla` | int | Su silla, `0..MaxJugadores-1`, en sentido horario. |

`activo`, `en_torneo` y `en_mano` son tres cosas distintas y hacen falta las
tres. Un jugador que hizo fold tiene `activo:false` pero `en_torneo:true`. Uno
que se conectó a mitad de mano tiene `en_torneo:true` pero `en_mano:false`.

---

## Acciones

```json
{"tipo": "raise", "monto": 60}
```

| `tipo` | `monto` | Cuándo es válida |
| ------ | ------- | ---------------- |
| `fold` | ignorado | Siempre. |
| `check` | ignorado | Solo si no hay apuesta pendiente (`apuesta_actual <= tu apuesta_ronda`). |
| `call` | ignorado | Solo si hay apuesta pendiente. Iguala automáticamente. |
| `bet` | **requerido** | Solo si no hay apuesta pendiente. |
| `raise` | **requerido** | Solo si hay apuesta pendiente y te alcanza para superarla. |
| `allin` | ignorado | Si te queda saldo. |

`acciones_validas` te dice cuáles son legales **ahora**. Cualquier otra se
descarta.

### `monto` es el TOTAL, no el incremento

Este es el punto donde más bots se rompen.

`monto` es el total al que llevás tu apuesta **en la ronda actual**, no cuántas
fichas agregás.

> Ya pusiste 20 en esta ronda. `apuesta_actual` es 40 y querés subir a 100.
>
> - Correcto: `{"tipo":"raise","monto":100}` — pagás 80 más.
> - Incorrecto: `{"tipo":"raise","monto":80}` — la Mesa entiende que querés
>   llevar tu apuesta a 80, no a 100.

Se eligió el total porque no admite ambigüedad: el incremento se puede
interpretar respecto de tu apuesta anterior o de la del rival, y las dos
lecturas producen bots que "funcionan" hasta que pierden una mano grande.

### Cuánto vale una subida válida

Para que un `bet` o un `raise` sea aceptado:

```
monto >= apuesta_actual + subida_minima      (subida mínima)
monto <= saldo + apuesta_ronda               (tu techo: el all-in)
```

`subida_minima` arranca en la ciega grande y pasa a ser el tamaño de la última
subida completa de la ronda.

**Excepción:** si `apuesta_actual + subida_minima` es mayor que tu techo, la
única subida legal es el all-in. Declaralo como `{"tipo":"allin"}`, no como un
`raise` corto: así la Mesa le aplica la regla de all-in corto (no reabre la
acción para quienes ya actuaron) en vez de rechazarlo por subida insuficiente.

---

## Garantías de la Mesa

Lo que podés dar por sentado:

1. **Nunca vas a recibir cartas privadas ajenas.** El campo `cartas` solo
   aparece en `mano_inicio` y solo trae las tuyas. Las de un rival solo llegan en
   `mano_fin.mostradas`, y solo si hubo showdown.
2. **La Mesa no confía en vos.** Cada acción se valida contra
   `acciones_validas`, contra tu saldo y contra la subida mínima. No hay forma de
   apostar fichas que no tenés.
3. **Nunca te vas a colgar la partida.** Si no respondés a tiempo, la Mesa juega
   por vos la **acción segura**: `check` si es gratis, `fold` si hay que pagar.
4. **Las acciones tardías se descartan.** Una respuesta con un `id_mano` que ya
   no corre se ignora en silencio.
5. **Las fichas se conservan.** La suma de fichas de todos los jugadores no
   cambia por una mano, más allá de cómo se reparta el pozo.

### El costo de un timeout

Cada vez que la Mesa aplica la acción segura en tu lugar te anota un timeout, y
eso **resta puntos en el ranking** ([`arena.md`](arena.md#puntuación)).

**Responder algo inválido cuesta exactamente lo mismo que no responder**: la
Mesa hace lo mismo en los dos casos. No hay ninguna ventaja en mandar una acción
dudosa "a ver si pasa".

---

## Errores frecuentes

| Síntoma | Causa casi segura |
| ------- | ----------------- |
| La mesa rechaza la conexión al arrancar | `version` no coincide exacto. Mandá `"1.0.0"`. |
| Tu bot decide con el saldo de otro | Estás buscándote en `estado.jugadores` por el token en vez de por `id_jugador` de la bienvenida. |
| Tus subidas se descartan siempre | Estás mandando el incremento en `monto` en vez del total. |
| Tus subidas se descartan a veces | No llegás a `apuesta_actual + subida_minima`. Con stack corto, usá `allin`. |
| Acumulás timeouts sin ser lento | Estás respondiendo a mensajes `estado` o `mano_fin`, o mandando el `id_mano` equivocado. |
| Recibís JSON cortado a la mitad | No estás acumulando en un buffer hasta el `\n`. |
| Tu bot se cuelga al terminar la partida | La mesa cerró el socket. Un `read` que devuelve EOF es el fin normal. |

---

## Reconexión

Si tu bot pierde el socket a mitad de partida, puede volver a conectarse con el
**mismo token**: la Mesa lo reconoce y le devuelve su silla y su saldo. La
conexión vieja se cierra.

Mientras estuviste desconectado, tus turnos se resolvieron con la acción segura
y cada uno sumó un timeout. Reconectar no los borra.

Dos bots distintos **no pueden compartir token**: el segundo en conectarse se
toma como una reconexión del primero y el primero queda fuera.

---

## Probar a mano

La mesa se puede hablar con `nc`, que es media razón por la que el protocolo es
JSON Lines:

```bash
./bin/mesa -addr :9000 -jugadores 2 -min-jugadores 2
```

En otra terminal:

```bash
nc localhost 9000
```

Y pegá, línea por línea:

```json
{"tipo":"saludo","version":"1.0.0","token":"demo"}
```

La mesa responde con la bienvenida. Con un segundo `nc` conectado y saludado, la
partida arranca y vas a ver llegar `mano_inicio` y `solicitar_accion`. Respondé:

```json
{"tipo":"accion","id_mano":"PEGA-ACA-EL-ID","accion":{"tipo":"call"}}
```

---

## Ver también

- [`bots/ejemplos/python/bot.py`](../bots/ejemplos/python/bot.py) — implementación completa y comentada, sin dependencias.
- [`bots/ejemplos/javascript/bot.js`](../bots/ejemplos/javascript/bot.js) — lo mismo, con E/S asíncrona.
- [`pkg/botsdk`](../pkg/botsdk/botsdk.go) — si escribís en Go, no implementes nada de esto: usá el SDK.
- [`docs/torneo.md`](torneo.md) — cómo inscribir tu bot en el torneo.
- [`docs/reglas.md`](reglas.md) — las reglas de poker que aplica la Mesa.
