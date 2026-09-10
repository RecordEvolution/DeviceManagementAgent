# Netzwerkstörung zwischen Edge-PC LAC_LS02 und IronFlock Appliance tls-sf015

**Analysebericht zur Übergabe an die TRUMPF IT / Netzwerkbetrieb**

| | |
|---|---|
| Erstellt am | 10.09.2026 |
| Erstellt von | IronFlock (Marko Petzold) |
| Betroffene Systeme | `LAC_LS02_WinPC` (10.130.23.35) und Appliance `tls-sf015.corp.trumpf.com` (136.230.111.59) |
| Status | Ursache eingegrenzt auf eine Komponente im Netzwerkpfad; Endgeräte als Ursache messtechnisch ausgeschlossen |

---

## 1. Zusammenfassung

Zwischen dem Windows-Edge-PC `LAC_LS02` und der IronFlock Appliance `tls-sf015` treten seit mindestens dem 02.09.2026 wiederkehrende Störungen auf, die jeweils mehrere Minuten bis mehrere Stunden andauern.

Während einer Störung gilt:

- **Neue TCP-Verbindungen** vom PC zur Appliance kommen nicht zustande, und zwar auf **allen** getesteten Ports (443 und 18080).
- **Bereits bestehende TCP-Verbindungen** zwischen denselben beiden Hosts laufen **ununterbrochen weiter** und übertragen Daten.
- **ICMP (Ping)** zwischen denselben beiden Hosts funktioniert **durchgehend**.
- **DNS** löst durchgehend korrekt auf.

Am 10.09.2026 wurde eine Störung von ca. 12:44 bis 12:55 Uhr **gleichzeitig auf beiden Endgeräten paketgenau mitgeschnitten**. Das Ergebnis ist eindeutig:

> Der PC hat in diesem Zeitraum **41 SYN-Pakete über seine physische Netzwerkkarte gesendet**. Auf dem PC wurde **kein einziges Paket von irgendeiner lokalen Komponente verworfen**. Auf der Appliance ist in diesem Zeitraum **kein einziges SYN-Paket angekommen**. Ab 12:55:00 Uhr kamen die SYN-Pakete wieder an und wurden von der Appliance innerhalb von 0,2 ms beantwortet.

Daraus folgt: Eine Komponente **im Netzwerkpfad** zwischen beiden Systemen verwirft neue TCP-Verbindungsanfragen still (ohne RST, ohne ICMP-Fehlermeldung), während sie ICMP und bereits etablierte Sessions weiterhin passieren lässt. Dieses Verhalten ist typisch für eine **zustandsbehaftete (stateful) Firewall bzw. ein IPS**.

**Wir bitten um Prüfung**, welche Komponente im Pfad dieses Verhalten zeigt und wodurch es ausgelöst und wieder aufgehoben wird.

---

## 2. Umgebung

### 2.1 Appliance

| Merkmal | Wert |
|---|---|
| Hostname | `tls-sf015.corp.trumpf.com` |
| IP-Adresse | 136.230.111.59 |
| Betriebssystem | Debian Linux |
| Netzwerkschnittstelle | `enp3s0` |
| Rolle | On-Premises-Plattform (IronFlock Appliance) für Maschinen- und Anwendungsdaten |

Die Appliance wurde am 09.09.2026 auf den Betrieb mit einem **TRUMPF-Unternehmenszertifikat** umgestellt (Wildcard `*.tls-sf015.corp.trumpf.com` und Apex, ausgestellt von `CA-TRUMPF-SUB-01`). Seitdem laufen alle Dienste über TLS auf **TCP 443**, terminiert durch einen Reverse Proxy:

| Subdomain | Dienst |
|---|---|
| `ws.tls-sf015.corp.trumpf.com` | WebSocket-Verbindung der Geräte (Dauerverbindung) |
| `registry.tls-sf015.corp.trumpf.com` | Container-Registry (Image-Pulls der Geräte) |
| `api.` / `auth.` / `login.` / `ide.` | Weitere Plattformdienste |

