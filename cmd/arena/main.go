// Comando arena: corre un torneo de bots completo a partir de un archivo de
// configuración JSON.
//
//	arena init                       # escribe un torneo.json de ejemplo
//	arena validar -config torneo.json
//	arena correr -config torneo.json
//	arena formatos                   # lista formatos y puntuaciones disponibles
//
// Con -casino-db, los participantes se validan contra cuentas reales del
// Casino y el resultado del torneo se suma al ranking histórico. Sin ese flag,
// la arena corre en modo abierto: el token de cada participante es
// directamente su identificador, que es lo que se quiere para probar en local.
//
// Este comando es, como cmd/mesa, un orquestador: es el único lugar que
// importa arena y casino a la vez (docs/interfaces.md §4).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/amvz1704/pokerFight/internal/arena"
	"github.com/amvz1704/pokerFight/internal/casino"
)

func main() {
	log.SetFlags(log.Ltime)

	args := os.Args[1:]
	if len(args) == 0 {
		imprimirUso()
		os.Exit(2)
	}

	comando, resto := args[0], args[1:]
	switch comando {
	case "correr":
		correr(resto)
	case "validar":
		validar(resto)
	case "init":
		inicializar(resto)
	case "formatos":
		listarFormatos()
	case "-h", "--help", "ayuda":
		imprimirUso()
	default:
		fmt.Fprintf(os.Stderr, "arena: comando desconocido %q\n\n", comando)
		imprimirUso()
		os.Exit(2)
	}
}

func imprimirUso() {
	fmt.Fprintln(os.Stderr, `uso: arena <comando> [opciones]

comandos:
  correr   -config <archivo.json> [-casino-db <db>] [-salida <dir>] [-verboso]
           Juega el torneo entero y escribe los resultados.
  validar  -config <archivo.json>
           Revisa la configuración y muestra los emparejamientos, sin jugar.
  init     [-config <archivo.json>]
           Escribe un torneo de ejemplo listo para editar.
  formatos
           Lista los formatos y las puntuaciones disponibles.

Ver docs/arena.md para la referencia completa de la configuracion.`)
}

func fatal(formato string, args ...any) {
	fmt.Fprintf(os.Stderr, "arena: "+formato+"\n", args...)
	os.Exit(1)
}

// --- correr ----------------------------------------------------------------

func correr(args []string) {
	fs := flag.NewFlagSet("correr", flag.ExitOnError)
	ruta := fs.String("config", "torneo.json", "archivo de configuración del torneo")
	casinoDB := fs.String("casino-db", "", "archivo del Casino: valida a los participantes contra cuentas reales y reporta el resultado al ranking")
	salida := fs.String("salida", "", "directorio de salida (pisa el del archivo de configuración)")
	verboso := fs.Bool("verboso", false, "muestra en la terminal el stdout y stderr de los bots")
	direccion := fs.String("host", "127.0.0.1", "host en el que escuchan las mesas. Usar 0.0.0.0 solo si los bots son remotos")
	fs.Parse(args)

	cfg, err := arena.CargarConfig(*ruta)
	if err != nil {
		fatal("%v", err)
	}
	if *salida != "" {
		cfg.Salida = *salida
	}

	formato, err := arena.BuscarFormato(cfg.Formato, cfg)
	if err != nil {
		fatal("%v", err)
	}
	puntuador, err := arena.BuscarPuntuador(cfg.Puntuacion, cfg)
	if err != nil {
		fatal("%v", err)
	}

	if err := os.MkdirAll(cfg.Salida, 0o755); err != nil {
		fatal("no se pudo crear el directorio de salida %s: %v", cfg.Salida, err)
	}

	// Con -casino-db los tokens se validan de verdad. Sin él, modo abierto.
	var c casino.Casino
	var validarToken arena.ValidarToken
	if *casinoDB != "" {
		c, err = casino.Nuevo(*casinoDB)
		if err != nil {
			fatal("no se pudo abrir el Casino en %s: %v", *casinoDB, err)
		}
		validarToken = c.ValidarToken
		log.Printf("usando el Casino de %s: los participantes necesitan cuenta y token de 'casino login'", *casinoDB)
	}

	var historial arena.Historial
	if cfg.GuardarHistorial {
		historial, err = arena.NuevoHistorialArchivos(filepath.Join(cfg.Salida, "manos"))
		if err != nil {
			fatal("no se pudo preparar el historial de manos: %v", err)
		}
		defer historial.Cerrar()
	}

	var lanzador arena.Lanzador
	if *verboso {
		// El stderr de los bots va a la terminal: es donde el participante ve
		// por qué se le cayó el suyo.
		lanzador = arena.LanzadorProceso{Salida: os.Stderr}
	}

	torneo := &arena.Torneo{
		Config:    cfg,
		Formato:   formato,
		Puntua:    puntuador,
		Lanzador:  lanzador,
		Validar:   validarToken,
		Historial: historial,
		Direccion: *direccion,
		Log:       log.Default(),
	}

	// Ctrl-C corta el torneo de forma ordenada: se detienen los bots en
	// marcha y se escribe igual lo que ya se jugó.
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	clasificacion, resultados, err := torneo.Correr(ctx)
	if err != nil {
		fatal("%v", err)
	}

	// El historial se cierra antes de reportar para que los archivos estén
	// completos en disco cuando el comando termine de imprimir.
	if historial != nil {
		if err := historial.Cerrar(); err != nil {
			log.Printf("aviso: el historial de manos no se cerró limpio: %v", err)
		}
	}

	guardarSalida(cfg, clasificacion, resultados)

	fmt.Println()
	fmt.Print(clasificacion.Tabla())

	if c != nil {
		if err := reportarAlCasino(c, cfg, resultados); err != nil {
			log.Printf("aviso: no se pudo reportar el torneo al Casino: %v", err)
		} else {
			log.Printf("resultados reportados al Casino de %s", *casinoDB)
		}
	}
}

