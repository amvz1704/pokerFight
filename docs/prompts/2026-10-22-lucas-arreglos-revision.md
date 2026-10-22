# Arreglos de la revision del motor: SDK, all-in corto, reconexion y cierre de bots

## Metadatos

- **Autor:** Lucas
- **Fecha:** 2026-10-22
- **Herramienta y modelo:** Claude Code (claude-opus-5-5)
- **Archivos afectados:**
  - `pkg/botsdk/tipos.go` (nuevo), `pkg/botsdk/externo_test.go` (nuevo)
  - `bots/base/`, `bots/aleatorio/`, `bots/conservador/` (usan solo `pkg/`)
  - `internal/mesa/mesa.go`, `internal/mesa/saldo.go`,
    `internal/mesa/mesa_apuestas_test.go` (nuevo)
  - `internal/mesa/servidor.go`, `internal/mesa/servidor_test.go`
  - `internal/arena/arena.go`, `internal/arena/enfrentamiento_test.go` (nuevo)
  - `README.md`, `docs/torneo.md`, `docs/protocolo.md`, `docs/reglas.md`,
    `docs/arena.md`, `docs/interfaces.md`
- **Commit relacionado:** _(completar al abrir el PR)_

## Objetivo

Corregir los cuatro problemas que encontro una revision del estado del
proyecto, cada uno con un test que falla sin el arreglo.

## Prompt (literal)

```text
Hola, puedes revisar el estado y los detalles de este proyecto por favor
```

```text
Sí, arregla los cuatro problemas en una rama nueva, con un test que falle sin cada arreglo.
```

## Salida usada

Todo, con revision. Los cuatro problemas y como se verifico cada uno:

1. **Un bot de Go escrito fuera del repo no compilaba.** `botsdk.Bot.Decidir`
   devuelve `protocolo.Accion`, y `internal/protocolo` no es importable desde
   otro modulo: los ejemplos del README y de `docs/torneo.md`, y los bots de
   `bots/` que se presentan como punto de partida, solo compilaban dentro de
   este repo. Arreglo: alias publicos en `pkg/botsdk/tipos.go`. Test:
   `TestUnBotDeOtroModuloCompila` compila esos ejemplos desde un modulo aparte.
2. **El all-in corto no dejaba pagar a quien ya habia actuado.** La Mesa solo
   volvia a pedir accion tras una subida completa, asi que ante un all-in corto
   el que ya habia hablado seguia en la mano sin igualar, y el excedente del
   all-in volvia como no igualado. Contradecia `docs/reglas.md`. Arreglo: tras
   cualquier aumento de la apuesta se vuelve a preguntar a los que quedaron
   debajo; si pueden volver a subir lo decide cuanto crecio la apuesta desde su
   ultima accion (regla de la TDA: varios all-in cortos que suman una subida
   completa reabren). De paso: ya no se le pide accion al unico jugador que
   puede hablar cuando no tiene nada que pagar. Tests en
   `mesa_apuestas_test.go`.
3. **La reconexion no funcionaba con la mesa llena**, que es siempre el caso en
   la arena: el servidor rechazaba antes del handshake si habia
   `MaxConexiones` registradas, sin saber que era el mismo jugador volviendo.
   Arreglo: el limite se controla despues del handshake y cuenta jugadores.
   Test: `TestServidorLlenoAceptaLaReconexionDeUnJugadorSentado`.
   **Consecuencia de seguridad que obligo a un cambio mas:** con la reconexion
   funcionando, en modo abierto un bot podia presentarse con el ID del rival
   (publico) y quitarle el asiento. La arena ahora da a cada bot que lanza un
   token secreto por enfrentamiento (`tokensDeSesion`). Test:
   `TestUnBotNoPuedeSuplantarAlRival`.
4. **Cada enfrentamiento perdia ~2 s por bot al cerrar y los bots terminaban
   matados.** Los defers detenian los bots antes de cerrar el servidor, que es
   lo que les avisa que la partida termino. Arreglo: cerrar el servidor al
   terminar `Jugar` y detener las sesiones al final, en paralelo. Test:
   `TestLosBotsTerminanSolosAlTerminarLaPartida`.

Cada test nuevo se corrio contra el commit anterior (`d82edc1`) y falla ahi.

## Decisiones de diseño que conviene revisar

- **No se subio `VersionProtocolo`.** El cambio de `acciones_validas` ante un
  all-in corto es la Mesa cumpliendo una regla que ya estaba documentada, y el
  formato de los mensajes no cambia. Ademas, el handshake exige version
  **exacta**, asi que subir aunque sea el PATCH rompe a todos los bots
  existentes: la tabla de CONTRIBUTING ("Textos -> PATCH") choca con esa regla
  y conviene que el equipo decida como versionar.
- **Bots remotos en modo abierto** siguen pudiendo ser suplantados (su token es
  conocido). Esta documentado en `docs/arena.md`: un torneo con bots remotos de
  terceros necesita Casino.
- **`cmd/mesa` en modo abierto** tampoco protege el token. Es una herramienta
  de desarrollo y esta anotado en `docs/interfaces.md` §2.1.

## Verificacion humana

- [ ] Lei linea por linea lo que se incorporo
- [x] Corri `gofmt`, `go vet` y `go test ./...` (`-race` no corre en la maquina
      de trabajo por falta de compilador de C, queda cubierto en CI)
- [x] Se agregaron tests que fallan sin cada arreglo
- [x] Confirme que respeta las interfaces de `docs/interfaces.md` (se actualizo
      el documento)
- [x] Se corrio un torneo de humo con bots en Go, Python y JavaScript

## Cambios que le hice a la salida

_(completar en la revision)_
