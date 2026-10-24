# Modo torneo por fases: el campeonato de las bases

## Metadatos

- **Autor:** Alvaro
- **Fecha:** 2026-10-23
- **Herramienta y modelo:** Claude Code (claude-sonnet-5-5)
- **Archivos afectados:**
  - `internal/campeonato/` (paquete nuevo completo: config, plan, sorteo,
    inscritos, estado, lobby, campeonato, vista, panel, `panel.html` + tests)
  - `cmd/campeonato/main.go` (nuevo)
  - `internal/mesa/mesa.go` (`ConfigPartida.Ciegas`, `StackPorJugador`,
    `HastaQueden`; desempate de quiebras simultaneas; clasificacion por fichas),
    `internal/mesa/servidor.go` (`ConfigServidor.AlConectar`)
  - `internal/crupier/pozo.go` (**arreglo de un bug de conservacion de fichas**)
  - `docs/campeonato.md` (nuevo), `docs/interfaces.md`, `docs/protocolo.md`,
    `docs/torneo.md`, `docs/reglas.md`, `docs/arena.md`, `README.md`,
    `CONTRIBUTING.md`, `Makefile`, `.gitignore`
- **Commit relacionado:** `a8864b8` (Mesa y Crupier), `a61ddce` (Campeonato) y el
  commit de documentación que los sigue, en la rama `campeonato/modo-torneo`

## Objetivo

Que se pueda correr el torneo de las bases del Poker Fight de Bots (clasificatoria
con mesas de 5 sorteadas de nuevo en cada ronda, final por eliminacion) y que la
cantidad de mesas y de jugadores se defina desde una interfaz. De paso, resolver el
issue #9 ("1 mesa a la vez"): hasta ahora cada mesa necesitaba su propio puerto.

## Prompt (literal)

```text
Revisa los cambios, y el issue generado. Se quiere hacer un torneo basado en estas reglas, así que falta un modo torneo donde se pueda definir por interfaz la cantidad de mesas y número de jugadores.
```

Con el PDF `Bases_Poker_Fight_de_Bots.docx.pdf` adjunto.

## Interpretaciones que conviene revisar

- **"Por interfaz"** se leyo como una interfaz de usuario: un **panel web** donde se
  define cuantas fases, mesas y jugadores por mesa, con vista previa del plan. Si se
  referia a una interfaz de Go (como `Formato`), el motor sirve igual (`Config` /
  `Plan` son la API) y el panel sobra.
- **Los valores entre corchetes de las bases** ([200] manos, [N] rondas, [X] ciegas)
  tienen un valor provisorio: lista completa en `docs/campeonato.md`, seccion
  "Decisiones que las bases dejan abiertas". Los mas importantes: 4 rondas de
  clasificatoria, el corte automatico de 16/32 clasificados, y que las fases finales
  **reinician el stack a 10 000**.

## Bugs que encontro la sesion

1. **Se perdian fichas en el pozo** (`internal/crupier/pozo.go`, ya estaba en
   `main`). Si alguien se retiraba despues de aportar mas que cualquiera de los que
   seguian en la mano (la ciega grande de 320 contra un all-in corto de 261),
   `Descomponer` armaba un sub-pozo sin elegibles y `DecidirGanadores` lo salteaba:
   las fichas desaparecian. Lo vio un test de torneo con 12 000 fichas que terminaban
   en 11 890; lo aislo un test de estres de 3000 semillas (la 1494 lo reproduce). Un
   estres con jugadores al azar **no** lo veia: hacen falta ciegas altas y all-ins
   cortos a la vez. Es el que habria falseado cualquier torneo con all-ins.
2. **El sorteo no era reproducible con semilla**: los alias eran aleatorios y el
   sorteo baraja a los jugadores en orden de alias, asi que la misma semilla armaba
   mesas distintas. Ahora los alias tambien salen de la semilla.
3. **Un jugador eliminado aparecia con "all-in"** en el panel (bandera de la mano
   anterior), y los numeros mezclaban `8300` con `14.000`.

## Problema previo que NO se arreglo (queda para otra tarea)

**Una accion tardia dentro de la misma mano se aplica a la jugada siguiente.**
`ConexionTCP` descarta las acciones con `id_mano` distinto, pero el `id_mano` no
cambia entre calles. Verificado con un test descartable: un `allin` que llega tarde
se tomo como respuesta a la solicitud siguiente. Con el plazo de 10 s de las bases y
bots con LLM va a pasar. El arreglo es un `id_solicitud` que el bot devuelva.

## Salida usada

Todo, con revision.

## Verificacion humana

- [ ] Lei linea por linea lo que se incorporo
- [x] Corri `gofmt`, `go vet` y `go test ./...` (`-race` no corre en la maquina de
      trabajo por falta de compilador de C, queda cubierto en CI)
- [x] Cada comportamiento nuevo de la Mesa tiene un test que **falla** si se rompe
      (se verifico a mano rompiendolos: quiebras simultaneas, orden por fichas,
      `HastaQueden`, `StackPorJugador`, la fusion del pozo, la regla de lineas
      ilegibles del lobby)
- [x] Se corrio un campeonato real, con binarios reales, 20 y 24 bots conectados por
      TCP a un unico puerto, manejado desde el panel en el navegador
- [x] Confirme que respeta las interfaces de `docs/interfaces.md` (se actualizo: §4.6,
      §4.7, §4.8 y §7)

## Cambios que le hice a la salida

_(completar en la revision)_
