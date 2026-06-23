#!/usr/bin/env python3
"""Mismo proposito que scripts/webhook-counter-listener.ps1 (ver ese
archivo para el porque) pero en Python, para test-az-cli.sh -- a
diferencia del entorno de test-az-cli.ps1/terraform (Windows, todo via
PowerShell), aqui no podemos asumir un interprete de PowerShell
disponible, asi que usamos Python (ya asumido como herramienta comun en
cualquier maquina con bash/curl/az CLI).

Cuenta cuantos POST reales recibe (la accion Http del workflow de Logic
Apps le apunta aqui) y expone el contador via GET /count, para confirmar
de forma positiva que el emulador hizo una llamada HTTP real -- ver
Fase 21 en ROADMAP.md.

Uso: python3 webhook-counter-listener.py <puerto>
"""
import http.server
import sys

count = 0


class Handler(http.server.BaseHTTPRequestHandler):
    def _respond(self, body=b""):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_POST(self):
        global count
        length = int(self.headers.get("Content-Length", 0))
        if length:
            self.rfile.read(length)
        if self.path != "/count":
            count += 1
        self._respond()

    def do_GET(self):
        if self.path.startswith("/count"):
            self._respond(str(count).encode())
        else:
            self._respond()

    def log_message(self, fmt, *args):
        pass  # silencioso: solo nos importa el contador, no el access log


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 10999
    http.server.HTTPServer(("127.0.0.1", port), Handler).serve_forever()
