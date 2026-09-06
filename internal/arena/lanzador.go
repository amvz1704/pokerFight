// Este archivo resuelve cómo se materializa un bot cuando arranca un
// enfrentamiento: o lo lanza la arena como subproceso, o ya está corriendo en
// la máquina del participante y solo hay que esperar a que se conecte.
package arena

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Lanzador pone en marcha el bot de un participante contra una mesa concreta.
// Es el tercer punto de extensión de la arena: una implementación que lance
// contenedores, que use SSH o que hable con un orquestador entra acá sin tocar
// nada más.
type Lanzador interface {
	// Lanzar arranca el bot de p contra la mesa que escucha en `direccion`
	// (host:puerto). Devuelve la sesión para poder detenerlo al terminar.
	//
	// Devolver error significa que el bot no pudo siquiera arrancar: la arena
	// da el enfrentamiento por no jugado. No hay que devolver error porque el
	// bot juegue mal o se caiga después: eso es parte del juego y lo resuelve
	// la mesa con la acción segura.
	Lanzar(ctx context.Context, p Participante, direccion string) (Sesion, error)
}

// Sesion es un bot en marcha.
type Sesion interface {
	// Detener corta el bot y libera sus recursos. Debe ser idempotente y no
	// bloquear indefinidamente.
	Detener() error
}

// LanzadorPorDefecto elige solo entre lanzar el bot y esperarlo, según el
// participante traiga o no un comando. Es lo que usa el torneo si no se le
// configura otro, y permite mezclar en el mismo torneo bots locales y bots
// que corren los participantes por su cuenta.
func LanzadorPorDefecto() Lanzador { return lanzadorMixto{} }

type lanzadorMixto struct{}

func (lanzadorMixto) Lanzar(ctx context.Context, p Participante, direccion string) (Sesion, error) {
	if p.EsRemoto() {
		return LanzadorRemoto{}.Lanzar(ctx, p, direccion)
	}
	return LanzadorProceso{}.Lanzar(ctx, p, direccion)
}

// --- Bots remotos ----------------------------------------------------------

// LanzadorRemoto no lanza nada: el participante corre su bot donde quiera y lo
// conecta él mismo con su token. La arena solo abre el puerto y espera.
//
// Es el modo para un torneo abierto por internet. A cambio, la arena no puede
// garantizar que el bot esté disponible: si no se conecta dentro de
// ConfigMesa.EsperaJugadoresMs, el enfrentamiento se da por no jugado.
type LanzadorRemoto struct{}

// Lanzar implementa Lanzador.
func (LanzadorRemoto) Lanzar(context.Context, Participante, string) (Sesion, error) {
	return sesionVacia{}, nil
}

type sesionVacia struct{}

func (sesionVacia) Detener() error { return nil }

// --- Bots como subproceso --------------------------------------------------

// LanzadorProceso ejecuta el comando del participante como subproceso local,
// agregándole los flags -addr y -token. Es el modelo de MIT Pokerbots: los
// bots viven en el repositorio del torneo y la arena los corre.
//
// # Advertencia de seguridad
//
// Ejecuta código de terceros con los permisos del usuario que corre la arena.
// No hay sandbox: un bot puede leer el disco, abrir la red y (si encuentra el
// archivo) mirar los historiales de manos de sus rivales. Para un torneo con
// participantes que no son del equipo, correr la arena dentro de un contenedor
// descartable o una VM, con un usuario sin privilegios. Ver docs/arena.md,
// sección "Seguridad".
type LanzadorProceso struct {
	// Salida, si no es nil, recibe el stdout y el stderr de los bots. Es donde
	// aparecen los logs y los panics del participante. nil los descarta.
	Salida io.Writer
	// PlazoCierreMs es cuánto se espera a que el bot termine solo tras
	// cerrarle la conexión, antes de matarlo. 0 usa plazoCierrePorDefecto.
	PlazoCierreMs uint64
}

const plazoCierrePorDefecto = 2000

// Lanzar implementa Lanzador.
func (l LanzadorProceso) Lanzar(ctx context.Context, p Participante, direccion string) (Sesion, error) {
	if len(p.Comando) == 0 {
		return nil, fmt.Errorf("arena: el participante %q no tiene comando para lanzar", p.ID)
	}

	argumentos := append([]string(nil), p.Comando[1:]...)
	argumentos = append(argumentos, "-addr", direccion, "-token", p.TokenEfectivo())

	// Sin ctx: el proceso no se mata con el contexto del enfrentamiento sino
	// desde Detener, que le da margen para cerrar limpio. exec.CommandContext
	// lo mataría de golpe y perderíamos el stderr del participante, que es
	// justo lo que necesita para depurar por qué su bot se cayó.
	cmd := exec.Command(p.Comando[0], argumentos...)
	cmd.Dir = p.Directorio
	if len(p.Entorno) > 0 {
		cmd.Env = append(os.Environ(), p.Entorno...)
	}

	salida := l.Salida
	if salida == nil {
		salida = io.Discard
	}
	cmd.Stdout = salida
	cmd.Stderr = salida

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("arena: no se pudo ejecutar %v: %w", p.Comando, err)
	}

	plazo := l.PlazoCierreMs
	if plazo == 0 {
		plazo = plazoCierrePorDefecto
	}
	s := &sesionProceso{cmd: cmd, plazoCierre: time.Duration(plazo) * time.Millisecond}
	s.terminado = make(chan struct{})
	go func() {
		cmd.Wait()
		close(s.terminado)
	}()

	// Si se cancela el torneo entero, no dejamos procesos huérfanos dando
	// vueltas.
	go func() {
		select {
		case <-ctx.Done():
			s.Detener()
		case <-s.terminado:
		}
	}()

	return s, nil
}

// sesionProceso es un bot corriendo como subproceso.
type sesionProceso struct {
	cmd         *exec.Cmd
	plazoCierre time.Duration
	terminado   chan struct{}

	unaVez sync.Once
	err    error
}

// Detener espera a que el bot termine solo (lo normal: la mesa le cerró la
// conexión y el proceso sale) y lo mata si se pasa del plazo.
func (s *sesionProceso) Detener() error {
	s.unaVez.Do(func() {
		select {
		case <-s.terminado:
			// Ya salió por su cuenta.
		case <-time.After(s.plazoCierre):
			if s.cmd.Process != nil {
				s.err = s.cmd.Process.Kill()
			}
			<-s.terminado
		}
	})
	return s.err
}
