# Listener HTTP minimo para los smoke tests de Logic Apps (Fase 21).
#
# Las secciones de Action Groups/Event Grid (Fase 20) apuntan sus webhooks
# a http://localhost:10999/webhook como simple placeholder: nunca arrancan
# un listener real ahi, asi que solo verifican el campo
# lastNotificationStatus/lastDeliveryStatus despues del intento de entrega
# (una garantia mas debil). La Fase 21 exige confirmar de forma POSITIVA
# que el emulador hizo una llamada HTTP real -- por eso este script
# arranca un listener de verdad, cuenta cuantos POST recibe, y expone el
# contador via GET /count para que test-az-cli.ps1 (y, via el mismo
# interprete, terraform/smoke-test/main.tf) puedan leerlo.
#
# Uso: powershell -NoProfile -File webhook-counter-listener.ps1 -Port 10999
param(
    [int]$Port = 10999
)

$listener = New-Object System.Net.HttpListener
$listener.Prefixes.Add("http://localhost:$Port/")
$listener.Start()

$count = 0
try {
    while ($listener.IsListening) {
        $context = $listener.GetContext()
        $request = $context.Request
        $response = $context.Response

        if ($request.HttpMethod -eq "POST" -and $request.Url.AbsolutePath -ne "/count") {
            $count++
        }

        $body = ""
        if ($request.Url.AbsolutePath -eq "/count") {
            $body = "$count"
        }
        $buffer = [System.Text.Encoding]::UTF8.GetBytes($body)
        $response.ContentLength64 = $buffer.Length
        $response.OutputStream.Write($buffer, 0, $buffer.Length)
        $response.Close()
    }
} finally {
    $listener.Stop()
}
