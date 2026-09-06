#!/usr/bin/env python3
"""Bot de ejemplo en Python para pokerFight.

Es la referencia para escribir un bot en cualquier lenguaje que no sea Go: no
usa ninguna libreria del proyecto, solo un socket TCP y JSON. Todo lo que hace
falta para competir esta en este archivo.

Uso (lo mismo que hace la arena cuando lo lanza):

    python bot.py -addr localhost:9000 -token mi-token

Protocolo, en una linea: un objeto JSON por linea, terminado en \\n, en las dos
direcciones. La referencia completa esta en docs/protocolo.md.
"""

import argparse
import json
import socket
import sys

VERSION_PROTOCOLO = "1.0.0"

# Palos y rangos tal como viajan por el protocolo.
PALOS = "cdhs"  # 0 treboles, 1 diamantes, 2 corazones, 3 picas
NOMBRES_RANGO = {11: "J", 12: "Q", 13: "K", 14: "A"}


def carta_legible(carta):
    """Convierte {"rango": 14, "palo": 3} en "As". Solo para los logs."""
    rango = NOMBRES_RANGO.get(carta["rango"], str(carta["rango"]))
    return f"{rango}{PALOS[carta['palo']]}"


class Mesa:
    """Conexion con la mesa: handshake y envio/recepcion de mensajes."""

    def __init__(self, direccion, token):
        host, puerto = direccion.rsplit(":", 1)
        self.socket = socket.create_connection((host, int(puerto)))
        # makefile da un iterador por lineas, que es exactamente el framing del
        # protocolo. Sin esto habria que juntar los bytes a mano: un recv puede
        # devolver media linea o dos y media.
        self.entrada = self.socket.makefile("r", encoding="utf-8", newline="\n")

        self.token = token
        self.id_jugador = token  # Provisorio hasta la bienvenida.
        self.silla = -1

    def enviar(self, mensaje):
        self.socket.sendall((json.dumps(mensaje) + "\n").encode("utf-8"))

    def recibir(self):
        linea = self.entrada.readline()
        if not linea:
            return None  # La mesa cerro: la partida termino.
        return json.loads(linea)

    def saludar(self):
        """Handshake. La bienvenida trae la identidad con la que la mesa nos
        conoce, que es la unica forma de encontrarnos en estado["jugadores"]:
        el token no sirve de identificador cuando hay un Casino detras."""
        self.enviar({"tipo": "saludo", "version": VERSION_PROTOCOLO, "token": self.token})
        bienvenida = self.recibir()

        if bienvenida is None:
            raise RuntimeError("la mesa cerro la conexion durante el saludo")
        if bienvenida["tipo"] == "error":
            raise RuntimeError(f"la mesa rechazo la conexion: {bienvenida.get('mensaje', '')}")
        if bienvenida["tipo"] != "bienvenida":
            raise RuntimeError(f"se esperaba bienvenida, llego {bienvenida['tipo']!r}")

        self.id_jugador = bienvenida.get("id_jugador") or self.token
        self.silla = bienvenida.get("silla", -1)
        return bienvenida


