package campeonato

import (
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

// Etapa es en qué punto está el campeonato.
type Etapa string

const (
	// EtapaInscripcion es antes de jugar la primera ronda. La configuración
	// todavía se puede cambiar.
	EtapaInscripcion Etapa = "inscripcion"
	// EtapaEnCurso es con al menos una ronda jugada y fases por delante.
	EtapaEnCurso Etapa = "en_curso"
	// EtapaTerminado es cuando ya no queda nada por jugar.
	EtapaTerminado Etapa = "terminado"
)

// Motivos por los que un jugador deja de competir.
const (
	MotivoQuiebra     = "quiebra"
	MotivoNoClasifico = "no_clasifico"
	MotivoEliminado   = "eliminado"
	MotivoCampeon     = "campeon"
)

// JugadorEstado es todo lo que el campeonato sabe de un bot que compite.
type JugadorEstado struct {
	// Alias es el identificador aleatorio con el que lo conocen las mesas.
	Alias string `json:"alias"`
	// ID es el de su inscripción. No sale de la organización.
	ID string `json:"id"`

	Fichas       uint64 `json:"fichas"`
	Timeouts     uint64 `json:"timeouts"`
	ManosJugadas uint64 `json:"manos_jugadas"`

	// Salio es la fase en la que dejó de competir, o -1 si sigue.
	Salio int `json:"salio_en"`
	// Motivo es por qué dejó de competir; vacío si sigue.
	Motivo string `json:"motivo,omitempty"`
	// Puesto es dónde terminó en su última mesa (en la clasificatoria, en la
	// tabla general al cerrar la fase). 0 mientras no haya terminado.
	Puesto int `json:"puesto,omitempty"`
}

// EnJuego indica si el jugador sigue compitiendo.
func (j *JugadorEstado) EnJuego() bool { return j.Salio < 0 }

// ResultadoMesa es cómo le fue a un jugador en una mesa de una ronda.
type ResultadoMesa struct {
	Alias        string `json:"alias"`
	FichasInicio uint64 `json:"fichas_inicio"`
	FichasFin    uint64 `json:"fichas_fin"`
	Posicion     int    `json:"posicion"`
	Timeouts     uint64 `json:"timeouts"`
}

// MesaJugada es una mesa de una ronda ya terminada.
type MesaJugada struct {
	ID        string          `json:"id"`
	Manos     uint64          `json:"manos"`
	Jugadores []ResultadoMesa `json:"jugadores"`
}

// RondaJugada es una ronda terminada.
type RondaJugada struct {
	Fase   int          `json:"fase"`
	Ronda  int          `json:"ronda"`
	Mesas  []MesaJugada `json:"mesas"`
	Inicio time.Time    `json:"inicio"`
	Fin    time.Time    `json:"fin"`
}

// Estado es lo que se guarda en disco después de cada ronda. Con esto se puede
// retomar el campeonato tras apagar la arena: la clasificatoria y la final de las
// bases se juegan en días distintos.
type Estado struct {
	Etapa Etapa `json:"etapa"`

	// Config es la configuración con la que se jugó. Se fija al empezar la
	// primera ronda y desde entonces manda ella, no el archivo: si alguien lo
	// edita a mitad de campeonato, las fases ya jugadas dejarían de coincidir
	// con las que dice el archivo.
	Config *Config `json:"config,omitempty"`

	// Fase es el índice de la fase en curso, y RondasJugadas cuántas rondas de
	// ella ya terminaron.
	Fase          int `json:"fase"`
	RondasJugadas int `json:"rondas_jugadas"`
	// EntraronFase es cuántos jugadores arrancaron la fase en curso. Hace falta
	// para decidir cuántos pasan cuando la cantidad es automática.
	EntraronFase int `json:"entraron_fase"`

	// Vivos son los alias que juegan la fase en curso, ordenados.
	Vivos []string `json:"vivos"`

	Jugadores map[string]*JugadorEstado `json:"jugadores"`
	Rondas    []RondaJugada             `json:"rondas"`

	// Campeon es el alias del ganador, cuando el campeonato terminó con uno.
	Campeon string `json:"campeon,omitempty"`
}

// nuevoEstado arma el estado inicial: un alias aleatorio para cada inscrito.
//
// Con semilla, los alias también salen de ella. Parece un detalle y no lo es: el
// sorteo de mesas baraja a los jugadores en orden de alias, así que con alias
// distintos en cada corrida el mismo sorteo con la misma semilla armaría mesas
// distintas, y "fijar la semilla repite el campeonato" dejaría de ser cierto.
func nuevoEstado(inscritos Inscritos, semilla *uint64) (*Estado, error) {
	e := &Estado{
		Etapa:     EtapaInscripcion,
		Jugadores: make(map[string]*JugadorEstado, len(inscritos)),
	}
	azar := NuevoAzar(semilla, aliasSal)
	for _, in := range inscritos {
		if err := e.agregarJugador(in, azar); err != nil {
			return nil, err
		}
	}
	e.Vivos = e.todosLosAlias()
	e.EntraronFase = len(e.Vivos)
	return e, nil
}

// aliasSal separa la secuencia de alias de las demás que salen de la semilla
// (sorteos, mazos): sin esto compartirían números y estarían correlacionados.
const aliasSal = 1 << 20

// agregarJugador le asigna un alias nuevo a un inscrito.
func (e *Estado) agregarJugador(in Inscrito, azar *rand.Rand) error {
	alias, err := e.aliasLibre(azar)
	if err != nil {
		return err
	}
	e.Jugadores[alias] = &JugadorEstado{Alias: alias, ID: in.ID, Salio: -1}
	return nil
}

// aliasLibre genera un alias aleatorio que no esté en uso. No se deriva del
// inscrito ni de nada que lo identifique: es lo que se le muestra a los demás
// bots, y tiene que servir para distinguir jugadores pero no para reconocer
// equipos.
func (e *Estado) aliasLibre(azar *rand.Rand) (string, error) {
	for intento := 0; intento < 100; intento++ {
		n := azar.Uint64()
		b := [3]byte{byte(n), byte(n >> 8), byte(n >> 16)}
		alias := "j-" + hex.EncodeToString(b[:])
		if _, existe := e.Jugadores[alias]; !existe {
			return alias, nil
		}
	}
	return "", fmt.Errorf("campeonato: no se pudo generar un alias libre")
}

func (e *Estado) todosLosAlias() []string {
	alias := make([]string, 0, len(e.Jugadores))
	for a := range e.Jugadores {
		alias = append(alias, a)
	}
	sort.Strings(alias)
	return alias
}

// aliasPorInscrito devuelve el alias de cada inscrito.
func (e *Estado) aliasPorInscrito() map[string]string {
	m := make(map[string]string, len(e.Jugadores))
	for alias, j := range e.Jugadores {
		m[j.ID] = alias
	}
	return m
}

// conciliar ajusta el estado cargado de disco a la lista de inscritos actual.
//
// Antes de jugar se aceptan altas y bajas (la inscripción cierra recién unos
// días antes). Una vez empezado, la lista es la que es: agregar o sacar a
// alguien a mitad de campeonato cambiaría los resultados ya jugados.
func (e *Estado) conciliar(inscritos Inscritos, semilla *uint64) error {
	enLista := make(map[string]bool, len(inscritos))
	for _, in := range inscritos {
		enLista[in.ID] = true
	}
	conAlias := e.aliasPorInscrito()

	if e.Etapa != EtapaInscripcion {
		for _, in := range inscritos {
			if _, ok := conAlias[in.ID]; !ok {
				return fmt.Errorf("campeonato: %q no estaba inscrito cuando empezó el campeonato y no se puede sumar ahora", in.ID)
			}
		}
		for id := range conAlias {
			if !enLista[id] {
				return fmt.Errorf("campeonato: %q compite pero ya no figura en la lista de inscritos", id)
			}
		}
		return nil
	}

	for alias, j := range e.Jugadores {
		if !enLista[j.ID] {
			delete(e.Jugadores, alias)
		}
	}
	azar := NuevoAzar(semilla, aliasSal, len(e.Jugadores))
	for _, in := range inscritos {
		if _, ok := conAlias[in.ID]; !ok {
			if err := e.agregarJugador(in, azar); err != nil {
				return err
			}
		}
	}
	e.Vivos = e.todosLosAlias()
	e.EntraronFase = len(e.Vivos)
	return nil
}
