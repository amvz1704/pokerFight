# Interfaces (documento primario)

> Regla del proyecto: **la documentacion de interfaces es mandatoria**. Este
> archivo es la fuente de verdad. Si el codigo y este documento difieren, el
> PR se rechaza hasta que uno de los dos se corrija.

## 1. Mapa de dependencias

```
bots (externos)  ->  pkg/botsdk  ->  internal/protocolo  <-  internal/mesa
                                             ^                    |
                                             |                    v
                                     internal/casino      internal/crupier
                                             |
                                             v
                                     internal/almacen
```

Reglas duras:

1. `internal/protocolo` **no importa a nadie** del proyecto.
2. `internal/crupier` **solo** importa `protocolo`. No sabe que existe la red.
3. `internal/mesa` importa `crupier` y `protocolo`. No importa `casino`
   (recibe la funcion `ValidarToken` por inyeccion).
4. `internal/casino` importa `almacen` y `protocolo`. No importa `mesa`.

## 2. Contrato Mesa <-> Bot

Transporte: **TCP + JSON Lines** (un objeto JSON por linea, terminado en `\n`).
Se eligio asi para que un bot pueda escribirse en cualquier lenguaje y para
poder depurar con `nc localhost 9000`.

### Handshake

```
bot  -> mesa : {"tipo":"saludo","version":"1.0.0","token":"..."}
mesa -> bot  : {"tipo":"bienvenida","version":"1.0.0","mensaje":"silla 3"}
```

Si la version no coincide, la Mesa responde `{"tipo":"error"}` y cierra.

### Ciclo de una mano

| # | Direccion | Tipo | Contenido |
|---|-----------|------|-----------|
| 1 | mesa -> bot | `mano_inicio` | `cartas` (2 privadas) + `estado` |
| 2 | mesa -> bot | `solicitar_accion` | `estado`, `acciones_validas`, `timeout_ms` |
| 3 | bot -> mesa | `accion` | `id_mano` + `accion` |
| 4 | mesa -> bot | `estado` | actualizacion tras cada accion ajena |
| 5 | mesa -> bot | `mano_fin` | `resultado` con repartos y cartas mostradas |

Los pasos 2-4 se repiten por cada ronda de apuestas (preflop, flop, turn, river).

### Reglas del contrato

- La Mesa **nunca** envia cartas privadas ajenas. El campo `cartas` solo
  aparece en `mano_inicio` y solo trae las del destinatario.
- La Mesa **nunca** confia en el bot: valida `accion` contra
  `acciones_validas` y contra el saldo. Accion invalida = se aplica
  `protocolo.AccionSegura`.
- Si el bot no responde antes de `timeout_ms`, la Mesa aplica
  `AccionSegura`: **check** si no hay apuesta pendiente, **fold** si la hay.
  Cada aplicacion suma 1 a `Estadisticas.Timeouts`.
- `Accion.Monto` es el **total** al que el jugador lleva su apuesta en la
  ronda, no el incremento. Evita ambiguedades en `raise`.
- `id_mano` viaja en la respuesta del bot para descartar acciones tardias.

## 3. Contrato Mesa -> Crupier

Definido en `internal/crupier/crupier.go`:

```go
type Crupier interface {
    NuevaMano(idMano string) error
    RepartirPrivadas(cantidadJugadores int) ([]protocolo.Mano, error)
    RepartirComunitarias(etapa protocolo.Etapa) ([]protocolo.Carta, error)
    Evaluar(privadas protocolo.Mano, comunitarias []protocolo.Carta) (Evaluacion, error)
    DecidirGanadores(participantes []Participante, comunitarias []protocolo.Carta, pozo *Pozo) (protocolo.ResultadoMano, error)
}
```

- `RepartirPrivadas` devuelve las manos **en orden de silla** (indice 0 = silla 0).
- `RepartirComunitarias` quema una carta antes del flop, del turn y del river.
- `Evaluacion.Puntaje` es un entero monotono: **comparar manos es comparar
  enteros**. Igual puntaje = empate = pozo dividido.