Zusätzlich erreichbar im LAN: **TCP 18080** (WAMP-Router direkt, ohne TLS) und **TCP 15001** (Registry im LAN, Altbestand).

### 2.2 Edge-PC

| Merkmal | Wert |
|---|---|
| Gerätename | `LAC_LS02_WinPC` |
| IP-Adresse | 10.130.23.35 |
| Betriebssystem | Windows, Mitglied der Domäne `corp.trumpf.com` |
| Zeitquelle | `srv01dc8.corp.trumpf.com` (NT5DS, Domänenhierarchie) |
| Netzwerkkarte | Intel(R) Ethernet Connection (17) I219-LM |
| System-Proxy | **nicht aktiv** (`ProxyEnable = 0`, kein PAC-Skript) |

Auf dem PC läuft der IronFlock-Geräteagent (`reagent`) als Windows-Dienst. Er hält eine **dauerhafte WebSocket-Verbindung** zu `wss://ws.tls-sf015.corp.trumpf.com:443`. Ferner laufen Docker Desktop sowie zwei Anwendungscontainer-Stacks.

### 2.3 Kommunikationsbeziehung

```
LAC_LS02_WinPC (10.130.23.35)  ──►  Appliance tls-sf015 (136.230.111.59)
        TCP 443   (Dauerverbindung Agent, Container-Image-Pulls)
        TCP 18080 (WAMP-Router, Anwendungscontainer)
        ICMP      (nur zu Diagnosezwecken)
```

---

## 3. Symptom und Auswirkung

Während einer Störung verliert der Geräteagent seine Verbindung und kann sie **nicht wieder aufbauen**. Für den Betrieb bedeutet das:

- Das Gerät erscheint in der Plattform als **offline**.
- **Software-Updates und Container-Image-Downloads schlagen fehl** (Zeitüberschreitung beim Registry-Login).
- Produktionsanwendungen auf dem PC können in dieser Zeit keine Daten an die Plattform übertragen.

Bemerkenswert und diagnostisch entscheidend: Solange die bestehende Verbindung des Agenten nicht abreißt, **bleibt das Gerät scheinbar online**, obwohl gleichzeitig keine neue Verbindung aufgebaut werden kann. Am 10.09.2026 um 09:57 Uhr war das der Fall: Der Agent war verbunden, gleichzeitig lief der Registry-Login von Docker in eine Zeitüberschreitung.

---

## 4. Beobachtete Störungsfenster

Ermittelt aus dem Protokoll des Geräteagenten (`C:\ProgramData\IronFlock\Reagent\reagent.log`), Meldung `dial tcp 136.230.111.59:443: i/o timeout`. Der Agent versucht während einer Störung etwa alle 2,2 Sekunden einen neuen Verbindungsaufbau, entsprechend rund 27 Versuche pro Minute.

| Datum | Beginn | Ende | Dauer |
|---|---|---|---|
| 02.09.2026 | 13:21 | 13:45 | 24 Minuten |
| 04.09.2026 | 02:30 | 06:30 | **4 Stunden** |
| 07.09.2026 | 12:43 | 12:50 | 8 Minuten |
| 08.09.2026 | 12:02 | 12:10 | 9 Minuten |
| 09.09.2026 | 19:29 | 19:42 | 14 Minuten |
| 10.09.2026 | 09:01 | 09:05 | 5 Minuten |
| 10.09.2026 | 12:15 | 12:16 | 1 Minute (durch Messsonde erfasst) |
| 10.09.2026 | ca. 12:44 | 12:55 | ca. 11 Minuten (**beidseitig paketgenau erfasst**) |

Es ist **kein Muster** in Uhrzeit oder Abstand erkennbar; ein zeitgesteuerter Vorgang scheidet damit als Auslöser aus. Die Störung endet jeweils **von selbst und abrupt**.

---

## 5. Messaufbau

Für die Analyse am 10.09.2026 wurde auf beiden Seiten gleichzeitig gemessen. Die Uhren beider Systeme wurden zuvor abgeglichen; die Abweichung lag unter einer Sekunde.

### 5.1 Auf dem Edge-PC

