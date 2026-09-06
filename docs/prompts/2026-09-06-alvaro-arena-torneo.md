# Arena de torneos, validacion del motor y documentacion de APIs

## Metadatos

- **Autor:** Alvaro
- **Fecha:** 2026-09-06
- **Herramienta y modelo:** Claude Code (claude-opus-5)
- **Archivos afectados:**
  - `internal/arena/` (paquete nuevo completo: arena, formato, puntuacion,
    lanzador, clasificacion, historial, config + tests)
  - `cmd/arena/main.go` (nuevo)
  - `pkg/poker/` (nuevo: evaluador movido desde `internal/crupier/evaluador.go`,
    ahora publico, + tests)
  - `pkg/botsdk/botsdk.go` (reescrito: API de `Turno`, identidad, reintentos)
  - `internal/protocolo/mensajes.go` (version 1.0.0, `id_jugador`/`silla` en la
    bienvenida, `en_torneo`/`en_mano` en `JugadorPublico`)
  - `internal/mesa/mesa.go` (separacion `EnMano`/`Activo`, timeouts por jugador,
    orden de eliminacion, `ReponerStack`, `ReconectarJugador`,
    `SentarJugadorEnSilla`, `ObservadorMano`)
  - `internal/mesa/turnos.go`, `servidor.go`
  - `internal/crupier/baraja.go` (barajado sembrable), `crupier.go` (repartos
    deterministas), `crupier_test.go` (nuevo)
  - `bots/base/` (nuevo), `bots/aleatorio/`, `bots/conservador/`,
    `bots/ejemplos/python/`, `bots/ejemplos/javascript/` (nuevos)
  - `docs/protocolo.md`, `docs/torneo.md` (nuevo), `docs/arena.md` (nuevo),
    `docs/interfaces.md`, `docs/reglas.md`
  - `README.md`, `CONTRIBUTING.md` (nuevo), `Makefile`, `.gitignore`,
    `.gitattributes` (nuevo), `.github/workflows/ci.yml` (nuevo)
- **Commit relacionado:** _(completar al abrir el PR)_

## Objetivo

Terminar la arena para poder lanzar un torneo estilo MIT Pokerbots: validar el
motor existente, hacerlo extensible y documentar las APIs para que los
participantes puedan conectar sus bots.

## Prompt (literal)

```text
Termina la arena, se quiere hacer algo como MIT Pokerbots, y queremos lanzar un torneo, valida el código, hazlo extensible y documenta las apis y todo para que los usuarios puedan conectar sus bots.
```

Respuestas a las preguntas de aclaracion:

- Toolchain: "Lo que ocupe menos espacio en disco" (se instalo Go 1.27 con
  winget; la maquina no tenia toolchain).
- Formato del torneo: round-robin heads-up.

## Salida usada

Todo, con revision. Es una sesion larga: el paquete `arena` completo, el
evaluador movido a `pkg/poker`, el SDK reescrito, los arreglos del motor y toda
la documentacion.

## Bugs del motor que encontro la sesion

Se dejan anotados porque son el resultado mas valioso y porque conviene tenerlos
a mano si alguno reaparece:

1. **Las sillas las asignaba el orden de conexion.** `SentarJugador` toma "la
   primera silla libre", y con bots que arrancan en paralelo eso es una carrera.
   La ida y la vuelta de un emparejamiento terminaban con la misma disposicion:
   eran literalmente la misma partida jugada dos veces, y toda la rotacion de
   posiciones no compensaba nada. Se vio porque los resultados de ida y vuelta
   salian identicos. Arreglado con `SentarJugadorEnSilla` +
   `ConfigServidor.SillaDe`.
2. **Un bot que se conectaba a mitad de mano recibia turno** sin tener cartas ni
   fichas en el pozo, porque el recorrido de turnos usaba `Activo` (sigue en el
   torneo) en vez de una nocion de "esta en esta mano". Arreglado separando
   `EnMano`.
