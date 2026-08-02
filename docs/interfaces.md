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

Al terminar la partida, la Mesa devuelve un `mesa.Resumen` y quien la
orquesta llama a `casino.RegistrarResultado`.

## 5. Tareas pendientes referenciadas por los `panic`

| Ref | Archivo | Que falta | Estimado | Responsable |
|-----|---------|-----------|----------|-------------|
| Crupier #1 | `crupier/baraja.go` | ya implementado | 4h | Enzo |
| Crupier #2 | `crupier/crupier.go` | reparto inicial y comunitarias | 2h | Enzo / Gandy |
| Crupier #3 | `crupier/pozo.go` | `Descomponer` (pozos laterales) | 3h | Gandy |
| Crupier #4 | `crupier/evaluador.go` | evaluacion de 5 y mejor de 7 | 4h | Enzo |
| Mesa #1 | `mesa/turnos.go`, `mesa/mesa.go` | turnos, ciegas, rondas | 5h | Lucas |
| Mesa #2 | `mesa/saldo.go` | contabilidad de fichas | 4h | Lucas |
| Mesa #3 | `mesa/servidor.go` | conexiones TCP | 5h | Alvaro |
| Casino #1 | `casino/cuentas.go`, `almacen/json.go` | cuentas y sesiones | 3h | Jhntn |
| Casino #2 | `casino/bots.go` | bots y versionado | 3h | Jhntn |
| Casino #3 | `casino/puntaje.go` | ranking | 2h | Jhntn |
