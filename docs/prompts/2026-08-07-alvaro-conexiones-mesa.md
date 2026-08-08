# Conexiones de la Mesa (tarea Mesa #3)

## Metadatos

- **Autor:** Alvaro
- **Fecha:** 2026-08-07
- **Herramienta y modelo:** Claude Code (claude-opus-5)
- **Archivos afectados:**
  - `internal/mesa/servidor.go` (nuevo contenido: `ConexionTCP`, `Servidor`)
  - `internal/mesa/conexion_prueba.go` (nuevo)
  - `internal/mesa/mesa.go` (solo `Mesa.Mu` y los locks)
  - `internal/mesa/servidor_test.go` (nuevo)
  - `internal/mesa/mesa_test.go` (nuevo)
  - `docs/interfaces.md` (secciones 4.1 y 4.2, tabla de tareas)
  - `README.md` (estado de tareas y especificaciones)
- **Commit relacionado:** _(completar al abrir el PR)_

## Objetivo

Implementar la tarea Mesa #3: las estructuras de conexion entre la Mesa y los
bots, con dos implementaciones de `ConexionMesaJugador` — una real sobre TCP y
otra en memoria para tests — mas el servidor que acepta conexiones y hace el
handshake del protocolo.

## Prompt (literal)

```text
Hacer pull de los cambios, e implementar el servidor.go  tipo las estructuras de las conexiones y funciones para conectar.  una para test (sin conexión) y otra real en red
```

Seguimiento:

```text
marcar las actividades realizadas
```

```text
cual más falta acerca de mesa
```

```text
hacer la 1 y marcar las actividades realizadas
```

("la 1" = `SentarJugador` / `LevantarJugador`, que los `panic` de `mesa.go`
atribuian a esta misma tarea Mesa #3 y no a Mesa #1.)

## Salida usada

Todo, con ajustes. Lo incorporado:

- `ConexionTCP` sobre `net.Conn` + `protocolo.Codec` (JSON Lines), con plazos
  reales via `SetReadDeadline` / `SetWriteDeadline`, descarte de acciones
  tardias por `id_mano` y contador de timeouts.
- `Servidor` con `Abrir` / `Servir` / `Escuchar` / `Cerrar`, handshake
  (`saludo` -> version -> `ValidarToken` -> `bienvenida`) y registro de
  conexiones por id de jugador.
- `ConexionPrueba`: sin red, con cola de acciones o funcion `Responder`,
  historial de mensajes recibidos y simulacion de bot lento / caido.
- `ParConexionesMemoria` (`net.Pipe`) para probar el protocolo completo sin
  abrir puertos.
- `Mesa.Mu` para sincronizar `Jugadores` entre las goroutines del Servidor y
  `Jugar`.
- 18 tests entre `servidor_test.go` y `mesa_test.go`, incluido uno de
  concurrencia (16 goroutines sentandose en 8 sillas) que solo tiene sentido
  con `-race`.

## Verificacion humana

- [ ] Lei linea por linea lo que se incorporo
- [x] Corri `go vet ./...` y `go test -race ./...` (ambos pasan). Ojo:
      `make lint` corre `go fmt ./...` y reformatearia
      `internal/crupier/pozo.go`, que ya venia sin formatear de otro modulo.
- [x] Agregue tests que cubren esto (ida y vuelta, accion tardia, timeout,
      abandono, cierre idempotente, handshake, version invalida, token
      invalido, panic de la Mesa, sillas en orden, mesa llena, duplicados,
      levantar, concurrencia, y el circuito completo socket -> handshake ->
      Mesa real)
- [x] Confirme que respeta las interfaces de `docs/interfaces.md`: la firma de
      `ConexionMesaJugador` no cambio y `ValidarToken` entra por inyeccion, sin
      importar el paquete `casino`

## Cambios que le hice a la salida

- Se separo `Abrir()` de `Servir(ctx)` para poder conocer el puerto real
  cuando se configura `:0` y para evitar carreras en los tests.
- `Abrir()` sostiene el mutex durante el `net.Listen`: dos llamadas
  concurrentes reservaban dos puertos distintos.
- Se elimino un error exportado que quedaba sin uso (`ErrMensajeInesperado`).
- Se documento la API de conexiones en `docs/interfaces.md` §4.1 y §4.2, porque
  la regla del equipo es que ese documento manda sobre el codigo.
- Se descarto la version propia de `SentarJugador` / `LevantarJugador`: la
  rama `mesa` ya traia la de Lucas, con otro modelo de datos (sillas fijas) del
  que depende su `Jugar`. Solo se le agrego `Mesa.Mu`.

## Pendientes que deja abiertos

- `Mesa.Jugar` esta a medias y `Mesa.Estado` sigue en `panic` (Mesa #1), igual
  que la contabilidad de fichas (Mesa #2). Todo acceso a `Jugadores` **debe
  tomar `Mesa.Mu`**.
- Tres divergencias entre el codigo de sillas y el contrato documentado, en
  `docs/interfaces.md` §4.2 (reconexion, espectador, `ActualJugadores`).
- `Jugador.Saldo` y `ApuestaRonda` son `uint64` mientras `protocolo` y
  `crupier.Pozo` usan `int64`: habra casts en cada construccion de estado.

- `cmd/mesa/main.go` sigue vacio (declara `package mesa`, no `main`, asi que
  `make build` falla ahi). No se escribio porque `Mesa.SentarJugador` y
  `Mesa.Jugar` todavia hacen `panic` (tareas Mesa #1 y #2).
- `docs/protocolo.md` muestra ejemplos con `"version":"1.0.0"` mientras el
  codigo usa `protocolo.VersionProtocolo = "0.0.1"`. Hay que alinear el doc.
