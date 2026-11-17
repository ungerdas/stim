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
- trackerfeil og avregistrerte torrenter: egne filtre, markering i listen og trackerens egen melding
- søk på alle trackerne dine gjennom Prowlarr, og last ned rett til valgfri klient, med merking av det du
  allerede har og forslag om å seede samme utgivelse fra filene du har
- egen innstillingsside (tannhjulet ved navnet) for klienter, servere og visning, så forsiden bare har listen
- ledig plass per klient under Innstillinger, og i sidemenyen når det er under 50 GiB igjen
- filer uten torrent: finn og slett rester i nedlastingsmappene på en server (over SSH)
- detaljpanel med oversikt, filer, trackere og peers (klikk på navnet)
- kryss-seeds samlet: samme utgivelse på flere trackere vises som én rad med trackerne, summert opplastet og
  samlet ratio, og kan foldes ut (slås av under Innstillinger)
- hvor mye som er lastet opp: for utvalget i sidemenyen, og per tracker og klient under «Se statistikk»
- pause og gjenoppta, også på tvers av klienter i samme operasjon
- slette torrenter, med eller uten filene, med varsel når andre torrenter bruker de samme filene
- varsel om dobbeltseeding: samme torrent i flere klienter, med filter og hjelp til å fjerne kopiene
- flytte torrenter mellom klienter på samme server, med valgfri flytting av filene til en ny mappe
- flytte torrenter mellom servere: Stim kopierer filene over SSH, og kan slette dem på den gamle serveren

Dette mangler foreløpig: legge til egne .torrent-filer og magnetlenker (utenom søket), endre
etikett/kategori, og **innlogging**.

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

Åpne `http://<server>:7846` og velg **Legg til klient**. Senere legger du til og endrer klienter under
**Innstillinger** (tannhjulet ved navnet øverst i sidemenyen, eller `/#innstillinger`).

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
kilden er urørt.

### Samme utgivelse på flere trackere

Bruker andre torrenter nøyaktig de samme filene, for eksempel samme utgivelse på flere trackere, tilbyr
dialogen å **ta dem med** (på som standard). Da flyttes de sammen, og filene flyttes eller kopieres bare én
gang: den siste av dem flytter filene, og de andre pekes dit og sjekkes på nytt før de starter igjen. Ved
flytting mellom servere slettes de gamle filene først når den siste har flyttet.

Tar du dem ikke med, flytter Stim bare torrenten du valgte, og de andre seeder videre som før. Filene blir
da liggende der de er, også med «Flytt filene til en annen mappe» eller «Slett filene på den gamle
serveren», så de andre ikke mister dem. Det samme gjelder hvis en av dem ikke lar seg flytte, eller hvis
filene bare delvis overlapper (en episode seedet fra inni en sesongpakke). Fremdriftslisten sier fra. Kategori/etikett og tagger følger med der målklienten støtter det. Ufullstendige torrenter
hoppes over.

Under **Filene** velger du om klientene deler disk:

- **Samme server:** begge klientene må se filene på **samme sti**. Det gjelder for klienter som kjører som
  samme bruker på samme server, slik som på en seedboks. Kjører en av dem i Docker med andre mount-punkter,
  vil sjekken i steg 3 feile, og ingenting blir endret.
- **En annen server:** Stim kopierer filene dit først (se under). Stim foreslår dette selv når kilden og
  målet ligger på ulike servere, ut fra SSH-verten, eller verten i adressen når SSH ikke er satt opp.

### Mellom servere

Med «En annen server» kopierer Stim filene til mappen du velger på målserveren før steg 2, og
målklienten peker på kopien. Mappen er forhåndsutfylt med målklientens standardmappe for nedlastinger.

- **Begge** klientene trenger SSH-tilgang (se under). Målserverens SSH-nøkkel må være lagret, altså at du
  har trykket «Test tilkobling» og lagret klienten, fordi Stim skal skrive dit.
