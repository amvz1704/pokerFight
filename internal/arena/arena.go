// Package arena corre torneos de bots: arma los emparejamientos, levanta una
// mesa por enfrentamiento, conecta a los bots, junta los resultados y produce
// una clasificación.
//
// Es el equivalente al "engine" de MIT Pokerbots: los participantes no corren
// nada del torneo, solo entregan un bot que habla el protocolo
// (docs/protocolo.md). La arena hace el resto.
//
// # Dónde está cada cosa
//
//	arena.go          Tipos base (Participante, Enfrentamiento, Resultado) y el Torneo.
//	formato.go        Cómo se arman los emparejamientos. Extensible por registro.
//	puntuacion.go     Cómo un resultado se convierte en puntos. Extensible por registro.
//	lanzador.go       Cómo se materializa un bot: subproceso local o conexión remota.
//	clasificacion.go  Agregación de resultados en la tabla final.
//	historial.go      Historial de manos en JSON Lines, para auditar y depurar.
//	config.go         Configuración del torneo en JSON.
//
// # Extender la arena
//
// Los dos puntos de extensión son Formato (qué se juega contra qué) y
// Puntuador (cómo se suma). Se registran por nombre y el archivo de
// configuración los elige por ese nombre, así que agregar un formato nuevo no
// obliga a tocar ni el runner ni el CLI:
//
//	func init() {
//	    arena.RegistrarFormato("suizo", func(cfg arena.ConfigTorneo) (arena.Formato, error) {
//	        return &sistemaSuizo{rondas: cfg.Rondas}, nil
//	    })
//	}
//
// La arena no importa el paquete casino: recibe la validación de tokens y el
// reporte de resultados por inyección, igual que la Mesa (docs/interfaces.md §4).
package arena

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/mesa"
)

// --- Participantes ---------------------------------------------------------

// Participante es un bot inscrito en el torneo.
type Participante struct {
	// ID identifica al participante dentro del torneo y es la clave de la
	// clasificación. Debe ser único y estable.
	ID string `json:"id"`
	// Nombre es el nombre a mostrar. Si está vacío se usa el ID.
	Nombre string `json:"nombre,omitempty"`
	// Token es lo que el bot manda en el saludo. Con un Casino detrás, es el
	// token de 'casino login'. En modo abierto vale cualquier cosa y se usa
	// como identificador; si está vacío, la arena usa el ID.
	Token string `json:"token,omitempty"`
	// Comando es el programa a ejecutar para levantar el bot, ya partido en
	// argumentos: ["python", "bot.py"], ["./bin/mi-bot"], etc. La arena le
	// agrega -addr y -token al final.
	//
	// Si está vacío, el bot es remoto: la arena no lo lanza, solo abre el
	// puerto y espera a que se conecte solo (ver LanzadorRemoto).
	Comando []string `json:"comando,omitempty"`
	// Directorio es el directorio de trabajo del comando. Vacío = el de la
	// arena.
	Directorio string `json:"directorio,omitempty"`
	// Entorno son variables de entorno extra para el proceso del bot, en
	// formato "CLAVE=valor".
	Entorno []string `json:"entorno,omitempty"`
}

// NombreVisible devuelve el nombre a mostrar, cayendo al ID si no hay nombre.
func (p Participante) NombreVisible() string {
	if p.Nombre != "" {
		return p.Nombre
	}
	return p.ID
}

// TokenEfectivo devuelve el token con el que el bot se va a presentar.
func (p Participante) TokenEfectivo() string {
	if p.Token != "" {
		return p.Token
	}
	return p.ID
}

// EsRemoto indica si el bot lo corre el participante por su cuenta.
func (p Participante) EsRemoto() bool { return len(p.Comando) == 0 }

// --- Enfrentamientos -------------------------------------------------------

