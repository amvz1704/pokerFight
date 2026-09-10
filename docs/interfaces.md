# Interfaces (documento primario)

> Regla del proyecto: **la documentacion de interfaces es mandatoria**. Este
> archivo es la fuente de verdad. Si el codigo y este documento difieren, el
> PR se rechaza hasta que uno de los dos se corrija.

Este documento describe los contratos **entre modulos**. El contrato con los
bots (que es publico y lo leen los participantes) esta en
[`protocolo.md`](protocolo.md).

## 1. Mapa de dependencias

```
bots (externos)  ->  pkg/botsdk  ->  internal/protocolo  <-  internal/mesa
                     pkg/poker   ->        ^                      |
                                           |                      v
                                   internal/casino        internal/crupier -> pkg/poker
                                           |                      ^
                                           v                      |
                                   internal/almacen        internal/arena --> internal/mesa
```

Reglas duras:

1. `internal/protocolo` **no importa a nadie** del proyecto.
2. `internal/crupier` importa `protocolo` y `pkg/poker`. No sabe que existe la red.
3. `internal/mesa` importa `crupier` y `protocolo`. No importa `casino`
   (recibe la funcion `ValidarToken` por inyeccion).
4. `internal/casino` importa `almacen` y `protocolo`. No importa `mesa`.
5. `internal/arena` importa `mesa` y `crupier`. **No importa `casino`**: igual
   que la Mesa, recibe `ValidarToken` por inyeccion.
6. `pkg/` es lo unico publico: es lo que pueden importar los bots de los
   participantes, que viven fuera del modulo y por lo tanto no pueden ver
   `internal/`.

### 1.1 Por que el evaluador esta en `pkg/poker`

`internal/` no es importable desde fuera del modulo: lo impone el compilador de
Go. Un participante que escribe su bot en Go no podria usar el evaluador del
Crupier, y tendria que reimplementarlo — con el riesgo de que su idea de que
mano gana no coincida con la de la mesa.

Por eso la evaluacion de manos vive en `pkg/poker` y el Crupier la usa desde
ahi. `crupier.Evaluacion` es un alias de `poker.Evaluacion`, no un tipo aparte.

Es el equivalente a la libreria que MIT Pokerbots le da a sus participantes.

## 2. Contrato Mesa <-> Bot

La referencia normativa es [`protocolo.md`](protocolo.md). Resumen para quien
trabaja en el motor:

Transporte: **TCP + JSON Lines** (un objeto JSON por linea, terminado en `\n`).
Se eligio asi para que un bot pueda escribirse en cualquier lenguaje y para
poder depurar con `nc localhost 9000`.

### Handshake

```
bot  -> mesa : {"tipo":"saludo","version":"1.0.0","token":"..."}
mesa -> bot  : {"tipo":"bienvenida","version":"1.0.0","id_jugador":"c-42","silla":0}
```

Si la version no coincide **exacta**, la Mesa responde `{"tipo":"error"}` y cierra.

La bienvenida se envia **despues** de sentar al jugador, no antes: recien ahi se
conoce su silla. `id_jugador` es imprescindible — es lo unico que le permite a un
bot encontrarse en `EstadoPublico.Jugadores`, porque con Casino su identificador
es el ID de cuenta y no el token que mando.

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
  aparece en `mano_inicio` y solo trae las del destinatario. Las de un rival
  solo llegan en `mano_fin.mostradas`, y solo si hubo showdown.
- La Mesa **nunca** confia en el bot: valida `accion` contra
  `acciones_validas`, contra el saldo y contra la subida minima.
- Si el bot no responde antes de `timeout_ms`, **o responde algo invalido**, la
  Mesa aplica `AccionSegura`: **check** si no hay apuesta pendiente, **fold** si
  la hay. Los dos casos suman 1 a `Jugador.Timeouts`, que viaja al resumen de la
  partida en `ResumenJugador.Timeouts`. Se cuentan igual a proposito: la Mesa
  hace lo mismo en los dos casos, y contarlos distinto le daria a un bot roto una
  ventaja sobre uno lento.
