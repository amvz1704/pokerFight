// Este archivo define cómo un resultado de enfrentamiento se convierte en
// puntos, y el registro que permite agregar fórmulas nuevas sin tocar el
// runner ni el CLI.
package arena

import (
	"fmt"
	"sort"
	"sync"
)

// Puntuador convierte el puesto de un participante en un enfrentamiento en
// puntos para la clasificación. Es el segundo punto de extensión de la arena.
//
// Tiene que ser una función pura de (resultado, puesto): la clasificación se
// arma sumando, y una fórmula que dependa de estado acumulado haría que el
// orden en que terminan los enfrentamientos (que el paralelismo vuelve
// impredecible) cambie la tabla final.
type Puntuador interface {
	// Nombre es el identificador, el mismo con el que se registra.
	Nombre() string
	// Puntos son los que suma este puesto. Puede ser negativo.
	Puntos(r ResultadoEnfrentamiento, p Puesto) float64
	// Unidad es cómo se llama lo que devuelve Puntos, para los encabezados de
	// la tabla ("fichas", "puntos"...).
	Unidad() string
}

// ConstructorPuntuador arma un Puntuador a partir de la configuración.
type ConstructorPuntuador func(cfg ConfigTorneo) (Puntuador, error)

var (
	muPuntuadores sync.RWMutex
	puntuadores   = map[string]ConstructorPuntuador{}
)

// RegistrarPuntuador da de alta un puntuador bajo un nombre. Pensado para
// llamarse desde un init(). Panica si el nombre ya está tomado.
func RegistrarPuntuador(nombre string, constructor ConstructorPuntuador) {
	muPuntuadores.Lock()
	defer muPuntuadores.Unlock()
	if _, existe := puntuadores[nombre]; existe {
		panic("arena: el puntuador " + nombre + " ya está registrado")
	}
	puntuadores[nombre] = constructor
}

// BuscarPuntuador construye el puntuador registrado con ese nombre.
func BuscarPuntuador(nombre string, cfg ConfigTorneo) (Puntuador, error) {
	muPuntuadores.RLock()
	constructor, existe := puntuadores[nombre]
	muPuntuadores.RUnlock()
	if !existe {
		return nil, fmt.Errorf("arena: no existe la puntuación %q (disponibles: %v)", nombre, PuntuadoresDisponibles())
	}
	return constructor(cfg)
}

// PuntuadoresDisponibles lista los nombres registrados, ordenados.
func PuntuadoresDisponibles() []string {
	muPuntuadores.RLock()
	defer muPuntuadores.RUnlock()
	nombres := make([]string, 0, len(puntuadores))
	for n := range puntuadores {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	return nombres
}

func init() {
	RegistrarPuntuador(NombrePuntuacionNeto, func(cfg ConfigTorneo) (Puntuador, error) {
		return PuntuacionNeto{PenalizacionTimeout: cfg.PenalizacionTimeout}, nil
	})
	RegistrarPuntuador(NombrePuntuacionPuestos, func(cfg ConfigTorneo) (Puntuador, error) {
		return PuntuacionPuestos{
			PuntosPorRivalSuperado: 10,
			PenalizacionTimeout:    cfg.PenalizacionTimeout,
		}, nil
	})
	RegistrarPuntuador(NombrePuntuacionVictorias, func(cfg ConfigTorneo) (Puntuador, error) {
		return PuntuacionVictorias{}, nil
	})
}

// --- Neto en fichas --------------------------------------------------------

// NombrePuntuacionNeto es el nombre registrado de la puntuación por fichas.
const NombrePuntuacionNeto = "neto"

// PuntuacionNeto suma las fichas ganadas o perdidas en cada enfrentamiento.
// Es la puntuación por defecto y la que usa MIT Pokerbots: mide *cuánto* mejor
// jugaste, no solo si ganaste, así que un bot que gana ajustado no queda igual
// que uno que arrasa.
//
// Como todos los enfrentamientos arrancan con el mismo stack y duran las mismas
// manos, la suma de netos de todos los participantes da cero salvo por los
// enfrentamientos que no se pudieron jugar.
type PuntuacionNeto struct {
	// PenalizacionTimeout son las fichas que se descuentan por cada vez que la
	// mesa tuvo que aplicar la acción segura en lugar de la respuesta del bot.
	// 0 desactiva la penalización: un bot lento ya se castiga solo, porque
	// foldear cuando había que pagar cuesta fichas de verdad.
	PenalizacionTimeout float64
}

// Nombre implementa Puntuador.
func (PuntuacionNeto) Nombre() string { return NombrePuntuacionNeto }

// Unidad implementa Puntuador.
func (PuntuacionNeto) Unidad() string { return "fichas" }

// Puntos implementa Puntuador.
func (p PuntuacionNeto) Puntos(_ ResultadoEnfrentamiento, puesto Puesto) float64 {
	return float64(puesto.Neto) - float64(puesto.Timeouts)*p.PenalizacionTimeout
}

// --- Puntos por puesto -----------------------------------------------------

// NombrePuntuacionPuestos es el nombre registrado de la puntuación por puesto.
const NombrePuntuacionPuestos = "puestos"

// PuntuacionPuestos da puntos por cada rival que quedó por debajo, sin mirar
// por cuántas fichas. Es la fórmula que ya usa el Casino para su ranking
// histórico (internal/casino/puntaje.go) y la que tiene sentido en un formato
// de mesa única, donde el neto en fichas depende de una sola partida.
type PuntuacionPuestos struct {
	PuntosPorRivalSuperado float64
	PenalizacionTimeout    float64
}

// Nombre implementa Puntuador.
func (PuntuacionPuestos) Nombre() string { return NombrePuntuacionPuestos }

// Unidad implementa Puntuador.
func (PuntuacionPuestos) Unidad() string { return "puntos" }

// Puntos implementa Puntuador.
func (p PuntuacionPuestos) Puntos(r ResultadoEnfrentamiento, puesto Puesto) float64 {
	superados := len(r.Puestos) - puesto.Posicion
	if superados < 0 {
		superados = 0
	}
	return float64(superados)*p.PuntosPorRivalSuperado - float64(puesto.Timeouts)*p.PenalizacionTimeout
}

// --- Victorias -------------------------------------------------------------

// NombrePuntuacionVictorias es el nombre registrado de la puntuación por
// victorias.
const NombrePuntuacionVictorias = "victorias"

// PuntuacionVictorias da 1 punto por enfrentamiento ganado y 0 por el resto.
// Es la más fácil de explicar y la más ruidosa: dos bots casi iguales terminan
// decidiéndose por el azar de unas pocas manos. Está para exhibiciones y para
// mostrar la tabla a alguien que no quiere leer netos en fichas.
type PuntuacionVictorias struct{}

// Nombre implementa Puntuador.
func (PuntuacionVictorias) Nombre() string { return NombrePuntuacionVictorias }

// Unidad implementa Puntuador.
func (PuntuacionVictorias) Unidad() string { return "victorias" }

// Puntos implementa Puntuador.
func (PuntuacionVictorias) Puntos(_ ResultadoEnfrentamiento, puesto Puesto) float64 {
	if puesto.Posicion == 1 {
		return 1
	}
	return 0
}
