package crupier

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand/v2"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// Fuente es de donde salen los numeros al azar del barajado. Existe para poder
// elegir entre dos comportamientos incompatibles pero ambos necesarios:
//
//   - En una partida real hace falta un barajado impredecible, aunque alguien
//     conozca el codigo y el momento exacto en que arranco la mesa.
//   - En un torneo hace falta poder repetir exactamente el mismo reparto, para
//     jugar el mismo emparejamiento en las dos posiciones con las mismas
//     cartas (es la forma estandar de sacarle varianza a un match) y para
//     poder reproducir una mano al depurar.
//
// Por eso Nuevo() usa crypto/rand y NuevoConSemilla() un generador sembrado.
type Fuente interface {
	// IntN devuelve un entero uniforme en [0, n). Panica si n <= 0.
	IntN(n int) int
}

// fuenteCriptografica usa crypto/rand: impredecible, sin semilla posible.
type fuenteCriptografica struct{}

func (fuenteCriptografica) IntN(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand solo falla si el sistema operativo no puede dar
		// aleatoriedad. Seguir barajando con algo predecible seria peor que
		// cortar: un mazo adivinable rompe el juego entero.
		panic("crupier: no se pudo obtener aleatoriedad del sistema: " + err.Error())
	}
	return int(v.Int64())
}

// FuenteCriptografica es el barajado impredecible, el que usa una mesa real.
func FuenteCriptografica() Fuente { return fuenteCriptografica{} }

// FuenteSembrada devuelve una Fuente determinista: dos mazos barajados con la
// misma semilla producen exactamente el mismo orden. Sirve para torneos
// reproducibles y para tests.
//
// No usar en una partida donde los jugadores puedan conocer la semilla: con
// ella se puede predecir el mazo entero.
func FuenteSembrada(semilla uint64) Fuente {
	return mrand.New(mrand.NewPCG(semilla, semilla^0x9E3779B97F4A7C15))
}

// Mazo es un mazo de 52 cartas.
type Mazo struct {
	cartas    [52]protocolo.Carta
	siguiente int    // indice de la proxima carta a robar
	fuente    Fuente // de donde salen los numeros del barajado
}

// NuevoMazo construye un mazo completo en orden canonico sin barajar, con
// barajado criptografico.
func NuevoMazo() Mazo { return NuevoMazoCon(FuenteCriptografica()) }

// NuevoMazoCon construye un mazo completo en orden canonico sin barajar,
// usando la fuente de aleatoriedad dada.
func NuevoMazoCon(fuente Fuente) Mazo {
	if fuente == nil {
		fuente = FuenteCriptografica()
	}
	m := Mazo{fuente: fuente}
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

// Barajar reordena el mazo usando Fisher-Yates.
// Resetea el indice de reparto al inicio del mazo.
func (m *Mazo) Barajar() error {
	if m.fuente == nil {
		m.fuente = FuenteCriptografica()
	}
	m.siguiente = 0
	n := len(m.cartas)
	for i := n - 1; i > 0; i-- {
		j := m.fuente.IntN(i + 1)
		if j < 0 || j > i {
			return fmt.Errorf("crupier: la fuente de aleatoriedad devolvio %d, fuera de [0,%d]", j, i)
		}
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