| Werkzeug | Konfiguration | Zweck |
|---|---|---|
| `pktmon` (Windows-Bordmittel) | Aufzeichnung aller Pakete an **allen** Netzwerkkomponenten, Filter auf 136.230.111.59 TCP 443 | Zeigt, ob ein Paket den Rechner verlässt, und benennt jede Komponente, die ein Paket verwirft |
| PowerShell-Messsonde | Alle 5 Sekunden: TCP 443 (Timeout 1,25 s), TCP 443 (Timeout 10 s), TCP 18080, ICMP, DNS | Misst Ausfall und Umfang der Störung protokollübergreifend |

`pktmon` protokolliert ein Paket auf seinem Weg durch den Netzwerkstapel an jeder beteiligten Komponente. **Komponente 9** ist die physische Netzwerkkarte (Intel I219-LM). Ein Paket, das dort in Richtung `Tx` erscheint, wurde an die Hardware zur Übertragung übergeben.

### 5.2 Auf der Appliance

| Werkzeug | Konfiguration | Zweck |
|---|---|---|
| `tcpdump` | `enp3s0`, Filter `host 10.130.23.35 and tcp port 443`, Ringpuffer | Zeigt, welche Pakete tatsächlich ankommen |
| Eigener Prüfdienst | Alle 10 Sekunden lokaler HTTPS-Aufruf gegen die eigene öffentliche Adresse, zusätzlich Status der Cloud-Anbindung | Schließt Fehler der Appliance selbst aus |

---

## 6. Messergebnisse

### 6.1 Gegenüberstellung beider Seiten, Störung vom 10.09.2026

| Messgröße | 12:46 bis 12:54 (Störung) | ab 12:55 (normal) |
|---|---|---|
| SYN-Pakete an der Netzwerkkarte des PCs gesendet (`Tx`, Komponente 9) | **41** | fortlaufend |
| Von einer PC-Komponente verworfene Pakete (`Drop`) | **0** | 0 |
| Vom PC empfangene SYN-ACK-Pakete | **0** | — |
| Auf der Appliance empfangene SYN-Pakete | **0** | 1 pro Minute, **jeweils in 0,2 ms beantwortet** |
| ICMP vom PC zur Appliance | **durchgehend erfolgreich** | erfolgreich |
| Bestehende TCP-Verbindung (Quellport 63411) | **durchgehend aktiv**, mehrere hundert Pakete pro Minute | aktiv |

### 6.2 Messsonde auf dem PC, Störung 12:15 Uhr

| Zeit | TCP 443 (1,25 s) | TCP 443 (10 s) | TCP 18080 | ICMP | DNS |
|---|---|---|---|---|---|
| 12:15:27 | fail | fail | fail | **ok** | 136.230.111.59 |
| 12:15:50 | fail | fail | fail | **ok** | 136.230.111.59 |
| 12:16:11 | fail | fail | fail | **ok** | 136.230.111.59 |
| 12:16:32 | fail | **ok** | ok | ok | 136.230.111.59 |

Die letzte Zeile ist der Moment der Erholung. Wichtig: Ein auf 10 Sekunden verlängerter Verbindungstimeout hilft während der Störung **nicht**. Es handelt sich also nicht um ein Laufzeit- oder Timing-Problem, sondern um vollständigen Paketverlust in eine Richtung.

### 6.3 Wiederanlauf um 12:55:00 Uhr (Auszug `tcpdump` auf der Appliance)

```
12:55:00.128626 IP 10.130.23.35.57446 > 136.230.111.59.443: Flags [S]
12:55:00.128801 IP 136.230.111.59.443 > 10.130.23.35.57446: Flags [S.]
12:56:00.131373 IP 10.130.23.35.57654 > 136.230.111.59.443: Flags [S]
12:56:00.131565 IP 136.230.111.59.443 > 10.130.23.35.57654: Flags [S.]
12:57:00.113783 IP 10.130.23.35.57872 > 136.230.111.59.443: Flags [S]
12:57:00.114039 IP 136.230.111.59.443 > 10.130.23.35.57872: Flags [S.]
```

