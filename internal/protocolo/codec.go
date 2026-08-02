package protocolo

import (
	"bufio"
	"encoding/json"
	"io"
)

// El transporte es JSON delimitado por saltos de linea (JSON Lines) sobre TCP.
// Motivos: es depurable con netcat, es agnostico del lenguaje del bot y no
// requiere dependencias externas.

// Codec serializa y deserializa mensajes sobre una conexion.
type Codec struct {
	lector    *bufio.Reader
	escritor  *bufio.Writer
	codificar *json.Encoder
}

// NuevoCodec construye un Codec sobre un lector y un escritor cualesquiera
// (una net.Conn cumple ambos, pero asi es testeable con buffers).
func NuevoCodec(r io.Reader, w io.Writer) *Codec {
	escritor := bufio.NewWriter(w)
	return &Codec{
		lector:    bufio.NewReader(r),
		escritor:  escritor,
		codificar: json.NewEncoder(escritor),
	}
}

// Enviar serializa v como una linea JSON y hace flush.
func (c *Codec) Enviar(v any) error {
	if err := c.codificar.Encode(v); err != nil {
		return err
	}
	return c.escritor.Flush()
}

// RecibirMensajeMesa lee una linea y la interpreta como MensajeMesa.
func (c *Codec) RecibirMensajeMesa() (MensajeMesa, error) {
	var m MensajeMesa
	linea, err := c.lector.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(linea, &m)
}

// RecibirMensajeBot lee una linea y la interpreta como MensajeBot.
func (c *Codec) RecibirMensajeBot() (MensajeBot, error) {
	var m MensajeBot
	linea, err := c.lector.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(linea, &m)
}
