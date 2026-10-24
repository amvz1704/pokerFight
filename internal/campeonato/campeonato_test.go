package campeonato

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amvz1704/pokerFight/internal/protocolo"
	"github.com/amvz1704/pokerFight/pkg/botsdk"
)

// --- Bots de prueba -------------------------------------------------------------

type decisor func(t botsdk.Turno) botsdk.Accion

func pasivo(t botsdk.Turno) botsdk.Accion {
	if t.Puede(botsdk.Check) {
		return t.Check()
	}
	return t.Pagar()
}

// conPar va all-in con un par en la mano y si no pasa o se retira. Es lo que
// hace que unos bots ganen fichas y otros las pierdan, sin azar propio: el
// campeonato de prueba tiene que ser reproducible.
func conPar(t botsdk.Turno) botsdk.Accion {
	if t.Mano[0].Rango == t.Mano[1].Rango && t.Puede(botsdk.AllIn) {
		return t.AllIn()
	}
	if t.Puede(botsdk.Check) {
		return t.Check()
	}
	return t.Fold()
}

// timido nunca paga: pasa si es gratis y si no se retira. Pierde las ciegas.
func timido(t botsdk.Turno) botsdk.Accion {
	if t.Puede(botsdk.Check) {
		return t.Check()
	}
	return t.Fold()
}

// botPrueba juega con un decisor y anota todo lo que ve.
type botPrueba struct {
	decidir decisor

	mu           sync.Mutex
	nombres      map[string]bool // Todo ID o nombre de jugador que vio en un estado.
	idsPropios   map[string]bool
	manos        int
	ciegaMin     int64
	ciegaMax     int64
	alIniciar    func(manos int) // Se llama en cada mano nueva.
	antesDeJugar func()
}

func nuevoBot(d decisor) *botPrueba {
	return &botPrueba{decidir: d, nombres: map[string]bool{}, idsPropios: map[string]bool{}}
}

func (b *botPrueba) Nombre() string { return "prueba" }

func (b *botPrueba) observar(e botsdk.EstadoPublico) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, j := range e.Jugadores {
		b.nombres[j.ID] = true
		b.nombres[j.Nombre] = true
	}
	if e.CiegaGrande > 0 {
		if b.ciegaMin == 0 || e.CiegaGrande < b.ciegaMin {
			b.ciegaMin = e.CiegaGrande
		}
		if e.CiegaGrande > b.ciegaMax {
			b.ciegaMax = e.CiegaGrande
		}
	}
}

func (b *botPrueba) Decidir(ctx context.Context, t botsdk.Turno) botsdk.Accion {
	b.observar(t.Estado)
	b.mu.Lock()
	b.idsPropios[t.IDJugador] = true
	b.mu.Unlock()
	if b.antesDeJugar != nil {
		b.antesDeJugar()
	}
	return b.decidir(t)
}

func (b *botPrueba) ManoInicio(e botsdk.EstadoPublico, _ botsdk.Mano) {
	b.observar(e)
	b.mu.Lock()
	b.manos++
	n := b.manos
	b.mu.Unlock()
	if b.alIniciar != nil {
		b.alIniciar(n)
	}
}

func (b *botPrueba) ManoFin(botsdk.ResultadoMano)             {}
func (b *botPrueba) EstadoActualizado(e botsdk.EstadoPublico) { b.observar(e) }

// --- Armado -------------------------------------------------------------------------

func inscritosDePrueba(t *testing.T, n int) Inscritos {
	t.Helper()
	var l Inscritos
	for i := 0; i < n; i++ {
		// Dos bots por equipo, como permiten las bases.
		if _, err := l.Agregar(fmt.Sprintf("Equipo %d", i/2+1), fmt.Sprintf("bot%d", i%2+1)); err != nil {
			t.Fatalf("Agregar: %v", err)
		}
	}
	return l
}

var ciegasDePrueba = Ciegas{Niveles: []uint64{20, 40, 80, 160, 320}, ManosPorNivel: 10}

