// Comando campeonato: corre el torneo por fases de las bases del Poker Fight de
// Bots (clasificatoria con mesas sorteadas de nuevo en cada ronda, y una final
// por eliminación), con un panel web para configurarlo y seguirlo.
//
//	campeonato init      escribe una configuración (y, con -demo, una lista de bots de ejemplo)
//	campeonato inscribir da de alta bots y entrega sus credenciales
//	campeonato plan      muestra cuántas mesas y jugadores tendría cada fase
//	campeonato servir    levanta el servidor de los bots y el panel
//
// A diferencia de `arena`, que arma un round-robin de antemano, el campeonato
// decide cada ronda con el resultado de la anterior, y los bots se conectan UNA
// vez a un único puerto y se quedan ahí mientras se los mueve de mesa en mesa.
// Ver docs/campeonato.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amvz1704/pokerFight/internal/campeonato"
)

func main() {
	log.SetFlags(log.Ltime)

	if len(os.Args) < 2 {
		imprimirUso()
		os.Exit(2)
	}
	comando, resto := os.Args[1], os.Args[2:]
	switch comando {
	case "servir":
		servir(resto)
	case "inscribir":
		inscribir(resto)
	case "plan":
		plan(resto)
	case "init":
		inicializar(resto)
	case "-h", "--help", "ayuda":
		imprimirUso()
	default:
		fmt.Fprintf(os.Stderr, "campeonato: comando desconocido %q\n\n", comando)
		imprimirUso()
		os.Exit(2)
	}
}

func imprimirUso() {
	fmt.Fprintln(os.Stderr, `uso: campeonato <comando> [opciones]

comandos:
  servir     -config <campeonato.json> -inscritos <inscritos.json>
             [-bots :9000] [-panel 127.0.0.1:8080] [-clave <clave>]
             Levanta el servidor al que se conectan los bots y el panel de control.
  inscribir  -inscritos <inscritos.json> -equipo <nombre> [-bot <nombre>]
             -inscritos <inscritos.json> -csv <equipos.csv>
             Da de alta bots y muestra sus credenciales (se muestran una sola vez acá).
  plan       -config <campeonato.json> -jugadores <n>
             Muestra cuántas mesas y jugadores tendría cada fase.
  init       -config <campeonato.json> [-demo <n> -inscritos <inscritos.json>]
             Escribe la configuración de las bases. Con -demo, también una lista de
             n bots de ejemplo que la arena lanza sola, para probar todo en una máquina.

Ver docs/campeonato.md para la referencia completa.`)
}

func fatal(formato string, args ...any) {
	// Los errores del paquete ya empiezan con "campeonato: ": sin sacarlo, el
	// mensaje saldría con el prefijo repetido.
	mensaje := strings.TrimPrefix(fmt.Sprintf(formato, args...), "campeonato: ")
	fmt.Fprintf(os.Stderr, "campeonato: %s\n", mensaje)
	os.Exit(1)
}

// --- servir ------------------------------------------------------------------------

func servir(args []string) {
	fs := flag.NewFlagSet("servir", flag.ExitOnError)
	rutaConfig := fs.String("config", "campeonato.json", "configuración del campeonato")
	rutaInscritos := fs.String("inscritos", "inscritos.json", "lista de bots inscritos, con sus credenciales")
	bots := fs.String("bots", ":9000", "dirección en la que se conectan los bots (host:puerto)")
	direccionPanel := fs.String("panel", "127.0.0.1:8080", "dirección del panel web. Por defecto solo escucha en esta máquina")
	clave := fs.String("clave", "", "clave de administración del panel (si se omite, se genera una)")
	fs.Parse(args)

	cfg, err := campeonato.CargarConfig(*rutaConfig)
	if err != nil {
		fatal("%v", err)
	}
	inscritos, err := campeonato.CargarInscritos(*rutaInscritos)
	if err != nil {
		fatal("%v", err)
	}

	c, err := campeonato.Nuevo(campeonato.Opciones{
		Config: cfg, Inscritos: inscritos, Direccion: *bots, RutaConfig: *rutaConfig, Log: log.Default(),
	})
	if err != nil {
		fatal("%v", err)
	}
	if err := c.Abrir(); err != nil {
		fatal("no se pudo abrir %s para los bots: %v", *bots, err)
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()
	defer c.Cerrar()

	go func() {
		if err := c.Servir(ctx); err != nil {
			log.Printf("servidor de bots detenido: %v", err)
		}
	}()

	if *clave == "" {
		*clave = campeonato.NuevaClave()
	}
	escucha, err := net.Listen("tcp", *direccionPanel)
	if err != nil {
		fatal("no se pudo abrir el panel en %s: %v", *direccionPanel, err)
	}
	panel := &http.Server{
		Handler:           campeonato.NuevoPanel(ctx, c, *clave).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := panel.Serve(escucha); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("panel detenido: %v", err)
		}
	}()

	if err := c.LanzarLocales(ctx); err != nil {
		log.Printf("aviso: %v", err)
	}

	locales := 0
	for _, in := range inscritos {
		if len(in.Comando) > 0 {
			locales++
		}
	}
	fmt.Printf("\n  %s\n", cfg.Nombre)
	fmt.Printf("  %d bots inscritos (%d los lanza esta arena, el resto se conecta solo)\n", len(inscritos), locales)
	fmt.Printf("  Bots, conectarse a:  %s\n", c.Direccion())
	fmt.Printf("  Panel:               http://%s/?clave=%s\n", escucha.Addr(), *clave)
	if !esLoopback(escucha.Addr()) {
		fmt.Printf("\n  AVISO: el panel escucha fuera de esta máquina y NO usa HTTPS: la clave viaja en claro.\n")
		fmt.Printf("         Ponelo detrás de un proxy con TLS o una VPN.\n")
	}
	fmt.Printf("  Ctrl-C para detener. El estado se guarda en %s/estado.json\n\n", cfg.Salida)

	<-ctx.Done()
	log.Printf("deteniendo...")
	apagado, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	panel.Shutdown(apagado)
}

