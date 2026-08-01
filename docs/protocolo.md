# Protocolo v1.0.0 — ejemplos crudos

Todos los mensajes son una linea JSON terminada en `\n`.

## Mesa -> Bot

Inicio de mano:

```json
{"tipo":"mano_inicio","version":"1.0.0","cartas":[{"rango":14,"palo":3},{"rango":13,"palo":3}],"estado":{"id_mano":"m-001","etapa":"preflop","comunitarias":[],"pozo":30,"apuesta_actual":20,"subida_minima":20,"ciega_chica":10,"ciega_grande":20,"posicion_boton":0,"jugadores":[],"historial_acciones":[]}}
```

Solicitud de accion:

```json
{"tipo":"solicitar_accion","version":"1.0.0","acciones_validas":["fold","call","raise","allin"],"timeout_ms":2000,"estado":{"id_mano":"m-001","etapa":"flop","comunitarias":[{"rango":2,"palo":0},{"rango":7,"palo":1},{"rango":14,"palo":2}],"pozo":80,"apuesta_actual":20,"subida_minima":20,"jugadores":[]}}
```

Fin de mano:

```json
{"tipo":"mano_fin","version":"1.0.0","resultado":{"id_mano":"m-001","comunitarias":[],"repartos":[{"id_jugador":"c-42","monto":80}],"descripcion":{"c-42":"par de ases"}}}
```

## Bot -> Mesa

```json
{"tipo":"saludo","version":"1.0.0","token":"eyJ..."}
{"tipo":"accion","id_mano":"m-001","accion":{"tipo":"raise","monto":60}}
{"tipo":"abandono"}
```

## Codificacion de cartas

`palo`: 0 treboles, 1 diamantes, 2 corazones, 3 picas.
`rango`: 2..14 (11 = J, 12 = Q, 13 = K, 14 = A).

Representacion legible en logs: `Ah`, `Ts`, `2c`.

## Probar a mano

```bash
nc localhost 9000
{"tipo":"saludo","version":"1.0.0","token":"demo"}
```