Die Appliance antwortet auf jedes ankommende SYN innerhalb von rund 0,2 Millisekunden. Vor 12:55:00 Uhr enthält der Mitschnitt über acht zusammenhängende Minuten mit mehreren hundert Paketen pro Minute **kein einziges SYN-Paket**, obwohl der PC in diesem Zeitraum nachweislich 41 SYN-Pakete gesendet hat.

---

## 7. Ausgeschlossene Ursachen

| Mögliche Ursache | Messung | Ergebnis |
|---|---|---|
| Ausfall oder Überlastung der Appliance | Lokaler HTTPS-Aufruf der Appliance gegen sich selbst, alle 10 s | durchgehend erfolgreich |
| Dienstausfall auf der Appliance | Container-Laufzeiten, Cloud-Anbindung, Registrierungen | seit Tagen unverändert, Anbindung durchgehend verbunden |
| Verbindungstabelle der Appliance voll | `nf_conntrack` | 80 von 65536 belegt, keine Kernel-Verwürfe |
| Warteschlange des Webservers voll | Listen-Queue des Reverse Proxy | leer (0 von 4096) |
| DNS | Auflösung während der Störung | durchgehend korrekt, 136.230.111.59 |
| Routing oder Leitungsausfall | ICMP während der Störung | durchgehend erfolgreich |
| Firewall oder Sicherheitssoftware auf dem PC | `pktmon` über alle Komponenten | **0 Verwürfe**, SYN erreicht die Netzwerkkarte |
| Unternehmens-Proxy | Registry-Einstellungen, Docker-Konfiguration | kein System-Proxy aktiv, Ausnahmen gesetzt |
| Zertifikat oder TLS | Fehlerzeitpunkt | Fehler tritt **vor** dem TLS-Handshake auf, reine TCP-Ebene |
| Zu kurzer Verbindungstimeout der Anwendung | Vergleichsmessung mit 10 Sekunden | ebenfalls erfolglos |

---

## 8. Schlussfolgerung

Alle Messungen zusammen ergeben ein widerspruchsfreies Bild:

1. Die SYN-Pakete werden vom PC erzeugt, durchlaufen den gesamten Netzwerkstapel ohne Verwurf und werden an die physische Netzwerkkarte übergeben.
2. Auf der Appliance kommen sie nicht an.
3. ICMP zwischen denselben beiden Hosts funktioniert zeitgleich.
4. Pakete bestehender TCP-Sessions zwischen denselben beiden Hosts passieren zeitgleich denselben Pfad.

Eine Komponente im Netzwerkpfad verwirft demnach gezielt **neue TCP-Verbindungsanfragen** dieser Quelle zu diesem Ziel, **still** (weder TCP-RST noch ICMP-Unreachable), während sie ICMP sowie Pakete bekannter Sessions weiterleitet. Das ist das charakteristische Verhalten einer **zustandsbehafteten Firewall oder eines IPS**, das eine Quelle temporär für neue Verbindungen sperrt.

---

## 9. Hinweise für die Suche

### 9.1 Auffälligkeit im Paketmitschnitt

Jedes SYN-Paket des PCs trägt:

- eine **nicht standardisierte TCP-Option, Kind 112**, mit dem Inhalt `0x040203030001`
- eine **MSS von 1328 Byte** (entspricht einer Pfad-MTU von 1368 Byte statt der üblichen 1500)

Beides deutet darauf hin, dass sich bereits **eine paketverändernde Komponente im Pfad** befindet, etwa ein Overlay-, SD-WAN- oder Tunnelmechanismus. Diese Komponente ist der erste Kandidat für die Untersuchung. Wir bitten um Auskunft, welches Produkt diese TCP-Option setzt.

### 9.2 Möglicher Auslöser auf unserer Seite

Der Geräteagent baut bei einer Störung etwa **27 neue Verbindungen pro Minute** auf, jeweils von einem neuen Quellport, und bricht sie nach 1,25 Sekunden ergebnislos ab. Dieses Muster kann von einem IPS als Portscan oder SYN-Flood klassifiziert werden.

