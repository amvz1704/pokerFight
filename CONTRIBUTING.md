# Contribuir a pokerFight

Las siete reglas del equipo estan en el [README](README.md#reglas-del-equipo).
Este documento es el detalle de como se cumplen.

- [Antes de escribir codigo](#antes-de-escribir-codigo)
- [Ramas y commits](#ramas-y-commits)
- [Checklist de PR](#checklist-de-pr)
- [Estilo](#estilo)
- [Tests](#tests)
- [Documentacion](#documentacion)
- [Prompts de IA](#prompts-de-ia)
- [Cambiar el protocolo](#cambiar-el-protocolo)
- [Donde tocar cada cosa](#donde-tocar-cada-cosa)

---

## Antes de escribir codigo

```bash
make build   # compila todo
make test    # tests
make lint    # gofmt + go vet
```

Si `make torneo-local` corre de punta a punta, el sistema esta sano.

`make test` usa `-race`, que necesita un compilador de C. En Windows sin uno,
corré `go test ./...` a secas: CI corre igual la version con `-race` en Linux, asi
que un race lo vamos a ver antes de mergear.

---

## Ramas y commits

Una rama por funcionalidad, desde `main`:

```
mesa/reconexion-de-jugadores
crupier/pozos-laterales
arena/formato-suizo
docs/protocolo-v2
```

**Commits descriptivos.** Un commit tiene que explicar *que cambia y por que*, no
repetir el diff.

```
Bien:  Mesa: separar "en la mano" de "en el torneo"

       Un bot que se conectaba a mitad de mano recibia turno sin tener
       cartas ni fichas en el pozo, porque el recorrido de turnos usaba
       Activo (sigue en el torneo) en vez de EnMano.

Mal:   fix
Mal:   cambios mesa.go
Mal:   agrega campo EnMano a Jugador
```

El ultimo esta mal porque describe el diff, que ya se ve. Lo que hace falta es el
*por que*.

---

## Checklist de PR

Antes de pedir revision:

- [ ] `make lint` y `make test` pasan.
- [ ] Si cambiaste un contrato entre modulos, **actualizaste
      [`docs/interfaces.md`](docs/interfaces.md)**. Es la regla #2 y es
      bloqueante.
- [ ] Si cambiaste el protocolo, actualizaste
      [`docs/protocolo.md`](docs/protocolo.md) y la version en
      `internal/protocolo/mensajes.go`. Ver [abajo](#cambiar-el-protocolo).
- [ ] Si cambiaste reglas de poker, actualizaste
      [`docs/reglas.md`](docs/reglas.md).
- [ ] Si usaste IA, documentaste el prompt en `docs/prompts/`.
- [ ] Hay tests para lo que agregaste, y **uno que falle sin tu cambio** si es un
      arreglo de bug.
- [ ] Los comentarios explican *por que*, no *que*.

---

## Estilo

**Todo en español**: nombres, comentarios, mensajes de error, documentacion. El
codigo ya esta escrito asi y mezclar idiomas lo hace peor. Las unicas excepciones
son los identificadores del protocolo (`fold`, `call`, `raise`, `allin`), que son
parte del contrato publico y estan en ingles porque es como se llaman en poker.

**Comentarios que explican por que.** El *que* ya lo dice el codigo.

```go
// Mal: repite el codigo.
// Recorre los jugadores y cuenta los activos.

// Bien: explica una decision que no se ve.
// Se recorre `participantes` y no el map: iterar un map en Go da un orden
// distinto en cada corrida, y el resultado de la mano termina en el
// historial. Dos replicas de la misma partida tienen que producir el mismo
// JSON.
```

**Errores con contexto.** `fmt.Errorf` con `%w` para envolver, y siempre
diciendo quien fallo:

```go
return fmt.Errorf("mesa [ID: %s]: el crupier no pudo iniciar la mano [ID: %s]: %w", m.ID, idMano, err)
```

**Concurrencia documentada.** Si una funcion asume que quien la llama ya tomo un
mutex, decilo en el comentario. `internal/mesa` lo hace en cada archivo.

---

## Tests

Los tests son en español y **el nombre describe la propiedad**, no la funcion:

```go
// Bien: se lee como una afirmacion verificable.
func TestIdaYVueltaRepartenLasMismasCartas(t *testing.T)
func TestPozoConservaLasFichasDelQueSeRetira(t *testing.T)

// Mal: no dice que tiene que pasar.
func TestJugar(t *testing.T)
func TestPozo2(t *testing.T)
```

**Los mensajes de fallo incluyen los valores.** Un test que dice solo "fallo" no
sirve para nada:

```go
t.Fatalf("total de fichas final = %d, se esperaba %d (no se puede crear ni perder fichas)", total, esperado)
```

**Un test por propiedad, no por metodo.** Lo que importa es que las fichas se
conserven, que un all-in corto no reabra la accion, que dos corridas den lo
mismo. No que `procesarAccion` devuelva `true`.

Herramientas que ya existen y conviene usar:

- `mesa.ConexionPrueba` — un bot falso sin red, con acciones programadas o una
  funcion de estrategia. Ver `internal/mesa/conexion_prueba.go`.
- `mesa.ParConexionesMemoria` — dos `ConexionTCP` unidas por `net.Pipe`, para
  probar el protocolo completo sin abrir puertos.
- `crupier.NuevoConSemilla` — un crupier determinista.
- Los helpers `cartas`/`cinco` de `pkg/poker/evaluador_test.go`, que dejan
  escribir manos como `"As Ks Qs Js Ts"`.

---

## Documentacion

Cada documento tiene un publico y no hay que mezclarlos:

| Documento | Publico | Regla |
| --------- | ------- | ----- |
| `docs/protocolo.md` | Participantes | Normativo. Si el codigo difiere, es un bug. |
| `docs/torneo.md` | Participantes | Como competir. Sin detalles de implementacion. |
| `docs/reglas.md` | Participantes | Las reglas de poker que aplica la mesa. |
| `docs/arena.md` | Organizadores | Correr y extender torneos. |
| `docs/interfaces.md` | Equipo | Contratos entre modulos. **Primario.** |

Si un cambio afecta a mas de uno, se actualizan todos en el mismo PR.

---

## Prompts de IA

Regla #4 del equipo. Si usaste IA para escribir codigo que entra al repo,
documentá el prompt en `docs/prompts/` con
[la plantilla](docs/prompts/PLANTILLA.md).

Nombre del archivo: `AAAA-MM-DD-persona-tema.md`.

No es burocracia: sirve para saber que partes del codigo nadie del equipo penso
en detalle, que es exactamente donde conviene mirar dos veces.

---

## Cambiar el protocolo

El protocolo lo consumen bots que no controlamos. Cambiarlo tiene un costo real.

`VersionProtocolo` en `internal/protocolo/mensajes.go` es semver sobre el
**contrato**:

| Cambio | Sube | Que hay que hacer |
| ------ | ---- | ----------------- |
| Textos, documentacion | PATCH | Actualizar `protocolo.md`. |
| Campo nuevo **opcional** | MINOR | Actualizar `protocolo.md` y los ejemplos de `bots/ejemplos/`. |
| Cualquier cosa que rompa un bot existente | MAYOR | Lo anterior, + avisar a los participantes con tiempo. |

Un campo nuevo obligatorio, renombrar un campo, cambiar el significado de uno
existente o quitar un tipo de mensaje son cambios **mayores**, aunque parezcan
chicos.

En todos los casos hay que actualizar la constante en el codigo, los ejemplos de
Python y JavaScript, y `docs/protocolo.md`. Un PR que cambia el protocolo y no
toca los tres no se mergea.

---

## Donde tocar cada cosa

| Quiero... | Voy a... |
| --------- | -------- |
| Agregar un formato de torneo | `internal/arena/formato.go` + `RegistrarFormato` |
| Cambiar como se puntua | `internal/arena/puntuacion.go` + `RegistrarPuntuador` |
| Correr los bots en contenedores | Implementar `arena.Lanzador` |
| Mandar las manos a otro lado | Implementar `mesa.ObservadorMano` |
| Cambiar la formula del ranking historico | `internal/casino/puntaje.go` |
| Cambiar como se evalua una mano | `pkg/poker/evaluador.go` (publico: es API) |
| Cambiar el orden de turnos o las ciegas | `internal/mesa/turnos.go` |
| Cambiar que acciones son validas | `internal/mesa/saldo.go` |
| Cambiar a SQLite | Implementar la interfaz de `internal/almacen` |

Los tres primeros y el cuarto son puntos de extension pensados para eso: no hace
falta tocar el runner ni el CLI. Estan documentados con ejemplos en
[`docs/arena.md`](docs/arena.md#extender-la-arena).
