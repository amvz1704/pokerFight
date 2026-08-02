package protocolo

import "fmt"

// Palo representa el palo de una carta.
type Palo uint8

const (
	Treboles  Palo = iota // c
	Diamantes             // d
	Corazones             // h
	Picas                 // s
)

var simbolosPalo = [4]rune{'c', 'd', 'h', 's'}

// String devuelve la notacion corta del palo (c, d, h, s).
func (p Palo) String() string { return string(simbolosPalo[p]) }

// Rango representa el valor de una carta. Va de Dos (2) a As (14).
// Se usan enteros para que la comparacion de manos sea trivial.
type Rango uint8

const (
	Dos    Rango = 2
	Tres   Rango = 3
	Cuatro Rango = 4
	Cinco  Rango = 5
	Seis   Rango = 6
	Siete  Rango = 7
	Ocho   Rango = 8
	Nueve  Rango = 9
	Diez   Rango = 10
	Jota   Rango = 11
	Reina  Rango = 12
	Rey    Rango = 13
	As     Rango = 14
)

var simbolosRango = map[Rango]string{
	2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7", 8: "8",
	9: "9", 10: "T", 11: "J", 12: "Q", 13: "K", 14: "A",
}

// String devuelve la notacion corta del rango (2..9, T, J, Q, K, A).
func (r Rango) String() string { return simbolosRango[r] }

// Carta es una carta de la baraja francesa de 52.
// Se serializa como string de dos caracteres, por ejemplo "As" (as de picas).
type Carta struct {
	Rango Rango `json:"rango"`
	Palo  Palo  `json:"palo"`
}

// String devuelve la carta en notacion estandar, por ejemplo "Th" o "2c".
func (c Carta) String() string { return fmt.Sprintf("%s%s", c.Rango, c.Palo) }

// Mano son las dos cartas privadas (hole cards) de un jugador.
type Mano [2]Carta
