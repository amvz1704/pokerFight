#!/usr/bin/env node
// Bot de ejemplo en JavaScript (Node.js) para pokerFight.
//
// No usa ninguna dependencia: solo el modulo `net` de Node y JSON. Es la
// referencia para escribir un bot en un lenguaje con entrada/salida asincrona,
// donde el bucle "leer mensaje, decidir, responder" del ejemplo de Python no
// se puede escribir tal cual.
//
// Uso (lo mismo que hace la arena cuando lo lanza):
//
//     node bot.js -addr localhost:9000 -token mi-token
//
// Protocolo: un objeto JSON por linea, terminado en \n, en las dos
// direcciones. La referencia completa esta en docs/protocolo.md.

'use strict';

const net = require('net');

const VERSION_PROTOCOLO = '1.0.0';
const PALOS = 'cdhs'; // 0 treboles, 1 diamantes, 2 corazones, 3 picas
const NOMBRES_RANGO = { 11: 'J', 12: 'Q', 13: 'K', 14: 'A' };

// --- Argumentos -------------------------------------------------------------

function leerArgumentos(argv) {
  const opciones = { addr: 'localhost:9000', token: 'bot-js' };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '-addr' || argv[i] === '--addr') opciones.addr = argv[++i];
    if (argv[i] === '-token' || argv[i] === '--token') opciones.token = argv[++i];
  }
  return opciones;
}

function cartaLegible(carta) {
  const rango = NOMBRES_RANGO[carta.rango] || String(carta.rango);
  return `${rango}${PALOS[carta.palo]}`;
}

// --- Turno ------------------------------------------------------------------

// Turno junta todo lo que se sabe al momento de decidir, y sabe construir
// acciones que la mesa vaya a aceptar.
class Turno {
  constructor(idJugador, mensaje, mano) {
    this.estado = mensaje.estado || {};
    this.validas = mensaje.acciones_validas || [];
    this.timeoutMs = mensaje.timeout_ms || 0;
    this.mano = mano;

    // Nuestra propia entrada del estado publico, ya resuelta. Se busca por el
    // id que devolvio la bienvenida: el token no sirve de identificador cuando
    // hay un Casino detras.
    this.yo = (this.estado.jugadores || []).find((j) => j.id === idJugador) || {
      saldo: 0,
      apuesta_ronda: 0,
    };
  }

  puede(tipo) {
    return this.validas.includes(tipo);
  }

  // Fichas que faltan poner para igualar. 0 si se puede pasar gratis.
  porIgualar() {
    return Math.max(0, (this.estado.apuesta_actual || 0) - (this.yo.apuesta_ronda || 0));
  }

  // Total maximo al que se puede llevar la apuesta: el all-in.
  techo() {
    return (this.yo.saldo || 0) + (this.yo.apuesta_ronda || 0);
  }

  // Total minimo que la mesa acepta como bet o raise.
  subidaMinima() {
    return (this.estado.apuesta_actual || 0) + (this.estado.subida_minima || 0);
  }

  // Lleva la apuesta de la ronda a `total` fichas, ajustando al rango que la
  // mesa acepta.
  //
  // OJO: en el protocolo, `monto` es el TOTAL al que se lleva la apuesta en la
  // ronda, no el incremento. Es el error mas comun al escribir un bot, y la
  // mesa lo castiga descartando la accion y aplicando la accion segura.
  apostar(total) {
    const tipo =
      (this.estado.apuesta_actual || 0) <= (this.yo.apuesta_ronda || 0) ? 'bet' : 'raise';

    // Llegar al techo es exactamente un all-in. Declararlo asi hace que la mesa
    // aplique la regla de all-in corto en vez de rechazarlo por subida
    // insuficiente.
    if (total >= this.techo() && this.puede('allin')) return { tipo: 'allin' };
    if (!this.puede(tipo)) return this.pasiva();

    total = Math.max(total, this.subidaMinima());
    if (total > this.techo()) {
      return this.puede('allin') ? { tipo: 'allin' } : this.pasiva();
    }
    return { tipo, monto: total };
  }