// guardarSalida escribe los tres archivos del torneo: la configuración
// efectiva (con los valores por defecto ya resueltos), el detalle de cada
// enfrentamiento y la clasificación.
func guardarSalida(cfg arena.ConfigTorneo, clasificacion arena.Clasificacion, resultados []arena.ResultadoEnfrentamiento) {
	rutaConfig := filepath.Join(cfg.Salida, "torneo.json")
	if err := cfg.Guardar(rutaConfig); err != nil {
		log.Printf("aviso: no se pudo guardar %s: %v", rutaConfig, err)
	}

	rutaResultados := filepath.Join(cfg.Salida, "enfrentamientos.jsonl")
	if err := arena.GuardarResultados(rutaResultados, resultados); err != nil {
		log.Printf("aviso: no se pudo guardar %s: %v", rutaResultados, err)
	}

	rutaClasificacion := filepath.Join(cfg.Salida, "clasificacion.json")
	if err := clasificacion.Guardar(rutaClasificacion); err != nil {
		log.Printf("aviso: no se pudo guardar %s: %v", rutaClasificacion, err)
	}

	rutaTabla := filepath.Join(cfg.Salida, "clasificacion.txt")
	if err := os.WriteFile(rutaTabla, []byte(clasificacion.Tabla()), 0o644); err != nil {
		log.Printf("aviso: no se pudo guardar %s: %v", rutaTabla, err)
	}

	log.Printf("resultados en %s/", cfg.Salida)
}

// reportarAlCasino suma el torneo al ranking histórico. La posición que se
// reporta es la del torneo (la de la clasificación final), no la de cada
// enfrentamiento: para el Casino, el torneo entero es una sola "partida".
func reportarAlCasino(c casino.Casino, cfg arena.ConfigTorneo, resultados []arena.ResultadoEnfrentamiento) error {
	// El Casino identifica a la gente por ID de cuenta, que es lo que
	// devolvió ValidarToken. La arena conoce a los participantes por su ID de
	// torneo. Con -casino-db los dos coinciden solo si el archivo de
	// configuración usa como id el ID de cuenta; si no, se reporta igual con
	// lo que hay y el Casino crea la fila.
	timeouts := make(map[string]uint64)
	for _, r := range resultados {
		for _, p := range r.Puestos {
			timeouts[p.IDParticipante] += p.Timeouts
		}
	}

	puntuador, err := arena.BuscarPuntuador(cfg.Puntuacion, cfg)
	if err != nil {
		return err
	}
	clasificacion := arena.Clasificar(cfg, puntuador, resultados)

	posiciones := make([]casino.ResultadoJugador, 0, len(clasificacion.Filas))
	for _, fila := range clasificacion.Filas {
		posiciones = append(posiciones, casino.ResultadoJugador{
			IDCuenta: fila.IDParticipante,
			Posicion: fila.Posicion,
			// El "saldo final" que ve el Casino es el neto en fichas del
			// torneo, desplazado para que nunca sea negativo (el campo es
			// uint64). Es informativo: el ranking se calcula con la posición.
			SaldoFinal: saldoNoNegativo(fila.FichasNetas),
			Timeouts:   timeouts[fila.IDParticipante],
		})
	}

	return c.RegistrarResultado(casino.ResultadoPartida{
		IDMesa:     cfg.Nombre,
		Posiciones: posiciones,
	})
}