func esLoopback(a net.Addr) bool {
	t, ok := a.(*net.TCPAddr)
	return ok && t.IP.IsLoopback()
}

// --- inscribir -------------------------------------------------------------------------

func inscribir(args []string) {
	fs := flag.NewFlagSet("inscribir", flag.ExitOnError)
	ruta := fs.String("inscritos", "inscritos.json", "lista de bots inscritos")
	equipo := fs.String("equipo", "", "nombre del equipo")
	bot := fs.String("bot", "", "nombre del bot (si se omite, bot1, bot2...)")
	csv := fs.String("csv", "", "CSV con columnas equipo,bot (el encabezado es opcional)")
	fs.Parse(args)

	if (*equipo == "") == (*csv == "") {
		fatal("inscribir: hay que pasar -equipo o -csv, pero no los dos")
	}

	var lista campeonato.Inscritos
	if _, err := os.Stat(*ruta); err == nil {
		if lista, err = campeonato.CargarInscritos(*ruta); err != nil {
			fatal("%v", err)
		}
	}

	var nuevos []campeonato.Inscrito
	if *csv != "" {
		archivo, err := os.Open(*csv)
		if err != nil {
			fatal("no se pudo abrir %s: %v", *csv, err)
		}
		defer archivo.Close()
		if nuevos, err = lista.ImportarCSV(archivo); err != nil {
			fatal("%v", err)
		}
	} else {
		in, err := lista.Agregar(*equipo, *bot)
		if err != nil {
			fatal("%v", err)
		}
		nuevos = []campeonato.Inscrito{in}
	}

	if err := lista.Guardar(*ruta); err != nil {
		fatal("no se pudo guardar %s: %v", *ruta, err)
	}

	fmt.Printf("%d bot(s) inscripto(s) en %s. Credenciales para entregar a cada equipo:\n\n", len(nuevos), *ruta)
	fmt.Printf("%-28s %-14s %s\n", "equipo", "bot", "token")
	for _, in := range nuevos {
		fmt.Printf("%-28s %-14s %s\n", in.Equipo, in.Bot, in.Token)
	}
	fmt.Printf("\nEl archivo %s tiene todas las credenciales: no lo subas a ningún repositorio.\n", *ruta)
}

// --- plan ------------------------------------------------------------------------------------

func plan(args []string) {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	ruta := fs.String("config", "campeonato.json", "configuración del campeonato")
	jugadores := fs.Int("jugadores", 64, "cantidad de bots inscritos")
	fs.Parse(args)

	cfg, err := campeonato.CargarConfig(*ruta)
	if err != nil {
		fatal("%v", err)
	}
	planes, err := campeonato.Plan(cfg, *jugadores)
	if err != nil {
		fatal("%v", err)
	}

	fmt.Printf("%s, con %d jugadores:\n\n", cfg.Nombre, *jugadores)
	for _, p := range planes {
		fmt.Printf("%d. %s (%s)\n", p.Indice+1, p.Nombre, p.Tipo)
		fmt.Printf("   entran %d -> %s -> pasan %d\n", p.Entran, p.MesasTexto, p.Salen)
		if p.Tipo == campeonato.Clasificatoria {
			fmt.Printf("   %d ronda(s) de hasta %d manos; las mesas se sortean de nuevo en cada una\n", p.Rondas, p.ManosPorMesa)
		} else {
			fmt.Printf("   una ronda, hasta %d manos por mesa\n", p.ManosPorMesa)
		}
		for _, a := range p.Avisos {
			fmt.Printf("   AVISO: %s\n", a)
		}
	}
}

