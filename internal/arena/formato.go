// Este archivo define cómo se arman los emparejamientos de un torneo y el
// registro que permite agregar formatos nuevos sin tocar el runner ni el CLI.
package arena

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// Formato decide qué se juega contra qué. Es el primer punto de extensión de
// la arena: para agregar un sistema suizo, una eliminación directa o mesas de
// 6 jugadores, alcanza con implementar esta interfaz y registrarla.
type Formato interface {
	// Nombre es el identificador del formato, el mismo con el que se registra
	// y con el que lo elige el archivo de configuración.
	Nombre() string
	// Emparejamientos devuelve todos los enfrentamientos del torneo, ya con su
	// ID, su semilla y su límite de manos.
	//
	// Se llama una sola vez, antes de que se juegue nada: la arena no soporta
	// hoy formatos donde la ronda 2 dependa del resultado de la ronda 1. Si
	// hiciera falta (eliminación directa), el camino es agregar un método
	// opcional a esta interfaz, no cambiar la que ya existe.
	Emparejamientos(cfg ConfigTorneo) ([]Enfrentamiento, error)
}

// ConstructorFormato arma un Formato a partir de la configuración del torneo.
type ConstructorFormato func(cfg ConfigTorneo) (Formato, error)

var (
	muFormatos sync.RWMutex
	formatos   = map[string]ConstructorFormato{}
)

// RegistrarFormato da de alta un formato bajo un nombre. Pensado para
// llamarse desde un init(). Panica si el nombre ya está tomado: un choque de
// nombres es un error de programación, y descubrirlo al arrancar es mejor que
// descubrirlo cuando el torneo eligió el formato equivocado.
func RegistrarFormato(nombre string, constructor ConstructorFormato) {
	muFormatos.Lock()
	defer muFormatos.Unlock()
	if _, existe := formatos[nombre]; existe {
		panic("arena: el formato " + nombre + " ya está registrado")
	}
	formatos[nombre] = constructor
}

// BuscarFormato construye el formato registrado con ese nombre.
func BuscarFormato(nombre string, cfg ConfigTorneo) (Formato, error) {
	muFormatos.RLock()
	constructor, existe := formatos[nombre]
	muFormatos.RUnlock()
	if !existe {
		return nil, fmt.Errorf("arena: no existe el formato %q (disponibles: %v)", nombre, FormatosDisponibles())
	}
	return constructor(cfg)
}

