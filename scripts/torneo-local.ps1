# Prueba de humo (version PowerShell de torneo-local.sh): compila todo y corre
# el torneo definido en torneo.json.
#
# Pensado para Windows, donde `make torneo-local` puede fallar si `bash` en el
# PATH resuelve al stub de WSL en vez del bash.exe de Git.
#
# Sin -CasinoDB corre en modo abierto: el token de cada participante es
# directamente su identificador. Con -CasinoDB, registra las cuentas que falten
# en ese archivo, corre el torneo validando contra el Casino y al final imprime
# el ranking historico.
#
# Uso:
#   .\scripts\torneo-local.ps1
#   .\scripts\torneo-local.ps1 -Config mi-torneo.json
#   .\scripts\torneo-local.ps1 -CasinoDB casino.json

param(
    [string]$Config = "torneo.json",
    [string]$CasinoDB = ""
)

Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "Compilando..."
go build -o bin/ ./cmd/... ./bots/...
if ($LASTEXITCODE -ne 0) {
    Write-Error "La compilacion fallo (codigo $LASTEXITCODE)."
    exit 1
}

if (-not (Test-Path $Config)) {
    Write-Host "No existe $Config, escribiendo uno de ejemplo..."
    & .\bin\arena.exe init -config $Config
}

$argumentos = @("correr", "-config", $Config)

if ($CasinoDB -ne "") {
    Write-Host "Registrando cuentas en $CasinoDB (las que ya existan dan error y se ignora)..."

    # Los ids de participante salen del propio archivo de torneo.
    $torneo = Get-Content $Config -Raw | ConvertFrom-Json
    foreach ($p in $torneo.participantes) {
        & .\bin\casino.exe -db $CasinoDB registrar -usuario $p.id 2>$null | Out-Null
    }

    Write-Host "AVISO: con -CasinoDB cada participante necesita su token en el archivo"
    Write-Host "       de torneo. Conseguilos con:"
    Write-Host "       .\bin\casino.exe -db $CasinoDB login -usuario <id>"
    $argumentos += @("-casino-db", $CasinoDB)
}

& .\bin\arena.exe @argumentos

if ($CasinoDB -ne "") {
    Write-Host "`nRanking historico:"
    & .\bin\casino.exe -db $CasinoDB ranking
}
