// Package campeonato corre el torneo por fases de las bases del Poker Fight de
// Bots: una clasificatoria de varias rondas con mesas sorteadas de nuevo en cada
// una, y una final por eliminación hasta una mesa final.
//
// Es distinto de internal/arena a propósito. La arena arma todos los
// enfrentamientos antes de jugar y levanta un puerto por partida: sirve para un
// round-robin entre bots que se conocen de antemano. El campeonato tiene que
// decidir cada ronda a partir del resultado de la anterior, y los bots se
// conectan UNA sola vez a un único servidor y se quedan ahí mientras se los
// mueve de mesa en mesa (ver Lobby).
//
// # Dónde está cada cosa
//
//	config.go       La configuración del campeonato y su validación.
//	plan.go         Cuántas mesas y de qué tamaño tiene cada fase (vista previa).
//	sorteo.go       El sorteo de mesas.
//	inscritos.go    Los bots inscritos y sus credenciales.
//	estado.go       Lo que se persiste entre rondas (fichas, eliminados).
//	lobby.go        El servidor único al que se conectan los bots.
//	campeonato.go   El motor: juega una ronda, avanza de fase, persiste.
//	panel.go        La interfaz web de control.
//
// Ni este paquete ni la Mesa importan el Casino: las credenciales salen de la
// lista de inscritos (docs/interfaces.md §4).
package campeonato

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Tipos de fase.
const (
	// Clasificatoria juega varias rondas con las mesas sorteadas de nuevo cada
	// vez. Cada bot conserva sus fichas de una ronda a la otra y el que se
	// queda sin ellas queda eliminado. Al final pasan los que más fichas tienen.
	Clasificatoria = "clasificatoria"
	// Eliminatoria juega una sola ronda por mesas y en cada mesa pasan los
	// últimos en pie.
	Eliminatoria = "eliminatoria"
)

// MaxJugadoresMesa es el tope de asientos de una mesa. El mazo alcanzaría para
// más, pero con más de diez la mano deja de ser poker de mesa y el estado que
// recibe cada bot crece sin ganar nada.
const MaxJugadoresMesa = 10

// MaxBotsPorEquipo es el límite de las bases: cada equipo entrega hasta dos bots.
const MaxBotsPorEquipo = 2

// Config es todo lo que define una edición del campeonato.
type Config struct {
	// Nombre identifica la edición.
	Nombre string `json:"nombre"`

	// Semilla, si está, hace el campeonato reproducible: el sorteo de mesas y el
	// mazo de cada una salen de ella. Si no está, el sorteo y el barajado usan
	// aleatoriedad criptográfica, que es lo que corresponde a un torneo de
	// verdad: nadie, ni la organización, puede predecir las cartas.
	Semilla *uint64 `json:"semilla,omitempty"`

	// Mesa son los parámetros de juego compartidos por todas las mesas.
	Mesa ConfigMesa `json:"mesa"`

	// Fases se juegan en orden: los que pasan una entran a la siguiente.
	Fases []Fase `json:"fases"`

	// Salida es el directorio donde se guarda el estado y el historial de manos.
	Salida string `json:"salida"`

	// GuardarHistorial escribe cada mano jugada en JSON Lines. Trae las cartas
	// de todos: es lo único que permite resolver un reclamo.
	GuardarHistorial bool `json:"guardar_historial"`
}

// ConfigMesa son los parámetros que no cambian entre fases.
type ConfigMesa struct {
	// TimeoutMs es el plazo del bot para responder cada jugada. Las bases
	// fijan 10 segundos; pasado ese plazo la arena juega check o fold.
	TimeoutMs uint64 `json:"timeout_ms"`
}