// --- init ----------------------------------------------------------------------------------------

func inicializar(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	ruta := fs.String("config", "campeonato.json", "archivo de configuración a escribir")
	demo := fs.Int("demo", 0, "además, arma una lista de n bots de ejemplo que la arena lanza sola")
	rutaInscritos := fs.String("inscritos", "inscritos.json", "lista de inscritos a escribir con -demo")
	forzar := fs.Bool("forzar", false, "sobrescribe los archivos si ya existen")
	fs.Parse(args)

	destinos := []string{*ruta}
	if *demo > 0 {
		destinos = append(destinos, *rutaInscritos)
	}
	for _, d := range destinos {
		if _, err := os.Stat(d); err == nil && !*forzar {
			fatal("%s ya existe. Usá -forzar si querés sobrescribirlo", d)
		}
	}

	cfg := campeonato.ConfigBases()
	if *demo > 0 {
		cfg = configDemo()
	}
	if err := cfg.Guardar(*ruta); err != nil {
		fatal("no se pudo escribir %s: %v", *ruta, err)
	}
	fmt.Printf("escrito %s\n", *ruta)

	if *demo > 0 {
		lista, err := listaDemo(*demo)
		if err != nil {
			fatal("%v", err)
		}
		if err := lista.Guardar(*rutaInscritos); err != nil {
			fatal("no se pudo escribir %s: %v", *rutaInscritos, err)
		}
		fmt.Printf("escrito %s (%d bots de ejemplo, lanzados por la arena)\n", *rutaInscritos, *demo)
		fmt.Printf("\nAntes de seguir: make build\nDespués:         campeonato servir -config %s -inscritos %s\n", *ruta, *rutaInscritos)
		return
	}
	fmt.Printf("\nSiguiente paso: campeonato inscribir -equipo \"Nombre\"  y  campeonato servir\n")
}

// configDemo es la configuración de las bases con ciegas que suben rápido y
// rondas cortas, para ver el campeonato entero en pocos minutos.
func configDemo() campeonato.Config {
	cfg := campeonato.ConfigBases()
	cfg.Nombre = "Demo local"
	cfg.Mesa.TimeoutMs = 3000
	cfg.Salida = "campeonato-demo"

	// La clasificatoria sube las ciegas despacio: con 10000 fichas y rondas de 40
	// manos, unas ciegas que suben rápido dejan a un solo sobreviviente antes de
	// llegar a la final y el demo no muestra casi nada. Las fases finales sí suben
	// rápido, para que cada mesa termine en pocas manos.
	cfg.Fases[0].Ciegas = campeonato.Ciegas{Niveles: []uint64{50, 100, 150, 250, 400, 600}, ManosPorNivel: 30}
	cfg.Fases[0].Rondas = 3
	cfg.Fases[0].ManosPorRonda = 40
	final := campeonato.Ciegas{Niveles: []uint64{100, 200, 400, 800, 1600, 3200}, ManosPorNivel: 15}
	for i := 1; i < len(cfg.Fases); i++ {
		cfg.Fases[i].Ciegas = final
		cfg.Fases[i].MaxManos = 200
	}
	return cfg
}

// listaDemo arma n bots de ejemplo, de a dos por equipo, que la arena lanza
// sola con los binarios de `make build`.
func listaDemo(n int) (campeonato.Inscritos, error) {
	// Mitad "base", un cuarto pasivo y un cuarto al azar: el que va all-in sin
	// criterio hace de motor de caos, pero no tanto como para que casi todos
	// quiebren en la clasificatoria.
	bots := [][]string{{"./bin/base"}, {"./bin/conservador"}, {"./bin/base"}, {"./bin/aleatorio"}}
	nombres := []string{"base", "conservador", "base", "aleatorio"}

	var lista campeonato.Inscritos
	for i := 0; i < n; i++ {
		if _, err := lista.Agregar(fmt.Sprintf("Equipo %02d", i/2+1), fmt.Sprintf("%s-%d", nombres[i%len(nombres)], i%2+1)); err != nil {
			return nil, err
		}
		lista[len(lista)-1].Comando = bots[i%len(bots)]
	}
	return lista, nil
}
