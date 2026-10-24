package campeonato

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amvz1704/pokerFight/internal/almacen"
	"github.com/amvz1704/pokerFight/internal/arena"
	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/mesa"
)

// Opciones arma un Campeonato.
type Opciones struct {
	Config    Config
	Inscritos Inscritos

	// Direccion es donde escuchan los bots, por ejemplo ":9000". ":0" pide un
	// puerto libre.
	Direccion string

	// RutaConfig, si está, es el archivo donde se guarda la configuración cuando
	// se cambia desde el panel.
	RutaConfig string

	// Lanzador pone en marcha los bots que tienen Comando. Es para las pruebas
	// en local: en el evento, cada equipo conecta el suyo. Si es nil se usa
	// arena.LanzadorProceso.
	Lanzador arena.Lanzador

	// Log recibe el progreso. Si es nil se usa el logger estándar.
	Log *log.Logger
}

// Campeonato es el motor del torneo por fases. Es seguro para uso concurrente:
// el panel lo consulta mientras las mesas juegan.
type Campeonato struct {
	log        *log.Logger
	inscritos  Inscritos
	porID      map[string]Inscrito
	almacen    *almacen.JSON
	rutaConfig string
	lanzador   arena.Lanzador
	lobby      *Lobby

	mu         sync.Mutex
	cfg        Config // Editable hasta la primera ronda; después manda est.Config.
	est        *Estado
	vivas      map[string]mesa.MesaInterface // Mesas en juego, por id.
	jugando    bool
	cancelar   context.CancelFunc
	pausar     bool
	ultimoErr  string
	modoAuto   bool
	sesiones   []arena.Sesion
	historial  *arena.HistorialArchivos
	historialD string
}

// ErrTerminado se devuelve al intentar jugar un campeonato que ya terminó.
var ErrTerminado = errors.New("campeonato: ya terminó, no queda nada por jugar")

// ErrYaEmpezo se devuelve al intentar cambiar la configuración después de
// jugar la primera ronda.
var ErrYaEmpezo = errors.New("campeonato: ya empezó, la configuración no se puede cambiar")

// Nuevo arma el campeonato. Si hay un estado guardado en el directorio de
// salida, lo retoma: los jugadores conservan su alias y sus fichas, así que un
// bot que se reconecta vuelve a ser el mismo.
func Nuevo(op Opciones) (*Campeonato, error) {
	cfg := op.Config
	if err := cfg.Validar(); err != nil {
		return nil, err
	}
	if err := op.Inscritos.Validar(); err != nil {
		return nil, err
	}
	if len(op.Inscritos) < 2 {
		return nil, errors.New("campeonato: hacen falta al menos 2 inscritos")
	}
	if op.Log == nil {
		op.Log = log.Default()
	}
	if op.Lanzador == nil {
		op.Lanzador = arena.LanzadorProceso{}
	}

	c := &Campeonato{
		log:        op.Log,
		inscritos:  op.Inscritos,
		porID:      make(map[string]Inscrito, len(op.Inscritos)),
		almacen:    almacen.NuevoJSON(filepath.Join(cfg.Salida, "estado.json")),
		rutaConfig: op.RutaConfig,
		lanzador:   op.Lanzador,
		cfg:        cfg,
		vivas:      make(map[string]mesa.MesaInterface),
	}
	for _, in := range op.Inscritos {
		c.porID[in.ID] = in
	}

	var guardado Estado
	if err := c.almacen.Cargar(&guardado); err != nil {
		return nil, fmt.Errorf("campeonato: no se pudo leer el estado guardado: %w", err)
	}
	if guardado.Jugadores == nil {
		est, err := nuevoEstado(op.Inscritos, cfg.Semilla)
		if err != nil {
			return nil, err
		}
		c.est = est
	} else {
		if err := guardado.conciliar(op.Inscritos, cfg.Semilla); err != nil {
			return nil, err
		}
		c.est = &guardado
		if guardado.Etapa != EtapaInscripcion {
			op.Log.Printf("campeonato: se retomó el estado guardado (%s, fase %d, %d ronda(s) jugadas)",
				guardado.Etapa, guardado.Fase+1, guardado.RondasJugadas)
		}
	}

	lobby, err := NuevoLobby(op.Direccion, op.Inscritos, c.est.aliasPorInscrito(), c.configVigente().Mesa.TimeoutMs, op.Log)
	if err != nil {
		return nil, err
	}
	c.lobby = lobby

	// Los alias se guardan ya, no recién con la primera ronda: un bot que se
	// conecta y se reconecta antes de empezar tiene que seguir siendo el mismo.
	if err := c.guardar(); err != nil {
		return nil, err
	}
	return c, nil
}