// FormatosDisponibles lista los nombres registrados, ordenados.
func FormatosDisponibles() []string {
	muFormatos.RLock()
	defer muFormatos.RUnlock()
	nombres := make([]string, 0, len(formatos))
	for n := range formatos {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	return nombres
}

func init() {
	RegistrarFormato(NombreRoundRobin, func(cfg ConfigTorneo) (Formato, error) {
		return RoundRobin{JugadoresPorMesa: 2}, nil
	})
	RegistrarFormato(NombreMesaUnica, func(cfg ConfigTorneo) (Formato, error) {
		return MesaUnica{}, nil
	})
}

// --- Round-robin heads-up --------------------------------------------------

// NombreRoundRobin es el nombre registrado del formato round-robin.
const NombreRoundRobin = "round-robin"

// RoundRobin enfrenta a cada participante contra cada otro. Es el formato del
// torneo: es el que menos depende de la suerte del sorteo, porque todos juegan
// contra todos la misma cantidad de manos.
//
// Con IdaYVuelta activo (lo normal), cada par juega dos partidas intercambiando
// las sillas y **con la misma semilla**. Eso significa que los dos bots reciben
// exactamente las mismas cartas en las mismas posiciones, una vez cada uno: si
// A le gana a B en las dos, le ganó jugando mejor, no porque le tocaran mejores
// manos. Es la misma reducción de varianza que usa MIT Pokerbots.
type RoundRobin struct {
	// JugadoresPorMesa es cuántos se sientan en cada enfrentamiento. 2 es
	// heads-up (lo normal); con más, se generan todas las combinaciones de ese
	// tamaño, que crecen rápido: con 8 participantes y mesas de 6 son 28
	// partidas.
	JugadoresPorMesa int
}

// Nombre implementa Formato.
func (RoundRobin) Nombre() string { return NombreRoundRobin }

// Emparejamientos implementa Formato.
func (f RoundRobin) Emparejamientos(cfg ConfigTorneo) ([]Enfrentamiento, error) {
	porMesa := f.JugadoresPorMesa
	if porMesa < 2 {
		porMesa = 2
	}
	if len(cfg.Participantes) < porMesa {
		return nil, fmt.Errorf("arena: el formato round-robin con mesas de %d necesita al menos %d participantes, hay %d",
			porMesa, porMesa, len(cfg.Participantes))
	}

	ids := cfg.IDsParticipantes()
	var enfrentamientos []Enfrentamiento
	ronda := 0

	combinaciones(ids, porMesa, func(grupo []string) {
		ronda++
		// La semilla sale del grupo y de la semilla base del torneo, no de un
		// contador: así el reparto de un emparejamiento no cambia si mañana se
		// agrega un participante nuevo al archivo de configuración.
		semilla := semillaDe(cfg.Semilla, grupo)

		participantes := append([]string(nil), grupo...)
		enfrentamientos = append(enfrentamientos, Enfrentamiento{
			ID:            fmt.Sprintf("r%03d-%s", ronda, unirIDs(participantes)),
			Ronda:         ronda,
			Participantes: participantes,
			Semilla:       semilla,
			Manos:         cfg.ManosPorEnfrentamiento,
		})

		if !cfg.IdaYVuelta {
			return
		}
		// La vuelta: mismas cartas, sillas rotadas. Con 2 jugadores es
		// simplemente el orden invertido; con más, una rotación alcanza para
		// que nadie repita posición respecto del botón.
		vuelta := rotar(participantes)
		enfrentamientos = append(enfrentamientos, Enfrentamiento{
			ID:            fmt.Sprintf("r%03d-%s-vuelta", ronda, unirIDs(participantes)),
			Ronda:         ronda,
			Participantes: vuelta,
			Semilla:       semilla,
			Manos:         cfg.ManosPorEnfrentamiento,
		})
	})

	return enfrentamientos, nil
}

// --- Mesa única ------------------------------------------------------------

// NombreMesaUnica es el nombre registrado del formato de mesa única.
const NombreMesaUnica = "mesa-unica"

// MesaUnica sienta a todos los participantes en una sola mesa y juega hasta
// que quede uno (o hasta el límite de manos). Es el formato "sit and go":
// más espectacular y mucho más ruidoso que el round-robin, porque el resultado
// depende de una sola partida. Sirve para una final o una exhibición, no para
// medir a quién programó mejor.
type MesaUnica struct{}

// Nombre implementa Formato.
func (MesaUnica) Nombre() string { return NombreMesaUnica }

// Emparejamientos implementa Formato.
func (MesaUnica) Emparejamientos(cfg ConfigTorneo) ([]Enfrentamiento, error) {
	ids := cfg.IDsParticipantes()
	if len(ids) < 2 {
		return nil, ErrSinParticipantes
	}
	if len(ids) > 8 {
		return nil, fmt.Errorf("arena: el formato mesa-unica admite hasta 8 jugadores, hay %d", len(ids))
	}
	return []Enfrentamiento{{
		ID:            "final",
		Ronda:         1,
		Participantes: ids,
		Semilla:       semillaDe(cfg.Semilla, ids),
		Manos:         cfg.ManosPorEnfrentamiento,
	}}, nil
}

// --- Ayudas ----------------------------------------------------------------

// combinaciones invoca fn con cada subconjunto de tamaño k de ids, en orden
// lexicográfico sobre las posiciones. El slice que recibe fn se reutiliza:
// copiarlo si hace falta guardarlo.
func combinaciones(ids []string, k int, fn func([]string)) {
	n := len(ids)
	if k > n {
		return
	}
	grupo := make([]string, k)
	var recorrer func(inicio, puestos int)
	recorrer = func(inicio, puestos int) {
		if puestos == k {
			fn(grupo)
			return
		}
		for i := inicio; i <= n-(k-puestos); i++ {
			grupo[puestos] = ids[i]
			recorrer(i+1, puestos+1)
		}
	}
	recorrer(0, 0)
}

// rotar devuelve una copia con los elementos corridos una posición: el primero
// pasa al final. Es lo que intercambia las sillas entre la ida y la vuelta.
func rotar(ids []string) []string {
	if len(ids) < 2 {
		return append([]string(nil), ids...)
	}
	rotados := make([]string, 0, len(ids))
	rotados = append(rotados, ids[1:]...)
	rotados = append(rotados, ids[0])
	return rotados
}

// semillaDe deriva la semilla de un enfrentamiento a partir de la semilla base
// del torneo y de quiénes juegan. Es determinista y no depende del orden de
// las sillas, así que la ida y la vuelta de un emparejamiento comparten
// semilla y por lo tanto comparten repartos.
func semillaDe(base uint64, ids []string) uint64 {
	ordenados := append([]string(nil), ids...)
	sort.Strings(ordenados)

	h := fnv.New64a()
	fmt.Fprintf(h, "%d", base)
	for _, id := range ordenados {
		h.Write([]byte{0})
		h.Write([]byte(id))
	}
	return h.Sum64()
}

// unirIDs arma un fragmento de nombre de archivo a partir de los IDs.
func unirIDs(ids []string) string {
	texto := ""
	for i, id := range ids {
		if i > 0 {
			texto += "-vs-"
		}
		texto += sanear(id)
	}
	return texto
}

// sanear deja solo caracteres seguros para un nombre de archivo. Los IDs de
// participante vienen de un archivo de configuración que puede tener
// cualquier cosa, y con ese ID se arma el nombre del historial de manos: sin
// esto, un ID como "../../x" escribiría fuera del directorio de salida.
func sanear(s string) string {
	saneado := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			saneado = append(saneado, r)
		default:
			saneado = append(saneado, '_')
		}
	}
	if len(saneado) == 0 {
		return "_"
	}
	return string(saneado)
}
