# Reglas implementadas (Texas Hold'em No Limit)

## Orden de una mano

1. Se mueve el boton una silla a la izquierda.
2. Se cobran ciega chica y ciega grande.
3. **Preflop**: 2 cartas privadas a cada jugador. Ronda de apuestas.
4. **Flop**: se quema 1 carta, se reparten 3 comunitarias. Ronda.
5. **Turn**: se quema 1, se reparte 1. Ronda.
6. **River**: se quema 1, se reparte 1. Ronda.
7. **Showdown**: gana la mejor mano de 5 entre las 7 disponibles.

## Posiciones

- 2 jugadores (heads-up): el boton es ciega chica y abre preflop.
- 3+: ciega chica a la izquierda del boton, ciega grande despues.
  Preflop abre el siguiente a la ciega grande. Postflop abre el primer
  activo a la izquierda del boton.

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

La escalera **A-2-3-4-5** es valida y el as cuenta como 1 (la mas baja).

## Reglas de apuesta

- Subida minima: igual a la ultima subida de la ronda (minimo la ciega grande).
- All-in por menos de una subida completa **no reabre** la accion para
  quienes ya actuaron.
- Un jugador all-in solo compite por el pozo que alcanzo a cubrir
  (pozos laterales).
- Empate: el pozo se divide; el resto indivisible va al primer jugador a la
  izquierda del boton.
