// Este archivo agrega los resultados de los enfrentamientos en la tabla final
// del torneo.
package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// FilaClasificacion es la línea de un participante en la tabla.
type FilaClasificacion struct {
	Posicion       int     `json:"posicion"`
	IDParticipante string  `json:"id_participante"`
	Nombre         string  `json:"nombre"`
	Puntos         float64 `json:"puntos"`

	Jugados  int `json:"enfrentamientos_jugados"`
	Ganados  int `json:"enfrentamientos_ganados"`
	Perdidos int `json:"enfrentamientos_perdidos"`
	// NoJugados son los enfrentamientos que le tocaban pero no se pudieron
	// jugar (su bot no arrancó, o no lo hizo el del rival). Se cuentan aparte
	// para poder distinguir "jugó mal" de "no jugó".
	NoJugados int `json:"enfrentamientos_no_jugados"`

	FichasNetas  int64  `json:"fichas_netas"`
	Timeouts     uint64 `json:"timeouts"`
	ManosJugadas uint64 `json:"manos_jugadas"`
}

// Clasificacion es la tabla final del torneo.
type Clasificacion struct {
	Torneo string              `json:"torneo"`
	Unidad string              `json:"unidad"`
	Filas  []FilaClasificacion `json:"filas"`
	// Incidencias son los enfrentamientos que no se pudieron jugar, con su
	// motivo. Van en la clasificación y no solo en el log porque un torneo con
	// partidas caídas no se puede publicar como si nada hubiera pasado.
	Incidencias []Incidencia `json:"incidencias,omitempty"`
}

// Incidencia es un enfrentamiento que no se jugó.
type Incidencia struct {
	IDEnfrentamiento string   `json:"id_enfrentamiento"`
	Participantes    []string `json:"participantes"`
	Motivo           string   `json:"motivo"`
}

// Clasificar agrega los resultados usando el puntuador dado.
//
// Un participante que no jugó ningún enfrentamiento aparece igual en la tabla,
// en el fondo y con sus NoJugados contados: si el bot de alguien nunca arrancó,
// eso tiene que verse en el resultado publicado, no desaparecer.
func Clasificar(cfg ConfigTorneo, puntuador Puntuador, resultados []ResultadoEnfrentamiento) Clasificacion {
	filas := make(map[string]*FilaClasificacion, len(cfg.Participantes))
	for _, p := range cfg.Participantes {
		filas[p.ID] = &FilaClasificacion{IDParticipante: p.ID, Nombre: p.NombreVisible()}
	}

	clasificacion := Clasificacion{Torneo: cfg.Nombre, Unidad: puntuador.Unidad()}

	for _, r := range resultados {
		if !r.Jugado() {
			clasificacion.Incidencias = append(clasificacion.Incidencias, Incidencia{
				IDEnfrentamiento: r.ID,
				Participantes:    r.Participantes,
				Motivo:           motivoDe(r),
			})
			for _, id := range r.Participantes {
				if fila, ok := filas[id]; ok {
					fila.NoJugados++
				}
			}
			continue
		}

		for _, puesto := range r.Puestos {
			fila, ok := filas[puesto.IDParticipante]
			if !ok {
				// Un puesto de alguien que no está inscrito: no debería pasar,
				// pero se agrega en vez de descartarlo en silencio.
				fila = &FilaClasificacion{IDParticipante: puesto.IDParticipante, Nombre: puesto.Nombre}
				filas[puesto.IDParticipante] = fila
			}
			fila.Puntos += puntuador.Puntos(r, puesto)
			fila.Jugados++
			if puesto.Posicion == 1 {
				fila.Ganados++
			} else {
				fila.Perdidos++
			}
			fila.FichasNetas += puesto.Neto
			fila.Timeouts += puesto.Timeouts
			fila.ManosJugadas += puesto.ManosJugadas
		}
	}

	clasificacion.Filas = make([]FilaClasificacion, 0, len(filas))
	for _, fila := range filas {
		clasificacion.Filas = append(clasificacion.Filas, *fila)
	}

	// Orden: puntos, después fichas netas, después menos timeouts, y por ID
	// para que el resultado sea determinista incluso con empate total.
	sort.Slice(clasificacion.Filas, func(i, j int) bool {
		a, b := clasificacion.Filas[i], clasificacion.Filas[j]
		switch {
		case a.Puntos != b.Puntos:
			return a.Puntos > b.Puntos
		case a.FichasNetas != b.FichasNetas:
			return a.FichasNetas > b.FichasNetas
		case a.Timeouts != b.Timeouts:
			return a.Timeouts < b.Timeouts
		default:
			return a.IDParticipante < b.IDParticipante
		}
	})
	for i := range clasificacion.Filas {
		clasificacion.Filas[i].Posicion = i + 1
	}

	return clasificacion
}

// motivoDe explica por qué un resultado no cuenta.
func motivoDe(r ResultadoEnfrentamiento) string {
	if r.Error != "" {
		return r.Error
	}
	return "la partida no produjo puestos"
}

// Guardar escribe la clasificación como JSON.
func (c Clasificacion) Guardar(ruta string) error {
	datos, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ruta, append(datos, '\n'), 0o644)
}

// Tabla arma la clasificación como texto alineado, para imprimir en la
// terminal o pegar en un mensaje.
func (c Clasificacion) Tabla() string {
	var b strings.Builder

	anchoNombre := len("participante")
	for _, f := range c.Filas {
		if len(f.Nombre) > anchoNombre {
			anchoNombre = len(f.Nombre)
		}
	}

	fmt.Fprintf(&b, "%-4s %-*s %12s %8s %8s %8s %12s %9s\n",
		"#", anchoNombre, "participante", c.Unidad, "jugados", "ganados", "perdidos", "fichas", "timeouts")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 4+1+anchoNombre+1+12+1+8+1+8+1+8+1+12+1+9))

	for _, f := range c.Filas {
		fmt.Fprintf(&b, "%-4d %-*s %12.0f %8d %8d %8d %+12d %9d\n",
			f.Posicion, anchoNombre, f.Nombre, f.Puntos, f.Jugados, f.Ganados, f.Perdidos, f.FichasNetas, f.Timeouts)
	}

	if len(c.Incidencias) > 0 {
		fmt.Fprintf(&b, "\n%d enfrentamiento(s) no se pudieron jugar:\n", len(c.Incidencias))
		for _, inc := range c.Incidencias {
			fmt.Fprintf(&b, "  %-28s %s\n", inc.IDEnfrentamiento, inc.Motivo)
		}
	}

	return b.String()
}

// GuardarResultados escribe los resultados de cada enfrentamiento como JSON
// Lines: una línea por partida, apta para procesar con jq o para cargar en
// una planilla.
func GuardarResultados(ruta string, resultados []ResultadoEnfrentamiento) (err error) {
	archivo, err := os.Create(ruta)
	if err != nil {
		return err
	}
	// El Close se hace en el defer y su error se propaga: en un archivo que se
	// escribe con buffer, el fallo de escritura aparece recién al cerrar.
	defer func() {
		if errCierre := archivo.Close(); err == nil {
			err = errCierre
		}
	}()

	codificador := json.NewEncoder(archivo)
	for _, r := range resultados {
		if err := codificador.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
