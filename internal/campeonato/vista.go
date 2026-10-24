package campeonato

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amvz1704/pokerFight/internal/mesa"
)

// Instantanea es una foto del campeonato para mostrar en el panel. Incluye qué
// equipo hay detrás de cada alias: es información solo para la organización y
// no tiene que salir del panel.
type Instantanea struct {
	Nombre string `json:"nombre"`
	Etapa  Etapa  `json:"etapa"`

	// Bots es la dirección a la que se conectan los bots.
	Bots string `json:"bots"`

	// Config es la configuración vigente y Plan lo que implica para
	// PlanJugadores inscritos.
	Config        Config     `json:"config"`
	Plan          []FasePlan `json:"plan"`
	PlanJugadores int        `json:"plan_jugadores"`

	// Fase es el índice de la fase en curso y FaseNombre su nombre. Ronda es la
	// próxima a jugar (1 = la primera) y RondasFase cuántas tiene la fase.
	Fase       int    `json:"fase"`
	FaseNombre string `json:"fase_nombre"`
	Ronda      int    `json:"ronda"`
	RondasFase int    `json:"rondas_fase"`

	// Jugando indica que hay una ronda en juego, Auto que el campeonato sigue
	// ronda tras ronda solo y Pausando que se pidió detenerlo al terminar la ronda.
	Jugando  bool `json:"jugando"`
	Auto     bool `json:"auto"`
	Pausando bool `json:"pausando"`

	// Conectados y Esperados cuentan a los que van a jugar la próxima ronda.
	Conectados int `json:"conectados"`
	Esperados  int `json:"esperados"`
	Inscritos  int `json:"inscritos"`

	Error string `json:"error,omitempty"`

	Jugadores []VistaJugador `json:"jugadores"`
	Mesas     []VistaMesa    `json:"mesas"`
	Rondas    []VistaRonda   `json:"rondas"`
	Campeon   string         `json:"campeon,omitempty"`
}

// VistaJugador es una fila de la tabla de jugadores.
type VistaJugador struct {
	Alias  string `json:"alias"`
	Equipo string `json:"equipo"`
	Bot    string `json:"bot"`

	Fichas       uint64 `json:"fichas"`
	Timeouts     uint64 `json:"timeouts"`
	ManosJugadas uint64 `json:"manos_jugadas"`

	// Estado es "en_juego", "eliminado", "no_clasifico", "quiebra" o "campeon".
	Estado string `json:"estado"`
	// SalioEn es el nombre de la fase en que dejó de competir, y FaseSalida su
	// índice (-1 si sigue compitiendo).
	SalioEn    string `json:"salio_en,omitempty"`
	FaseSalida int    `json:"fase_salida"`
	Puesto     int    `json:"puesto,omitempty"`

	Conectado      bool      `json:"conectado"`
	Caidas         int       `json:"caidas"`
	UltimoContacto time.Time `json:"ultimo_contacto,omitempty"`
}

// VistaMesa es una mesa en juego, con lo público de su estado. Las cartas
// privadas no están: ni siquiera el panel las ve mientras se juega.
type VistaMesa struct {
	ID           string         `json:"id"`
	Mano         int            `json:"mano"`
	Etapa        string         `json:"etapa"`
	Pozo         int64          `json:"pozo"`
	CiegaChica   int64          `json:"ciega_chica"`
	CiegaGrande  int64          `json:"ciega_grande"`
	Comunitarias []string       `json:"comunitarias"`
	Asientos     []VistaAsiento `json:"asientos"`
}

// VistaAsiento es un jugador sentado en una mesa.
type VistaAsiento struct {
	Alias     string `json:"alias"`
	Fichas    int64  `json:"fichas"`
	Apuesta   int64  `json:"apuesta"`
	EnMano    bool   `json:"en_mano"`
	AllIn     bool   `json:"allin"`
	Retirado  bool   `json:"retirado"`
	Eliminado bool   `json:"eliminado"`
	Conectado bool   `json:"conectado"`
}

// VistaRonda resume una ronda ya jugada.
type VistaRonda struct {
	Fase       int       `json:"fase"`
	FaseNombre string    `json:"fase_nombre"`
	Ronda      int       `json:"ronda"`
	Mesas      int       `json:"mesas"`
	Manos      uint64    `json:"manos"`
	Fin        time.Time `json:"fin"`
}