// configVigente es la configuración que rige: la congelada si el campeonato ya
// empezó, la editable si no. Requiere c.mu o acceso exclusivo (constructor).
func (c *Campeonato) configVigente() Config {
	if c.est.Config != nil {
		return *c.est.Config
	}
	return c.cfg
}

// guardar persiste el estado. Requiere c.mu o acceso exclusivo.
func (c *Campeonato) guardar() error {
	if err := c.almacen.Guardar(c.est); err != nil {
		return fmt.Errorf("campeonato: no se pudo guardar el estado: %w", err)
	}
	return nil
}

// Abrir reserva el puerto de los bots.
func (c *Campeonato) Abrir() error { return c.lobby.Abrir() }

// Servir acepta a los bots hasta que se cancele el contexto o se llame a Cerrar.
func (c *Campeonato) Servir(ctx context.Context) error { return c.lobby.Servir(ctx) }

// Direccion es la dirección real de escucha.
func (c *Campeonato) Direccion() string { return c.lobby.Direccion() }

// Cerrar corta a los bots y detiene los que se lanzaron por comando. Si hay una
// ronda en juego, la cancela.
func (c *Campeonato) Cerrar() {
	c.mu.Lock()
	cancelar := c.cancelar
	sesiones := c.sesiones
	c.sesiones = nil
	c.mu.Unlock()

	if cancelar != nil {
		cancelar()
	}
	c.lobby.Cerrar()
	detenerTodas(sesiones)
}

func detenerTodas(sesiones []arena.Sesion) {
	var espera sync.WaitGroup
	for _, s := range sesiones {
		espera.Add(1)
		go func(s arena.Sesion) {
			defer espera.Done()
			s.Detener()
		}(s)
	}
	espera.Wait()
}

// LanzarLocales pone en marcha los bots que tienen Comando, apuntando al lobby.
// Es lo que permite probar el campeonato entero en una sola máquina con los bots
// de ejemplo.
func (c *Campeonato) LanzarLocales(ctx context.Context) error {
	direccion := direccionLocal(c.lobby.Direccion())
	for _, in := range c.inscritos {
		if len(in.Comando) == 0 {
			continue
		}
		sesion, err := c.lanzador.Lanzar(ctx, arena.Participante{
			ID: in.ID, Token: in.Token, Comando: in.Comando, Directorio: in.Directorio,
		}, direccion)
		if err != nil {
			return fmt.Errorf("campeonato: no se pudo lanzar el bot de %s: %w", in.ID, err)
		}
		c.mu.Lock()
		c.sesiones = append(c.sesiones, sesion)
		c.mu.Unlock()
	}
	return nil
}

// direccionLocal traduce una dirección de escucha a una a la que se pueda
// conectar un proceso de esta máquina: ":9000" y "[::]:9000" se escuchan en todas
// las interfaces, pero no siempre se pueden marcar tal cual.
func direccionLocal(escucha string) string {
	host, puerto, err := net.SplitHostPort(escucha)
	if err != nil {
		return escucha
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, puerto)
}

// Config devuelve la configuración vigente.
func (c *Campeonato) Config() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configVigente()
}