// Fase es una etapa del campeonato.
type Fase struct {
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`

	// JugadoresPorMesa es el tamaño de mesa buscado. Si la división no es
	// exacta, algunas mesas llevan uno más (las bases: "mesas de 5; si la
	// división no es exacta, algunas mesas tienen 6").
	JugadoresPorMesa int `json:"jugadores_por_mesa"`

	// Mesas fija la cantidad de mesas en vez de derivarla del tamaño. Con 0, se
	// calcula a partir de JugadoresPorMesa. Sirve para fases definidas por la
	// cantidad de mesas y no por su tamaño ("4 mesas de 2 o 4 jugadores": según
	// cuántos lleguen, cambia el tamaño pero siempre son cuatro).
	Mesas int `json:"mesas"`

	// Stack son las fichas con las que arrancan todos los jugadores de la fase.
	// 0 significa que conservan las que traen: solo tiene sentido a partir de la
	// segunda fase. La primera siempre necesita un valor.
	Stack uint64 `json:"stack"`

	// Ciegas es la estructura de ciegas de la fase.
	Ciegas Ciegas `json:"ciegas"`

	// --- Solo Clasificatoria ---

	// Rondas es cuántas rondas se juegan.
	Rondas int `json:"rondas"`
	// ManosPorRonda es el tope de manos de cada ronda. Una mesa termina antes si
	// queda un solo jugador con fichas.
	ManosPorRonda int64 `json:"manos_por_ronda"`
	// Clasifican es cuántos pasan a la fase siguiente, los que más fichas
	// tengan. 0 los elige solos según la cantidad de inscritos (ClasificanAuto).
	Clasifican int `json:"clasifican"`

	// --- Solo Eliminatoria ---

	// AvanzanPorMesa es cuántos pasan de cada mesa: los últimos en pie. La mesa
	// termina cuando quedan exactamente esos.
	AvanzanPorMesa int `json:"avanzan_por_mesa"`
	// MaxManos es el tope de manos de cada mesa. Es una red de seguridad: con
	// ciegas que suben, una mesa termina sola, pero dos bots que nunca apuestan
	// podrían alargarla mucho. Al llegar al tope pasan los que más fichas tengan.
	MaxManos int64 `json:"max_manos"`
}

// Ciegas es la estructura de ciegas de una fase: una lista de niveles que se
// recorre a medida que avanzan las manos.
type Ciegas struct {
	// Niveles son las ciegas chicas de cada nivel, de menor a mayor. La grande es
	// siempre el doble. Al llegar al último, ese nivel se repite.
	Niveles []uint64 `json:"niveles"`
	// ManosPorNivel es cuántas manos dura cada nivel. En una clasificatoria se
	// cuenta sobre todas las rondas de la fase, no por ronda: las ciegas siguen
	// subiendo entre una ronda y la siguiente, que es lo que fuerza las
	// eliminaciones.
	ManosPorNivel int64 `json:"manos_por_nivel"`
}

// Nivel devuelve las ciegas de la mano número `mano` de la fase (la primera
// vale 1), sin repartir por rondas: es el conteo acumulado.
func (c Ciegas) Nivel(mano int64) (chica, grande uint64) {
	if len(c.Niveles) == 0 {
		return 0, 0
	}
	indice := int64(0)
	if c.ManosPorNivel > 0 && mano > 0 {
		indice = (mano - 1) / c.ManosPorNivel
	}
	if indice >= int64(len(c.Niveles)) {
		indice = int64(len(c.Niveles)) - 1
	}
	chica = c.Niveles[indice]
	return chica, 2 * chica
}

// ConfigBases es la configuración que sale de las bases del torneo.
//
// Lo que las bases dejan entre corchetes ([200] manos, [X], [N]) está acá con un
// valor provisorio, marcado en docs/campeonato.md como pendiente de definir. Es
// lo primero que hay que revisar antes de lanzar.
func ConfigBases() Config {
	niveles := []uint64{50, 75, 100, 150, 200, 300, 400, 600, 800, 1200}
	return Config{
		Nombre:           "Poker Fight de Bots 2026",
		Mesa:             ConfigMesa{TimeoutMs: 10000},
		Salida:           "campeonato",
		GuardarHistorial: true,
		Fases: []Fase{
			{
				Nombre: "Clasificatoria", Tipo: Clasificatoria,
				JugadoresPorMesa: 5, Stack: 10000,
				Rondas: 4, ManosPorRonda: 200,
				Ciegas: Ciegas{Niveles: niveles, ManosPorNivel: 100},
			},
			{
				Nombre: "Final: mesas de 4", Tipo: Eliminatoria,
				JugadoresPorMesa: 4, AvanzanPorMesa: 2, Stack: 10000, MaxManos: 600,
				Ciegas: Ciegas{Niveles: niveles, ManosPorNivel: 30},
			},
			{
				Nombre: "Final: 4 mesas", Tipo: Eliminatoria,
				Mesas: 4, AvanzanPorMesa: 1, Stack: 10000, MaxManos: 600,
				Ciegas: Ciegas{Niveles: niveles, ManosPorNivel: 30},
			},
			{
				Nombre: "Mesa final", Tipo: Eliminatoria,
				Mesas: 1, AvanzanPorMesa: 1, Stack: 10000, MaxManos: 800,
				Ciegas: Ciegas{Niveles: niveles, ManosPorNivel: 30},
			},
		},
	}
}

// Validar completa los valores por defecto y verifica que la configuración
// tenga sentido. Modifica el receptor, así que se llama sobre un puntero.
func (c *Config) Validar() error {
	if strings.TrimSpace(c.Nombre) == "" {
		c.Nombre = "campeonato"
	}
	if c.Salida == "" {
		c.Salida = "campeonato"
	}
	if c.Mesa.TimeoutMs == 0 {
		c.Mesa.TimeoutMs = 10000
	}
	if len(c.Fases) == 0 {
		return fmt.Errorf("campeonato: hace falta al menos una fase")
	}

	for i := range c.Fases {
		if err := c.Fases[i].validar(i); err != nil {
			return err
		}
	}
	return nil
}

func (f *Fase) validar(i int) error {
	nombre := f.Nombre
	if strings.TrimSpace(nombre) == "" {
		f.Nombre = fmt.Sprintf("Fase %d", i+1)
		nombre = f.Nombre
	}
	donde := func(formato string, args ...any) error {
		return fmt.Errorf("campeonato: fase %d (%s): %s", i+1, nombre, fmt.Sprintf(formato, args...))
	}

	switch f.Tipo {
	case Clasificatoria, Eliminatoria:
	default:
		return donde("tipo %q desconocido (usar %q o %q)", f.Tipo, Clasificatoria, Eliminatoria)
	}

	if f.Mesas < 0 {
		return donde("mesas no puede ser negativo")
	}
	if f.Mesas == 0 {
		if f.JugadoresPorMesa < 2 || f.JugadoresPorMesa > MaxJugadoresMesa {
			return donde("jugadores_por_mesa debe estar entre 2 y %d (o fijar la cantidad de mesas), es %d", MaxJugadoresMesa, f.JugadoresPorMesa)
		}
	}

	if i == 0 && f.Stack == 0 {
		return donde("la primera fase necesita un stack inicial (las bases usan 10000)")
	}

	if len(f.Ciegas.Niveles) == 0 {
		return donde("hace falta al menos un nivel de ciegas")
	}
	for _, n := range f.Ciegas.Niveles {
		if n == 0 {
			return donde("una ciega chica no puede ser 0")
		}
	}
	if f.Ciegas.ManosPorNivel <= 0 {
		f.Ciegas.ManosPorNivel = 100
	}

	if f.Tipo == Clasificatoria {
		if f.Rondas < 1 {
			return donde("rondas debe ser al menos 1")
		}
		if f.ManosPorRonda < 1 {
			return donde("manos_por_ronda debe ser al menos 1")
		}
		if f.Clasifican < 0 {
			return donde("clasifican no puede ser negativo")
		}
		return nil
	}

	if f.AvanzanPorMesa < 1 {
		return donde("avanzan_por_mesa debe ser al menos 1")
	}
	if f.MaxManos < 1 {
		f.MaxManos = 1000
	}
	return nil
}

// CargarConfig lee la configuración de un archivo JSON y la valida.
func CargarConfig(ruta string) (Config, error) {
	var cfg Config
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return cfg, fmt.Errorf("campeonato: no se pudo leer %s: %w", ruta, err)
	}
	if err := cfg.desdeJSON(datos); err != nil {
		return cfg, fmt.Errorf("campeonato: %s no es una configuración válida: %w", ruta, err)
	}
	return cfg, nil
}

// desdeJSON decodifica rechazando campos desconocidos: uno mal escrito se
// ignoraría en silencio y el campeonato correría con otra configuración que la
// que su autor cree.
func (c *Config) desdeJSON(datos []byte) error {
	decodificador := json.NewDecoder(strings.NewReader(string(datos)))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(c); err != nil {
		return err
	}
	return c.Validar()
}

// Guardar escribe la configuración como JSON.
func (c Config) Guardar(ruta string) error {
	datos, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ruta, append(datos, '\n'), 0o644)
}
