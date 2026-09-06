# Reglas implementadas (Texas Hold'em No Limit)

Estas son las reglas que aplica la Mesa. Si tu bot y este documento no
coinciden, el que manda es este documento — y si el codigo no lo cumple, es un
bug: abri un issue.

## Orden de una mano

1. Se mueve el boton una silla a la izquierda (al siguiente jugador con fichas).
2. Se cobran ciega chica y ciega grande.
3. **Preflop**: 2 cartas privadas a cada jugador. Ronda de apuestas.
4. **Flop**: se quema 1 carta, se reparten 3 comunitarias. Ronda.
5. **Turn**: se quema 1, se reparte 1. Ronda.
6. **River**: se quema 1, se reparte 1. Ronda.
7. **Showdown**: gana la mejor mano de 5 entre las 7 disponibles.

Si en cualquier punto queda un solo jugador sin retirarse, la mano termina ahi:
se lleva el pozo sin mostrar cartas y sin repartir las comunitarias que
faltaban.

## Posiciones

- **2 jugadores (heads-up):** el boton es ciega chica y abre preflop. Postflop
  abre el otro.
- **3 o mas:** ciega chica a la izquierda del boton, ciega grande despues.
  Preflop abre el siguiente a la ciega grande. Postflop abre el primer jugador
  en mano a la izquierda del boton.

La ciega grande tiene **opcion**: si nadie subio, cuando le llega el turno
preflop puede pasar o subir.

## Quien juega cada mano

Los jugadores de una mano se fijan **al empezarla**. Un bot que se conecta con la
mano en curso queda sentado pero fuera de esa mano: no recibe cartas, no se le
pide accion y no participa del pozo. Entra en la siguiente.

## Jerarquia de manos (de mayor a menor)

| # | Jugada | Desempate |
|---|--------|-----------|
| 1 | Escalera real | no hay, se parte |
| 2 | Escalera de color | carta mas alta |
| 3 | Poker | cuarteto, luego kicker |
| 4 | Full house | trio, luego par |
| 5 | Color | cartas de mayor a menor |
| 6 | Escalera | carta mas alta |
| 7 | Trio | trio, luego 2 kickers |
| 8 | Doble par | par alto, par bajo, kicker |
| 9 | Par | par, luego 3 kickers |
| 10 | Carta alta | 5 cartas de mayor a menor |

**Los palos no desempatan.** Dos manos con el mismo valor empatan y dividen el
pozo.

La escalera **A-2-3-4-5** ("la rueda") es valida y el as cuenta como 1: es la
escalera mas baja, y pierde contra 6-5-4-3-2.

El evaluador esta en [`pkg/poker`](../pkg/poker/evaluador.go) y es publico: tu
bot puede usar exactamente el mismo que la mesa.

## Reglas de apuesta

- **Sin limite (No Limit):** se puede apostar cualquier cantidad entre la subida
  minima y todo el stack.
- **`Accion.Monto` es el total** al que se lleva la apuesta en la ronda, no el
  incremento.
- **Subida minima:** igual a la ultima subida completa de la ronda, con un piso
  de la ciega grande. Formalmente, un `bet` o `raise` valido cumple:

  ```
  monto >= apuesta_actual + subida_minima
  monto <= saldo + apuesta_ronda           (el techo: el all-in)
  ```

- **All-in por menos de una subida completa** es valido pero **no reabre** la
  accion para quienes ya actuaron: pueden pagar la diferencia o retirarse, pero
  no volver a subir.
- Si la subida minima no entra en tu stack, la unica subida legal es el all-in.
  Hay que declararlo como `allin`, no como un `raise` corto.

## Pozos laterales

Un jugador all-in solo compite por el pozo que alcanzo a cubrir.

El pozo se descompone en niveles segun cuanto aporto cada uno. Ejemplo: A va
all-in por 50, B y C apuestan 200 cada uno.

| Sub-pozo | Monto | Quienes compiten |
|----------|-------|------------------|
| Principal | 150 (50 x 3) | A, B, C |
| Lateral | 300 (150 x 2) | B, C |

Las fichas de quien se retira **se quedan en el pozo**, pero deja de poder
ganarlo.

Si alguien apuesta mas de lo que cualquier rival podia cubrir, ese excedente
forma su propio sub-pozo y **vuelve a el**, gane o pierda la mano: nadie lo
igualo.

## Reparto de un empate

El pozo se divide en partes iguales entre los ganadores.

Si no divide exacto, la ficha (o las fichas) sobrantes van al **primer ganador a
la izquierda del boton**.

## Fin de la partida

Depende de la configuracion:

- **Por cantidad de manos**, si esta fijada.
- **Por eliminacion**, cuando queda un solo jugador con fichas.
- **Nunca por eliminacion** si esta activa la reposicion de stack: ahi todos
  vuelven al stack inicial en cada mano y solo termina por cantidad de manos.
  Es el modo que usa el round-robin del torneo (ver
  [`arena.md`](arena.md#los-tres-parámetros-que-definen-la-calidad-del-torneo)).

## Lo que la Mesa hace por vos

Si no respondes a tiempo, o respondes algo que no es valido, la Mesa juega la
**accion segura** en tu lugar: `check` si no hay apuesta pendiente, `fold` si la
hay. En los dos casos te anota un timeout, que resta puntos en el ranking.

## Lo que no esta implementado

- Otras variantes (Omaha, Stud, Short Deck).
- Estructuras de ciegas crecientes.
- Ante, straddle, run it twice.
- Rake.
