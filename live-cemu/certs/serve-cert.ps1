# ============================================================
# navicemu v1.3.2 - temporary HTTPS server to distribute rootCA.pem
# Phone (same WiFi) visits:  https://<LAN-IP>:8443/
#   -> download rootCA.pem -> install per the guide
#   -> revisit: no more "not secure" warning = trust works
# Note: this is only for testing the cert; navicemu (certs auto-detected) will
# serve 8766 directly.
# Usage: pwsh serve-cert.ps1   (Ctrl+C to stop)
# ============================================================
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $MyInvocation.MyCommand.Path)

$port = 8443
$lanIp = (Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Where-Object { $_.InterfaceAlias -match 'WLAN|Wi-Fi|Wireless' -and $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } |
    Select-Object -First 1).IPAddress
if (-not $lanIp) { $lanIp = '127.0.0.1' }

if (-not (Test-Path server.pem) -or -not (Test-Path rootCA.pem)) {
    throw 'server.pem / rootCA.pem missing - run gen-cert.ps1 first'
}

Write-Host ''
Write-Host '======================================================' -ForegroundColor Green
Write-Host "  Phone (same WiFi) open:  https://$lanIp`:$port/" -ForegroundColor Cyan
Write-Host '  1) tap rootCA.pem to download & install'
Write-Host '  2) revisit this page - no warning = cert trusted'
Write-Host '  Ctrl+C to stop this server'
Write-Host '======================================================' -ForegroundColor Green
Write-Host ''

python -c "import http.server, ssl; httpd = http.server.HTTPServer(('0.0.0.0', $port), http.server.SimpleHTTPRequestHandler); ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); ctx.load_cert_chain('server.pem', 'server.key'); httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True); print(f'[serve] https://0.0.0.0:$port/ ready'); httpd.serve_forever()"