3. **Un bot no podia saber quien era.** La bienvenida no traia identidad, y con
   Casino el ID del jugador es el de cuenta, no el token. Los bots de ejemplo
   hacian `id = token`, que solo funciona en modo abierto. Arreglado con
   `id_jugador` en la bienvenida.
4. **`docs/protocolo.md` decia version `1.0.0` y el codigo `0.0.1`.** Un bot
   escrito siguiendo la documentacion no podia conectarse. Se unifico en `1.0.0`.
5. **`DecidirGanadores` construia los repartos iterando un map**, con orden
   distinto en cada corrida. El resultado va al historial y al mensaje
   `mano_fin`: dos replicas de la misma partida no eran comparables.
6. **La regla de la ficha sobrante no estaba implementada.** `reglas.md` decia
   que va al primero a la izquierda del boton; iba al primero que apareciera.
   Resuelto haciendo significativo el orden de `participantes`.
7. **Los timeouts nunca llegaban al ranking.** La conexion los contaba pero
   `ResumenJugador` no los exponia, asi que el Casino siempre recibia 0. Estaba
   anotado como pendiente conocido en el README.
8. **La clasificacion final ordenaba solo por saldo**, asi que todos los
   eliminados empataban en 0 y el orden lo decidia el azar del recorrido.
   Arreglado con el orden de eliminacion invertido.
9. **`ReconectarJugador` cerraba la conexion vieja con `Mu` tomada**, lo que
   podia frenar la mesa entera durante el plazo de pensar de un bot. (Bug
   introducido y corregido en la misma sesion.)
10. **`internal/crupier` no tenia ningun test** y `evaluador_test.go` estaba en
    `.gitignore`. Es la parte mas matematica del sistema.

## Decisiones de diseño que conviene revisar

- **`pkg/poker`**: el evaluador salio de `internal/` para que los bots de los
  participantes puedan usarlo. `internal/` no es importable desde fuera del
  modulo. Cambia la regla dura #2 del mapa de dependencias.
- **`ReponerStack`**: sin el, una partida de "200 manos" terminaba en 30 por
  eliminacion y el neto se saturaba en ±stack. Es lo que hace que el
  round-robin mida habilidad y no quien quebro primero.
- **Barajado sembrable** (`crupier.NuevoConSemilla`): necesario para que la ida
  y la vuelta usen las mismas cartas. Una mesa suelta sigue usando
  `crypto/rand`.
- **La API de `botsdk` es incompatible con la anterior** (`Decidir` ahora recibe
  un `Turno`). Se hizo ahora porque nadie escribio todavia un bot contra la
  version vieja y el protocolo cambio igual.

## Verificacion humana

- [ ] Lei linea por linea lo que se incorporo
- [x] Corri `make lint` y los tests (`go test ./...`; `-race` no corre en la
      maquina de trabajo por falta de compilador de C, queda cubierto en CI)
- [x] Se agregaron tests: `pkg/poker` (evaluador completo),
      `internal/crupier` (mazo, pozos laterales, determinismo),
      `internal/arena` (config, formatos, clasificacion),
      `internal/mesa` (timeouts, sillas, reconexion, ida y vuelta)
- [x] Confirme que respeta las interfaces de `docs/interfaces.md` (se actualizo
      el documento con los contratos nuevos y la regla #2 modificada)
- [x] Se corrio un torneo real de punta a punta con bots en Go, Python y
      JavaScript

## Cambios que le hice a la salida

_(completar en la revision)_

Nota de la verificacion automatica: el torneo multilenguaje dio una señal fuerte
de que el determinismo funciona. Los bots de Python y JavaScript implementan la
misma estrategia, y al enfrentarse dieron `+4740` y `-4740` exactamente
invertidos entre la ida y la vuelta: con las mismas cartas y la misma estrategia,
intercambiar las sillas espeja el resultado. Vale la pena repetir esa prueba si
alguna vez se sospecha del reparto.