- `DecidirGanadores` resuelve tambien los pozos laterales via `Pozo.Descomponer`.

## 4. Contrato Casino -> Mesa

La Mesa no importa el paquete `casino`. Recibe una funcion:

```go
ValidarToken func(token string) (idCuenta, nombre string, err error)
```

`(*casino.motor).ValidarToken` ya tiene exactamente esa forma (metodo de
`Casino`, la interfaz publica que devuelve `casino.Nuevo`), asi que se
inyecta directo en `mesa.NuevoServidor` sin envoltorios — ver el ejemplo en
§4.1.

Al terminar la partida, la Mesa devuelve un `mesa.ResumenPartida` y quien la
orquesta llama a `casino.RegistrarResultado`. `cmd/mesa` (flag
`-casino-db`) es ese orquestador: es el unico lugar que importa `mesa` y
`casino` a la vez, y convierte `mesa.ResumenJugador` a
`casino.ResultadoJugador` (`IDJugador` pasa a `IDCuenta` porque, con
`-casino-db`, `ValidarToken` ya devolvio el ID de cuenta como identificador
del jugador). Sin `-casino-db`, la mesa corre en modo abierto (el token del
saludo es directamente el ID del jugador) y no reporta a nadie.

### 4.1 Interfaz Casino (`internal/casino`)

```go
type Casino interface {
    Registrar(usuario string) (Cuenta, error)
    Login(usuario string) (token string, err error)
    ValidarToken(token string) (idCuenta, nombre string, err error)

    RegistrarBot(idCuenta, nombre, version string) (Bot, error)
    ListarBots(idCuenta string) ([]Bot, error)

    RegistrarResultado(resultado ResultadoPartida) error
    Estadisticas(idCuenta string) (Estadisticas, error)
    Ranking() ([]Estadisticas, error)

    Cuenta(idCuenta string) (Cuenta, error)
}
```

- `Login` no pide contraseña: es el modelo mínimo que hoy pide el protocolo
  (el bot manda un token en el saludo, nada más). Si el equipo necesita
  autenticación real, es un cambio a este contrato, no un detalle de
  implementación.