// Instantanea arma la foto actual.
func (c *Campeonato) Instantanea() Instantanea {
	c.mu.Lock()
	cfg := c.configVigente()
	snap := Instantanea{
		Nombre:     cfg.Nombre,
		Etapa:      c.est.Etapa,
		Bots:       c.lobby.Direccion(),
		Config:     cfg,
		Fase:       c.est.Fase,
		FaseNombre: nombreDeFase(cfg, c.est.Fase),
		Ronda:      c.est.RondasJugadas + 1,
		Jugando:    c.jugando,
		Auto:       c.modoAuto,
		Pausando:   c.pausar,
		Inscritos:  len(c.inscritos),
		Error:      c.ultimoErr,
		Campeon:    c.est.Campeon,
		// Listas vacías y no nil: Go serializa un slice nil como null, y la
		// página recorre estas listas sin comprobarlo.
		Plan:   []FasePlan{},
		Mesas:  []VistaMesa{},
		Rondas: []VistaRonda{},
	}
	if c.est.Fase < len(cfg.Fases) {
		if f := cfg.Fases[c.est.Fase]; f.Tipo == Clasificatoria {
			snap.RondasFase = f.Rondas
		} else {
			snap.RondasFase = 1
		}
	}

	jugadores := make([]VistaJugador, 0, len(c.est.Jugadores))
	for _, j := range c.est.Jugadores {
		in := c.porID[j.ID]
		v := VistaJugador{
			Alias: j.Alias, Equipo: in.Equipo, Bot: in.Bot,
			Fichas: j.Fichas, Timeouts: j.Timeouts, ManosJugadas: j.ManosJugadas,
			Estado: "en_juego", Puesto: j.Puesto, FaseSalida: j.Salio,
		}
		if !j.EnJuego() {
			v.Estado = j.Motivo
			v.SalioEn = nombreDeFase(cfg, j.Salio)
		}
		if j.Motivo == MotivoCampeon {
			v.Estado = MotivoCampeon
		}
		jugadores = append(jugadores, v)
	}
	vivos := append([]string(nil), c.est.Vivos...)

	for _, r := range c.est.Rondas {
		var manos uint64
		for _, m := range r.Mesas {
			manos += m.Manos
		}
		snap.Rondas = append(snap.Rondas, VistaRonda{
			Fase: r.Fase, FaseNombre: nombreDeFase(cfg, r.Fase), Ronda: r.Ronda + 1,
			Mesas: len(r.Mesas), Manos: manos, Fin: r.Fin,
		})
	}
	// Se copia el mapa de mesas: mientras se arma una ronda se le agregan
	// entradas bajo el lock, y leerlo después sin él sería una carrera.
	enJuego := make(map[string]mesa.MesaInterface, len(c.vivas))
	vivas := make([]string, 0, len(c.vivas))
	for id, m := range c.vivas {
		enJuego[id] = m
		vivas = append(vivas, id)
	}
	c.mu.Unlock()

	// Lo que sigue consulta las mesas y el lobby, que tienen sus propios locks.
	for i := range jugadores {
		if p := c.lobby.Presencia(jugadores[i].Alias); p != nil {
			v := p.vista()
			jugadores[i].Conectado = v.Conectado
			jugadores[i].Caidas = v.Caidas
			jugadores[i].UltimoContacto = v.UltimoContacto
		}
	}
	sort.Slice(jugadores, func(a, b int) bool { return ordenJugadores(jugadores[a], jugadores[b]) })
	snap.Jugadores = jugadores

	snap.Esperados = len(vivos)
	for _, alias := range vivos {
		if p := c.lobby.Presencia(alias); p != nil && p.Conectado() {
			snap.Conectados++
		}
	}

	sort.Strings(vivas)
	for _, id := range vivas {
		m := enJuego[id]
		estado := m.Estado()
		vm := VistaMesa{
			ID: id, Mano: numeroDeMano(estado.IDMano), Etapa: string(estado.Etapa),
			Pozo: estado.Pozo, CiegaChica: estado.CiegaChica, CiegaGrande: estado.CiegaGrande,
			Comunitarias: []string{}, Asientos: []VistaAsiento{},
		}
		for _, carta := range estado.Comunitarias {
			vm.Comunitarias = append(vm.Comunitarias, carta.String())
		}
		for _, j := range estado.Jugadores {
			a := VistaAsiento{
				Alias: j.ID, Fichas: j.Saldo, Apuesta: j.ApuestaRonda,
				EnMano: j.EnMano, AllIn: j.AllIn, Retirado: j.EnMano && !j.Activo,
				Eliminado: !j.EnTorneo,
			}
			if p := c.lobby.Presencia(j.ID); p != nil {
				a.Conectado = p.Conectado()
			}
			vm.Asientos = append(vm.Asientos, a)
		}
		snap.Mesas = append(snap.Mesas, vm)
	}

	snap.PlanJugadores = snap.Inscritos
	if planes, err := Plan(cfg, snap.Inscritos); err == nil {
		snap.Plan = planes
	}
	return snap
}

// ordenJugadores ordena la tabla: primero los que siguen y el campeón, por
// fichas; después los que salieron, el que llegó más lejos primero y, dentro de
// una misma fase, el de mejor puesto.
//
// No alcanza con ordenar por fichas: quien pierde en una fase final queda con 0,
// igual que quien quebró en la clasificatoria, y mezclarlos escondería quién
// llegó hasta dónde.
func ordenJugadores(a, b VistaJugador) bool {
	enA, enB := sigueEnCarrera(a), sigueEnCarrera(b)
	if enA != enB {
		return enA
	}
	if enA {
		if a.Fichas != b.Fichas {
			return a.Fichas > b.Fichas
		}
		return a.Alias < b.Alias
	}
	if a.FaseSalida != b.FaseSalida {
		return a.FaseSalida > b.FaseSalida
	}
	// Dentro de una misma fase, quien quedó en una tabla (tiene puesto) va antes
	// que quien quebró, que no llegó a clasificarse en ninguna.
	if (a.Puesto > 0) != (b.Puesto > 0) {
		return a.Puesto > 0
	}
	if a.Puesto != b.Puesto {
		return a.Puesto < b.Puesto
	}
	if a.Fichas != b.Fichas {
		return a.Fichas > b.Fichas
	}
	return a.Alias < b.Alias
}

// sigueEnCarrera es verdadero para quien compite todavía y para el campeón.
func sigueEnCarrera(j VistaJugador) bool {
	return j.Estado == "en_juego" || j.Estado == MotivoCampeon
}

// numeroDeMano saca el número del final del id de mano ("f1-r1-m01-mano-37").
func numeroDeMano(idMano string) int {
	i := strings.LastIndex(idMano, "-")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(idMano[i+1:])
	if err != nil {
		return 0
	}
	return n
}
