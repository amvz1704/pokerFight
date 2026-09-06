// Este archivo define la configuración de un torneo y su formato en disco.
// Un torneo es un archivo JSON: eso es lo que se versiona, lo que se comparte
// con los participantes y lo que hace reproducible una edición.
package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ConfigTorneo es todo lo que define una edición del torneo.
type ConfigTorneo struct {
	// Nombre identifica la edición. Aparece en los reportes.
	Nombre string `json:"nombre"`
	// Formato es el nombre de un formato registrado (ver FormatosDisponibles).
	Formato string `json:"formato"`
	// Puntuacion es el nombre de un puntuador registrado (ver
	// PuntuadoresDisponibles).
	Puntuacion string `json:"puntuacion"`

	// ManosPorEnfrentamiento es cuántas manos dura cada partida. 0 significa
	// jugar hasta que quede un solo jugador con fichas.
	//
	// Es el parámetro que más define la calidad del torneo: con pocas manos
	// gana el que tuvo suerte. Para un round-robin heads-up, menos de 200
	// manos por par mide más el azar que la habilidad.
	ManosPorEnfrentamiento int64 `json:"manos_por_enfrentamiento"`

	// IdaYVuelta hace que cada emparejamiento se juegue dos veces con las
	// sillas intercambiadas y las mismas cartas. Recomendado: cancela la
	// ventaja posicional y buena parte de la suerte del reparto.
	IdaYVuelta bool `json:"ida_y_vuelta"`

	// ReponerStack devuelve a todos al stack inicial antes de cada mano y
	// puntúa por fichas acumuladas. Es lo que hace que
	// ManosPorEnfrentamiento signifique algo: sin reposición, la partida
	// termina en cuanto alguien quiebra (muchas veces en 30 manos de 200) y
	// el resultado se satura en ±stack, midiendo quién quebró primero en vez
	// de cuánto mejor juega cada uno.
	//
	// Se recomienda para round-robin. Para mesa-unica hay que dejarlo en
	// false: ahí la gracia es justamente eliminar jugadores.
	ReponerStack bool `json:"reponer_stack"`

	// Semilla es la base de la que se derivan las semillas de cada partida.
	// Fijarla hace el torneo entero reproducible. 0 también es una semilla
	// válida y perfectamente reproducible.
	Semilla uint64 `json:"semilla"`

	// Paralelismo es cuántos enfrentamientos se juegan a la vez. Cada uno
	// abre un puerto y lanza sus bots, así que subirlo acorta mucho un
	// round-robin. Ojo con los bots que consumen CPU: si se pisan entre
	// ellos, empiezan a aparecer timeouts que no son culpa de nadie.
	// 0 o menos se trata como 1.
	Paralelismo int `json:"paralelismo"`

	// PenalizacionTimeout es cuánto descuenta cada timeout, en las unidades
	// del puntuador elegido.
	PenalizacionTimeout float64 `json:"penalizacion_timeout"`

	// Mesa son los parámetros de juego compartidos por todas las partidas.
	Mesa ConfigMesa `json:"mesa"`

	// Participantes son los bots inscritos.
	Participantes []Participante `json:"participantes"`

	// Salida es el directorio donde se escriben resultados, clasificación e
	// historiales. Vacío = "resultados".
	Salida string `json:"salida"`

	// GuardarHistorial activa el historial de manos en JSON Lines. Ocupa
	// bastante en un round-robin largo (una línea por mano y partida), pero es
	// la única forma de auditar un reclamo del estilo "mi bot no pudo haber
	// perdido esa mano".
	GuardarHistorial bool `json:"guardar_historial"`
}

// CargarConfig lee un torneo desde un archivo JSON y lo valida.
func CargarConfig(ruta string) (ConfigTorneo, error) {
	var cfg ConfigTorneo

	datos, err := os.ReadFile(ruta)
	if err != nil {
		return cfg, fmt.Errorf("arena: no se pudo leer %s: %w", ruta, err)
	}

	decodificador := json.NewDecoder(strings.NewReader(string(datos)))
	// Un campo con el nombre mal escrito en el JSON se ignoraría en silencio y
	// el torneo correría con otra configuración que la que su autor cree.
	// Preferimos fallar al arrancar.
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("arena: %s no es una configuración de torneo válida: %w", ruta, err)
	}

	if err := cfg.Validar(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Guardar escribe la configuración en un archivo JSON. Sirve para dejar junto
// a los resultados la configuración exacta con la que se corrió la edición,
// valores por defecto ya resueltos incluidos.
func (c ConfigTorneo) Guardar(ruta string) error {
	datos, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ruta, append(datos, '\n'), 0o644)
}

