package crupier

import (
	"fmt"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Crupier administra el mazo, el pozo y las cartas comunitarias de una mano.
// No conoce la red ni las cuentas: esa responsabilidad pertenece a Mesa y Casino.
type Crupier struct {
	mazo         Mazo
	pozo         *Pozo
	manos        map[string]protocolo.Mano // cartas privadas por ID de jugador
	comunitarias []protocolo.Carta         // crece: 0 preflop -> 3 flop -> 4 turn -> 5 river
	etapa        protocolo.Etapa
}

// Nuevo crea un Crupier vacio. Llamar NuevaMano antes de cada mano.
func Nuevo() *Crupier {
	return &Crupier{
		mazo:         NuevoMazo(),
		pozo:         NuevoPozo(nil),
		manos:        make(map[string]protocolo.Mano),
		comunitarias: make([]protocolo.Carta, 0, 5),
		etapa:        protocolo.PreFlop,
	}
}

// NuevaMano baraja el mazo, reinicia el pozo y limpia el estado de la mano anterior.
// Debe llamarse al inicio de cada mano con la lista de jugadores activos.
func (c *Crupier) NuevaMano(jugadores []string) error {
	c.mazo = NuevoMazo()
	if err := c.mazo.Barajar(); err != nil {
		return err
	}
	c.pozo.Reiniciar(jugadores)
	c.manos = make(map[string]protocolo.Mano, len(jugadores))
	c.comunitarias = c.comunitarias[:0]
	c.etapa = protocolo.PreFlop
	return nil
}

// RepartirPrivadas roba dos cartas por jugador en dos vueltas (estilo Texas Hold'em),
// guarda las manos internamente asociadas a cada ID y las devuelve en un map.
func (c *Crupier) RepartirPrivadas(jugadores []string) (map[string]protocolo.Mano, error) {
	if len(jugadores) == 0 {
		return nil, fmt.Errorf("crupier: la lista de jugadores esta vacia")
	}
	manos := make(map[string]protocolo.Mano, len(jugadores))

	// Vuelta 1: una carta a cada jugador en orden.
	for _, id := range jugadores {
		carta, err := c.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: repartir (vuelta 1, jugador %q): %w", id, err)
		}
		m := manos[id]
		m[0] = carta
		manos[id] = m
	}

	// Vuelta 2: segunda carta a cada jugador en orden.
	for _, id := range jugadores {
		carta, err := c.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: repartir (vuelta 2, jugador %q): %w", id, err)
		}
		m := manos[id]
		m[1] = carta
		manos[id] = m
	}

	c.manos = manos
	return manos, nil
}

// ManoDeJugador devuelve las cartas privadas de un jugador.
// Retorna false si el jugador no recibio cartas en esta mano.
func (c *Crupier) ManoDeJugador(idJugador string) (protocolo.Mano, bool) {
	m, ok := c.manos[idJugador]
	return m, ok
}

// Quemar descarta la carta del tope del mazo sin revelarla (burn card).
// Se llama automaticamente dentro de Flop, Turn y River, pero queda
// expuesta para que la Mesa pueda invocarla en flujos especiales.
func (c *Crupier) Quemar() error {
	return c.mazo.Quemar()
}

// Flop quema una carta y revela las tres del flop.
// Solo valido en etapa PreFlop.
func (c *Crupier) Flop() ([]protocolo.Carta, error) {
	if c.etapa != protocolo.PreFlop {
		return nil, fmt.Errorf("crupier: flop requiere etapa preflop, actual: %s", c.etapa)
	}
	if err := c.mazo.Quemar(); err != nil {
		return nil, err
	}
	cartas, err := c.robarN(3)
	if err != nil {
		return nil, err
	}
	c.comunitarias = append(c.comunitarias, cartas...)
	c.etapa = protocolo.Flop
	return cartas, nil
}

// Turn quema una carta y revela la del turn.
// Solo valido en etapa Flop.
func (c *Crupier) Turn() (protocolo.Carta, error) {
	if c.etapa != protocolo.Flop {
		return protocolo.Carta{}, fmt.Errorf("crupier: turn requiere etapa flop, actual: %s", c.etapa)
	}
	if err := c.mazo.Quemar(); err != nil {
		return protocolo.Carta{}, err
	}
	carta, err := c.mazo.Robar()
	if err != nil {
		return protocolo.Carta{}, err
	}
	c.comunitarias = append(c.comunitarias, carta)
	c.etapa = protocolo.Turn
	return carta, nil
}

// River quema una carta y revela la del river.
// Solo valido en etapa Turn.
func (c *Crupier) River() (protocolo.Carta, error) {
	if c.etapa != protocolo.Turn {
		return protocolo.Carta{}, fmt.Errorf("crupier: river requiere etapa turn, actual: %s", c.etapa)
	}
	if err := c.mazo.Quemar(); err != nil {
		return protocolo.Carta{}, err
	}
	carta, err := c.mazo.Robar()
	if err != nil {
		return protocolo.Carta{}, err
	}
	c.comunitarias = append(c.comunitarias, carta)
	c.etapa = protocolo.River
	return carta, nil
}

// Apostar registra monto fichas del jugador al pozo.
func (c *Crupier) Apostar(idJugador string, monto int64) error {
	return c.pozo.Apostar(idJugador, monto)
}

// Fold retira al jugador de los elegibles del pozo.
func (c *Crupier) Fold(idJugador string) {
	c.pozo.Fold(idJugador)
}

// Comunitarias devuelve una copia de las cartas comunitarias reveladas hasta ahora.
func (c *Crupier) Comunitarias() []protocolo.Carta {
	copia := make([]protocolo.Carta, len(c.comunitarias))
	copy(copia, c.comunitarias)
	return copia
}

// TotalPozo devuelve el total de fichas comprometidas al pozo.
func (c *Crupier) TotalPozo() int64 {
	return c.pozo.Total()
}

// DescomponerPozo devuelve el desglose del pozo en sub-pozos (principal + laterales).
func (c *Crupier) DescomponerPozo() []SubPozo {
	return c.pozo.Descomponer()
}

// EtapaActual devuelve la fase en curso de la mano.
func (c *Crupier) EtapaActual() protocolo.Etapa {
	return c.etapa
}

// robarN roba exactamente n cartas del mazo.
func (c *Crupier) robarN(n int) ([]protocolo.Carta, error) {
	cartas := make([]protocolo.Carta, 0, n)
	for range n {
		carta, err := c.mazo.Robar()
		if err != nil {
			return nil, fmt.Errorf("crupier: robar %d cartas: %w", n, err)
		}
		cartas = append(cartas, carta)
	}
	return cartas, nil
}
