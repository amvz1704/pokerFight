package mesa

import "github.com/amvz1704/pokerFight/internal/protocolo"

// Es la interfaz que representa la conexión de un jugador con la mesa. Cualquier
// tipo de conexión a implementar (websocket, TCP, de prueba, etc.) debe implementar
// esta interfaz.
// Esta interfaz es utilizada por la mesa para enviar mensajes a los jugadores y
// recibir mensajes (acciones) de los jugadores.
// NOTA: Al ser una interfaz, no es necesario que las estructuras que la usen la llamen
// como puntero. Esto se debe a que en Go, las interfaces se manejan internamente como
// punteros. Por lo tanto, no habrán conexiones duplicadas ni problemas de memoria o red.
type ConexionMesaJugador interface {
	EnviarMensaje(m protocolo.MensajeMesa, timeoutMs uint64) error // La mesa envía un mensaje al jugador. Debe tener un timeout si no recibe confirmación de la respuesta. En caso no reciba confirmación, se retira al jugador de la mesa.
	SolicitarAccion(timeoutMs uint64) (protocolo.Accion, error)    // La mesa solicita una acción al jugador cuando sea su turno. Debe tener un timeout si no recibe confirmación de la respuesta. En caso no reciba confirmación, se opta por la acción más segura (check o fold) según el estado de la partida. En caso de error, se opta por la acción más segura (check o fold) según el estado de la partida.
	Cerrar() error                                                 // Cerrar la conexión con el jugador. Se llama cuando se termina la partida o el jugador se retira voluntariamente.
}