// Enfrentamiento es una partida a jugar entre un conjunto de participantes.
// Un formato produce la lista completa antes de que empiece el torneo.
type Enfrentamiento struct {
	// ID identifica la partida. Se usa como ID de mesa y como nombre del
	// archivo de historial, así que tiene que ser único y apto para nombre de
	// archivo.
	ID string `json:"id"`
	// Ronda agrupa enfrentamientos que se juegan a la vez. Solo es
	// informativa: la arena no ordena por ronda.
	Ronda int `json:"ronda"`
	// Participantes son los IDs que se sientan, en orden de silla. El orden
	// importa: en heads-up, la silla 0 arranca con el botón.
	Participantes []string `json:"participantes"`
	// Semilla es la que usa el crupier de esta partida. Dos enfrentamientos
	// con la misma semilla y la misma cantidad de jugadores reciben
	// exactamente los mismos repartos; es así como el formato de ida y vuelta
	// hace que los dos bots jueguen las mismas cartas en las dos posiciones.
	Semilla uint64 `json:"semilla"`
	// Manos es el límite de manos de la partida. 0 = hasta que quede uno solo.
	Manos int64 `json:"manos"`
}

// Puesto es cómo le fue a un participante en un enfrentamiento.
type Puesto struct {
	IDParticipante string `json:"id_participante"`
	Nombre         string `json:"nombre"`
	Posicion       int    `json:"posicion"` // 1 = ganador.
	Fichas         int64  `json:"fichas"`   // Saldo al terminar.
	Neto           int64  `json:"neto"`     // Fichas ganadas o perdidas respecto del stack inicial.
	Timeouts       uint64 `json:"timeouts"`
	ManosJugadas   uint64 `json:"manos_jugadas"`
}

// ResultadoEnfrentamiento es el cierre de una partida del torneo.
type ResultadoEnfrentamiento struct {
	Enfrentamiento
	Puestos  []Puesto      `json:"puestos"`
	Duracion time.Duration `json:"duracion_ns"`
	// Error, si no está vacío, es el motivo por el que la partida no se pudo
	// jugar (un bot que no arrancó, la mesa que no llegó al mínimo de
	// jugadores). Los puestos pueden venir igual, parciales.
	Error string `json:"error,omitempty"`
}

// Jugado indica si el enfrentamiento produjo un resultado utilizable.
func (r ResultadoEnfrentamiento) Jugado() bool { return r.Error == "" && len(r.Puestos) > 0 }

// --- Configuración de mesa -------------------------------------------------

// ConfigMesa son los parámetros de juego que comparten todas las partidas del
// torneo.
type ConfigMesa struct {
	CiegaChica  uint64 `json:"ciega_chica"`
	CiegaGrande uint64 `json:"ciega_grande"`
	Stack       uint64 `json:"stack"`
	// TimeoutMs es el plazo que tiene un bot para responder a su turno. Es el
	// parámetro que más define el torneo: subilo si querés dejar lugar a bots
	// que piensan, bajalo si querés castigar la lentitud.
	TimeoutMs uint64 `json:"timeout_ms"`
	// EsperaJugadoresMs es cuánto espera la mesa a que se conecten todos los
	// bots antes de dar el enfrentamiento por perdido.
	EsperaJugadoresMs uint64 `json:"espera_jugadores_ms"`
}

// PorDefecto rellena los campos en cero con valores razonables y valida el
// resto. Se llama sola desde ConfigTorneo.Validar.
func (c *ConfigMesa) PorDefecto() error {
	if c.CiegaChica == 0 {
		c.CiegaChica = 10
	}
	if c.CiegaGrande == 0 {
		c.CiegaGrande = 2 * c.CiegaChica
	}
	if c.Stack == 0 {
		c.Stack = 200 * c.CiegaGrande
	}
	if c.TimeoutMs == 0 {
		c.TimeoutMs = 2000
	}
	if c.EsperaJugadoresMs == 0 {
		c.EsperaJugadoresMs = 15000
	}

	if c.CiegaGrande <= c.CiegaChica {
		return fmt.Errorf("arena: la ciega grande (%d) debe ser mayor que la chica (%d)", c.CiegaGrande, c.CiegaChica)
	}
	if c.Stack < c.CiegaGrande {
		return fmt.Errorf("arena: el stack (%d) no alcanza para pagar una ciega grande (%d)", c.Stack, c.CiegaGrande)
	}
	return nil
}

// --- Torneo ----------------------------------------------------------------

