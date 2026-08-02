package crupier

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Mazo es un mazo de 52 cartas con barajado criptografico.
type Mazo struct {
	cartas    [52]protocolo.Carta
	siguiente int // indice de la proxima carta a robar
}

// NuevoMazo construye un mazo completo en orden canonico sin barajar.
func NuevoMazo() Mazo {
	var m Mazo
	i := 0
	for _, palo := range []protocolo.Palo{
		protocolo.Treboles,
		protocolo.Diamantes,
		protocolo.Corazones,
		protocolo.Picas,
	} {
		for rango := protocolo.Dos; rango <= protocolo.As; rango++ {
			m.cartas[i] = protocolo.Carta{Rango: rango, Palo: palo}
			i++
		}
	}
	return m
}

// Barajar reordena el mazo usando Fisher-Yates con crypto/rand.
// Resetea el indice de reparto al inicio del mazo.
func (m *Mazo) Barajar() error {
	m.siguiente = 0
	n := len(m.cartas)
	for i := n - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return fmt.Errorf("crupier: barajar: %w", err)
		}
		j := int(jBig.Int64())
		m.cartas[i], m.cartas[j] = m.cartas[j], m.cartas[i]
	}
	return nil
}

// Robar devuelve la carta del tope del mazo y avanza el indice.
func (m *Mazo) Robar() (protocolo.Carta, error) {
	if m.siguiente >= len(m.cartas) {
		return protocolo.Carta{}, fmt.Errorf("crupier: mazo agotado")
	}
	c := m.cartas[m.siguiente]
	m.siguiente++
	return c, nil
}

// Quemar descarta la carta del tope sin devolverla (burn card).
func (m *Mazo) Quemar() error {
	_, err := m.Robar()
	return err
}

// Restantes devuelve cuantas cartas quedan en el mazo.
func (m *Mazo) Restantes() int {
	return len(m.cartas) - m.siguiente
}