  // La mejor accion que no arriesga nada.
  pasiva() {
    if (this.puede('check')) return { tipo: 'check' };
    if (this.puede('call') && this.porIgualar() <= (this.estado.ciega_grande || 0) * 3) {
      return { tipo: 'call' };
    }
    return { tipo: 'fold' };
  }
}

// --- Estrategia -------------------------------------------------------------

// Aca va tu estrategia. Esta es deliberadamente simple: sube con par o con dos
// cartas altas, y en cualquier otro caso juega pasivo.
function decidir(turno) {
  if (turno.mano.length === 2) {
    const [a, b] = turno.mano.map((c) => c.rango);
    const esPar = a === b;
    const dosAltas = Math.max(a, b) >= 12 && Math.min(a, b) >= 10;

    if (esPar || dosAltas) {
      return turno.apostar((turno.estado.ciega_grande || 20) * 3);
    }
  }
  return turno.pasiva();
}

// --- Conexion ---------------------------------------------------------------

function main() {
  const opciones = leerArgumentos(process.argv.slice(2));
  const [host, puerto] = opciones.addr.split(':');

  const socket = net.createConnection({ host, port: Number(puerto) });
  socket.setEncoding('utf8');

  let idJugador = opciones.token; // Provisorio hasta la bienvenida.
  let saludado = false;
  let mano = [];

  // El socket entrega los bytes como vengan: un 'data' puede traer media linea
  // o dos y media. Este buffer rearma las lineas, que son el framing real del
  // protocolo.
  let buffer = '';

  const enviar = (mensaje) => socket.write(JSON.stringify(mensaje) + '\n');

  socket.on('connect', () => {
    enviar({ tipo: 'saludo', version: VERSION_PROTOCOLO, token: opciones.token });
  });

  socket.on('data', (fragmento) => {
    buffer += fragmento;

    let corte;
    while ((corte = buffer.indexOf('\n')) >= 0) {
      const linea = buffer.slice(0, corte).trim();
      buffer = buffer.slice(corte + 1);
      if (linea === '') continue;

      let mensaje;
      try {
        mensaje = JSON.parse(linea);
      } catch (err) {
        console.error('linea ilegible de la mesa:', linea);
        continue;
      }
      manejar(mensaje);
    }
  });

  function manejar(mensaje) {
    if (!saludado) {
      if (mensaje.tipo === 'error') {
        console.error('la mesa rechazo la conexion:', mensaje.mensaje || '');
        socket.end();
        process.exitCode = 1;
        return;
      }
      if (mensaje.tipo !== 'bienvenida') {
        console.error(`se esperaba bienvenida, llego ${mensaje.tipo}`);
        socket.end();
        process.exitCode = 1;
        return;
      }
      saludado = true;
      idJugador = mensaje.id_jugador || opciones.token;
      console.error(`conectado como ${idJugador} en la silla ${mensaje.silla}`);
      return;
    }

    switch (mensaje.tipo) {
      case 'mano_inicio':
        mano = mensaje.cartas || [];
        console.error(`mano nueva: ${mano.map(cartaLegible).join(' ')}`);
        break;

      case 'solicitar_accion': {
        const turno = new Turno(idJugador, mensaje, mano);
        // El id_mano se devuelve tal cual: la mesa lo usa para descartar
        // acciones que llegan tarde, de una mano ya cerrada.
        enviar({
          tipo: 'accion',
          version: VERSION_PROTOCOLO,
          id_mano: turno.estado.id_mano || '',
          accion: decidir(turno),
        });
        break;
      }

      case 'mano_fin':
      case 'estado':
        // Informativos: no hay que responder.
        break;

      case 'error':
        console.error('error de la mesa:', mensaje.mensaje || '');
        socket.end();
        break;
    }
  }

  socket.on('end', () => console.error('la mesa cerro la conexion: partida terminada'));
  socket.on('error', (err) => {
    console.error('error de conexion:', err.message);
    process.exitCode = 1;
  });
}

main();