// Validar completa los valores por defecto y verifica que la configuración
// tenga sentido. Modifica el receptor, así que se llama sobre un puntero.
func (c *ConfigTorneo) Validar() error {
	if c.Nombre == "" {
		c.Nombre = "torneo"
	}
	if c.Formato == "" {
		c.Formato = NombreRoundRobin
	}
	if c.Puntuacion == "" {
		c.Puntuacion = NombrePuntuacionNeto
	}
	if c.Salida == "" {
		c.Salida = "resultados"
	}
	if c.Paralelismo <= 0 {
		c.Paralelismo = 1
	}
	if err := c.Mesa.PorDefecto(); err != nil {
		return err
	}

	// Con reposición no hay eliminación posible, así que una partida sin
	// límite de manos no terminaría nunca.
	if c.ReponerStack && c.ManosPorEnfrentamiento <= 0 {
		return fmt.Errorf("arena: reponer_stack necesita manos_por_enfrentamiento > 0, si no la partida no termina nunca")
	}

	if len(c.Participantes) < 2 {
		return ErrSinParticipantes
	}

	vistos := make(map[string]bool, len(c.Participantes))
	for i, p := range c.Participantes {
		if p.ID == "" {
			return fmt.Errorf("arena: el participante #%d no tiene id", i+1)
		}
		if vistos[p.ID] {
			return fmt.Errorf("arena: el id de participante %q está repetido", p.ID)
		}
		vistos[p.ID] = true
	}

	// Dos participantes con el mismo token serían el mismo jugador para la
	// mesa: el segundo en conectarse se tomaría como una reconexión del
	// primero y el enfrentamiento nunca llegaría al mínimo de jugadores. Es un
	// error difícil de diagnosticar en caliente, así que se corta acá.
	porToken := make(map[string]string, len(c.Participantes))
	for _, p := range c.Participantes {
		token := p.TokenEfectivo()
		if otro, existe := porToken[token]; existe {
			return fmt.Errorf("arena: los participantes %q y %q comparten el token %q", otro, p.ID, token)
		}
		porToken[token] = p.ID
	}

	return nil
}

// IDsParticipantes devuelve los IDs en orden alfabético. El orden es
// determinista a propósito: los emparejamientos y sus semillas se derivan de
// él, así que el mismo archivo de configuración tiene que producir siempre el
// mismo torneo.
func (c ConfigTorneo) IDsParticipantes() []string {
	ids := make([]string, 0, len(c.Participantes))
	for _, p := range c.Participantes {
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	return ids
}

// BuscarParticipante devuelve el participante con ese ID.
func (c ConfigTorneo) BuscarParticipante(id string) (Participante, error) {
	for _, p := range c.Participantes {
		if p.ID == id {
			return p, nil
		}
	}
	return Participante{}, fmt.Errorf("arena: no hay ningún participante con id %q", id)
}

// BuscarParticipantes resuelve una lista de IDs, conservando el orden (que es
// el orden de sillas del enfrentamiento).
func (c ConfigTorneo) BuscarParticipantes(ids []string) ([]Participante, error) {
	participantes := make([]Participante, 0, len(ids))
	for _, id := range ids {
		p, err := c.BuscarParticipante(id)
		if err != nil {
			return nil, err
		}
		participantes = append(participantes, p)
	}
	return participantes, nil
}

// ConfigEjemplo devuelve una configuración de torneo lista para editar, con
// los dos bots de sparring del repositorio. Es lo que escribe
// `arena init`.
func ConfigEjemplo() ConfigTorneo {
	return ConfigTorneo{
		Nombre:                 "torneo-local",
		Formato:                NombreRoundRobin,
		Puntuacion:             NombrePuntuacionNeto,
		ManosPorEnfrentamiento: 200,
		IdaYVuelta:             true,
		ReponerStack:           true,
		Semilla:                20260906,
		Paralelismo:            2,
		PenalizacionTimeout:    0,
		GuardarHistorial:       true,
		Salida:                 "resultados",
		Mesa: ConfigMesa{
			CiegaChica:        10,
			CiegaGrande:       20,
			Stack:             4000,
			TimeoutMs:         2000,
			EsperaJugadoresMs: 15000,
		},
		// Los comandos apuntan a los binarios que deja `make build`. En Windows
		// no hace falta escribir el .exe: os/exec lo agrega solo.
		Participantes: []Participante{
			{ID: "aleatorio", Nombre: "Bot Aleatorio", Comando: []string{"./bin/aleatorio"}},
			{ID: "conservador", Nombre: "Bot Conservador", Comando: []string{"./bin/conservador"}},
			{ID: "base", Nombre: "Bot Base", Comando: []string{"./bin/base"}},
		},
	}
}