func saldoNoNegativo(neto int64) uint64 {
	if neto < 0 {
		return 0
	}
	return uint64(neto)
}

// --- validar ---------------------------------------------------------------

func validar(args []string) {
	fs := flag.NewFlagSet("validar", flag.ExitOnError)
	ruta := fs.String("config", "torneo.json", "archivo de configuración del torneo")
	fs.Parse(args)

	cfg, err := arena.CargarConfig(*ruta)
	if err != nil {
		fatal("%v", err)
	}
	formato, err := arena.BuscarFormato(cfg.Formato, cfg)
	if err != nil {
		fatal("%v", err)
	}
	if _, err := arena.BuscarPuntuador(cfg.Puntuacion, cfg); err != nil {
		fatal("%v", err)
	}
	enfrentamientos, err := formato.Emparejamientos(cfg)
	if err != nil {
		fatal("%v", err)
	}

	manos := cfg.ManosPorEnfrentamiento
	fmt.Printf("torneo:        %s\n", cfg.Nombre)
	fmt.Printf("formato:       %s (%d participantes)\n", cfg.Formato, len(cfg.Participantes))
	fmt.Printf("puntuacion:    %s\n", cfg.Puntuacion)
	fmt.Printf("semilla:       %d\n", cfg.Semilla)
	fmt.Printf("ciegas/stack:  %d/%d, stack %d, timeout %d ms\n",
		cfg.Mesa.CiegaChica, cfg.Mesa.CiegaGrande, cfg.Mesa.Stack, cfg.Mesa.TimeoutMs)
	if manos > 0 {
		fmt.Printf("enfrentamientos: %d x %d manos = %d manos en total\n",
			len(enfrentamientos), manos, int64(len(enfrentamientos))*manos)
	} else {
		fmt.Printf("enfrentamientos: %d, cada uno hasta que quede un solo jugador\n", len(enfrentamientos))
	}

	remotos := 0
	for _, p := range cfg.Participantes {
		if p.EsRemoto() {
			remotos++
		}
	}
	if remotos > 0 {
		fmt.Printf("\naviso: %d participante(s) sin comando: la arena va a esperar a que se conecten solos.\n", remotos)
	}

	fmt.Println("\nemparejamientos:")
	for _, e := range enfrentamientos {
		fmt.Printf("  %-34s sillas: %v  semilla: %d\n", e.ID, e.Participantes, e.Semilla)
	}
	fmt.Println("\nla configuracion es valida.")
}

// --- init ------------------------------------------------------------------

func inicializar(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	ruta := fs.String("config", "torneo.json", "archivo a escribir")
	forzar := fs.Bool("forzar", false, "sobrescribe el archivo si ya existe")
	fs.Parse(args)

	if _, err := os.Stat(*ruta); err == nil && !*forzar {
		fatal("%s ya existe. Usá -forzar si querés sobrescribirlo", *ruta)
	}

	cfg := arena.ConfigEjemplo()
	if err := cfg.Guardar(*ruta); err != nil {
		fatal("no se pudo escribir %s: %v", *ruta, err)
	}
	fmt.Printf("escrito %s. Editalo y corré: arena correr -config %s\n", *ruta, *ruta)
}

// --- formatos --------------------------------------------------------------

func listarFormatos() {
	fmt.Println("formatos disponibles:")
	for _, n := range arena.FormatosDisponibles() {
		fmt.Printf("  %s\n", n)
	}
	fmt.Println("\npuntuaciones disponibles:")
	for _, n := range arena.PuntuadoresDisponibles() {
		fmt.Printf("  %s\n", n)
	}
	fmt.Println("\nPara agregar los tuyos, ver docs/arena.md (seccion \"Extender la arena\").")
}
