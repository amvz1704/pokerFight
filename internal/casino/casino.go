// Package casino administra cuentas, sesiones, el versionado de bots por
// cuenta y el ranking. No importa el paquete mesa (ver docs/interfaces.md
// §1 y §4): la Mesa recibe ValidarToken por inyección, y quien orquesta
// ambos convierte mesa.ResumenPartida a casino.ResultadoPartida al llamar a
// RegistrarResultado. Casino tampoco juega: no conoce cartas ni reglas de
// poker, solo cuentas y resultados ya cerrados.
package casino

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Cuenta es un usuario registrado en el Casino.
type Cuenta struct {
	ID      string    `json:"id"`
	Usuario string    `json:"usuario"`
	Creada  time.Time `json:"creada"`
}

// Bot es una versión de un bot subida por una cuenta. Una cuenta puede tener
// varias versiones del mismo bot a lo largo del tiempo (docs/interfaces.md,
// tarea Casino #2: "bots y versionado").
type Bot struct {
	ID       string    `json:"id"`
	IDCuenta string    `json:"id_cuenta"`
	Nombre   string    `json:"nombre"`
	Version  string    `json:"version"`
	Creado   time.Time `json:"creado"`
}

// Estadisticas acumula el desempeño de una cuenta a través de partidas.
// Timeouts existe porque el protocolo lo pide explícitamente
// (docs/interfaces.md §2: "Cada aplicación [de la acción segura por plazo
// vencido] suma 1 a Estadisticas.Timeouts").
type Estadisticas struct {
	IDCuenta        string `json:"id_cuenta"`
	PartidasJugadas uint64 `json:"partidas_jugadas"`
	PartidasGanadas uint64 `json:"partidas_ganadas"` // Posicion == 1.
	Timeouts        uint64 `json:"timeouts"`
	Puntaje         int64  `json:"puntaje"`
}

// ResultadoJugador es la posición final de un jugador en una partida, tal
// como la reporta la Mesa (ver mesa.ResumenJugador). Es un tipo propio de
// Casino, no el de Mesa: así Casino no tiene que importar el paquete mesa.
// Quien orquesta ambos hace la conversión al llamar RegistrarResultado.
type ResultadoJugador struct {
	IDCuenta   string
	Posicion   int // 1 es el ganador.
	SaldoFinal uint64
	Timeouts   uint64 // Cuántas veces se le aplicó la acción segura por plazo vencido en esa partida.
}

// ResultadoPartida es el resumen de una partida ya terminada, listo para
// sumarse a las estadísticas históricas de cada cuenta involucrada.
type ResultadoPartida struct {
	IDMesa     string
	Posiciones []ResultadoJugador
}

// Casino administra cuentas, sesiones, bots versionados y ranking.
type Casino interface {
	// Registrar crea una cuenta nueva. Falla si el usuario ya existe.
	Registrar(usuario string) (Cuenta, error)
	// Login emite un token de sesión nuevo para una cuenta ya registrada.
	// No hay contraseña: es el modelo mínimo que pide hoy el protocolo
	// (ver docs/protocolo.md, el bot manda el token en el saludo). Reforzar
	// la autenticación queda fuera de este alcance hasta que haya una
	// especificación que lo pida.
	Login(usuario string) (token string, err error)
	// ValidarToken traduce un token a la identidad de la cuenta. Tiene
	// exactamente la forma de mesa.ValidarToken (docs/interfaces.md §4),
	// así que un *motor se puede inyectar directo en mesa.NuevoServidor.
	ValidarToken(token string) (idCuenta, nombre string, err error)

	// RegistrarBot da de alta una nueva versión de un bot para una cuenta.
	RegistrarBot(idCuenta, nombre, version string) (Bot, error)
	// ListarBots devuelve las versiones de bots subidas por una cuenta, en
	// el orden en que se registraron.
	ListarBots(idCuenta string) ([]Bot, error)

	// RegistrarResultado suma el resultado de una partida ya jugada a las
	// estadísticas históricas de cada cuenta involucrada.
	RegistrarResultado(resultado ResultadoPartida) error
	// Estadisticas devuelve las estadísticas acumuladas de una cuenta.
	Estadisticas(idCuenta string) (Estadisticas, error)
	// Ranking devuelve las estadísticas de todas las cuentas, ordenadas de
	// mayor a menor puntaje.
	Ranking() ([]Estadisticas, error)

	// Cuenta devuelve los datos de una cuenta por su ID.
	Cuenta(idCuenta string) (Cuenta, error)
}

// nuevoID genera un identificador aleatorio con el prefijo dado (por
// ejemplo "c" para cuentas, "tok" para tokens). No es secuencial a
// propósito: evita que alguien adivine cuántas cuentas hay contando IDs.
func nuevoID(prefijo string) string {
	var b [16]byte
	// La fuente de aleatoriedad es la misma que usa el Crupier para
	// barajar (crypto/rand): un ID de sesión no debería ser adivinable.
	_, err := rand.Read(b[:])
	if err != nil {
		// crypto/rand solo falla si el sistema operativo no puede darnos
		// aleatoriedad, algo tan grave que no tiene sentido seguir.
		panic("casino: no se pudo generar un identificador aleatorio: " + err.Error())
	}
	return prefijo + "-" + hex.EncodeToString(b[:])
}
