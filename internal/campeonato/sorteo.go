package campeonato

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
)

// fuenteCriptografica es una fuente de números al azar impredecible, para el
// sorteo de un torneo de verdad. Implementa rand.Source.
type fuenteCriptografica struct{}

func (fuenteCriptografica) Uint64() uint64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		// Igual que en el crupier y el Casino: un sorteo previsible es peor que
		// no sortear.
		panic("campeonato: no se pudo obtener aleatoriedad del sistema: " + err.Error())
	}
	return binary.LittleEndian.Uint64(b[:])
}

// NuevoAzar arma el generador de un sorteo. Con semilla, es determinista y
// distinto para cada `sal` (fase, ronda...): dos sorteos distintos del mismo
// campeonato no repiten la permutación. Sin semilla, es criptográfico.
func NuevoAzar(semilla *uint64, sal ...int) *rand.Rand {
	if semilla == nil {
		return rand.New(fuenteCriptografica{})
	}
	return rand.New(rand.NewPCG(mezclar(*semilla, sal...), *semilla^0x9E3779B97F4A7C15))
}

// SemillaDeMesa deriva la semilla del mazo de una mesa a partir de la del
// campeonato. Sin semilla devuelve nil: el crupier usa barajado criptográfico.
func SemillaDeMesa(semilla *uint64, sal ...int) *uint64 {
	if semilla == nil {
		return nil
	}
	derivada := mezclar(*semilla^0xD1B54A32D192ED03, sal...)
	return &derivada
}

// mezclar combina una semilla con enteros de contexto (fase, ronda, mesa) de
// forma que cambiar cualquiera dé un resultado distinto.
func mezclar(base uint64, sal ...int) uint64 {
	h := base
	for _, s := range sal {
		h ^= uint64(s) + 0x9E3779B97F4A7C15 + (h << 6) + (h >> 2)
		h *= 0xBF58476D1CE4E5B9
		h ^= h >> 31
	}
	return h
}

// Sortear reparte los jugadores en mesas de los tamaños dados, al azar. El
// orden dentro de cada mesa es el orden de las sillas, que también sale del
// sorteo: nadie tiene una posición garantizada respecto del botón.
//
// No modifica `ids`.
func Sortear(ids []string, tamanos []int, azar *rand.Rand) ([][]string, error) {
	total := 0
	for _, t := range tamanos {
		total += t
	}
	if total != len(ids) {
		return nil, fmt.Errorf("campeonato: las mesas suman %d asientos y hay %d jugadores", total, len(ids))
	}

	mezclados := append([]string(nil), ids...)
	azar.Shuffle(len(mezclados), func(i, j int) { mezclados[i], mezclados[j] = mezclados[j], mezclados[i] })

	mesas := make([][]string, len(tamanos))
	desde := 0
	for i, t := range tamanos {
		mesas[i] = mezclados[desde : desde+t : desde+t]
		desde += t
	}
	return mesas, nil
}