- `ResultadoJugador`/`ResultadoPartida` son tipos propios de `casino`, no los
  de `mesa` (regla dura #4 del mapa de dependencias): quien orquesta ambos
  hace la conversión.
- La fórmula de puntaje (`casino/puntaje.go`, `puntosPorResultado`) no tenía
  especificación previa: hoy suma `puntosPorPuesto` (10) por cada rival
  superado y resta `puntosPorTimeout` (2) por cada timeout de esa partida.
  Es el único lugar a tocar si el equipo define otra fórmula.
- **Pendiente conocido:** `mesa.ResumenJugador` todavía no cuenta los
  timeouts por jugador (la conexión sí los cuenta, `ConexionTCP.Timeouts()`,
  pero no llegan al resumen de la partida), así que hoy
  `ResultadoJugador.Timeouts` siempre llega en 0 desde `cmd/mesa`. Falta
  sumar ese campo a `ResumenJugador` y poblarlo en `mesa.Jugar` para que el
  ranking penalice timeouts de verdad.
- Persistencia: `internal/almacen.JSON` (un solo archivo JSON, escritura
  atómica vía archivo temporal + rename). `casino.Nuevo(rutaArchivo)` carga
  el archivo si existe y arranca vacío si no. Pensado para cambiarse por
  SQLite sin que `casino` cambie cómo lo usa (`Cargar`/`Guardar` de un valor
  cualquiera, `almacen` no conoce `Cuenta`/`Bot`/etc).

## 4.1 Conexiones (`internal/mesa/servidor.go`)

Toda conexión con un bot cumple `ConexionMesaJugador`. Hay dos
implementaciones:

| Tipo | Archivo | Uso |
|------|---------|-----|
| `ConexionTCP` | `mesa/servidor.go` | Real: JSON Lines sobre `net.Conn`. |
| `ConexionPrueba` | `mesa/conexion_prueba.go` | Sin red: acciones programadas + historial de mensajes. |

```go
// Real
s := mesa.NuevoServidor(mesa.ConfigServidor{Direccion: ":9000", TimeoutHandshakeMs: 5000}, m, casino.ValidarToken)
s.Escuchar(ctx) // bloquea: acepta, hace handshake y llama a m.SentarJugador

// De prueba
cx := mesa.NuevaConexionPrueba("c-1", protocolo.Accion{Tipo: protocolo.Call})
m.SentarJugador("c-1", "BotAlpha", cx)
```

Contrato de errores que la Mesa debe manejar:

- `ErrTiempoAgotado`: aplicar `protocolo.AccionSegura` y sumar 1 a los timeouts.
- `ErrJugadorAbandono` / `ErrConexionCerrada`: levantar al jugador de la mesa.
- `protocolo.ErrAccionAusente` o acción inválida: aplicar `AccionSegura`.

`ConexionTCP` recuerda el `id_mano` del último `estado` enviado y **descarta
las acciones cuyo `id_mano` no coincida** (acciones tardías). `ParConexionesMemoria`
devuelve dos `ConexionTCP` unidas por `net.Pipe` para probar el protocolo
completo sin abrir puertos.

## 4.2 Sillas y concurrencia

`Mesa.Jugadores` es un arreglo de tamaño `MaxJugadores` indexado por silla;
`nil` significa silla libre. `SentarJugador` toma la silla libre más baja y da
el `StackInicial`; `LevantarJugador` cierra la conexión y libera la silla.

> **Importante para Mesa #1 y #2:** `Jugadores` lo toca el Servidor desde la
> goroutine de cada conexión mientras `Jugar` corre en otra. **Todo acceso debe
> tomar `Mesa.Mu`**, incluido `ObtenerJugadoresActivos`, que asume que quien la
> llama ya lo hizo.

Divergencias con el contrato de `MesaInterface` que quedan por resolver:

- El comentario de `SentarJugador` dice que un jugador sentado pero
  desconectado "se vuelve a conectar"; hoy se rechaza cualquier duplicado.
- El comentario original de `LevantarJugador` decía que el jugador podía
  quedarse de espectador; hoy se le cierra la conexión.
- `CfgMesa.ActualJugadores` no lo actualiza nadie.

## 5. Tareas pendientes referenciadas por los `panic`

| Ref | Archivo | Que falta | Estimado | Responsable |
|-----|---------|-----------|----------|-------------|
| Crupier #1 | `crupier/baraja.go` | Ya implementado | 4h | Enzo |
| Crupier #2 | `crupier/crupier.go` | Ya implementado reparto inicial y comunitarias | 2h | Enzo / Gandy |
| Crupier #3 | `crupier/pozo.go` | Ya implementado (pozos laterales) | 3h | Gandy |
| Crupier #4 | `crupier/evaluador.go` | Ya implementada evaluacion de 5 en 7 | 4h | Enzo |
| Mesa #1 | `mesa/turnos.go`, `mesa/mesa.go` | Ya implementado (turnos, ciegas, rondas, `Estado`) | 5h | Lucas |
| Mesa #2 | `mesa/saldo.go` | Ya implementado (contabilidad de fichas, validación de acciones) | 4h | Lucas |
| Mesa #3 | `mesa/servidor.go` | Ya implementado (servidor TCP, handshake, conexión de prueba) | 5h | Alvaro |
| Casino #1 | `casino/cuentas.go`, `almacen/json.go` | Ya implementado (cuentas, login, tokens, persistencia JSON) | 3h | Jhntn |
| Casino #2 | `casino/bots.go` | Ya implementado (alta y versionado de bots por cuenta) | 3h | Jhntn |
| Casino #3 | `casino/puntaje.go` | Ya implementado (fórmula de puntos y ranking) | 2h | Jhntn |