- Filene går **gjennom Stim**: lastes ned fra den gamle serveren og opp til den nye. Kjører Stim hjemme,
  er det altså hjemmelinjens opplastingshastighet som setter farten. Dataene går ikke over BitTorrent, så
  ingenting telles som nedlasting eller opplasting på trackeren.
- Stim sjekker at hver fil finnes med riktig størrelse, og at det er plass på målserveren (med 256 MiB
  til overs), før kopieringen starter. Filer skrives som `navn.stimpart` og får riktig navn når de er
  ferdige. Filer som allerede ligger der med riktig størrelse, kopieres ikke på nytt; målklientens sjekk i
  steg 3 avgjør om de er like. Flytter du flere utgaver av samme utgivelse til samme mappe, kopieres filene
  altså bare én gang. Ligger det en annen fil med samme navn og annen størrelse der, stopper Stim i stedet
  for å skrive over den.
- Feiler sjekken, eller du avbryter, slettes det Stim kopierte (og mappene Stim laget) igjen, og kilden er
  urørt. Ryker forbindelsen midt i en torrent med flere filer, beholdes de ferdige filene, så neste forsøk
  fortsetter der.
- **Slett filene på den gamle serveren etterpå** gjøres bare når målklienten har sjekket kopien og tatt
  over, og gjennom kildeklienten selv. Stim sletter ikke filer som andre torrenter på den gamle serveren
  bruker, i **alle** klientene du har lagt inn der (kryss-seeding, eller en episode seedet fra inni en
  sesongpakke). Klarer ikke Stim å sjekke det, for eksempel fordi en av klientene er utilgjengelig,
  beholdes filene. Fremdriftslisten sier fra når filene ble beholdt, og hvorfor.

### SSH-tilgang

Alle klienttyper bruker SSH for å flytte mellom servere. Noen trenger det også ellers:

| Klient | Trenger ellers SSH for | Hvor Stim leser torrentfilen |
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
**Innstillinger → Endre → SSH-tilgang** på Deluge-klienten:

- **SSH-vert, port, brukernavn** og enten **passord** eller **privat nøkkel** (uten passordfrase)
- **Mappen med torrentfilene**, vanligvis `.config/deluge/state` relativt til hjemmemappen. Har du flere
  Deluge-instanser på samme server, har hver sin mappe.

«Test tilkobling» viser hvor mange torrentfiler Stim fant, og serverens SSH-nøkkel. Nøkkelen lagres, og
endres den senere, nekter Stim å koble til til du har godtatt den nye under Endre.

Over SFTP leser Stim `.torrent`-filer, og ved flytting mellom servere filene til torrentene som flyttes.
Stim skriver bare til mappen du velger når du flytter, og sletter bare det Stim selv kopierte. Men
SSH-innloggingen gir full tilgang til kontoen på serveren, og den lagres i klartekst i
`config/instances.json`. Bruk gjerne en egen nøkkel kun for Stim.

## Søk på trackerne

