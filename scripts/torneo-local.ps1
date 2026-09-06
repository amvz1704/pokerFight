# Prueba de humo (version PowerShell de torneo-local.sh): levanta una mesa
# local y conecta 2 bots de ejemplo para jugar un torneo corto de punta a
# punta. Pensado para Windows, donde `make torneo-local` puede fallar si
# `bash` en el PATH resuelve al stub de WSL en vez del bash.exe de Git.
#
# Sin -CasinoDB corre en modo abierto (igual que torneo-local.sh): el token
# que manda cada bot es directamente su identificador, sin pasar por Casino.
# Con -CasinoDB, registra las 2 cuentas de prueba en ese archivo (si no
# existen todavia), hace login para conseguir tokens reales, corre la mesa
# con -casino-db, y al final imprime el ranking actualizado.
#
# Uso:
#   .\scripts\torneo-local.ps1
#   .\scripts\torneo-local.ps1 -Rondas 20 -Addr :9010
#   .\scripts\torneo-local.ps1 -CasinoDB casino.json

param(
    [string]$Addr = ":9000",
    [int]$Rondas = 10,
    [string]$CasinoDB = ""
)

Set-Location (Join-Path $PSScriptRoot "..")

New-Item -ItemType Directory -Force -Path bin | Out-Null

Write-Host "Compilando mesa, casino y bots de ejemplo..."
go build -o bin/mesa.exe ./cmd/mesa
go build -o bin/casino.exe ./cmd/casino
go build -o bin/bot-aleatorio.exe ./bots/aleatorio
go build -o bin/bot-conservador.exe ./bots/conservador
if ($LASTEXITCODE -ne 0) {
    Write-Error "La compilacion fallo (codigo $LASTEXITCODE)."
    exit 1
}

$mesaAddr = if ($Addr.StartsWith(":")) { "localhost$Addr" } else { $Addr }
$token1 = "bot-aleatorio-1"
$token2 = "bot-conservador-1"
$mesaArgs = @("-addr", $Addr, "-jugadores", "2", "-min-jugadores", "2", "-rondas", $Rondas)

if ($CasinoDB -ne "") {
    Write-Host "Registrando cuentas de prueba en $CasinoDB (si no existen ya)..."
    & .\bin\casino.exe -db $CasinoDB registrar -usuario bot-aleatorio-1 2>$null | Out-Null
    & .\bin\casino.exe -db $CasinoDB registrar -usuario bot-conservador-1 2>$null | Out-Null

    $token1 = (& .\bin\casino.exe -db $CasinoDB login -usuario bot-aleatorio-1) | Select-Object -Last 1
    $token2 = (& .\bin\casino.exe -db $CasinoDB login -usuario bot-conservador-1) | Select-Object -Last 1
    if (-not $token1 -or -not $token2) {
        Write-Error "No se pudo obtener un token de login desde $CasinoDB."
        exit 1
    }
    $mesaArgs += @("-casino-db", $CasinoDB)
    Write-Host "Mesa usando el Casino de $CasinoDB"
}

Write-Host "Levantando la mesa en $Addr ($Rondas rondas)..."
$mesaProc = Start-Process -FilePath ".\bin\mesa.exe" -ArgumentList $mesaArgs -NoNewWindow -PassThru
Start-Sleep -Seconds 1

Write-Host "Conectando bot-aleatorio y bot-conservador..."
$bot1Proc = Start-Process -FilePath ".\bin\bot-aleatorio.exe" -ArgumentList @("-addr", $mesaAddr, "-token", $token1) -NoNewWindow -PassThru
$bot2Proc = Start-Process -FilePath ".\bin\bot-conservador.exe" -ArgumentList @("-addr", $mesaAddr, "-token", $token2) -NoNewWindow -PassThru

try {
    $mesaProc.WaitForExit()
}
finally {
    foreach ($p in @($bot1Proc, $bot2Proc)) {
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $p.Id -ErrorAction SilentlyContinue
        }
    }
}

if ($CasinoDB -ne "") {
    Write-Host "`nRanking actualizado:"
    & .\bin\casino.exe -db $CasinoDB ranking
}