// configDePrueba es un campeonato chico con las tres clases de fase de las bases:
// clasificatoria de dos rondas, una eliminatoria por mesas y una mesa final.
func configDePrueba(t *testing.T) Config {
	t.Helper()
	semilla := uint64(2026)
	cfg := Config{
		Nombre:  "prueba",
		Semilla: &semilla,
		Salida:  t.TempDir(),
		Mesa:    ConfigMesa{TimeoutMs: 3000},
		Fases: []Fase{
			{Nombre: "Clasificatoria", Tipo: Clasificatoria, JugadoresPorMesa: 5, Stack: 1000,
				Rondas: 2, ManosPorRonda: 30, Clasifican: 4, Ciegas: ciegasDePrueba},
			{Nombre: "Semifinal", Tipo: Eliminatoria, JugadoresPorMesa: 2, AvanzanPorMesa: 1,
				Stack: 1000, MaxManos: 300, Ciegas: ciegasDePrueba},
			{Nombre: "Final", Tipo: Eliminatoria, Mesas: 1, AvanzanPorMesa: 1,
				Stack: 1000, MaxManos: 300, Ciegas: ciegasDePrueba},
		},
	}
	if err := cfg.Validar(); err != nil {
		t.Fatalf("configuracion de prueba invalida: %v", err)
	}
	return cfg
}

func silencioso() *log.Logger { return log.New(io.Discard, "", 0) }