// ValidarToken traduce el token que manda un bot a su identidad. Es la misma
// forma que mesa.ValidarToken: la arena tampoco importa el paquete casino
// (docs/interfaces.md §4). Si es nil, las mesas corren en modo abierto y el
// token es directamente el identificador del jugador.
type ValidarToken = mesa.ValidarToken

// Torneo corre una lista de enfrentamientos y agrega los resultados.
type Torneo struct {
	Config   ConfigTorneo
	Formato  Formato
	Puntua   Puntuador
	Lanzador Lanzador
	// Validar es la validación de tokens que se le pasa a cada mesa. nil =
	// modo abierto.
	Validar ValidarToken
	// Historial, si no es nil, recibe cada mano jugada. Se cierra al terminar.
	Historial Historial
	// Log recibe el progreso del torneo. Si es nil, se usa el logger estándar.
	Log *log.Logger

	// Direccion es el host al que se le pide puerto para cada mesa. Vacío =
	// "127.0.0.1", que es lo correcto para bots lanzados localmente: no se
	// expone el torneo a la red sin querer. Poné "0.0.0.0" solo si los bots
	// son remotos y tenés que aceptarlos desde afuera.
	Direccion string
}

// ErrSinParticipantes se devuelve cuando el torneo no tiene con quién jugar.
var ErrSinParticipantes = errors.New("arena: hacen falta al menos 2 participantes")

// Correr juega el torneo completo y devuelve la clasificación final junto a
// los resultados de cada enfrentamiento.
//
// Un enfrentamiento que falla no aborta el torneo: se anota con su Error y se
// sigue. Un bot roto de un participante no puede arruinarle la competencia al
// resto.
func (t *Torneo) Correr(ctx context.Context) (Clasificacion, []ResultadoEnfrentamiento, error) {
	if len(t.Config.Participantes) < 2 {
		return Clasificacion{}, nil, ErrSinParticipantes
	}
	if t.Formato == nil {
		return Clasificacion{}, nil, errors.New("arena: el torneo no tiene formato")
	}
	if t.Puntua == nil {
		return Clasificacion{}, nil, errors.New("arena: el torneo no tiene puntuador")
	}
	if t.Lanzador == nil {
		t.Lanzador = LanzadorPorDefecto()
	}
	if t.Log == nil {
		t.Log = log.Default()
	}

	enfrentamientos, err := t.Formato.Emparejamientos(t.Config)
	if err != nil {
		return Clasificacion{}, nil, fmt.Errorf("arena: no se pudieron armar los emparejamientos: %w", err)
	}
	if len(enfrentamientos) == 0 {
		return Clasificacion{}, nil, errors.New("arena: el formato no produjo ningún enfrentamiento")
	}

	t.Log.Printf("arena: torneo %q, formato %s, %d participantes, %d enfrentamientos",
		t.Config.Nombre, t.Formato.Nombre(), len(t.Config.Participantes), len(enfrentamientos))

	resultados := t.correrEnfrentamientos(ctx, enfrentamientos)

	// Los enfrentamientos terminan en orden impredecible por el paralelismo.
	// Se reordenan para que el archivo de resultados sea comparable entre
	// corridas.
	sort.SliceStable(resultados, func(i, j int) bool {
		if resultados[i].Ronda != resultados[j].Ronda {
			return resultados[i].Ronda < resultados[j].Ronda
		}
		return resultados[i].ID < resultados[j].ID
	})

	clasificacion := Clasificar(t.Config, t.Puntua, resultados)
	return clasificacion, resultados, nil
}