**Søk på trackerne** øverst i sidemenyen (eller `/#sok`) søker i alle trackerne du har satt opp i
[Prowlarr](https://prowlarr.com) samtidig. Prowlarr har innloggingene og vet hvordan hver tracker skal spørres;
Stim logger ikke inn på trackerne selv.

1. Under **Innstillinger → Søk på trackerne**: adressen til Prowlarr (for eksempel
   `http://192.168.50.5:9696`) og API-nøkkelen fra Prowlarrs *Settings → General*. «Test tilkobling» viser
   hvilke trackere Prowlarr har.
2. Søk, velg trackere og eventuelt TV eller film. Treffene vises med de nyeste først. Trykk på et
   kolonnenavn for å sortere på seedere, størrelse, alder osv.; Stim husker sorteringen du valgte sist.
3. **Last ned** henter `.torrent`-filen gjennom Prowlarr og legger den i klienten du velger, i mappen og
   kategorien/etiketten du oppgir (tomt: klienten bestemmer selv).

Treffene merkes:

- **Har den:** akkurat denne torrenten ligger allerede i en klient (samme info-hash). Den klienten kan ikke
  velges.
- **Har utgivelsen:** samme utgivelse fra en annen tracker ligger i en klient (samme navn og omtrent samme
  størrelse, som for kryss-seeds). Last ned-vinduet foreslår da den klienten og mappen der filene ligger, så
  klienten sjekker dem og seeder uten å laste ned på nytt.
- **Freeleech** når trackeren sier det.

Prowlarrs nedlastingslenker inneholder API-nøkkelen, så de blir på Stim-serveren; nettleseren får bare en
ID per treff, som gjelder i to timer. API-nøkkelen lagres i `config/instances.json` og sendes aldri til
nettleseren.

For rTorrent lager Stim mappen du velger først (gjennom rTorrent, eller over SSH bak ruTorrent), siden
rTorrent ellers bare lager torrentens egen mappe og står stille hvis mappen over mangler.

## Trackerfeil og avregistrerte torrenter

Stim leser trackerstatus fra hver klient og deler problemene i to filtre i sidemenyen:

- **Avregistrert:** trackeren kjenner ikke torrenten lenger (slettet, «trumped», erstattet). Det går ikke over
  av seg selv. Stim kjenner igjen de vanlige meldingene, for eksempel «Unregistered torrent», «torrent not
  found» og «not registered», også norske varianter. «Host not found» og andre nettverksfeil regnes ikke med.
- **Trackerfeil:** alt annet som feiler, for eksempel at trackeren er nede eller ikke svarer. Det går ofte over.

En torrent regnes bare som et problem når ingen av trackerne dens virker. Trackerkolonnen markerer den, og
meldingen vises når du holder musen over eller åpner detaljene.

| Klient | Hvor statusen kommer fra |
| --- | --- |
| qBittorrent 5.1+ | feilflagg i listen; meldingene hentes samlet for de som har feil, og gjenbrukes i 2 minutter |
| qBittorrent eldre | torrenter uten fungerende tracker slås opp hver for seg, 25 om gangen |
| Deluge | trackerstatusen; en feil huskes i 30 minutter mens Deluge prøver igjen («Announce Sent») |
| Transmission | feilfeltet (trackerfeil og -advarsler) |
| rTorrent | meldingen rTorrent selv viser («Tracker: [...]») |

## Ledig plass

Under **Innstillinger** står plassen som er igjen der hver klient lagrer nye torrenter, og i sidemenyen
vises den under klienten når det er under 50 GiB igjen. Tallet kommer fra qBittorrent og Deluge direkte,
fra Transmission hvert 30. sekund, og fra rTorrent via en torrent som kjører i standardmappen.

## Filer uten torrent

Under **Innstillinger → Servere** har hver server **Finn filer uten torrent**. Stim logger inn over SSH,
lister mappene som torrentene på serveren lagres i, og sammenligner med torrentene i *alle* klientene i
Stim:

- Bare det som ligger rett i disse mappene sammenlignes. En mappe som hører til en torrent, får være i fred i
  sin helhet, også om den har ekstra filer. Mapper inne i en torrents egen mappe ses ikke gjennom.
- Skjulte filer, ting endret den siste timen, og ting med samme navn som en torrent (uten hensyn til
  punktum, mellomrom og `.mkv`) vises aldri.
- Hjemmemappen og mapper rett under roten ses ikke gjennom.
- Svarer ikke alle klientene på serveren, stopper skanningen. Svarer ikke en klient på en annen server, sier
  Stim fra, siden det kan være samme maskin lagt inn under et annet navn.

Du velger selv hva som skal slettes. Rett før sletting skanner Stim på nytt, og sletter bare det som fortsatt
ikke brukes. En klient som ikke er lagt inn i Stim kan bruke de samme mappene; filene dens vil da stå i
listen. Se over før du sletter.

## Detaljer

Klikk på navnet til en torrent for å åpne detaljpanelet: oversikt (sti, datoer, info-hash, biter, kommentar),
filer med fremdrift, trackere med status, melding og antall seedere og leechere, og tilkoblede peers.
Passkeys i trackeradressene maskeres i Stim før de sendes til nettleseren. Panelet oppdateres hvert 3.
sekund. Klikk på navnet til en samlet kryss-seed-rad for å folde den ut.

## Kryss-seeds i listen

Torrenter med samme navn og omtrent samme størrelse regnes som samme utgivelse, for eksempel seedet på flere
trackere. Navn sammenlignes uten forskjell på store og små bokstaver, punktum, mellomrom og understrek, og
uten filendelse (`.mkv`). Størrelsene kan skille med opptil 2 %, som i cross-seed, fordi én tracker kan ha
en `.nfo` eller sample som en annen mangler.
Med **Samle kryss-seeds** på (standard; slås av under Innstillinger, og valget huskes i nettleseren) vises
de som én rad:

- **Tracker** viser alle trackerne, og merket ved navnet sier hvor mange
- **Lastet opp** er summen, og **Ratio** er summen delt på størrelsen, altså hvor mange ganger filene er
  lastet opp til sammen
- **Status** viser det som trenger oppmerksomhet først (feil, nedlasting, pause), ellers «Seeder»
- pilen foran navnet folder ut én rad per torrent

Å velge raden velger alle torrentene i den, så Pause, Flytt og Slett gjelder alle. Fold ut for å velge
enkelttorrenter. Filtre og søk gjelder hver torrent for seg: skjuler du en tracker, forsvinner bare den fra
raden.

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
internal/torrentfile/     leser .torrent-filer (info-hash, filliste) uten å endre dem
internal/remotefiles/     filer over SFTP: lese, kopiere, liste, slette
internal/orphans/         finner filer uten torrent
internal/prowlarr/        søker og henter torrentfiler gjennom Prowlarr
internal/search/          søk fra grensesnittet og nedlasting til en klient
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
| `GET` | `/api/instances/{id}/savepath` | klientens standardmappe for nedlastinger |
| `GET` | `/api/torrents` | status per klient + alle torrenter |
| `POST` | `/api/torrents/action` | `{"action": "pause" \| "resume", "items": [{"instanceId", "hash"}]}` |
| `POST` | `/api/torrents/remove` | `{"items": [{"instanceId", "hash"}], "deleteData": false}`; filene slettes bare med `"deleteData": true` |
| `GET` | `/api/details?instance=…&hash=…` | filer, trackere (passkeys maskert), peers og egenskaper for én torrent |
| `POST` | `/api/orphans/scan` | `{"server"}`: filer uten torrent i serverens nedlastingsmapper |
| `POST` | `/api/orphans/delete` | `{"server", "paths": []}`: sjekker på nytt og sletter det som fortsatt er rester |
| `GET`/`PUT` | `/api/search/settings` | Prowlarr-adressen (nøkkelen vises aldri; utelatt nøkkel = behold) |
| `POST` | `/api/search/test` | `{"prowlarrUrl", "apiKey"}`: test uten å lagre |
| `GET` | `/api/search/indexers` | trackerne i Prowlarr |
| `POST` | `/api/search` | `{"query", "indexerIds": [], "categories": []}` |
| `POST` | `/api/search/grab` | `{"id", "instanceId", "savePath", "label", "paused"}`: last ned et treff til en klient |
| `GET` | `/api/transfers` | flyttejobber, nyeste først |
| `POST` | `/api/transfers` | `{"targetId", "removeFromSource", "newPath", "items": [{"instanceId", "hash"}]}`; mellom servere også `"copyFiles": true, "targetPath", "deleteSourceFiles"` |
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
- Flytting mellom servere sender dataene via Stim, ikke direkte fra server til server.
