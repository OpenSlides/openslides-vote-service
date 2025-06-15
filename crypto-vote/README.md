# Demo Server für Crypto-Vote

Dieser Demo-Server stellt eine einfache Benutzeroberfläche für das Crypto-Vote-System bereit.

## Starten des Servers

```bash
cd crypto-vote
go build demoserver
./demoserver
```

Der Server läuft standardmäßig auf `http://localhost:8080`.

## Verwendung

### Admin-Interface

Besuchen Sie `http://localhost:8080/admin` für die Administrationsoberfläche.

**Konfiguration einer Abstimmung:**

1. **User-IDs der Wähler**: Geben Sie die IDs der Benutzer ein, die abstimmen dürfen
   - Einzelne IDs: `1,2,3,5`
   - Bereiche: `1-5,10,12,17-18`
   - Gemischt: `1-3,5,7-9,15`

2. **User-IDs der Poll-Worker**: Geben Sie die IDs der Poll-Worker ein
   - Gleiche Syntax wie bei den Wähler-IDs

3. **Maximale Stimmen-Größe**: Anzahl der Bytes pro Stimme (Standard: 1024)

4. Klicken Sie auf **"Abstimmung starten"** um die Abstimmung zu beginnen

5. Mit **"Abstimmung stoppen"** können Sie die laufende Abstimmung beenden

### Client-Interface

Besuchen Sie `http://localhost:8080` für die Client-Oberfläche.

**Als Wähler teilnehmen:**

1. Geben Sie Ihre **User-ID** ein (muss in der Admin-Konfiguration als Wähler eingetragen sein)

2. Klicken Sie auf **"Verbinden"**

3. Der Client wartet automatisch, bis eine Abstimmung gestartet wird

4. Sobald die Abstimmung läuft:
   - Das WebAssembly-Modul wird geladen und initialisiert
   - Eine EventSource-Verbindung zum Server wird aufgebaut
   - Events werden im Event-Log angezeigt
   - Das Abstimmungsfeld wird verfügbar

5. Geben Sie Ihre **Stimme** in das Textfeld ein

## Technische Details

### Authentifizierung

- Der Demo-Server verwendet eine einfache Token-basierte Authentifizierung
- Clients senden ihre User-ID als `Authorization: Bearer <user_id>` Header
- **Achtung**: Dies ist nur für Demo-Zwecke! In Produktion sollte echte Authentifizierung verwendet werden

### WebAssembly Integration

- Das WASM-Modul (`crypto_vote.wasm`) wird automatisch geladen
- Der JavaScript-Wrapper (`crypto_vote.js`) kümmert sich um Memory-Management
- Events vom Server werden an das WASM-Modul weitergeleitet

### Server-Sent Events (SSE)

- Der `/board` Endpoint stellt einen SSE-Stream bereit
- Clients erhalten Echtzeit-Updates über den Abstimmungsstatus
- Das WASM-Modul verarbeitet eingehende Events

### API-Endpoints

- `GET /`: Client-Interface
- `GET /admin`: Admin-Interface
- `POST /start`: Abstimmung starten
- `POST /stop`: Abstimmung stoppen
- `GET /board`: SSE-Stream für Events
- `POST /publish_key_public`: Öffentlichen Schlüssel veröffentlichen
- `POST /publish_key_secret`: Geheimen Schlüssel veröffentlichen
- `POST /vote`: Stimme abgeben

## Entwicklung

### Dateien

- `demoserver.go`: Go-Server
- `demoserver-static/admin.html`: Admin-Interface
- `demoserver-static/client.html`: Client-Interface
- `demoserver-static/htmx.min.js`: HTMX-Bibliothek für Admin-Interface
- `wrapper/crypto_vote.js`: JavaScript-Wrapper für WASM
- `wrapper/crypto_vote.wasm`: Kompiliertes WASM-Modul

### WASM-Entwicklung

Das WASM-Modul ist in Zig geschrieben (`src/wasm_app.zig`). Nach Änderungen muss es neu kompiliert werden:

```bash
# Zig-Build-Kommando hier einfügen
```

Die kompilierte `crypto_vote.wasm` wird automatisch in den Server eingebettet.
