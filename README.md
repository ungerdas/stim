# Stim

Ett webgrensesnitt for alle torrentklientene dine. Stim samler torrentene fra flere
instanser – også av ulike klienttyper – i én liste, og snakker direkte med hver klient
over klientens eget API.

**Støttede klienter**

| Klient | Testet mot | Kobler til |
| --- | --- | --- |
| qBittorrent | 4.6.7 og 5.3.0 | WebUI-API-et (`/api/v2`) |
| Deluge | 2.2.0 | JSON-RPC i webgrensesnittet (`/json`) |

rTorrent og Transmission er planlagt. Adapterlaget er laget for at nye klienter kan
legges til uten å røre resten av koden, se [Legge til en ny klienttype](#legge-til-en-ny-klienttype).

## Status

Tidlig skjelett. Dette fungerer:

- legge til, endre og fjerne klienter, med tilkoblingstest
- samlet torrentliste på tvers av klienter, med søk, sortering og filter på klient og status
- pause og gjenoppta, også på tvers av klienter i samme operasjon

Dette mangler foreløpig: legge til og slette torrenter, detaljvisning (filer, trackere,
peers), endre etikett/kategori, og **innlogging**.

> **Ingen innlogging ennå.** Alle som når porten kan pause og gjenoppta torrenter og
> endre klientoppsettet. Hold Stim på lokalnettet eller bak en revers proxy med
> autentisering (f.eks. Authelia). Passordene til klientene sendes aldri til
> nettleseren, men ligger i klartekst i `config/instances.json` (filrettighet 0600).

## Kjøre med Docker

```sh
git clone https://github.com/ungerdas/stim.git
cd stim
mkdir -p config          # må eies av brukeren containeren kjører som (UID 1000)
docker compose up -d --build
```

Åpne `http://<server>:7846` og velg **Legg til klient**.

- **qBittorrent:** adressen til WebUI-et, f.eks. `http://192.168.1.10:8080`, pluss brukernavn og passord.
- **Deluge:** adressen til webgrensesnittet, f.eks. `http://192.168.1.10:8112`, pluss passordet til
  webgrensesnittet. Stim kobler webgrensesnittet til første daemon i Connection Manager hvis det
  ikke allerede er tilkoblet.
- **Seedboks bak HTTP-innlogging** (nettleseren spør om brukernavn/passord før du ser klienten):
  fyll ut HTTP-brukernavn og -passord under «Bak revers proxy eller med selvsignert sertifikat».

## Kjøre uten Docker

Stim er én binærfil med webgrensesnittet innebygd. Krever Go 1.24+ og Node 22+ for å bygge.

```sh
make build
STIM_HOST=0.0.0.0 ./bin/stim
```

| Miljøvariabel | Standard | Betydning |
| --- | --- | --- |
| `STIM_HOST` | `127.0.0.1` | Adressen Stim lytter på. Docker-imaget setter `0.0.0.0`. |
| `STIM_PORT` | `7846` | Port. |
| `STIM_CONFIG_DIR` | `config` | Mappen der `instances.json` lagres. |

## Utvikling

To terminaler:

```sh
make dev-api   # Go-backend på http://127.0.0.1:7846
make dev-web   # Vite på http://localhost:5173, proxyer /api til backend
```

`make test` kjører `go vet` og enhetstestene.

### Oppbygging

```
cmd/stim/                 oppstart
internal/client/          felles modell + Client-grensesnittet alle adaptere implementerer
internal/client/qbittorrent/
internal/client/deluge/
internal/store/           klientoppsettet (instances.json)
internal/manager/         én levende klient per instans, samlet og kort mellomlagret visning
internal/api/             HTTP-API + servering av webgrensesnittet
internal/webui/           innebygd frontend-build
web/                      React + Vite + TypeScript
```

### Legge til en ny klienttype

1. Lag en pakke under `internal/client/<navn>/` som implementerer `client.Client`
   (`Version`, `Torrents`, `Pause`, `Resume`) og oversetter klientens tilstander til `client.State`.
2. Registrer den i pakkens `init()` med `client.Register(...)`. `client.Info` forteller
   webgrensesnittet hva skjemaet skal vise (trengs brukernavn, eksempel-URL, støtte for etiketter/tagger).
3. Importer pakken i `cmd/stim/main.go`.

Webgrensesnittet henter klienttypene fra `/api/kinds` og trenger ingen endringer.

### API

| Metode | Sti | |
| --- | --- | --- |
| `GET` | `/api/kinds` | klienttyper som støttes |
| `GET` | `/api/instances` | klientoppsett (uten passord) |
| `POST` | `/api/instances` | legg til klient |
| `PUT` | `/api/instances/{id}` | endre klient (utelatt passord = behold) |
| `DELETE` | `/api/instances/{id}` | fjern klient |
| `POST` | `/api/instances/test` | test tilkobling uten å lagre |
| `POST` | `/api/instances/{id}/retry` | prøv igjen etter avvist innlogging |
| `GET` | `/api/torrents` | status per klient + alle torrenter |
| `POST` | `/api/torrents/action` | `{"action": "pause" \| "resume", "items": [{"instanceId", "hash"}]}` |

## Kjente begrensninger

- Hele torrentlisten hentes fra hver klient ved hver oppdatering (mellomlagret i 2 sekunder).
  Det holder for noen tusen torrenter; inkrementell synk (`sync/maindata` i qBittorrent,
  `web.update_ui` i Deluge) er neste steg for store samlinger.
- Etter et avvist passord slutter Stim å prøve på egen hånd, fordi qBittorrent sperrer
  adresser etter noen få mislykkede innlogginger. Lagre klienten på nytt eller velg «prøv igjen».
- Deluge oppgir tracker som domene (`example.org`), qBittorrent som fullt vertsnavn
  (`tracker.example.org`).
- Deluge 1.x er forsøkt støttet (egne metodenavn for pause/gjenoppta), men ikke testet.