// correrEnfrentamientos juega la lista con el paralelismo configurado.
func (t *Torneo) correrEnfrentamientos(ctx context.Context, enfrentamientos []Enfrentamiento) []ResultadoEnfrentamiento {
	paralelismo := t.Config.Paralelismo
	if paralelismo <= 0 {
		paralelismo = 1
	}
	if paralelismo > len(enfrentamientos) {
		paralelismo = len(enfrentamientos)
	}

	resultados := make([]ResultadoEnfrentamiento, len(enfrentamientos))
	cupos := make(chan struct{}, paralelismo)
	var espera sync.WaitGroup
	var completados int64
	var mu sync.Mutex

	for i, e := range enfrentamientos {
		// Cancelar el torneo deja de lanzar partidas nuevas; las que ya
		// arrancaron se cortan solas por el contexto.
		if ctx.Err() != nil {
			resultados[i] = ResultadoEnfrentamiento{Enfrentamiento: e, Error: ctx.Err().Error()}
			continue
		}

		espera.Add(1)
		cupos <- struct{}{}
		go func(i int, e Enfrentamiento) {
			defer espera.Done()
			defer func() { <-cupos }()

			resultados[i] = t.jugarEnfrentamiento(ctx, e)

			mu.Lock()
			completados++
			hechos := completados
			mu.Unlock()
			t.Log.Printf("arena: [%d/%d] %s — %s", hechos, len(enfrentamientos), e.ID, resumenCorto(resultados[i]))
		}(i, e)
	}
	espera.Wait()
	return resultados
}

// resumenCorto arma la línea de log de un enfrentamiento terminado.
func resumenCorto(r ResultadoEnfrentamiento) string {
	if r.Error != "" {
		return "ERROR: " + r.Error
	}
	texto := ""
	for i, p := range r.Puestos {
		if i > 0 {
			texto += ", "
		}
		texto += fmt.Sprintf("%s %+d", p.Nombre, p.Neto)
	}
	return texto
}

// jugarEnfrentamiento levanta una mesa, conecta a los bots y juega la partida.
func (t *Torneo) jugarEnfrentamiento(ctx context.Context, e Enfrentamiento) ResultadoEnfrentamiento {
	inicio := time.Now()
	resultado := ResultadoEnfrentamiento{Enfrentamiento: e}
	terminar := func(formato string, args ...any) ResultadoEnfrentamiento {
		resultado.Error = fmt.Sprintf(formato, args...)
		resultado.Duracion = time.Since(inicio)
		return resultado
	}

	participantes, err := t.Config.BuscarParticipantes(e.Participantes)
	if err != nil {
		return terminar("%v", err)
	}

	// Un crupier sembrado: dos enfrentamientos con la misma Semilla reparten
	// exactamente las mismas cartas. Es lo que hace comparables la ida y la
	// vuelta de un emparejamiento.
	cpr := crupier.NuevoConSemilla(e.Semilla)

	cfgMesa := mesa.ConfigMesa{
		MaxJugadores: uint8(len(participantes)),
		MinJugadores: uint8(len(participantes)),
		Timeout:      t.Config.Mesa.TimeoutMs,
	}
	if t.Historial != nil {
		cfgMesa.Observador = t.Historial.Observador(e.ID)
	}

	m := mesa.NuevaMesa(e.ID, cfgMesa, mesa.ConfigPartida{
		CiegaMenor:     t.Config.Mesa.CiegaChica,
		CiegaMayor:     t.Config.Mesa.CiegaGrande,
		StackInicial:   t.Config.Mesa.Stack,
		CantidadRondas: rondasDe(e.Manos),
		ReponerStack:   t.Config.ReponerStack,
	}, cpr)

	direccion := t.Direccion
	if direccion == "" {
		direccion = "127.0.0.1"
	}
	// Las sillas las fija el enfrentamiento, no el orden en que los bots
	// logren conectarse. Sin esto, la ida y la vuelta de un emparejamiento
	// pueden terminar con la misma disposición de sillas y ser, en la
	// práctica, la misma partida jugada dos veces: la rotación de posiciones
	// dejaría de compensar nada.
	sillas := make(map[string]int, len(participantes))
	for silla, p := range participantes {
		// La mesa conoce al jugador por lo que devuelve ValidarToken: el token
		// en modo abierto, el ID de cuenta con Casino. Se indexa por los dos.
		sillas[p.ID] = silla
		sillas[p.TokenEfectivo()] = silla
	}

	servidor := mesa.NuevoServidor(mesa.ConfigServidor{
		Direccion:          direccion + ":0", // Puerto libre: se corren muchas mesas a la vez.
		TimeoutHandshakeMs: t.Config.Mesa.EsperaJugadoresMs,
		MaxConexiones:      len(participantes),
		Log:                t.Log,
		SillaDe: func(idJugador string) (int, bool) {
			silla, ok := sillas[idJugador]
			return silla, ok
		},
	}, m, t.Validar)

	if err := servidor.Abrir(); err != nil {
		return terminar("no se pudo abrir el puerto de la mesa: %v", err)
	}
	defer servidor.Cerrar()

	ctxPartida, cancelar := context.WithCancel(ctx)
	defer cancelar()

	go func() {
		if err := servidor.Servir(ctxPartida); err != nil {
			t.Log.Printf("arena: %s: servidor detenido: %v", e.ID, err)
		}
	}()

	// Se lanzan los bots ya con la dirección real de la mesa. Los que fallen al
	// arrancar se reportan como error del enfrentamiento: jugar 1v0 no tiene
	// sentido, y darle la victoria por defecto al rival sin avisar sería peor.
	sesiones := make([]Sesion, 0, len(participantes))
	defer func() {
		for _, s := range sesiones {
			s.Detener()
		}
	}()
	for _, p := range participantes {
		s, err := t.Lanzador.Lanzar(ctxPartida, p, servidor.Direccion())
		if err != nil {
			return terminar("no se pudo lanzar el bot de %s: %v", p.NombreVisible(), err)
		}
		sesiones = append(sesiones, s)
	}

	if err := esperarJugadores(ctxPartida, m, len(participantes), t.Config.Mesa.EsperaJugadoresMs); err != nil {
		return terminar("%v (conectados %d de %d)", err, m.Sentados(), len(participantes))
	}

	resumen, err := m.Jugar(ctxPartida)
	resultado.Duracion = time.Since(inicio)
	resultado.Puestos = puestosDesde(resumen, participantes)
	if err != nil {
		resultado.Error = err.Error()
	}
	return resultado
}