Sollte eine solche Schwellwertregel greifen, entstünde eine sich selbst verstärkende Schleife: Eine kurze Störung löst das Nachfassen des Agenten aus, das Nachfassen löst eine Sperre aus, und die Sperre verlängert die Störung auf Minuten.

**Wir passen das Rückfallverhalten des Agenten unabhängig davon an** (exponentiell wachsende Wartezeit statt fester Sekunde). Für die Ursachenklärung ist jedoch wichtig zu wissen, ob eine solche Regel existiert und mit welcher Sperrdauer sie arbeitet.

### 9.3 Konkrete Prüfpunkte

1. **Firewall- und IPS-Protokolle** für Quelle 10.130.23.35 zu Ziel 136.230.111.59, Ports 443 und 18080, zu den unter Abschnitt 4 genannten Zeitstempeln.
2. **Schwellwerte pro Quelladresse**: Verbindungsrate, gleichzeitige Sessions, Flood- oder Scan-Erkennung, Reputationsmechanismen. Insbesondere: **wie lange sperrt die jeweilige Regel?** Die beobachteten Störungsdauern liegen zwischen 1 Minute und 4 Stunden.
3. **Session- und NAT-Tabellen** der beteiligten Komponenten: Erschöpfung, Grenzwerte pro Quelle, Aufräumintervalle.
4. **Redundanz und asymmetrisches Routing**: Falls mehrere Firewalls im aktiv-aktiv-Betrieb laufen, könnte eine bestehende Session auf Knoten A bekannt sein, während neue SYN-Pakete auf Knoten B treffen. Das würde exakt erklären, warum bestehende Verbindungen weiterlaufen und neue scheitern.
5. **IPS-Signaturen** auf dauerhafte WebSocket-Verbindungen über Port 443, die zu einer temporären Sperre der Quelle führen können.
6. **NAC bzw. 802.1X**: Reauthentifizierung des Switchports des PCs zu den genannten Zeitpunkten.
7. **Die unter 9.1 genannte paketverändernde Komponente**: Verhalten bei Sessionaufbau, eigene Zustandstabellen, Grenzwerte.

---

## 10. Verfügbare Artefakte

Die folgenden Rohdaten können auf Anfrage bereitgestellt werden:

| Artefakt | Ort | Inhalt |
|---|---|---|
| Paketmitschnitt Appliance | `/var/log/pcap/lac443.pcap00` | Alle Pakete zwischen beiden Hosts auf Port 443 |
| Paketmitschnitt PC | `C:\netdiag\pktmon.etl` und `live.txt` | Alle Pakete über alle Netzwerkkomponenten inklusive Verwurfsgründen |
| Messsonde PC | `C:\netdiag\probe.csv` | Verbindungstests im 5-Sekunden-Takt |
| Agentenprotokoll PC | `C:\ProgramData\IronFlock\Reagent\reagent.log` | Alle Verbindungsversuche mit Zeitstempel |
| Prüfprotokoll Appliance | `/var/log/lac-diag.log` | Eigenprüfung der Appliance im 10-Sekunden-Takt |

Die Messungen laufen weiter, sodass weitere Störungsfenster automatisch erfasst werden.

---

## 11. Was wir von der TRUMPF IT benötigen

1. Auskunft, **welche Netzwerkkomponenten** im Pfad zwischen dem VLAN des Edge-PCs und dem Segment der Appliance liegen.
2. Prüfung der **Firewall- und IPS-Protokolle** zu den genannten Zeitstempeln für die genannte Quell-Ziel-Beziehung.
3. Auskunft, ob eine **Regel zur Begrenzung der Verbindungsrate pro Quelladresse** existiert, und falls ja, mit welchem Schwellwert und welcher Sperrdauer.
4. Auskunft, welches Produkt die unter 9.1 beschriebene **TCP-Option 112** setzt und die Pfad-MTU auf 1368 Byte reduziert.

Für Rückfragen und für eine gemeinsame Messung während einer laufenden Störung stehen wir zur Verfügung.
