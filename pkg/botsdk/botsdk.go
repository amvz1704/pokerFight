// Package botsdk es la librería pública para escribir bots en Go. Habla el
// protocolo Mesa <-> Bot (docs/protocolo.md) por TCP + JSON Lines,
// dependiendo únicamente de internal/protocolo — igual que la Mesa, pero sin
// importarla a ella (ver el mapa de dependencias en docs/interfaces.md §1).
package botsdk

import (
	"context"
	"fmt"
	"net"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Bot es la interfaz que cualquier bot escrito en Go debe implementar.
type Bot interface {
	// Nombre identifica al bot ante la mesa (solo informativo).
	Nombre() string
	// Decidir devuelve la acción a jugar en el turno actual. Si tarda más
	// que el timeout que indica la mesa, esta aplica la acción segura
	// (protocolo.AccionSegura) en su lugar.
	Decidir(ctx context.Context, estado protocolo.EstadoPublico, mias protocolo.Mano, validas []protocolo.TipoAccion) protocolo.Accion
}

// Opciones configura la conexión del bot con la mesa.
type Opciones struct {
	Direccion string // host:puerto de la mesa, por ejemplo "localhost:9000".
	Token     string // token emitido por el Casino. En modo abierto (mesa sin Casino) es el identificador del jugador.
}

// Correr conecta el bot a la mesa, hace el handshake y responde a cada
// solicitud de acción invocando Decidir, hasta que la mesa cierre la
// conexión, el bot se desconecte o el contexto se cancele. Bloquea.
func Correr(ctx context.Context, bot Bot, opciones Opciones) error {
	var dialer net.Dialer
	conexion, err := dialer.DialContext(ctx, "tcp", opciones.Direccion)
	if err != nil {
		return fmt.Errorf("botsdk: no se pudo conectar a %s: %w", opciones.Direccion, err)
	}
	defer conexion.Close()

	codec := protocolo.NuevoCodec(conexion, conexion)

	saludo := protocolo.MensajeBot{
		Tipo:    protocolo.MsgSaludo,
		Version: protocolo.VersionProtocolo,
		Token:   opciones.Token,
	}
	if err := codec.Enviar(saludo); err != nil {
		return fmt.Errorf("botsdk: error al enviar saludo: %w", err)
	}

	bienvenida, err := codec.RecibirMensajeMesa()
	if err != nil {
		return fmt.Errorf("botsdk: error al recibir bienvenida: %w", err)
	}
	switch bienvenida.Tipo {
	case protocolo.MsgError:
		return fmt.Errorf("botsdk: la mesa rechazó la conexión: %s", bienvenida.Mensaje)
	case protocolo.MsgBienvenida:
		// Ok, seguimos.
	default:
		return fmt.Errorf("botsdk: se esperaba bienvenida, llegó %q", bienvenida.Tipo)
	}

	var misCartas protocolo.Mano

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := codec.RecibirMensajeMesa()
		if err != nil {
			return err
		}

		switch msg.Tipo {
		case protocolo.MsgManoInicio:
			if msg.Cartas != nil {
				misCartas = *msg.Cartas
			}

		case protocolo.MsgSolicitarAccion:
			var estado protocolo.EstadoPublico
			if msg.Estado != nil {
				estado = *msg.Estado
			}
			accion := bot.Decidir(ctx, estado, misCartas, msg.AccionesValidas)
			respuesta := protocolo.MensajeBot{
				Tipo:    protocolo.MsgAccion,
				Version: protocolo.VersionProtocolo,
				IDMano:  estado.IDMano,
				Accion:  &accion,
			}
			if err := codec.Enviar(respuesta); err != nil {
				return fmt.Errorf("botsdk: error al enviar acción: %w", err)
			}

		case protocolo.MsgManoFin, protocolo.MsgEstado:
			// Informativos: el bot no necesita responder.

		case protocolo.MsgError:
			return fmt.Errorf("botsdk: error de la mesa: %s", msg.Mensaje)
		}
	}
}