// rondasDe traduce el límite de manos de un enfrentamiento al formato que
// espera mesa.ConfigPartida (-1 = sin límite).
func rondasDe(manos int64) int64 {
	if manos <= 0 {
		return -1
	}
	return manos
}

// puestosDesde convierte el resumen de la mesa en los puestos del torneo.
func puestosDesde(resumen mesa.ResumenPartida, participantes []Participante) []Puesto {
	nombres := make(map[string]string, len(participantes))
	// El ID con el que la mesa conoce al jugador es el que devolvió
	// ValidarToken: en modo abierto es el token, con Casino es el ID de
	// cuenta. Se indexa por las dos claves para poder mapear en ambos casos.
	porToken := make(map[string]string, len(participantes))
	for _, p := range participantes {
		nombres[p.ID] = p.NombreVisible()
		porToken[p.TokenEfectivo()] = p.ID
	}

	puestos := make([]Puesto, 0, len(resumen.Posiciones))
	for _, pos := range resumen.Posiciones {
		id := pos.IDJugador
		if idParticipante, ok := porToken[id]; ok {
			id = idParticipante
		}
		nombre := nombres[id]
		if nombre == "" {
			nombre = pos.Nombre
		}
		if nombre == "" {
			nombre = id
		}
		puestos = append(puestos, Puesto{
			IDParticipante: id,
			Nombre:         nombre,
			Posicion:       pos.Posicion,
			Fichas:         int64(pos.SaldoFinal),
			// El neto lo calcula la Mesa: con reposición de stack no es
			// SaldoFinal - stack, sino la suma de todas las manos.
			Neto:         pos.Neto,
			Timeouts:     pos.Timeouts,
			ManosJugadas: pos.ManosJugadas,
		})
	}
	return puestos
}

// esperarJugadores bloquea hasta que se sienten `faltan` jugadores, se agote
// el plazo o se cancele el contexto.
func esperarJugadores(ctx context.Context, m mesa.MesaInterface, faltan int, esperaMs uint64) error {
	limite := time.NewTimer(time.Duration(esperaMs) * time.Millisecond)
	defer limite.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		if m.Sentados() >= faltan {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-limite.C:
			return fmt.Errorf("los bots no se conectaron dentro de los %d ms", esperaMs)
		case <-tick.C:
		}
	}
}
