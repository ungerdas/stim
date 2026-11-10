# Stim

Ett webgrensesnitt for alle torrentklientene dine. Stim samler torrentene fra flere
instanser – også av ulike klienttyper – i én liste, og snakker direkte med hver klient
over klientens eget API.

**Støttede klienter**

| Klient | Testet mot | Kobler til |
| --- | --- | --- |
| qBittorrent | 4.6.7 og 5.3.0 | WebUI-API-et (`/api/v2`) |
| Deluge | 2.2.0 | JSON-RPC i webgrensesnittet (`/json`) |
| Transmission | 4.0.5 | RPC (`/transmission/rpc`) |
| rTorrent | 0.9.8, direkte og bak ruTorrent | XML-RPC (`/RPC2`, eller ruTorrents httprpc-plugin) |

Adapterlaget er laget for at nye klienter kan legges til uten å røre resten av koden, se
[Legge til en ny klienttype](#legge-til-en-ny-klienttype).

## Status

Tidlig skjelett. Dette fungerer:

- legge til, endre og fjerne klienter, med tilkoblingstest
- samlet torrentliste på tvers av klienter, med søk, sortering og filter på klient og status
- trackerfilter: huk av hvilke trackere som skal vises (valget huskes i nettleseren)
- hvor mye som er lastet opp: for utvalget i sidemenyen, og per tracker og klient under «Se statistikk»
- pause og gjenoppta, også på tvers av klienter i samme operasjon
- slette torrenter, med eller uten filene, med varsel når andre torrenter bruker de samme filene
- varsel om dobbeltseeding: samme torrent i flere klienter, med filter og hjelp til å fjerne kopiene
- flytte torrenter mellom klienter på samme server, med valgfri flytting av filene til en ny mappe

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
- **Transmission:** adressen til serveren, f.eks. `http://192.168.1.10:9091`, pluss brukernavn og passord
  fra `rpc-username`/`rpc-password` hvis innlogging er slått på. Adressen til webgrensesnittet
  (`…/transmission/web/`) eller RPC-adressen virker også.
- **rTorrent:** adressen til ruTorrent (f.eks. `https://server/brukernavn/rutorrent`), og Stim bruker
  ruTorrents httprpc-plugin. Eller en XML-RPC-adresse rett mot rTorrent (`…/RPC2`). Brukernavn og passord er
  innloggingen i nettleseren foran ruTorrent, hvis det er en.
- **Seedboks bak HTTP-innlogging** (nettleseren spør om brukernavn/passord før du ser klienten):
  for qBittorrent og Deluge fyller du det ut under «Bak revers proxy eller med selvsignert sertifikat». For
  Transmission og rTorrent er det selve brukernavnet og passordet.

## Flytte torrenter mellom klienter

Velg torrenter, trykk **Flytt til …** og velg målklient. Stim flytter én torrent av gangen:

1. henter `.torrent`-filen fra kildeklienten og sjekker at den hører til riktig torrent
2. legger den til **stoppet** i målklienten, pekt på filene der de ligger
3. lar målklienten sjekke filene, og krever at den finner 100 %
4. fjerner torrenten fra kildeklienten, **alltid uten å slette data** (kan slås av, så begge seeder)
5. starter den i målklienten, og flytter eventuelt filene til en ny mappe

Finner ikke målklienten alle filene, eller du avbryter før steg 4, fjernes kopien fra målklienten igjen og
kilden er urørt. Kategori/etikett og tagger følger med der målklienten støtter det. Ufullstendige torrenter
hoppes over.

Begge klientene må se filene på **samme sti**. Det gjelder for klienter som kjører som samme bruker på
samme server, slik som på en seedboks. Kjører en av dem i Docker med andre mount-punkter, vil sjekken i
steg 3 feile, og ingenting blir endret.

### SSH-tilgang

| Klient | Trenger SSH for | Hvor Stim leser torrentfilen |
| --- | --- | --- |
| qBittorrent | ingenting | gjennom API-et |
| Deluge | å flytte torrenter herfra | mappen du oppgir, vanligvis `.config/deluge/state` |
| Transmission | å flytte torrenter herfra | stien Transmission selv oppgir |
| rTorrent | bak ruTorrent: å flytte herfra og å slette filer | rTorrents session-mappe |

Over en ren `/RPC2`-adresse henter Stim torrentfilen og sletter filer gjennom rTorrent selv. Nyere ruTorrent
stopper kommandoene som trengs for det, og da bruker Stim SSH. Uten SSH nekter Stim å slette filene og lar
torrenten ligge (stoppet), i stedet for å fjerne den og etterlate filene. Sletting over SSH krever at
serverens nøkkel er lagret, altså at du har trykket «Test tilkobling» og lagret klienten.

rTorrent kan ikke flytte filer selv, så «Flytt filene til en annen mappe» er ikke tilgjengelig med rTorrent
som mål. Flytting til rTorrent bak ruTorrent krever at mappen ligger innenfor ruTorrents `$topDirectory`;
ellers dropper ruTorrent mappen, rTorrent finner ikke filene, og Stim ruller flyttingen tilbake.

Kjører klienten i Docker, oppgir Transmission og rTorrent stier slik containeren ser dem. Da må SSH-brukeren se
de samme stiene, ellers finner ikke Stim torrentfilen.

### Fra Deluge: SSH-tilgang

qBittorrent gir fra seg `.torrent`-filen over API-et (4.5 og nyere). Deluge gjør ikke det, så for å flytte
**fra** Deluge henter Stim filen fra Deluges `state`-mappe på serveren over SFTP. Fyll inn under
**Endre → SSH-tilgang** på Deluge-klienten:

- **SSH-vert, port, brukernavn** og enten **passord** eller **privat nøkkel** (uten passordfrase)
- **Mappen med torrentfilene**, vanligvis `.config/deluge/state` relativt til hjemmemappen. Har du flere
  Deluge-instanser på samme server, har hver sin mappe.

«Test tilkobling» viser hvor mange torrentfiler Stim fant, og serverens SSH-nøkkel. Nøkkelen lagres, og
endres den senere, nekter Stim å koble til til du har godtatt den nye under Endre.

Stim leser bare `.torrent`-filer over SFTP, men SSH-innloggingen gir full tilgang til kontoen på serveren,
og den lagres i klartekst i `config/instances.json`. Bruk gjerne en egen nøkkel kun for Stim.

## Dobbeltseeding

Ligger samme torrent i mer enn én klient, viser Stim et varsel, og torrentene får et «2×»-merke. Samme
torrent i to klienter melder seg på trackeren to ganger. Cross-seeds (samme filer, men en annen torrent fra
en annen tracker) telles ikke.

**Vis dem** filtrerer listen til kopiene, sortert så de havner ved siden av hverandre. **Behold kopien i …**
velger kopiene i de andre klientene, som du så fjerner med **Slett …**. Ligger kopiene i samme mappe, deler de
filer: velg da «Fjern bare torrentene». **Skjul** fjerner varselet til det dukker opp nye.

Hybride torrenter (v1+v2) kan ha ulik ID i ulike klienter (v1-hash i klienter på libtorrent 1.2, avkortet
v2-hash på libtorrent 2). qBittorrent oppgir begge, og Stim bruker det til å kjenne dem igjen.

## Opplastet mengde

Sidemenyen viser hvor mye torrentene i listen har lastet opp, med ratio, og følger filtrene du har valgt.
**Se statistikk** viser det samme per tracker og per klient.

- **Lastet opp** er summen av det hver torrent har lastet opp, slik klienten oppgir det. Bare torrenter
  som ligger i klientene nå er med, og en torrent som flyttes til en annen klient begynner på 0 der.
- **Totalt i klienten** er klientens egen sum over alle tider, også for fjernede torrenter, inkludert
  protokolltrafikk. qBittorrent fører den alltid. Deluge fører den bare med Stats-pluginen slått på, og
  teller da fra den ble slått på.

## Kjøre uten Docker

Stim er én binærfil med webgrensesnittet innebygd. Krever Go 1.26+ og Node 22+ for å bygge.

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
internal/transfer/        flytting mellom klienter (jobber, tilbakerulling)
internal/torrentfile/     leser .torrent-filer uten å endre info-hashen
internal/remotefiles/     henter filer over SFTP
internal/manager/         én levende klient per instans, samlet og kort mellomlagret visning
internal/api/             HTTP-API + servering av webgrensesnittet
internal/webui/           innebygd frontend-build
web/                      React + Vite + TypeScript
```

### Legge til en ny klienttype

1. Lag en pakke under `internal/client/<navn>/` som implementerer `client.Client`
   (`Version`, `Torrents`, `Pause`, `Resume`, `Remove`) og oversetter klientens tilstander til
   `client.State`. For flytting implementerer den også `client.Transferer`, `client.Mover` hvis klienten kan
   flytte filer, og `client.Exporter` og/eller `client.TorrentFilePather` for å få tak i `.torrent`-filen.
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
| `POST` | `/api/torrents/remove` | `{"items": [{"instanceId", "hash"}], "deleteData": false}`; filene slettes bare med `"deleteData": true` |
| `GET` | `/api/transfers` | flyttejobber, nyeste først |
| `POST` | `/api/transfers` | `{"targetId", "removeFromSource", "newPath", "items": [{"instanceId", "hash"}]}` |
| `POST` | `/api/transfers/{id}/cancel` | avbryt en flyttejobb |

## Kjente begrensninger

- qBittorrent synkroniseres inkrementelt (`sync/maindata`), så bare endringer hentes. Fra Deluge
  hentes hele listen ved hver oppdatering (mellomlagret i 2 sekunder), noe som holder for noen
  tusen torrenter.
- Etter et avvist passord slutter Stim å prøve på egen hånd, fordi qBittorrent sperrer
  adresser etter noen få mislykkede innlogginger. Lagre klienten på nytt eller velg «prøv igjen».
- Trackere vises som nettstedets domene (`tracker.example.org` blir `example.org`), slik at samme
  tracker grupperes likt fra qBittorrent og Deluge. Har en torrent flere trackere, vises én av dem.
- Deluge 1.x er forsøkt støttet (egne metodenavn for pause/gjenoppta), men ikke testet.
- rTorrent fører ingen totalsum over tid, bare siden omstart. «Lagt til» er tidspunktet ruTorrent registrerte,
  eller når rTorrent lastet torrenten.
- Transmission-etiketter vises som tagger, siden en torrent kan ha flere.