- `Accion.Monto` es el **total** al que el jugador lleva su apuesta en la
  ronda, no el incremento. Evita ambiguedades en `raise`.
- `id_mano` viaja en la respuesta del bot para descartar acciones tardias.

### 2.1 Reconexion

Un bot que pierde el socket puede volver con el **mismo token**: el Servidor
intenta `SentarJugador` y, si ese ID ya tiene silla, cae a `ReconectarJugador`,
que reemplaza la conexion conservando saldo, silla y estado en la mano en curso.
El socket viejo se cierra.

`SentarJugador` y `ReconectarJugador` son operaciones distintas a proposito:
sentar dos veces el mismo ID es un error (dos bots con el mismo token queriendo
ocupar dos sillas), reconectar es explicito.

`ReconectarJugador` cierra la conexion vieja **fuera de `Mu`**: cerrar una
`ConexionTCP` toma el mutex de esa conexion, que puede estar tomado por un
`SolicitarAccion` en curso, y hacerlo con `Mu` tomada frenaria la mesa entera
durante todo el plazo que el bot tenga para pensar.

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

### 3.1 El orden de `participantes` es significativo

La Mesa arma `participantes` **en orden de accion, empezando por la izquierda
del boton**. De ahi sale, sin que el Crupier tenga que saber donde esta el boton,
la regla de `reglas.md` sobre el reparto de un pozo que no divide exacto: la
ficha sobrante va al primero de esa lista entre los ganadores.

`ResultadoMano.Repartos` se emite recorriendo `participantes`, **no** iterando
el map interno: iterar un map en Go da un orden distinto en cada corrida, y el
resultado de la mano termina en el historial y en el mensaje `mano_fin`. Dos
replicas de la misma partida tienen que producir exactamente el mismo JSON.

### 3.2 Barajado sembrable

`crupier.Nuevo()` usa `crypto/rand`: impredecible, para una partida real.
`crupier.NuevoConSemilla(n)` usa un generador sembrado: dos crupiers con la
misma semilla reparten exactamente las mismas cartas, mano por mano.

Existen los dos porque son necesidades incompatibles y ambas reales: una mesa
suelta necesita un mazo que nadie pueda predecir; un torneo necesita poder jugar
el mismo emparejamiento en las dos posiciones con las mismas cartas.

**No usar `NuevoConSemilla` donde los jugadores puedan conocer la semilla:** con
ella se puede predecir el mazo entero.

## 4. Contrato Casino -> Mesa / Arena

Ni la Mesa ni la Arena importan el paquete `casino`. Reciben una funcion:

```go
ValidarToken func(token string) (idCuenta, nombre string, err error)
```

`(*casino.motor).ValidarToken` ya tiene exactamente esa forma, asi que se
inyecta directo en `mesa.NuevoServidor` y en `arena.Torneo.Validar` sin
envoltorios.

Al terminar, quien orquesta convierte el resumen al tipo propio de Casino y
llama a `RegistrarResultado`. Hay dos orquestadores, y son los unicos lugares
que importan `casino` junto a `mesa` o `arena`:

- `cmd/mesa` con `-casino-db`: una mesa suelta reporta su partida.
- `cmd/arena` con `-casino-db`: el torneo entero se reporta como una sola
  "partida", con la posicion de la clasificacion final.

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

- `Login` no pide contraseña: es el modelo minimo que hoy pide el protocolo (el
  bot manda un token en el saludo, nada mas). Si el equipo necesita
  autenticacion real, es un cambio a este contrato, no un detalle de
  implementacion.
