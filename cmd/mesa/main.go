// Comando mesa: levanta un servidor TCP de poker (ver docs/protocolo.md) y
// corre la partida hasta que se cumpla la condición de fin (límite de rondas
// o un solo jugador activo).
//
// Por defecto corre en "modo abierto", sin Casino: el token que manda el bot
// en el saludo se usa directamente como su identificador (ver ValidarToken
// en internal/mesa/servidor.go). Si se pasa -casino-db, valida a los
// jugadores contra cuentas reales del Casino (docs/interfaces.md §4) y, al
// terminar la partida, le reporta el resultado para que quede en el ranking
// — este comando es el "orquestador" que menciona esa sección: es el único
// lugar que conoce ambos paquetes, ni Mesa ni Casino se importan entre sí.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amvz1704/pokerFight/internal/casino"
	"github.com/amvz1704/pokerFight/internal/crupier"
	"github.com/amvz1704/pokerFight/internal/mesa"
)

func main() {
	addr := flag.String("addr", ":9000", "dirección de escucha (host:puerto)")
	maxJugadores := flag.Uint("jugadores", 6, "cantidad máxima de jugadores en la mesa (2-8)")
	minJugadores := flag.Uint("min-jugadores", 2, "cantidad mínima de jugadores conectados para arrancar la partida")
	ciegaChica := flag.Uint64("ciega-chica", 10, "ciega chica")
	ciegaGrande := flag.Uint64("ciega-grande", 20, "ciega grande")
	stack := flag.Uint64("stack", 1000, "fichas iniciales por jugador")
	timeoutMs := flag.Uint64("timeout-ms", 2000, "plazo en milisegundos para que un bot responda a su turno")
	rondas := flag.Int64("rondas", -1, "cantidad de rondas a jugar (-1 = hasta que quede un solo jugador)")
	id := flag.String("id", "mesa-1", "identificador de la mesa")
	casinoDB := flag.String("casino-db", "", "archivo del Casino (ver cmd/casino): si se pasa, valida a los jugadores contra cuentas reales y reporta el resultado al ranking. Si se omite, corre en modo abierto")
	flag.Parse()

	if *maxJugadores < 2 || *maxJugadores > 8 {
		log.Fatalf("mesa: -jugadores debe estar entre 2 y 8, recibido %d", *maxJugadores)
	}
	if *minJugadores < 2 || *minJugadores > *maxJugadores {
		log.Fatalf("mesa: -min-jugadores debe estar entre 2 y -jugadores (%d), recibido %d", *maxJugadores, *minJugadores)
	}
	if *ciegaGrande != 2*(*ciegaChica) {
		log.Printf("mesa: aviso, la ciega grande (%d) no es el doble de la chica (%d)", *ciegaGrande, *ciegaChica)
	}

	m := mesa.NuevaMesa(*id, mesa.ConfigMesa{
		MaxJugadores: uint8(*maxJugadores),
		MinJugadores: uint8(*minJugadores),
		Timeout:      *timeoutMs,
	}, mesa.ConfigPartida{
		CiegaMenor:     *ciegaChica,
		CiegaMayor:     *ciegaGrande,
		StackInicial:   *stack,
		CantidadRondas: *rondas,
	}, crupier.Nuevo())

	// Con -casino-db, un *casino.motor valida los tokens: su método
	// ValidarToken ya tiene la forma de mesa.ValidarToken (docs/interfaces.md
	// §4), así que se inyecta directo, sin envoltorios. Sin -casino-db,
	// validar=nil deja a la mesa en modo abierto (solo desarrollo y pruebas
	// de humo, como make torneo-local).
	var c casino.Casino
	var validar mesa.ValidarToken
	if *casinoDB != "" {
		var err error
		c, err = casino.Nuevo(*casinoDB)
		if err != nil {
			log.Fatalf("mesa: no se pudo abrir el Casino en %s: %v", *casinoDB, err)
		}
		validar = c.ValidarToken
		log.Printf("mesa [%s]: usando el Casino de %s (jugadores deben tener cuenta y token de 'casino login')", *id, *casinoDB)
	}

	servidor := mesa.NuevoServidor(mesa.ConfigServidor{
		Direccion:          *addr,
		TimeoutHandshakeMs: *timeoutMs,
		MaxConexiones:      int(*maxJugadores),
	}, m, validar)

	if err := servidor.Abrir(); err != nil {
		log.Fatalf("mesa: no se pudo abrir %s: %v", *addr, err)
	}
	log.Printf("mesa [%s]: escuchando en %s, esperando %d-%d jugadores", *id, servidor.Direccion(), *minJugadores, *maxJugadores)

	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()
	defer servidor.Cerrar()

	go func() {
		if err := servidor.Servir(ctx); err != nil {
			log.Printf("mesa [%s]: servidor detenido: %v", *id, err)
		}
	}()

	if !esperarJugadores(ctx, m, int(*minJugadores)) {
		log.Printf("mesa [%s]: cancelada antes de completar el mínimo de jugadores", *id)
		return
	}

	log.Printf("mesa [%s]: mínimo de jugadores alcanzado, arrancando la partida", *id)
	resumen, err := m.Jugar(ctx)
	if err != nil {
		log.Printf("mesa [%s]: la partida terminó con error: %v", *id, err)
	}

	fmt.Printf("Mesa [%s]: partida terminada, %d rondas jugadas.\n", resumen.IDMesa, resumen.CantidadRondasJugadas)
	for _, p := range resumen.Posiciones {
		fmt.Printf("  #%d %-20s %d fichas\n", p.Posicion, p.IDJugador, p.SaldoFinal)
	}

	if c != nil {
		if err := reportarResultado(c, resumen); err != nil {
			log.Printf("mesa [%s]: no se pudo reportar el resultado al Casino: %v", *id, err)
		} else {
			log.Printf("mesa [%s]: resultado reportado al Casino", *id)
		}
	}
}

// reportarResultado convierte el resumen de la Mesa al tipo propio de
// Casino (que no importa el paquete mesa, ver docs/interfaces.md §1) y lo
// registra. IDJugador es el ID de cuenta que devolvió ValidarToken, así que
// coincide con lo que RegistrarResultado espera en IDCuenta.
//
// Los timeouts salen del resumen: la Mesa cuenta por jugador cada vez que tuvo
// que aplicar la acción segura, sea por plazo vencido o por acción inválida.
func reportarResultado(c casino.Casino, resumen mesa.ResumenPartida) error {
	posiciones := make([]casino.ResultadoJugador, 0, len(resumen.Posiciones))
	for _, p := range resumen.Posiciones {
		posiciones = append(posiciones, casino.ResultadoJugador{
			IDCuenta:   p.IDJugador,
			Posicion:   p.Posicion,
			SaldoFinal: p.SaldoFinal,
			Timeouts:   p.Timeouts,
		})
	}
	return c.RegistrarResultado(casino.ResultadoPartida{
		IDMesa:     resumen.IDMesa,
		Posiciones: posiciones,
	})
}

// esperarJugadores bloquea hasta que haya al menos min jugadores sentados o
// se cancele el contexto. Devuelve false si se canceló antes de llegar.
func esperarJugadores(ctx context.Context, m mesa.MesaInterface, min int) bool {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if m.Sentados() >= min {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}