// campeonatoDePrueba arma un campeonato escuchando en un puerto libre y lo apaga
// al terminar el test.
func campeonatoDePrueba(t *testing.T, cfg Config, ins Inscritos) *Campeonato {
	t.Helper()
	c, err := Nuevo(Opciones{Config: cfg, Inscritos: ins, Direccion: "127.0.0.1:0", Log: silencioso()})
	if err != nil {
		t.Fatalf("Nuevo: %v", err)
	}
	if err := c.Abrir(); err != nil {
		t.Fatalf("Abrir: %v", err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	go c.Servir(ctx)
	t.Cleanup(func() {
		cancelar()
		c.Cerrar()
	})
	return c
}

// conectar arranca un bot de verdad (botsdk, por TCP) y devuelve el canal con
// lo que devuelve Correr cuando el bot termina.
func conectar(c *Campeonato, in Inscrito, bot botsdk.Bot) <-chan error {
	fin := make(chan error, 1)
	go func() {
		fin <- botsdk.Correr(context.Background(), bot, botsdk.Opciones{
			Direccion: c.Direccion(), Token: in.Token,
		})
	}()
	return fin
}

func esperar(t *testing.T, plazo time.Duration, que string, cumple func() bool) {
	t.Helper()
	limite := time.Now().Add(plazo)
	for !cumple() {
		if time.Now().After(limite) {
			t.Fatalf("pasaron %v sin que ocurriera: %s", plazo, que)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func esperarConectados(t *testing.T, c *Campeonato, n int) {
	t.Helper()
	esperar(t, 10*time.Second, fmt.Sprintf("que se conecten %d bots (hay %d)", n, c.lobby.Conectados()),
		func() bool { return c.lobby.Conectados() >= n })
}

func esperarEtapa(t *testing.T, c *Campeonato, etapa Etapa) {
	t.Helper()
	esperar(t, 90*time.Second, "que el campeonato llegue a "+string(etapa),
		func() bool { return c.Instantanea().Etapa == etapa })
}

// --- El campeonato completo ---------------------------------------------------------

// TestUnCampeonatoCompletoDePuntaAPunta es la prueba que importa: doce bots de
// verdad se conectan UNA vez al lobby y el campeonato los mueve de mesa en mesa
// por las tres fases hasta coronar a uno. Todo lo demas son piezas de esto.
func TestUnCampeonatoCompletoDePuntaAPunta(t *testing.T) {
	cfg := configDePrueba(t)
	ins := inscritosDePrueba(t, 12)
	c := campeonatoDePrueba(t, cfg, ins)

	decisores := []decisor{pasivo, conPar, timido}
	bots := make([]*botPrueba, len(ins))
	fines := make([]<-chan error, len(ins))
	for i, in := range ins {
		bots[i] = nuevoBot(decisores[i%len(decisores)])
		fines[i] = conectar(c, in, bots[i])
	}
	esperarConectados(t, c, len(ins))

	if err := c.Iniciar(context.Background(), true); err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	esperarEtapa(t, c, EtapaTerminado)

	snap := c.Instantanea()
	if snap.Campeon == "" {
		t.Fatal("el campeonato termino sin campeon")
	}

	// Quien salio y por que: 12 jugadores, 1 campeon, 3 eliminados en las dos
	// fases finales y 8 que no pasaron la clasificatoria.
	cuenta := map[string]int{}
	for _, j := range snap.Jugadores {
		cuenta[j.Estado]++
	}
	if cuenta[MotivoCampeon] != 1 || cuenta["en_juego"] != 0 {
		t.Errorf("estados finales = %v, se esperaba 1 campeon y ninguno en juego", cuenta)
	}
	if cuenta[MotivoEliminado] != 3 {
		t.Errorf("eliminados en las fases finales = %d, se esperaban 3 (2 semifinales + 1 final)", cuenta[MotivoEliminado])
	}
	if fuera := cuenta[MotivoQuiebra] + cuenta[MotivoNoClasifico]; fuera != 8 {
		t.Errorf("fuera en la clasificatoria = %d, se esperaban 8 (pasan 4 de 12)", fuera)
	}

	// Las fichas no se crean ni se pierden: en cada mesa de cada ronda, y en la
	// clasificatoria tambien en total (12 bots x 1000).
	c.mu.Lock()
	rondas := append([]RondaJugada(nil), c.est.Rondas...)
	c.mu.Unlock()
	if len(rondas) != 4 { // 2 de clasificatoria + semifinal + final
		t.Fatalf("se jugaron %d rondas, se esperaban 4", len(rondas))
	}
	for _, r := range rondas {
		var totalFin uint64
		for _, m := range r.Mesas {
			var ini, fin uint64
			for _, j := range m.Jugadores {
				ini += j.FichasInicio
				fin += j.FichasFin
			}
			if ini != fin {
				t.Errorf("mesa %s: arrancaron con %d fichas y terminaron con %d", m.ID, ini, fin)
				for _, j := range m.Jugadores {
					t.Logf("   %s: %d -> %d (puesto %d, %d timeouts)", j.Alias, j.FichasInicio, j.FichasFin, j.Posicion, j.Timeouts)
				}
			}
			totalFin += fin
		}
		if r.Fase == 0 && totalFin != 12*1000 {
			t.Errorf("clasificatoria, ronda %d: %d fichas en total, se esperaban 12000", r.Ronda+1, totalFin)
		}
	}

	// Anonimato: lo unico que un bot ve de los demas es un alias aleatorio.
	// Ni el equipo, ni el nombre del bot, ni el id de inscripcion.
	alias := regexp.MustCompile(`^j-[0-9a-f]{6}$`)
	for i, b := range bots {
		b.mu.Lock()
		for nombre := range b.nombres {
			if !alias.MatchString(nombre) {
				t.Errorf("el bot %d vio %q como nombre de un jugador: tenia que ser un alias aleatorio", i, nombre)
			}
			for _, in := range ins {
				if strings.Contains(nombre, in.ID) || strings.Contains(nombre, in.Equipo) {
					t.Errorf("el bot %d vio un nombre que delata a %s: %q", i, in.ID, nombre)
				}
			}
		}
		b.mu.Unlock()
	}

	// Las ciegas subieron a lo largo del campeonato.
	maximo := int64(0)
	for _, b := range bots {
		b.mu.Lock()
		if b.ciegaMax > maximo {
			maximo = b.ciegaMax
		}
		b.mu.Unlock()
	}
	if maximo <= 2*int64(ciegasDePrueba.Niveles[0]) {
		t.Errorf("la ciega grande mas alta que vio un bot fue %d: las ciegas no subieron", maximo)
	}

	// Cada bot jugo todo el campeonato con la MISMA conexion: nunca se
	// desconecto ni se le pidio que volviera a conectarse. Es la promesa de las
	// bases ("basta con conectarlo y dejarlo ejecutandose").
	c.Cerrar()
	for i, fin := range fines {
		select {
		case err := <-fin:
			if !errors.Is(err, botsdk.ErrMesaCerro) {
				t.Errorf("el bot %d termino con %v, se esperaba un cierre limpio de la mesa", i, err)
			}
		case <-time.After(10 * time.Second):
			t.Errorf("el bot %d no termino cuando se cerro el campeonato", i)
		}
	}
}

// TestElCampeonatoSeRetomaDesdeElDisco: la clasificatoria y la final de las
// bases son dias distintos. Al volver a levantar la arena, todo sigue donde
// estaba, y los bots conservan su alias.
func TestElCampeonatoSeRetomaDesdeElDisco(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1] // Solo la clasificatoria.
	cfg.Fases[0].Rondas = 2
	ins := inscritosDePrueba(t, 6)

	primero := campeonatoDePrueba(t, cfg, ins)
	for i, in := range ins {
		conectar(primero, in, nuevoBot([]decisor{pasivo, conPar}[i%2]))
	}
	esperarConectados(t, primero, len(ins))

	// Se juega UNA de las dos rondas.
	if err := primero.JugarRonda(context.Background()); err != nil {
		t.Fatalf("JugarRonda: %v", err)
	}
	antes := primero.Instantanea()
	if antes.Etapa != EtapaEnCurso || antes.Ronda != 2 {
		t.Fatalf("tras una ronda: etapa %s, proxima ronda %d; se esperaba en_curso y la 2", antes.Etapa, antes.Ronda)
	}
	primero.Cerrar()

	// Se levanta de nuevo sobre el mismo directorio.
	segundo, err := Nuevo(Opciones{Config: cfg, Inscritos: ins, Direccion: "127.0.0.1:0", Log: silencioso()})
	if err != nil {
		t.Fatalf("Nuevo sobre el estado guardado: %v", err)
	}
	t.Cleanup(segundo.Cerrar)
	despues := segundo.Instantanea()

	if despues.Etapa != EtapaEnCurso || despues.Ronda != 2 {
		t.Errorf("tras retomar: etapa %s, proxima ronda %d; se esperaba en_curso y la 2", despues.Etapa, despues.Ronda)
	}
	fichas := func(s Instantanea) map[string]uint64 {
		m := map[string]uint64{}
		for _, j := range s.Jugadores {
			m[j.Alias] = j.Fichas
		}
		return m
	}
	a, d := fichas(antes), fichas(despues)
	if len(a) != len(d) {
		t.Fatalf("tras retomar hay %d jugadores, antes habia %d", len(d), len(a))
	}
	for alias, f := range a {
		if d[alias] != f {
			t.Errorf("%s tenia %d fichas y al retomar tiene %d (o cambio de alias)", alias, f, d[alias])
		}
	}
}

// --- Lo que pasa cuando algo falla ---------------------------------------------------

// TestUnBotQueNoSeConectaJuegaConLaAccionSegura es la regla 4 de las bases:
// "si se desconecta, la arena juega por el de forma segura (check cuando sea
// posible; si no, fold) hasta que vuelva". La mesa no se traba y el ausente
// va perdiendo sus fichas en las ciegas.
func TestUnBotQueNoSeConectaJuegaConLaAccionSegura(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1]
	cfg.Fases[0].Rondas = 1
	cfg.Fases[0].Clasifican = 2
	cfg.Fases[0].Ciegas = Ciegas{Niveles: []uint64{50}, ManosPorNivel: 100}
	ins := inscritosDePrueba(t, 5)
	c := campeonatoDePrueba(t, cfg, ins)

	// Se conectan 4 de 5.
	for _, in := range ins[:4] {
		conectar(c, in, nuevoBot(pasivo))
	}
	esperarConectados(t, c, 4)

	inicio := time.Now()
	if err := c.JugarRonda(context.Background()); err != nil {
		t.Fatalf("JugarRonda: %v", err)
	}
	if demora := time.Since(inicio); demora > 20*time.Second {
		t.Errorf("la ronda tardo %v: un bot ausente no puede frenar la mesa", demora)
	}

	conectados := map[string]bool{}
	for _, v := range c.lobby.VistaPresencias() {
		conectados[v.Alias] = v.Conectado
	}
	var ausente *VistaJugador
	snap := c.Instantanea()
	for i := range snap.Jugadores {
		if !conectados[snap.Jugadores[i].Alias] {
			ausente = &snap.Jugadores[i]
		}
	}
	if ausente == nil {
		t.Fatal("no se encontro al jugador que no se conecto")
	}
	if ausente.ManosJugadas == 0 {
		t.Error("el ausente tiene que haber jugado manos: la arena juega por el")
	}
	if ausente.Timeouts == 0 {
		t.Error("las jugadas hechas por la arena tienen que contar como timeouts")
	}
	if ausente.Fichas >= 1000 {
		t.Errorf("el ausente tiene %d fichas, tenia que ir perdiendolas en las ciegas", ausente.Fichas)
	}
}

// TestUnBotQueSeCaeYVuelveSigueEnLaMismaPartida: pierde el socket a mitad de la
// ronda y se reconecta con su token. Conserva alias y fichas, y la mesa sigue
// hablandole por el socket nuevo.
func TestUnBotQueSeCaeYVuelveSigueEnLaMismaPartida(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1]
	cfg.Fases[0].Rondas = 1
	cfg.Fases[0].ManosPorRonda = 80
	cfg.Fases[0].Clasifican = 2
	cfg.Fases[0].Ciegas = Ciegas{Niveles: []uint64{10}, ManosPorNivel: 100}
	ins := inscritosDePrueba(t, 5)
	c := campeonatoDePrueba(t, cfg, ins)

	for _, in := range ins[1:] {
		conectar(c, in, nuevoBot(pasivo))
	}

	// El bot caedizo corta su conexion tras 10 manos y vuelve 200 ms despues.
	caedizo := nuevoBot(pasivo)
	var manosPorConexion []int
	var mu sync.Mutex
	registrarConexion := func(manos int) {
		mu.Lock()
		manosPorConexion = append(manosPorConexion, manos)
		mu.Unlock()
	}
	cortada := make(chan struct{})
	ctx1, cortar := context.WithCancel(context.Background())
	var una sync.Once
	caedizo.alIniciar = func(n int) {
		if n == 10 {
			una.Do(func() { close(cortada); cortar() })
		}
	}
	terminoElPrimero := make(chan struct{})
	go func() {
		botsdk.Correr(ctx1, caedizo, botsdk.Opciones{Direccion: c.Direccion(), Token: ins[0].Token})
		close(terminoElPrimero)
	}()
	esperarConectados(t, c, 5)

	finSegundo := make(chan error, 1)
	segundo := nuevoBot(pasivo)
	go func() {
		<-cortada
		<-terminoElPrimero
		caedizo.mu.Lock()
		registrarConexion(caedizo.manos)
		caedizo.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
		finSegundo <- botsdk.Correr(context.Background(), segundo, botsdk.Opciones{Direccion: c.Direccion(), Token: ins[0].Token})
	}()

	if err := c.JugarRonda(context.Background()); err != nil {
		t.Fatalf("JugarRonda: %v", err)
	}

	mu.Lock()
	antes := manosPorConexion
	mu.Unlock()
	if len(antes) != 1 || antes[0] < 10 {
		t.Fatalf("manos que vio la primera conexion = %v, se esperaban al menos 10", antes)
	}

	segundo.mu.Lock()
	manosDespues := segundo.manos
	idsDespues := len(segundo.idsPropios)
	segundo.mu.Unlock()
	if manosDespues == 0 {
		t.Error("el bot volvio a conectarse pero la mesa nunca le hablo por la conexion nueva")
	}

	// Misma identidad antes y despues.
	caedizo.mu.Lock()
	idsAntes := len(caedizo.idsPropios)
	var aliasAntes string
	for id := range caedizo.idsPropios {
		aliasAntes = id
	}
	caedizo.mu.Unlock()
	segundo.mu.Lock()
	for id := range segundo.idsPropios {
		if id != aliasAntes {
			t.Errorf("tras reconectarse su identidad es %s y antes era %s", id, aliasAntes)
		}
	}
	segundo.mu.Unlock()
	if idsAntes != 1 || idsDespues > 1 {
		t.Errorf("identidades vistas: antes %d, despues %d; cada una deberia ser una sola", idsAntes, idsDespues)
	}

	if v := c.lobby.Presencia(aliasAntes).vista(); v.Caidas == 0 {
		t.Error("el lobby no registro la caida del bot")
	}
}

// TestUnaCredencialDesconocidaNoEntra: el lobby no es un servidor abierto.
func TestUnaCredencialDesconocidaNoEntra(t *testing.T) {
	c := campeonatoDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))

	fin := conectar(c, Inscrito{Token: "pfb-inventado"}, nuevoBot(pasivo))
	select {
	case err := <-fin:
		if err == nil || !strings.Contains(err.Error(), "rechaz") {
			t.Fatalf("error = %v, se esperaba que la mesa rechazara la conexion", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el bot con una credencial inventada no fue rechazado")
	}
	if c.lobby.Conectados() != 0 {
		t.Errorf("hay %d conectados tras un intento rechazado", c.lobby.Conectados())
	}
}

// TestCancelarDescartaLaRondaEntera: no se aplica ningun resultado parcial, y si
// era la primera ronda el campeonato vuelve a poder configurarse.
func TestCancelarDescartaLaRondaEntera(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Mesa.TimeoutMs = 1000
	ins := inscritosDePrueba(t, 10)
	c := campeonatoDePrueba(t, cfg, ins)

	for _, in := range ins {
		b := nuevoBot(pasivo)
		b.antesDeJugar = func() { time.Sleep(150 * time.Millisecond) } // Para dar tiempo a cancelar.
		conectar(c, in, b)
	}
	esperarConectados(t, c, len(ins))

	if err := c.Iniciar(context.Background(), true); err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	esperar(t, 10*time.Second, "que haya mesas en juego", func() bool { return len(c.Instantanea().Mesas) > 0 })

	c.Cancelar()
	esperar(t, 20*time.Second, "que la ronda cancelada termine", func() bool { return !c.Instantanea().Jugando })

	snap := c.Instantanea()
	if snap.Etapa != EtapaInscripcion {
		t.Errorf("etapa = %s, tras cancelar la primera ronda se esperaba %s", snap.Etapa, EtapaInscripcion)
	}
	if len(snap.Rondas) != 0 {
		t.Errorf("hay %d rondas registradas tras cancelar", len(snap.Rondas))
	}
	for _, j := range snap.Jugadores {
		if j.ManosJugadas != 0 || j.Estado != "en_juego" {
			t.Errorf("%s: %d manos, estado %s; no se tendria que haber aplicado nada", j.Alias, j.ManosJugadas, j.Estado)
		}
	}
	if err := c.ActualizarConfig(cfg); err != nil {
		t.Errorf("tras cancelar la primera ronda la configuracion deberia poder cambiarse: %v", err)
	}
}

func TestLaConfiguracionNoSeCambiaDespuesDeEmpezar(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1]
	cfg.Fases[0].Rondas = 1
	ins := inscritosDePrueba(t, 4)
	c := campeonatoDePrueba(t, cfg, ins)
	for _, in := range ins {
		conectar(c, in, nuevoBot(pasivo))
	}
	esperarConectados(t, c, 4)

	// Antes de empezar se puede, y lo que se cambia rige.
	nueva := cfg
	nueva.Fases = append([]Fase(nil), cfg.Fases...)
	nueva.Fases[0].Clasifican = 3
	if err := c.ActualizarConfig(nueva); err != nil {
		t.Fatalf("ActualizarConfig antes de empezar: %v", err)
	}
	if got := c.Config().Fases[0].Clasifican; got != 3 {
		t.Errorf("clasifican = %d, se esperaba 3", got)
	}
	if err := c.ActualizarConfig(Config{}); err == nil {
		t.Error("una configuracion invalida tiene que rechazarse")
	}

	if err := c.JugarRonda(context.Background()); err != nil {
		t.Fatalf("JugarRonda: %v", err)
	}
	if err := c.ActualizarConfig(nueva); err == nil {
		t.Error("cambiar la configuracion con el campeonato empezado tiene que fallar")
	}
	if err := c.JugarRonda(context.Background()); !errors.Is(err, ErrTerminado) {
		t.Errorf("jugar un campeonato terminado dio %v, se esperaba ErrTerminado", err)
	}
}

// TestUnaFaseDondeNadieSeEliminaNoSeJuega: si en una mesa pasan todos, no hay
// nada que decidir y no se reparten cartas.
func TestUnaFaseDondeNadieSeEliminaNoSeJuega(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = []Fase{
		{Nombre: "Clasificatoria", Tipo: Clasificatoria, JugadoresPorMesa: 3, Stack: 1000, Rondas: 1,
			ManosPorRonda: 10, Clasifican: 3, Ciegas: ciegasDePrueba},
		{Nombre: "Pasan todos", Tipo: Eliminatoria, JugadoresPorMesa: 3, AvanzanPorMesa: 3,
			Stack: 0, MaxManos: 50, Ciegas: ciegasDePrueba},
		{Nombre: "Final", Tipo: Eliminatoria, Mesas: 1, AvanzanPorMesa: 1,
			Stack: 1000, MaxManos: 100, Ciegas: ciegasDePrueba},
	}
	ins := inscritosDePrueba(t, 3)
	c := campeonatoDePrueba(t, cfg, ins)
	for _, in := range ins {
		conectar(c, in, nuevoBot(pasivo))
	}
	esperarConectados(t, c, 3)

	if err := c.Iniciar(context.Background(), true); err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	esperarEtapa(t, c, EtapaTerminado)

	c.mu.Lock()
	defer c.mu.Unlock()
	saltada := c.est.Rondas[1]
	for _, m := range saltada.Mesas {
		if m.Manos != 0 {
			t.Errorf("la mesa %s de la fase donde pasan todos jugo %d manos", m.ID, m.Manos)
		}
	}
	if c.est.Campeon == "" {
		t.Error("tras la fase saltada, la final tendria que haber coronado a alguien")
	}
}

// --- Decisiones internas --------------------------------------------------------------

// fabricar arma un campeonato sin red con jugadores de fichas dadas, para probar
// las reglas de clasificacion sin jugar.
func fabricar(t *testing.T, fichas ...uint64) (*Campeonato, []string) {
	t.Helper()
	cfg := configDePrueba(t)
	c, err := Nuevo(Opciones{Config: cfg, Inscritos: inscritosDePrueba(t, len(fichas)), Direccion: "127.0.0.1:0", Log: silencioso()})
	if err != nil {
		t.Fatalf("Nuevo: %v", err)
	}
	t.Cleanup(c.Cerrar)

	alias := c.est.todosLosAlias()
	for i, a := range alias {
		c.est.Jugadores[a].Fichas = fichas[i]
	}
	c.est.Vivos = alias
	c.est.EntraronFase = len(alias)
	return c, alias
}

func TestLosQueClasificanSonLosDeMasFichas(t *testing.T) {
	c, alias := fabricar(t, 500, 4000, 1500, 3000, 2500, 100)
	fase := Fase{Clasifican: 3}

	pasan := c.cerrarClasificatoria(fase, 0)
	if len(pasan) != 3 {
		t.Fatalf("pasan %d, se esperaban 3", len(pasan))
	}
	esperado := []string{alias[1], alias[3], alias[4]} // 4000, 3000, 2500
	for i, a := range esperado {
		if pasan[i] != a {
			t.Errorf("pasa en el puesto %d: %s (%d fichas), se esperaba %s (%d)",
				i+1, pasan[i], c.est.Jugadores[pasan[i]].Fichas, a, c.est.Jugadores[a].Fichas)
		}
	}
	if j := c.est.Jugadores[alias[0]]; j.Motivo != MotivoNoClasifico || j.Puesto != 5 {
		t.Errorf("el de 500 fichas: motivo %q, puesto %d; se esperaba no_clasifico y 5", j.Motivo, j.Puesto)
	}
}

// TestElEmpateEnElCorteSeDesempataPorTimeouts: dos bots con las mismas fichas
// en el ultimo lugar que clasifica. Pasa el que menos veces dejo a la arena
// jugar por el, que es el mas confiable.
func TestElEmpateEnElCorteSeDesempataPorTimeouts(t *testing.T) {
	c, alias := fabricar(t, 3000, 2000, 2000, 1000)
	c.est.Jugadores[alias[1]].Timeouts = 9
	c.est.Jugadores[alias[2]].Timeouts = 2

	pasan := c.cerrarClasificatoria(Fase{Clasifican: 2}, 0)

	if len(pasan) != 2 || pasan[0] != alias[0] || pasan[1] != alias[2] {
		t.Fatalf("pasan %v, se esperaban %s (3000) y %s (empate, menos timeouts)", pasan, alias[0], alias[2])
	}
}

func TestClasificanAutomaticoUsaLaCantidadDeQuienesEntraron(t *testing.T) {
	c, _ := fabricar(t, 100, 200, 300, 400, 500, 600, 700, 800)
	pasan := c.cerrarClasificatoria(Fase{Clasifican: 0}, 0)
	if want := ClasificanAuto(8); len(pasan) != want {
		t.Fatalf("con 8 jugadores pasaron %d, ClasificanAuto(8) = %d", len(pasan), want)
	}
}

func TestNoPasanMasJugadoresDeLosQueQuedan(t *testing.T) {
	c, _ := fabricar(t, 100, 200, 300)
	if pasan := c.cerrarClasificatoria(Fase{Clasifican: 10}, 0); len(pasan) != 3 {
		t.Fatalf("pasan %d, se esperaban los 3 que quedan", len(pasan))
	}
}

func TestSeConcilianLosInscritosSoloAntesDeEmpezar(t *testing.T) {
	ins := inscritosDePrueba(t, 4)
	est, err := nuevoEstado(ins, nil)
	if err != nil {
		t.Fatalf("nuevoEstado: %v", err)
	}
	aliasAntes := est.aliasPorInscrito()

	// Antes de empezar: altas y bajas, y los que siguen conservan su alias.
	mas := append(Inscritos(nil), ins[1:]...)
	if _, err := mas.Agregar("Equipo Nuevo", "bot1"); err != nil {
		t.Fatalf("Agregar: %v", err)
	}
	if err := est.conciliar(mas, nil); err != nil {
		t.Fatalf("conciliar antes de empezar: %v", err)
	}
	if len(est.Jugadores) != 4 {
		t.Errorf("hay %d jugadores, se esperaban 4 (uno menos, uno mas)", len(est.Jugadores))
	}
	aliasDespues := est.aliasPorInscrito()
	for _, in := range ins[1:] {
		if aliasAntes[in.ID] != aliasDespues[in.ID] {
			t.Errorf("%s cambio de alias al conciliar", in.ID)
		}
	}
	if _, sigue := aliasDespues[ins[0].ID]; sigue {
		t.Error("el inscrito dado de baja sigue en el estado")
	}

	// Una vez empezado, la lista es la que es.
	est.Etapa = EtapaEnCurso
	if err := est.conciliar(ins, nil); err == nil {
		t.Error("cambiar la lista de inscritos con el campeonato empezado tiene que fallar")
	}
}

// TestLosAliasSalenDeLaSemilla: el sorteo baraja a los jugadores en orden de
// alias. Si los alias fueran distintos en cada corrida, la misma semilla armaria
// mesas distintas y el campeonato no se podria repetir.
func TestLosAliasSalenDeLaSemilla(t *testing.T) {
	ins := inscritosDePrueba(t, 8)
	semilla := uint64(11)

	a, _ := nuevoEstado(ins, &semilla)
	b, _ := nuevoEstado(ins, &semilla)
	for id, alias := range a.aliasPorInscrito() {
		if b.aliasPorInscrito()[id] != alias {
			t.Errorf("%s tuvo alias %s y %s con la misma semilla", id, alias, b.aliasPorInscrito()[id])
		}
	}

	otra := uint64(12)
	c, _ := nuevoEstado(ins, &otra)
	if fmt.Sprint(a.todosLosAlias()) == fmt.Sprint(c.todosLosAlias()) {
		t.Error("dos semillas distintas dieron los mismos alias")
	}

	d, _ := nuevoEstado(ins, nil)
	e, _ := nuevoEstado(ins, nil)
	if fmt.Sprint(d.todosLosAlias()) == fmt.Sprint(e.todosLosAlias()) {
		t.Error("sin semilla, dos campeonatos recibieron los mismos alias")
	}
}

// TestUnaLineaIlegibleNoDesconectaAlBot: el SDK no reintenta, asi que cortarle
// el socket a un bot por una sola respuesta mal formada lo sacaria del
// campeonato para siempre por un error que pudo ser de una sola jugada.
func TestUnaLineaIlegibleNoDesconectaAlBot(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1]
	cfg.Fases[0].Rondas = 1
	cfg.Fases[0].Clasifican = 2
	cfg.Fases[0].ManosPorRonda = 6
	ins := inscritosDePrueba(t, 3)
	c := campeonatoDePrueba(t, cfg, ins)

	conectar(c, ins[1], nuevoBot(pasivo))
	conectar(c, ins[2], nuevoBot(pasivo))

	// El tercer jugador es un cliente crudo: habla el protocolo a mano.
	conn, err := net.Dial("tcp", c.Direccion())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, `{"tipo":"saludo","version":%q,"token":%q}`+"\n", protocolo.VersionProtocolo, ins[0].Token)
	lector := bufio.NewScanner(conn)
	lector.Buffer(make([]byte, 0, 1<<16), 1<<20)
	if !lector.Scan() || !strings.Contains(lector.Text(), `"bienvenida"`) {
		t.Fatalf("no llego la bienvenida: %q", lector.Text())
	}
	esperarConectados(t, c, 3)

	var solicitudes atomic.Int32
	go func() {
		for lector.Scan() {
			if !strings.Contains(lector.Text(), `"solicitar_accion"`) {
				continue
			}
			solicitudes.Add(1)
			// Cada respuesta es basura, no JSON.
			fmt.Fprint(conn, "esto no es json\n")
		}
	}()

	if err := c.JugarRonda(context.Background()); err != nil {
		t.Fatalf("JugarRonda: %v", err)
	}
	if n := solicitudes.Load(); n < 2 {
		t.Fatalf("el cliente crudo recibio %d solicitud(es): tras la primera linea ilegible dejo de recibir, lo desconectaron", n)
	}

	// Un socket cerrado de verdad si lo marca como caido, en el primer mensaje que falle.
	conn.Close()
	esperar(t, 10*time.Second, "que el lobby se entere de que el socket se cerro", func() bool {
		p := c.lobby.Presencia(c.est.aliasPorInscrito()[ins[0].ID])
		p.EnviarMensaje(protocolo.MensajeMesa{Tipo: protocolo.MsgEstado}, 500)
		return !p.Conectado()
	})
}
