// Comando casino: CLI de cuentas, bots y ranking. Persiste en un archivo
// JSON (ver internal/almacen). No juega ni conoce la Mesa directamente: ver
// docs/interfaces.md §4 para cómo se conectan (Mesa recibe ValidarToken por
// inyección).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/amvz1704/pokerFight/internal/casino"
)

func main() {
	db := flag.String("db", "casino.json", "archivo donde el Casino persiste cuentas, bots y estadísticas")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		imprimirUso()
		os.Exit(2)
	}

	c, err := casino.Nuevo(*db)
	if err != nil {
		fatal("no se pudo abrir %s: %v", *db, err)
	}

	comando, resto := args[0], args[1:]
	switch comando {
	case "registrar":
		registrar(c, resto)
	case "login":
		login(c, resto)
	case "bot":
		registrarBot(c, resto)
	case "bots":
		listarBots(c, resto)
	case "ranking":
		ranking(c)
	case "estadisticas":
		estadisticas(c, resto)
	default:
		fmt.Fprintf(os.Stderr, "casino: comando desconocido %q\n\n", comando)
		imprimirUso()
		os.Exit(2)
	}
}

func imprimirUso() {
	fmt.Fprintln(os.Stderr, `uso: casino [-db archivo.json] <comando> [opciones]

comandos:
  registrar -usuario <nombre>                crea una cuenta nueva
  login -usuario <nombre>                    emite un token de sesión
  bot -cuenta <id> -nombre <n> -version <v>  registra una versión de bot
  bots -cuenta <id>                          lista los bots de una cuenta
  ranking                                    muestra el ranking completo
  estadisticas -cuenta <id>                  muestra las estadísticas de una cuenta`)
}

func fatal(formato string, args ...any) {
	fmt.Fprintf(os.Stderr, "casino: "+formato+"\n", args...)
	os.Exit(1)
}

func registrar(c casino.Casino, args []string) {
	fs := flag.NewFlagSet("registrar", flag.ExitOnError)
	usuario := fs.String("usuario", "", "nombre de usuario")
	fs.Parse(args)
	if *usuario == "" {
		fatal("registrar: -usuario es obligatorio")
	}

	cuenta, err := c.Registrar(*usuario)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("cuenta creada: %s (id: %s)\n", cuenta.Usuario, cuenta.ID)
}

func login(c casino.Casino, args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	usuario := fs.String("usuario", "", "nombre de usuario")
	fs.Parse(args)
	if *usuario == "" {
		fatal("login: -usuario es obligatorio")
	}

	token, err := c.Login(*usuario)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Println(token)
}

func registrarBot(c casino.Casino, args []string) {
	fs := flag.NewFlagSet("bot", flag.ExitOnError)
	idCuenta := fs.String("cuenta", "", "id de la cuenta (el que devuelve 'registrar')")
	nombre := fs.String("nombre", "", "nombre del bot")
	version := fs.String("version", "", "versión del bot")
	fs.Parse(args)
	if *idCuenta == "" || *nombre == "" || *version == "" {
		fatal("bot: -cuenta, -nombre y -version son obligatorios")
	}

	b, err := c.RegistrarBot(*idCuenta, *nombre, *version)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("bot registrado: %s v%s (id: %s)\n", b.Nombre, b.Version, b.ID)
}

func listarBots(c casino.Casino, args []string) {
	fs := flag.NewFlagSet("bots", flag.ExitOnError)
	idCuenta := fs.String("cuenta", "", "id de la cuenta")
	fs.Parse(args)
	if *idCuenta == "" {
		fatal("bots: -cuenta es obligatorio")
	}

	lista, err := c.ListarBots(*idCuenta)
	if err != nil {
		fatal("%v", err)
	}
	if len(lista) == 0 {
		fmt.Println("esa cuenta todavía no registró ningún bot")
		return
	}
	for _, b := range lista {
		fmt.Printf("%-25s v%-10s %s\n", b.Nombre, b.Version, b.Creado.Format("2006-01-02 15:04"))
	}
}

func ranking(c casino.Casino) {
	lista, err := c.Ranking()
	if err != nil {
		fatal("%v", err)
	}
	if len(lista) == 0 {
		fmt.Println("todavía no hay resultados registrados")
		return
	}

	fmt.Printf("%-4s %-30s %8s %8s %8s %8s\n", "#", "cuenta", "puntaje", "partidas", "ganadas", "timeouts")
	for i, e := range lista {
		nombre := e.IDCuenta
		if cuenta, err := c.Cuenta(e.IDCuenta); err == nil {
			nombre = cuenta.Usuario
		}
		fmt.Printf("%-4d %-30s %8d %8d %8d %8d\n", i+1, nombre, e.Puntaje, e.PartidasJugadas, e.PartidasGanadas, e.Timeouts)
	}
}

func estadisticas(c casino.Casino, args []string) {
	fs := flag.NewFlagSet("estadisticas", flag.ExitOnError)
	idCuenta := fs.String("cuenta", "", "id de la cuenta")
	fs.Parse(args)
	if *idCuenta == "" {
		fatal("estadisticas: -cuenta es obligatorio")
	}

	est, err := c.Estadisticas(*idCuenta)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("partidas jugadas: %d\npartidas ganadas: %d\ntimeouts: %d\npuntaje: %d\n",
		est.PartidasJugadas, est.PartidasGanadas, est.Timeouts, est.Puntaje)
}