// ActualizarConfig cambia la configuración. Solo se puede antes de jugar la
// primera ronda: después, las fases ya jugadas dejarían de coincidir con las que
// dice la configuración.
func (c *Campeonato) ActualizarConfig(nueva Config) error {
	if err := nueva.Validar(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.est.Etapa != EtapaInscripcion || c.jugando {
		return ErrYaEmpezo
	}
	// El directorio de salida no se cambia en caliente: ahí está el estado y el
	// campeonato ya lo abrió.
	nueva.Salida = c.cfg.Salida

	if c.rutaConfig != "" {
		if err := nueva.Guardar(c.rutaConfig); err != nil {
			return err
		}
	}
	c.cfg = nueva
	return nil
}

// Plan calcula las fases para `jugadores` inscritos; con 0 usa los reales.
func (c *Campeonato) Plan(jugadores int) ([]FasePlan, error) {
	if jugadores <= 0 {
		jugadores = len(c.inscritos)
	}
	return Plan(c.Config(), jugadores)
}

// --- Jugar ------------------------------------------------------------------------

// asientoDeMesa es una mesa armada, con quienes la juegan.
type asientoDeMesa struct {
	id      string
	alias   []string // En orden de silla.
	mesa    mesa.MesaInterface
	omitida bool // Nadie se elimina en esta mesa: no se juega.
	fichas  map[string]uint64
	resumen mesa.ResumenPartida
	err     error
}

// JugarRonda juega la próxima ronda y avanza el campeonato. Bloquea hasta que
// terminen todas las mesas.
//
// Si se cancela (ctx), la ronda se descarta entera: no se aplica ningún
// resultado parcial y la próxima llamada la juega de nuevo con un sorteo nuevo.
// Prefiere perder una ronda a dejar a unas mesas en un estado y a otras en otro.
func (c *Campeonato) JugarRonda(ctx context.Context) error {
	c.mu.Lock()
	if c.est.Etapa == EtapaTerminado {
		c.mu.Unlock()
		return ErrTerminado
	}
	if c.jugando {
		c.mu.Unlock()
		return errors.New("campeonato: ya hay una ronda en juego")
	}
	c.jugando = true

	// La primera ronda congela la configuración.
	empezoAhora := c.est.Etapa == EtapaInscripcion
	if empezoAhora {
		congelada := c.cfg
		c.est.Config = &congelada
		c.est.Etapa = EtapaEnCurso
	}
	cfg := *c.est.Config
	faseIdx, ronda := c.est.Fase, c.est.RondasJugadas
	fase := cfg.Fases[faseIdx]

	// Al abrir una fase con stack fijo, todos arrancan iguales. Con stack 0
	// conservan lo que traen (las bases no dicen si la final los reinicia: ver
	// docs/campeonato.md).
	if ronda == 0 {
		c.est.EntraronFase = len(c.est.Vivos)
		if fase.Stack > 0 {
			for _, alias := range c.est.Vivos {
				c.est.Jugadores[alias].Fichas = fase.Stack
			}
		}
	}
	vivos := append([]string(nil), c.est.Vivos...)
	fichas := make(map[string]uint64, len(vivos))
	for _, alias := range vivos {
		fichas[alias] = c.est.Jugadores[alias].Fichas
	}
	ctxRonda, cancelar := context.WithCancel(ctx)
	c.cancelar = cancelar
	c.ultimoErr = ""
	c.mu.Unlock()

	inicio := time.Now()
	mesas, err := c.armarYJugar(ctxRonda, cfg, fase, faseIdx, ronda, vivos, fichas)
	cancelar()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.jugando = false
	c.cancelar = nil
	c.vivas = make(map[string]mesa.MesaInterface)

	if err != nil {
		c.ultimoErr = err.Error()
		// Si no llegó a jugarse nada, el campeonato vuelve a estar sin empezar y
		// la configuración se puede seguir tocando.
		if empezoAhora && len(c.est.Rondas) == 0 {
			c.est.Etapa = EtapaInscripcion
			c.est.Config = nil
		}
		return err
	}

	c.aplicarRonda(cfg, faseIdx, ronda, mesas, inicio)
	if err := c.guardar(); err != nil {
		c.ultimoErr = err.Error()
		return err
	}
	return nil
}

// armarYJugar sortea las mesas de la ronda, las juega todas a la vez y devuelve
// sus resultados.
func (c *Campeonato) armarYJugar(ctx context.Context, cfg Config, fase Fase, faseIdx, ronda int, vivos []string, fichas map[string]uint64) ([]*asientoDeMesa, error) {
	tamanos, aviso, err := RepartirMesas(len(vivos), fase.JugadoresPorMesa, fase.Mesas)
	if err != nil {
		return nil, fmt.Errorf("campeonato: no se pueden armar las mesas de %q: %w", fase.Nombre, err)
	}
	if aviso != "" {
		c.log.Printf("campeonato: %s", aviso)
	}
	sorteadas, err := Sortear(vivos, tamanos, NuevoAzar(cfg.Semilla, faseIdx, ronda))
	if err != nil {
		return nil, err
	}

	// En una clasificatoria las ciegas siguen subiendo entre rondas; en una
	// eliminatoria cada mesa cuenta desde cero.
	desplazamiento := int64(0)
	if fase.Tipo == Clasificatoria {
		desplazamiento = int64(ronda) * fase.ManosPorRonda
	}
	ciegas := func(n int64) (uint64, uint64) { return fase.Ciegas.Nivel(desplazamiento + n) }

	var observar func(id string) mesa.ObservadorMano
	if cfg.GuardarHistorial {
		h, err := c.historialPara(cfg)
		if err != nil {
			return nil, err
		}
		observar = h.Observador
	}

	mesas := make([]*asientoDeMesa, len(sorteadas))
	for i, alias := range sorteadas {
		id := fmt.Sprintf("f%d-r%d-m%02d", faseIdx+1, ronda+1, i+1)
		a := &asientoDeMesa{id: id, alias: alias, fichas: make(map[string]uint64, len(alias))}
		for _, j := range alias {
			a.fichas[j] = fichas[j]
		}

		if fase.Tipo == Eliminatoria && fase.AvanzanPorMesa >= len(alias) {
			a.omitida = true
			mesas[i] = a
			continue
		}

		partida := mesa.ConfigPartida{
			StackInicial:    fase.Stack,
			StackPorJugador: a.fichas,
			Ciegas:          ciegas,
			HastaQueden:     1,
		}
		switch fase.Tipo {
		case Clasificatoria:
			partida.CantidadRondas = fase.ManosPorRonda
		case Eliminatoria:
			partida.CantidadRondas = fase.MaxManos
			partida.HastaQueden = fase.AvanzanPorMesa
		}
		partida.CiegaMenor, partida.CiegaMayor = ciegas(1)

		cfgMesa := mesa.ConfigMesa{
			MaxJugadores: uint8(len(alias)),
			MinJugadores: uint8(len(alias)),
			Timeout:      cfg.Mesa.TimeoutMs,
		}
		if observar != nil {
			cfgMesa.Observador = observar(id)
		}

		var cpr crupier.Crupier = crupier.Nuevo()
		if semilla := SemillaDeMesa(cfg.Semilla, faseIdx, ronda, i); semilla != nil {
			cpr = crupier.NuevoConSemilla(*semilla)
		}
		a.mesa = mesa.NuevaMesa(id, cfgMesa, partida, cpr)

		for silla, j := range alias {
			p := c.lobby.Presencia(j)
			if p == nil {
				return nil, fmt.Errorf("campeonato: el jugador %s no está en el lobby", j)
			}
			if err := a.mesa.SentarJugadorEnSilla(j, j, silla, p); err != nil {
				return nil, fmt.Errorf("campeonato: no se pudo sentar a %s en %s: %w", j, id, err)
			}
		}
		mesas[i] = a
	}

	c.mu.Lock()
	for _, a := range mesas {
		if a.mesa != nil {
			c.vivas[a.id] = a.mesa
		}
	}
	c.mu.Unlock()

	c.log.Printf("campeonato: %s, ronda %d: %s, %d jugadores",
		fase.Nombre, ronda+1, DescribirMesas(tamanos), len(vivos))

	var espera sync.WaitGroup
	for _, a := range mesas {
		if a.omitida {
			continue
		}
		espera.Add(1)
		go func(a *asientoDeMesa) {
			defer espera.Done()
			a.resumen, a.err = a.mesa.Jugar(ctx)
		}(a)
	}
	espera.Wait()

	// Se vacía el historial en cuanto termina la ronda: si se cae la arena, lo
	// jugado hasta acá tiene que estar en disco.
	if c.historial != nil {
		c.historial.Cerrar()
	}

	for _, a := range mesas {
		if a.err != nil {
			return nil, fmt.Errorf("campeonato: la mesa %s no terminó: %w", a.id, a.err)
		}
	}
	return mesas, nil
}

// historialPara abre el historial de manos la primera vez que hace falta.
func (c *Campeonato) historialPara(cfg Config) (*arena.HistorialArchivos, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.historial != nil {
		return c.historial, nil
	}
	directorio := filepath.Join(cfg.Salida, "manos")
	h, err := arena.NuevoHistorialArchivos(directorio)
	if err != nil {
		return nil, fmt.Errorf("campeonato: no se pudo preparar el historial de manos: %w", err)
	}
	c.historial, c.historialD = h, directorio
	return h, nil
}

// aplicarRonda vuelca los resultados de las mesas al estado y, si la fase
// terminó, decide quién pasa. Requiere c.mu.
func (c *Campeonato) aplicarRonda(cfg Config, faseIdx, ronda int, mesas []*asientoDeMesa, inicio time.Time) {
	fase := cfg.Fases[faseIdx]
	registro := RondaJugada{Fase: faseIdx, Ronda: ronda, Inicio: inicio, Fin: time.Now()}

	// Posiciones por mesa, en orden. Las mesas omitidas (nadie se elimina) se
	// ordenan por fichas.
	porMesa := make([][]string, len(mesas))

	for i, a := range mesas {
		jugada := MesaJugada{ID: a.id}
		if a.omitida {
			orden := append([]string(nil), a.alias...)
			sort.SliceStable(orden, func(x, y int) bool { return a.fichas[orden[x]] > a.fichas[orden[y]] })
			porMesa[i] = orden
			for pos, alias := range orden {
				jugada.Jugadores = append(jugada.Jugadores, ResultadoMesa{
					Alias: alias, FichasInicio: a.fichas[alias], FichasFin: a.fichas[alias], Posicion: pos + 1,
				})
			}
			registro.Mesas = append(registro.Mesas, jugada)
			continue
		}

		for _, p := range a.resumen.Posiciones {
			j := c.est.Jugadores[p.IDJugador]
			inicial := a.fichas[p.IDJugador]
			j.Fichas = p.SaldoFinal
			j.Timeouts += p.Timeouts
			j.ManosJugadas += p.ManosJugadas
			porMesa[i] = append(porMesa[i], p.IDJugador)
			jugada.Jugadores = append(jugada.Jugadores, ResultadoMesa{
				Alias: p.IDJugador, FichasInicio: inicial, FichasFin: p.SaldoFinal,
				Posicion: p.Posicion, Timeouts: p.Timeouts,
			})
		}
		jugada.Manos = a.resumen.CantidadRondasJugadas
		registro.Mesas = append(registro.Mesas, jugada)
	}
	c.est.Rondas = append(c.est.Rondas, registro)
	c.est.RondasJugadas++

	var pasan []string
	cerrada := true

	switch fase.Tipo {
	case Clasificatoria:
		// Los que se quedaron sin fichas están eliminados.
		restantes := c.est.Vivos[:0:0]
		for _, alias := range c.est.Vivos {
			j := c.est.Jugadores[alias]
			if j.Fichas == 0 {
				c.sacar(j, faseIdx, MotivoQuiebra, 0)
				continue
			}
			restantes = append(restantes, alias)
		}
		c.est.Vivos = restantes

		// La fase termina al jugar todas las rondas, o antes si ya no quedan
		// jugadores para sortear.
		cerrada = c.est.RondasJugadas >= fase.Rondas || len(c.est.Vivos) < 2
		if cerrada {
			pasan = c.cerrarClasificatoria(fase, faseIdx)
		}

	case Eliminatoria:
		for _, orden := range porMesa {
			avanzan := fase.AvanzanPorMesa
			if avanzan > len(orden) {
				avanzan = len(orden)
			}
			pasan = append(pasan, orden[:avanzan]...)
			for pos, alias := range orden[avanzan:] {
				c.sacar(c.est.Jugadores[alias], faseIdx, MotivoEliminado, avanzan+pos+1)
			}
			for pos, alias := range orden[:avanzan] {
				c.est.Jugadores[alias].Puesto = pos + 1
			}
		}
	}

	if !cerrada {
		return
	}
	c.cerrarFase(cfg, faseIdx, pasan)
}

// sacar marca a un jugador como fuera de competencia.
func (c *Campeonato) sacar(j *JugadorEstado, fase int, motivo string, puesto int) {
	j.Salio = fase
	j.Motivo = motivo
	if puesto > 0 {
		j.Puesto = puesto
	}
}

// cerrarClasificatoria ordena a los que siguen por fichas y devuelve a los que
// pasan. Desempata por menos timeouts (el bot más confiable) y, si sigue el
// empate, por alias: un orden arbitrario pero estable, para que el resultado no
// dependa de en qué orden se recorrió el mapa.
func (c *Campeonato) cerrarClasificatoria(fase Fase, faseIdx int) []string {
	ranking := append([]string(nil), c.est.Vivos...)
	sort.SliceStable(ranking, func(a, b int) bool {
		ja, jb := c.est.Jugadores[ranking[a]], c.est.Jugadores[ranking[b]]
		switch {
		case ja.Fichas != jb.Fichas:
			return ja.Fichas > jb.Fichas
		case ja.Timeouts != jb.Timeouts:
			return ja.Timeouts < jb.Timeouts
		default:
			return ja.Alias < jb.Alias
		}
	})

	pasan := fase.Clasifican
	if pasan == 0 {
		pasan = ClasificanAuto(c.est.EntraronFase)
	}
	if pasan > len(ranking) {
		pasan = len(ranking)
	}

	for i, alias := range ranking {
		j := c.est.Jugadores[alias]
		j.Puesto = i + 1
		if i >= pasan {
			c.sacar(j, faseIdx, MotivoNoClasifico, i+1)
		}
	}
	return ranking[:pasan]
}

// cerrarFase pasa a la fase siguiente con los que clasificaron, o termina el
// campeonato.
func (c *Campeonato) cerrarFase(cfg Config, faseIdx int, pasan []string) {
	sort.Strings(pasan)

	esUltima := faseIdx == len(cfg.Fases)-1
	if esUltima || len(pasan) <= 1 {
		c.est.Etapa = EtapaTerminado
		c.est.Vivos = pasan
		if len(pasan) == 1 {
			campeon := c.est.Jugadores[pasan[0]]
			campeon.Motivo = MotivoCampeon
			campeon.Puesto = 1
			c.est.Campeon = campeon.Alias
		}
		c.log.Printf("campeonato: terminó")
		return
	}

	c.est.Fase = faseIdx + 1
	c.est.RondasJugadas = 0
	c.est.Vivos = pasan
	c.est.EntraronFase = len(pasan)
}

// --- Control de la ejecución ---------------------------------------------------------

// Iniciar juega rondas en segundo plano. Con auto, sigue ronda tras ronda hasta
// que termine el campeonato, se pida una pausa o falle algo; sin él, juega una
// sola. El contexto tiene que durar lo que dure el servidor, no el pedido que lo
// inició.
func (c *Campeonato) Iniciar(ctx context.Context, auto bool) error {
	c.mu.Lock()
	if c.est.Etapa == EtapaTerminado {
		c.mu.Unlock()
		return ErrTerminado
	}
	if c.jugando {
		c.mu.Unlock()
		return errors.New("campeonato: ya hay una ronda en juego")
	}
	c.pausar = false
	c.modoAuto = auto
	c.mu.Unlock()

	go func() {
		for {
			if err := c.JugarRonda(ctx); err != nil {
				c.log.Printf("campeonato: la ronda no se pudo jugar: %v", err)
				break
			}
			c.mu.Lock()
			seguir := auto && !c.pausar && c.est.Etapa != EtapaTerminado
			c.mu.Unlock()
			if !seguir {
				break
			}
		}
		c.mu.Lock()
		c.modoAuto = false
		c.pausar = false
		c.mu.Unlock()
	}()
	return nil
}

// Pausar hace que el modo automático se detenga al terminar la ronda en juego.
func (c *Campeonato) Pausar() {
	c.mu.Lock()
	c.pausar = true
	c.mu.Unlock()
}

// Cancelar corta la ronda en juego y la descarta.
func (c *Campeonato) Cancelar() {
	c.mu.Lock()
	cancelar := c.cancelar
	c.pausar = true
	c.mu.Unlock()
	if cancelar != nil {
		cancelar()
	}
}

// ListoParaJugar dice cuántos de los que van a jugar la próxima ronda tienen
// conexión. Los que no, van a jugar igual con la acción segura: es lo que piden
// las bases, pero el organizador tiene que poder verlo antes de apretar el botón.
func (c *Campeonato) ListoParaJugar() (conectados, total int) {
	c.mu.Lock()
	vivos := append([]string(nil), c.est.Vivos...)
	c.mu.Unlock()
	for _, alias := range vivos {
		if p := c.lobby.Presencia(alias); p != nil && p.Conectado() {
			conectados++
		}
	}
	return conectados, len(vivos)
}

// nombreDeFase es el nombre de la fase para mostrar.
func nombreDeFase(cfg Config, i int) string {
	if i < 0 || i >= len(cfg.Fases) {
		return ""
	}
	return strings.TrimSpace(cfg.Fases[i].Nombre)
}