- `ResultadoJugador`/`ResultadoPartida` son tipos propios de `casino`, no los de
  `mesa` (regla dura #4): quien orquesta ambos hace la conversion.
- La formula de puntaje (`casino/puntaje.go`) suma `puntosPorPuesto` (10) por
  cada rival superado y resta `puntosPorTimeout` (2) por cada timeout. Es el
  unico lugar a tocar si el equipo define otra formula. La arena tiene su propia
  puntuacion, independiente y extensible (ver [`arena.md`](arena.md#puntuación)).
- Persistencia: `internal/almacen.JSON` (un solo archivo JSON, escritura atomica
  via archivo temporal + rename). Pensado para cambiarse por SQLite sin que
  `casino` cambie como lo usa.

## 4.2 Conexiones (`internal/mesa/servidor.go`)

Toda conexion con un bot cumple `ConexionMesaJugador`. Hay dos implementaciones:

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
- `ErrJugadorAbandono`: tratar como fold.
- `ErrConexionCerrada`: aplicar la accion segura (el bot puede reconectar).
- `protocolo.ErrAccionAusente` o accion invalida: aplicar `AccionSegura` y sumar
  1 a los timeouts.

`ConexionTCP` recuerda el `id_mano` del ultimo `estado` enviado y **descarta las
acciones cuyo `id_mano` no coincida** (acciones tardias). `ParConexionesMemoria`
devuelve dos `ConexionTCP` unidas por `net.Pipe` para probar el protocolo
completo sin abrir puertos.

### 4.2.1 Asignacion de sillas

`ConfigServidor.SillaDe`, si no es nil, decide en que silla se sienta cada
jugador; el Servidor llama entonces a `SentarJugadorEnSilla` en vez de
`SentarJugador`.

Existe porque "la primera silla libre" es una asignacion **por orden de
llegada**, y el orden de llegada de unos bots que arrancan en paralelo es una
carrera. Sin esto, la ida y la vuelta de un emparejamiento pueden terminar con la
misma disposicion de sillas y ser, en la practica, la misma partida jugada dos
veces: la rotacion de posiciones del torneo no compensaria nada.

## 4.3 Sillas, estado de mano y concurrencia

`Mesa.Jugadores` es un arreglo de tamaño `MaxJugadores` indexado por silla;
`nil` significa silla libre.

> **Importante:** `Jugadores` lo toca el Servidor desde la goroutine de cada
> conexion mientras `Jugar` corre en otra. **Todo acceso debe tomar `Mesa.Mu`**,
> incluido `ObtenerJugadoresActivos`, que asume que quien la llama ya lo hizo.

### Las tres nociones de "esta jugando"

No son intercambiables y hacen falta las tres:

| Campo | Significa | Cuando cambia |
|-------|-----------|---------------|
| `Jugador.Activo` | Sigue en la partida (le quedan fichas). | Se apaga al quedar en 0, salvo con `ReponerStack`. |
| `Jugador.EnMano` | Recibio cartas en la mano en curso. | Se recalcula al empezar cada mano. |
| `Jugador.Retirado` | Hizo fold en la mano en curso. | Se reinicia en cada mano. |

Entre `Activo` y `EnMano` hay una ventana real: un bot que se conecta a mitad de
mano queda `Activo` pero con `EnMano=false` hasta la mano siguiente. **Todo el
recorrido de turnos y ciegas usa `EnMano`**; solo el avance del boton entre manos
usa `Activo`. Sin esa separacion, a un jugador recien sentado se le pediria
accion en una mano en la que no tiene cartas ni fichas en el pozo.

En `EstadoPublico`, los tres se exponen como `activo` (en la mano),
`en_torneo` (con fichas) y `en_mano` (recibio cartas).

### 4.4 Clasificacion final

`resumenFinal` ordena por `ResumenJugador.Neto`, y entre jugadores con el mismo
neto (tipicamente 0, todos eliminados) usa el **orden de eliminacion invertido**:
el que aguanto mas va mas arriba. Sin eso, todos los eliminados empatarian y el
orden lo decidiria el azar del recorrido.

`Neto` es el numero por el que se clasifica, no `SaldoFinal`: con
`ConfigPartida.ReponerStack`, `SaldoFinal` es solo el resultado de la ultima
mano.

### 4.5 Reposicion de stack

`ConfigPartida.ReponerStack` devuelve a todos al `StackInicial` antes de cada
mano y lleva aparte `Jugador.NetoAcumulado`. Cambia que mide la partida:

- **Sin reposicion**, la partida es un torneo: termina en cuanto alguien queda
  sin fichas, y `CantidadRondas` es apenas un techo. Mide **quien quebro
  primero**.
- **Con reposicion**, las manos son independientes y se juegan todas. Mide
  **cuantas fichas de ventaja saca un bot por mano**, que es mucho menos ruidoso.

Exige `CantidadRondas > 0`: sin eliminacion posible, una partida sin limite de
manos no termina nunca.

## 5. Contrato Mesa -> Observador

```go
type ObservadorMano interface {
    ManoTerminada(mano ManoJugada)
}
```

Punto de extension para historiales, replays o telemetria. La Mesa lo invoca de
forma **sincrona y sin tener tomado `Mu`** al cerrar cada mano, asi que una
implementacion lenta frena la partida: si hay que escribir a disco, con buffer.

`ManoJugada` se pasa como estructura y no como lista de parametros para poder
agregarle campos sin romper a quien ya lo implemente.

`ManoJugada.Privadas` trae las cartas de **todos** los que jugaron la mano, no
solo las de quienes llegaron al showdown. Es informacion que **jamas puede
llegar a un bot**: existe para el historial de auditoria, porque una mano que
termina en fold no revela nada por protocolo y sin esto no habria forma de
responder a un reclamo sobre ella.

## 6. Contratos de la Arena (`internal/arena`)

La arena tiene tres puntos de extension, todos documentados con ejemplos en
[`arena.md`](arena.md#extender-la-arena):

| Interfaz | Decide | Registro |
|----------|--------|----------|
| `Formato` | Que se juega contra que. | `RegistrarFormato(nombre, constructor)` |
| `Puntuador` | Como un resultado se convierte en puntos. | `RegistrarPuntuador(nombre, constructor)` |
| `Lanzador` | Como se materializa un bot (subproceso, remoto, contenedor). | Campo `Torneo.Lanzador` |

Los dos primeros se eligen **por nombre desde el archivo de configuracion**, asi
que agregar uno nuevo no obliga a tocar el runner ni el CLI.

`Puntuador.Puntos` **tiene que ser una funcion pura de `(resultado, puesto)`**:
la clasificacion se arma sumando, y una formula con estado acumulado haria que el
orden en que terminan los enfrentamientos —que el paralelismo vuelve
impredecible— cambie la tabla final.

`Formato.Emparejamientos` se llama **una sola vez, antes de jugar nada**. Hoy no
hay soporte para formatos donde una ronda dependa del resultado de la anterior
(eliminacion directa, suizo de verdad); el camino es un metodo opcional nuevo,
no cambiar el que existe.

## 7. Estado del proyecto

Todas las tareas originales estan implementadas. Lo que sigue son las
divergencias y limitaciones conocidas:

| Tema | Estado |
|------|--------|
| `CfgMesa.ActualJugadores` | Ya la mantiene la Mesa (`SentarJugador` / `LevantarJugador`). |
| Reconexion de un jugador sentado | Resuelta con `ReconectarJugador` (§2.1). |
| Timeouts por jugador en el resumen | Resuelto: `ResumenJugador.Timeouts`. |
| `LevantarJugador` cierra la conexion | Sigue asi. No hay modo espectador. |
| Formatos con rondas dependientes | No soportado (§6). |
| Sandbox para bots de terceros | No hay. Ver [`arena.md`](arena.md#seguridad). |
| Autenticacion real en el Casino | No hay: `Login` no pide contraseña (§4.1). |
