// Este archivo guarda el historial de manos del torneo. Existe por una razón
// concreta: cuando un participante dice "mi bot no pudo haber jugado eso", la
// única respuesta aceptable es un archivo con las cartas, las acciones y el
// reparto de esa mano exacta.
package arena

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/amvz1704/pokerFight/internal/mesa"
	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Historial entrega un observador de manos por enfrentamiento.
type Historial interface {
	// Observador devuelve el receptor de manos del enfrentamiento dado. Se
	// llama una vez por partida, antes de que empiece.
	Observador(idEnfrentamiento string) mesa.ObservadorMano
	// Cerrar vacía los buffers y cierra los archivos abiertos.
	Cerrar() error
}

// ManoRegistrada es una mano tal como queda en el historial. Es todo lo que
// hace falta para reconstruirla: quién estaba, con qué cartas, qué hizo cada
// uno y cómo se repartió el pozo.
type ManoRegistrada struct {
	Enfrentamiento string                       `json:"enfrentamiento"`
	IDMesa         string                       `json:"id_mesa"`
	IDMano         string                       `json:"id_mano"`
	Instante       time.Time                    `json:"instante"`
	Comunitarias   []protocolo.Carta            `json:"comunitarias"`
	Pozo           int64                        `json:"pozo"`
	Jugadores      []protocolo.JugadorPublico   `json:"jugadores"`
	Acciones       []protocolo.AccionRegistrada `json:"acciones"`
	Repartos       []protocolo.Reparto          `json:"repartos"`
	// Privadas son las cartas de todos los que jugaron la mano, incluidas las
	// de quienes se retiraron sin llegar al showdown. Es justamente lo que
	// hace auditable una mano que termino en fold.
	Privadas  map[string]protocolo.Mano `json:"privadas,omitempty"`
	Mostradas map[string]protocolo.Mano `json:"mostradas,omitempty"`
	Jugadas   map[string]string         `json:"jugadas,omitempty"`
}

// --- Historial en archivos -------------------------------------------------

// HistorialArchivos escribe un archivo JSON Lines por enfrentamiento dentro de
// un directorio: una línea por mano.
//
// Se eligió un archivo por partida y no uno solo para todo el torneo porque
// los enfrentamientos corren en paralelo: con un archivo compartido, o se
// serializa la escritura (y se frenan las partidas entre sí) o se mezclan las
// manos de distintas mesas.
type HistorialArchivos struct {
	directorio string

	mu       sync.Mutex
	abiertos []*escritorMano
}

// Verificación en tiempo de compilación de que cumple el contrato.
var _ Historial = (*HistorialArchivos)(nil)

// NuevoHistorialArchivos crea (si hace falta) el directorio y devuelve el
// historial.
func NuevoHistorialArchivos(directorio string) (*HistorialArchivos, error) {
	if err := os.MkdirAll(directorio, 0o755); err != nil {
		return nil, err
	}
	return &HistorialArchivos{directorio: directorio}, nil
}

// Observador implementa Historial.
func (h *HistorialArchivos) Observador(idEnfrentamiento string) mesa.ObservadorMano {
	// sanear porque el ID viene del archivo de configuración del torneo y acá
	// se convierte en una ruta.
	ruta := filepath.Join(h.directorio, sanear(idEnfrentamiento)+".jsonl")

	archivo, err := os.Create(ruta)
	if err != nil {
		// Un historial que no se puede escribir no debe tumbar el torneo: se
		// devuelve un observador que descarta. El error se ve igual, porque el
		// archivo esperado no aparece en el directorio de salida.
		return observadorNulo{}
	}

	e := &escritorMano{
		enfrentamiento: idEnfrentamiento,
		archivo:        archivo,
		escritor:       bufio.NewWriter(archivo),
	}

	h.mu.Lock()
	h.abiertos = append(h.abiertos, e)
	h.mu.Unlock()
	return e
}

// Cerrar implementa Historial.
func (h *HistorialArchivos) Cerrar() error {
	h.mu.Lock()
	abiertos := h.abiertos
	h.abiertos = nil
	h.mu.Unlock()

	var primero error
	for _, e := range abiertos {
		if err := e.cerrar(); err != nil && primero == nil {
			primero = err
		}
	}
	return primero
}

// escritorMano escribe las manos de un enfrentamiento.
type escritorMano struct {
	enfrentamiento string

	mu       sync.Mutex
	archivo  *os.File
	escritor *bufio.Writer
}

// ManoTerminada implementa mesa.ObservadorMano.
//
// La Mesa la llama de forma síncrona entre mano y mano, así que se escribe con
// buffer: hacer un write al sistema de archivos por mano frenaría la partida
// sin necesidad. El flush ocurre al cerrar el historial.
func (e *escritorMano) ManoTerminada(mano mesa.ManoJugada) {
	registro := ManoRegistrada{
		Enfrentamiento: e.enfrentamiento,
		IDMesa:         mano.IDMesa,
		IDMano:         mano.IDMano,
		Instante:       time.Now().UTC(),
		Comunitarias:   mano.Resultado.Comunitarias,
		Pozo:           mano.Estado.Pozo,
		Jugadores:      mano.Estado.Jugadores,
		Acciones:       mano.Estado.HistorialAcciones,
		Repartos:       mano.Resultado.Repartos,
		Privadas:       mano.Privadas,
		Mostradas:      mano.Resultado.Mostradas,
		Jugadas:        mano.Resultado.Descripcion,
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.escritor == nil {
		return
	}
	if datos, err := json.Marshal(registro); err == nil {
		e.escritor.Write(datos)
		e.escritor.WriteByte('\n')
	}
}

func (e *escritorMano) cerrar() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.escritor == nil {
		return nil
	}
	err := e.escritor.Flush()
	e.escritor = nil
	if errCierre := e.archivo.Close(); err == nil {
		err = errCierre
	}
	return err
}

// --- Sin historial ---------------------------------------------------------

// observadorNulo descarta todo. Es lo que se usa cuando el historial está
// apagado o cuando no se pudo abrir el archivo.
type observadorNulo struct{}

func (observadorNulo) ManoTerminada(mesa.ManoJugada) {}