class Turno:
    """Todo lo que se sabe en el momento de decidir."""

    def __init__(self, mesa, mensaje, mano):
        self.estado = mensaje.get("estado") or {}
        self.validas = mensaje.get("acciones_validas") or []
        self.timeout_ms = mensaje.get("timeout_ms", 0)
        self.mano = mano

        # Nuestra propia entrada del estado, ya resuelta.
        self.yo = {"saldo": 0, "apuesta_ronda": 0}
        for jugador in self.estado.get("jugadores", []):
            if jugador["id"] == mesa.id_jugador:
                self.yo = jugador
                break

    def puede(self, tipo):
        return tipo in self.validas

    @property
    def por_igualar(self):
        """Fichas que faltan poner para igualar. 0 si se puede pasar gratis."""
        return max(0, self.estado.get("apuesta_actual", 0) - self.yo.get("apuesta_ronda", 0))

    @property
    def techo(self):
        """Total maximo al que se puede llevar la apuesta: el all-in."""
        return self.yo.get("saldo", 0) + self.yo.get("apuesta_ronda", 0)

    @property
    def subida_minima(self):
        """Total minimo que la mesa acepta como bet o raise."""
        return self.estado.get("apuesta_actual", 0) + self.estado.get("subida_minima", 0)

    def apostar(self, total):
        """Lleva la apuesta de la ronda a `total` fichas, ajustando al rango
        que la mesa acepta.

        OJO: en el protocolo, `monto` es el TOTAL al que se lleva la apuesta en
        la ronda, no el incremento. Es el error mas comun al escribir un bot y
        la mesa lo castiga descartando la accion.
        """
        tipo = "bet" if self.estado.get("apuesta_actual", 0) <= self.yo.get("apuesta_ronda", 0) else "raise"

        if total >= self.techo and self.puede("allin"):
            # Llegar al techo es exactamente un all-in. Declararlo asi hace que
            # la mesa aplique la regla de all-in corto en vez de rechazarlo por
            # subida insuficiente.
            return {"tipo": "allin"}
        if not self.puede(tipo):
            return self.pasiva()

        total = max(total, self.subida_minima)
        if total > self.techo:
            return {"tipo": "allin"} if self.puede("allin") else self.pasiva()
        return {"tipo": tipo, "monto": total}

    def pasiva(self):
        """La mejor accion que no arriesga nada: pasar si es gratis, si no
        pagar si es barato, si no retirarse."""
        if self.puede("check"):
            return {"tipo": "check"}
        if self.puede("call") and self.por_igualar <= self.estado.get("ciega_grande", 0) * 3:
            return {"tipo": "call"}
        return {"tipo": "fold"}


def decidir(turno):
    """Aca va tu estrategia. Esta es deliberadamente simple.

    Sube con un par en la mano o con una carta alta acompañada; si no, juega
    pasivo. Le gana al bot conservador y pierde contra el bot base: es el punto
    de partida, no un bot competitivo.
    """
    if len(turno.mano) == 2:
        rango_a, rango_b = turno.mano[0]["rango"], turno.mano[1]["rango"]
        es_par = rango_a == rango_b
        tiene_carta_alta = max(rango_a, rango_b) >= 12  # Q, K o A.

        if es_par or (tiene_carta_alta and min(rango_a, rango_b) >= 10):
            return turno.apostar(turno.estado.get("ciega_grande", 20) * 3)

    return turno.pasiva()


def main():
    parser = argparse.ArgumentParser(description="Bot de ejemplo para pokerFight")
    parser.add_argument("-addr", default="localhost:9000", help="direccion de la mesa (host:puerto)")
    parser.add_argument("-token", default="bot-python", help="token de sesion")
    argumentos = parser.parse_args()

    mesa = Mesa(argumentos.addr, argumentos.token)
    mesa.saludar()
    print(f"conectado como {mesa.id_jugador} en la silla {mesa.silla}", file=sys.stderr)

    mano = []  # Las cartas privadas de la mano en curso.

    while True:
        mensaje = mesa.recibir()
        if mensaje is None:
            print("la mesa cerro la conexion: partida terminada", file=sys.stderr)
            return

        tipo = mensaje["tipo"]

        if tipo == "mano_inicio":
            mano = mensaje.get("cartas") or []
            print(f"mano nueva: {' '.join(carta_legible(c) for c in mano)}", file=sys.stderr)

        elif tipo == "solicitar_accion":
            turno = Turno(mesa, mensaje, mano)
            accion = decidir(turno)
            # El id_mano se devuelve tal cual: la mesa lo usa para descartar
            # acciones que llegan tarde, de una mano ya cerrada.
            mesa.enviar({
                "tipo": "accion",
                "version": VERSION_PROTOCOLO,
                "id_mano": turno.estado.get("id_mano", ""),
                "accion": accion,
            })

        elif tipo == "mano_fin":
            # Informativo: repartos, cartas mostradas en el showdown y la
            # descripcion de cada jugada. Buen lugar para aprender del rival.
            pass

        elif tipo == "estado":
            # Actualizacion tras una accion ajena. No hay que responder.
            pass

        elif tipo == "error":
            print(f"error de la mesa: {mensaje.get('mensaje', '')}", file=sys.stderr)
            return


if __name__ == "__main__":
    main()
