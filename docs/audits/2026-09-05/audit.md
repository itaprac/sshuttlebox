# Audyt sshuttlebox

Data: 2026-09-05. Commit: `ede63430db1bf7e6b233c8bd96474b0d195ad5af`.
Repozytorium: `/Users/itaprac/Documents/dev/sshuttlebox`.
Ścieżka zadania `/Users/itaprac/Documents/dev/active/sshuttlebox` nie istnieje.

Audyt obejmuje CLI, TUI, zapis konfiguracji, stan tuneli, uruchamianie SSH/SFTP, import i eksport, testy, CI oraz statyczny przegląd strony dokumentacji. Nie zmieniono kodu aplikacji. Dodano ten raport i kod prób odtwarzających błędy.

Najpierw należy naprawić obsługę haseł, ochronę konfiguracji i cykl życia tuneli. Główny problem architektury to wykonywanie operacji na plikach i procesach bezpośrednio przez interfejs oraz brak wspólnej warstwy operacji dla CLI i TUI. Testy przechodzą, ale nie obejmują istotnych błędów w tych miejscach.

P1 oznacza poprawkę pilną, przed kolejnym wydaniem. P2 oznacza poprawkę w następnym cyklu prac. Priorytety uwzględniają skutki i warunki wystąpienia.

## Weryfikacja

- `go test ./...`: wynik pozytywny.
- `go vet ./...`: wynik pozytywny.
- `go test -race -cover ./...`: wynik pozytywny, bez zgłoszeń detektora wyścigów.
- Pokrycie instrukcji: `internal/cli` 66,1%, `internal/config` 70,0%, `internal/tunnelstate` 68,5%. Pakiet `cmd/shbx` nie ma testów.
- Środowisko wykonania: macOS arm64, Go 1.27.0. CI deklaruje Go 1.22 oraz macOS i Linux. Lokalnie nie zweryfikowano Go 1.22 ani Linuxa.
- 12 dodatkowych testów odtworzyło poniższe scenariusze w kopii `/tmp/sshuttlebox-audit.4jc0yM`. Próby używały tymczasowego katalogu domowego, fikcyjnych hostów i atrap SSH. Próba SFTP użyła systemowego `/usr/bin/sftp` z atrapą transportu, bez połączenia sieciowego.
- Test hasła obejmuje prawdziwy lokalny PTY i sztuczny tekst aplikacji. Potwierdza działanie mechanizmu wstrzykiwania, nie incydent ujawnienia hasła użytkownika.
- Testy TUI obejmują model, klawisze i renderowanie tekstu. Nie przeprowadzono sesji z rzeczywistym zdalnym serwerem ani pełnej oceny wizualnej terminala.
- Detektor wyścigów Go nie wykrywa utraty aktualizacji pomiędzy oddzielnymi procesami zapisującymi JSON. Nie wykonano skanowania podatności zależności.

Kod prób: [audit_repro_test.go.txt](/Users/itaprac/Documents/dev/sshuttlebox/docs/audits/2026-09-05/audit_repro_test.go.txt).
Próby celowo potwierdzają obecność błędów. Po poprawkach trzeba odwrócić odpowiednie asercje, zanim staną się testami regresji. Aby powtórzyć próby, skopiuj plik do `internal/cli/audit_repro_test.go` w osobnej kopii tego commitu i wykonaj `go test ./internal/cli -run TestAudit -v -count=1 -timeout=45s`.

## Potwierdzone problemy

### 1. P1: zapisane hasło może trafić do aplikacji po zalogowaniu

Kod: [cli.go:3021](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:3021).

`copySSHOutputAndInjectPassword` obserwuje cały strumień PTY i wysyła hasło przy pierwszym tekście zakończonym `password:`. Nie wie, czy trwa uwierzytelnianie SSH. Jeśli klucz lub agent zaloguje użytkownika bez użycia zapisanego hasła, późniejszy prompt aplikacji, a nawet odpowiednio zakończony komunikat, uruchomi wysłanie sekretu do jej wejścia. Może on trafić do logów lub historii poleceń.

Próba `TestAuditPasswordInjectedIntoApplicationPrompt` podała tekst `Authenticated with public key.\r\nApplication password:` i odebrała fikcyjne zapisane hasło po drugiej stronie PTY.

Poprawka: oddzielić pobieranie hasła od strumienia sesji. Rozważyć kontrolowany helper `SSH_ASKPASS`, który obsługuje właściwy rodzaj żądania i nie przekazuje sekretu w argumentach procesu. Samo zawężenie wyrażenia rozpoznającego prompt nie usuwa problemu. OpenSSH udostępnia wymuszenie helpera przez `SSH_ASKPASS_REQUIRE=force`: [dokumentacja ssh](https://man.openbsd.org/ssh#SSH_ASKPASS_REQUIRE).

### 2. P1: TUI nadpisuje zmiany wykonane z innego terminala

Kod: [tui.go:242](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:242), [tui.go:2130](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:2130), [config.go:201](/Users/itaprac/Documents/dev/sshuttlebox/internal/config/config.go:201).

Model ładuje konfigurację przy otwarciu, a później zapisuje cały swój egzemplarz. Otwarcie TUI, dodanie hosta przez CLI w drugim terminalu i zapisanie nawet niezmienionego formularza usuwa nowego hosta. Próba `TestAuditStaleTUIOverwritesCLIChanges` potwierdza utratę wpisu.

Poprawka: wspólna operacja odczyt-zmiana-zapis pod blokadą między procesami. Formularz powinien przekazywać zmianę konkretnego wpisu oraz jego oczekiwaną wersję. Konflikt edycji tego samego wpisu należy pokazać użytkownikowi. Atomowa podmiana pliku chroni przed częściowym zapisem, ale sama nie rozwiązuje utraty aktualizacji.

### 3. P1: po błędzie odczytu TUI pozwala nadpisać uszkodzoną konfigurację

Kod: [tui.go:247](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:247), [tui.go:267](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:267), [tui.go:1929](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:1929).

Po błędzie parsowania aplikacja tworzy puste mapy i pozostawia aktywne formularze. Otwarcie formularza usuwa komunikat błędu. Zapis zastępuje uszkodzony plik nową konfiguracją, bez zachowania oryginału. Próba `TestAuditBrokenConfigCanBeOverwrittenByTUI` uzyskała nowy JSON z jednym hostem i `version: 0`.

Poprawka: osobny stan błędu ładowania, blokujący mutacje. Pokazać ścieżkę, przyczynę, ponowienie odczytu i odzyskiwanie z kopii. Inicjalizacja pustych danych powinna dotyczyć wyłącznie nieistniejącej konfiguracji.

### 4. P1: litera q zamyka formularze

Kod: [tui.go:180](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:180), [tui.go:503](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:503), [tui.go:552](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:552).

Formularze sprawdzają globalne `Quit`, zawierające `q`, zanim przekażą znak do pola tekstowego. Nie można wpisać `q` w nazwie, adresie, ścieżce ani haśle. Aplikacja kończy pracę i traci niezapisane dane. `TestAuditTypingQQuitsForms` potwierdza to dla formularza hosta i tunelu.

Poprawka: w polach tekstowych zwykłe znaki zawsze przekazywać do edytora. `q` powinno zamykać wyłącznie ekrany nawigacji. Formularz może używać `Esc` do anulowania i `Ctrl+C` do wyjścia.

### 5. P1: tunnel remove --yes pozostawia działający tunel bez wpisu konfiguracji

Kod: [cli.go:1007](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:1007), [cli.go:1029](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:1029).

`--yes` omija blokadę usuwania działającego tunelu, ale zatrzymanie jest wykonywane tylko dla `--force`. Po usunięciu nazwy dalsze `tunnel stop db` zgłasza `tunnel "db" not found`. Proces i przekierowanie mogą pozostać aktywne. `TestAuditRemoveYesLeavesRunningTunnel` potwierdza zachowanie konfiguracji i stanu na atrapie aktywnego SSH.

Poprawka: `--yes` powinno pomijać tylko pytanie. Działający tunel należy zablokować albo zatrzymać przez tę samą operację co `--force`, a wpis usunąć dopiero po potwierdzeniu zatrzymania.

### 6. P1: zmiana nazwy działającego tunelu odrywa konfigurację od procesu

Kod: [tui.go:2196](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:2196), [tunnelstate.go:173](/Users/itaprac/Documents/dev/sshuttlebox/internal/tunnelstate/tunnelstate.go:173).

Zmiana nazwy modyfikuje tylko mapę konfiguracji. Stan procesu i ścieżka gniazda pozostają pod poprzednią nazwą. Nowa nazwa wygląda na zatrzymaną, a stara nie jest już dostępna w normalnym CLI. `TestAuditRenameRunningTunnelLosesState` potwierdza rozbieżność. Edycja portów pod tą samą nazwą także nie przeładowuje istniejącego przekierowania, choć szczegóły prezentują nową konfigurację.

Poprawka na teraz: wymagać zatrzymania przed zmianą nazwy i parametrów aktywnego tunelu. Docelowo nadać tunelowi stałe ID, niezależne od nazwy wyświetlanej, i przechowywać parametry faktycznie uruchomionej instancji.

### 7. P2: renderowanie TUI uruchamia SSH dla wszystkich wierszy

Kod: [tui.go:1159](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:1159), [tui.go:2375](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:2375), [tunnelstate.go:208](/Users/itaprac/Documents/dev/sshuttlebox/internal/tunnelstate/tunnelstate.go:208).

Każdy wiersz wywołuje `Get`, ponownie czyta i parsuje cały plik stanu, a dla zapisanego gniazda uruchamia synchroniczne `ssh -O check`. Przycięcie listy następuje po tym wszystkim. Przy liczbie wpisów stanu zbliżonej do liczby tuneli powoduje to kwadratowy wzrost ilości parsowanych danych, plus liniową liczbę procesów SSH. Panel szczegółów dodaje kolejne sprawdzenie.

`TestAuditRenderProbesInvisibleRows`: 40 procesów dla listy ograniczonej do 8 linii. Ostatni przebieg trwał 993 ms z celowym opóźnieniem 10 ms w każdej atrapie SSH. To pomiar lokalnego scenariusza syntetycznego, nie opóźnienia rzeczywistego SSH.

Dodatkowo `Get` usuwa wpisy uznane za nieaktywne, więc rysowanie ekranu może zapisywać plik. Błędy sprawdzania są przedstawiane jako `stopped`. Nie ma okresowego odświeżania statusu bez zdarzeń użytkownika. Start tunelu z hasłem wykonuje się synchronicznie w `Update`, a brak limitu czasu może zablokować obsługę klawiszy.

Poprawka: `View` ma wyłącznie formatować stan modelu. Status odczytywać w `tea.Cmd`, z limitem czasu, ograniczoną współbieżnością i wynikiem przekazanym przez komunikat. Używać jednej migawki stanu, odświeżać ją po operacji i okresowo. Rozróżnić `starting`, `running`, `stopping`, `stopped` i `unknown/error`. Wiersze spoza widoku pominąć przed renderowaniem.

### 8. P2: zapis hasła nie naprawia zbyt szerokich praw istniejącego pliku

Kod: [config.go:212](/Users/itaprac/Documents/dev/sshuttlebox/internal/config/config.go:212).

`os.WriteFile(..., 0600)` nadaje prawa nowemu plikowi, lecz pozostawia prawa istniejącego. Jeśli konfigurację skopiowano lub zapisano edytorem jako `0644`, dodanie hasła zachowuje `0644`. Próba `TestAuditSaveDoesNotSecureExistingFile` to potwierdza. Dostęp innych użytkowników zależy również od praw katalogów nadrzędnych. `doctor --fix` może naprawić plik, ale zapis sekretu nie powinien tego wymagać.

Poprawka: tworzyć prywatny plik tymczasowy z `0600`, zapisać dane i atomowo zastąpić konfigurację. Tę samą politykę zastosować do kopii i eksportu z hasłami. Systemowy magazyn poświadczeń można dodać później jako osobną zmianę.

### 9. P2: SFTP ls/get/put nie używa zapisanego hasła przy standardowym batch mode

Kod: [cli.go:2494](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:2494), [cli.go:2969](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:2969).

Kod dodaje `sftp -b`, a następnie czeka w PTY na pytanie o hasło. Systemowe SFTP przekazało transportowi `-obatchmode yes`. Ten tryb wyłącza pytania o hasło, więc samo PTY nie umożliwia uwierzytelniania zapisaną wartością. Operacja może nadal działać, jeśli wystarczy klucz lub agent.

`TestAuditSFTPPasswordBatchDisablesPasswordAuth` przechwycił argumenty prawdziwego `/usr/bin/sftp` przy użyciu lokalnej atrapy transportu. Zasady potwierdzają [dokumentacja SFTP](https://man.openbsd.org/sftp#b) oraz [BatchMode](https://man.openbsd.org/ssh_config#BatchMode).

Poprawka: jawnie skonfigurować uwierzytelnianie dla operacji batch. Rozdzielić je od wejścia poleceń SFTP i użyć bezpiecznego mechanizmu pobrania hasła. Dodać integrację z lokalnym testowym serwerem dopuszczającym wyłącznie hasło, z osobnymi przypadkami błędnego hasła i nieznanego klucza hosta.

### 10. P2: import uszkadza cytowane ścieżki, także z własnego eksportu

Kod: [ssh_config_import.go:198](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/ssh_config_import.go:198).

`strings.Fields` rozcina tekst także wewnątrz cudzysłowów. Eksport `IdentityFile "/tmp/my keys/id_ed25519"` i ponowny import dają `/tmp/my`. Potwierdza to `TestAuditSSHConfigRoundTripLosesQuotedPath`.

Parser rozpoznaje tylko część składni. Z analizy kodu wynika również brak obsługi `Include`, `Host *`, zakresów `Match`, `Key=Value` i pełnych reguł pierwszeństwa. Pominięcie tych elementów nie generuje ostrzeżenia. Przepisanie aliasu na `HostName` może też ominąć opcje przypisane do pierwotnego aliasu w OpenSSH, np. `ProxyJump`.

Poprawka: jawnie ustalić zakres importu i informować o pominiętych dyrektywach. Dla pełniejszej zgodności zachować alias OpenSSH albo użyć resolvera zgodnego z OpenSSH. Rozwiązanie musi uwzględnić semantykę konfiguracji, a nie tylko dzielenie wierszy. Referencja: [ssh_config](https://man.openbsd.org/ssh_config).

### 11. P2: krótki terminal ukrywa aktywne pole formularza i błąd

Kod: [tui.go:842](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:842), [tui.go:1302](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:1302).

Formularze renderują wszystkie pola, po czym `View` przycina wynik od dołu. Nie ma przewijania za fokusem ani miejsca zarezerwowanego na błąd. W terminalu 80x15 można edytować niewidoczne pole Group i nie zobaczyć komunikatu walidacji. `TestAuditShortFormHidesFocusedInputAndErrors` potwierdza oba objawy. Istniejące testy wysokości obejmują głównie ekran listy.

Poprawka: przewijany formularz utrzymujący aktywne pole w widoku oraz stały obszar komunikatów. Nazwy pól tunelu powinny zależeć od typu, np. local target dla tunelu remote. Dla dynamic należy schować pola nieużywane i zastąpić swobodny tekst Type wyborem.

### 12. P2: doctor raportuje FAIL, ale proces kończy się sukcesem

Kod: [doctor.go:47](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/doctor.go:47), [cmd/shbx/main.go:11](/Users/itaprac/Documents/dev/sshuttlebox/cmd/shbx/main.go:11).

`runDoctor` drukuje raport i bezwarunkowo zwraca `nil`. Skrypt używający kodu zakończenia uzna kontrolę za poprawną nawet po błędzie parsowania konfiguracji. `TestAuditDoctorReportsFailureWithSuccessReturn` potwierdza `FAIL` wraz z brakiem błędu zwrotnego.

Poprawka: zachować ustrukturyzowaną listę wyników do końca obsługi komendy. Zwracać niezerowy kod przy poziomie FAIL i rozważyć `doctor --json` do automatyzacji.

## Dalsze uwagi z analizy kodu

Te punkty nie mają osobnych prób odtwarzających w dołączonym pliku.

- [config.go:212](/Users/itaprac/Documents/dev/sshuttlebox/internal/config/config.go:212) oraz [tunnelstate.go:86](/Users/itaprac/Documents/dev/sshuttlebox/internal/tunnelstate/tunnelstate.go:86) zapisują bezpośrednio w pliku docelowym. Przerwanie zapisu może zostawić niepełny JSON. Potrzebne są atomowy zapis i blokada całej aktualizacji, również dla stanu tuneli.
- [cli.go:2448](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:2448) ignoruje błąd `ssh -O exit`, a [tunnelstate.go:126](/Users/itaprac/Documents/dev/sshuttlebox/internal/tunnelstate/tunnelstate.go:126) usuwa wpis przed wysłaniem SIGTERM i ignoruje błąd sygnału. Nie ma potwierdzenia zakończenia procesu. Operacja może ogłosić sukces mimo nieudanego zatrzymania. Historyczny wariant bez ControlPath polega wyłącznie na PID i nie sprawdza tożsamości procesu.
- [tui.go:2130](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:2130) oraz [tui.go:697](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:697) zmieniają mapy modelu przed zapisem. Po błędzie zapisu stan interfejsu może różnić się od dysku. Model powinien przyjąć nową konfigurację dopiero po udanej operacji.
- [config.go:132](/Users/itaprac/Documents/dev/sshuttlebox/internal/config/config.go:132) sprawdza przy restore jedynie JSON i dodatnią wersję. Nie sprawdza obsługiwanej wersji, portów, pustych hostów i referencji tuneli. `Load` nie sprawdza nawet dodatniej wersji. Jedna walidacja domenowa powinna obowiązywać przy wczytaniu, imporcie, przywróceniu i edycji.
- [tui.go:223](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/tui.go:223) kończy TUI po SSH/SFTP. Powrót z sesji do tej samej listy, kursora i filtra byłby bardziej użyteczny. Uruchamianie tunelu ma już częściowo taki powrót.
- [cli.go:2569](/Users/itaprac/Documents/dev/sshuttlebox/internal/cli/cli.go:2569) skleja cel SFTP bez nawiasów dla IPv6. Składnia SFTP wymaga nawiasów, gdy adres zawiera dwukropki: [dokumentacja](https://man.openbsd.org/sftp). Warto dodać przypadki IPv6 dla SSH, SFTP i obu stron przekierowania.
- [docs/index.html:1235](/Users/itaprac/Documents/dev/sshuttlebox/docs/index.html:1235) wywołuje `done` także przy odrzuconym zapisie do schowka przez `.then(done, done)`. Strona pokaże Copied mimo błędu. Animowane przewijanie nie sprawdza preferencji ograniczenia ruchu. To mniejsze poprawki strony dokumentacji.

## Kierunek architektury

`cli.go` ma 3384 linie, a `tui.go` 2658. Sama długość nie jest błędem, ale oba pliki łączą obowiązki, które już prowadzą do różnych zachowań CLI i TUI.

| Część | Odpowiedzialność po zmianie |
| --- | --- |
| `internal/domain` | Host, Tunnel, Group, walidacja i stałe ID. Bez plików, procesów i UI. |
| `internal/store` | Odczyt, atomowy zapis, blokada między procesami, wersja i migracja danych. Oddzielne repozytoria konfiguracji i stanu wykonania. |
| `internal/ssh` | Argumenty SSH/SFTP, procesy, limity czasu, kanał uwierzytelniania i sterowanie gniazdem. |
| `internal/service` | Wspólne operacje dla CLI i TUI, np. zmiana hosta, zmiana tunelu, start, stop i usunięcie. |
| `internal/cli` i `internal/tui` | Parsowanie wejścia, wywołanie operacji i prezentacja wyniku. |

To propozycja granic odpowiedzialności. Nie trzeba tworzyć wszystkich pakietów jednocześnie. Najpierw warto wydzielić operacje zapisu i zarządzania tunelami, bo rozwiązują potwierdzone błędy. JSON jest wystarczający dla lokalnego menedżera; audyt nie wykazał potrzeby dodania bazy danych ani demona.

`State.Entry.Command` jest obecnie jednocześnie tekstem do prezentacji i źródłem parsowanego celu SSH. Stan powinien przechowywać cel, ścieżkę gniazda, PID i tożsamość instancji jako osobne dane. Formatowanie polecenia nie powinno decydować o tym, czy można zarządzać procesem.

`App` wstrzykuje tylko część wejścia i wyjścia. Funkcje wykonawcze używają globalnych `os.Stdin`, `os.Stdout`, `os.Stderr` i zmiennych środowiskowych. Małe interfejsy repozytorium i runnera uproszczą testowanie błędów oraz pozwolą TUI wykonywać operacje poza pętlą zdarzeń.

## Kolejność prac i kryteria odbioru

1. Poprawić `q`, blokadę edycji po błędzie odczytu oraz kanał hasła. Próby muszą potwierdzać zachowanie danych i brak sekretu w strumieniu aplikacji.
2. Wprowadzić bezpieczny zapis i konflikty edycji. Dodać test dwóch procesów aktualizujących różne wpisy oraz test błędu zapisu. Sam `-race` tego nie zastępuje.
3. Ujednolicić start, stop, usunięcie i zmianę tunelu. Potwierdzać zakończenie procesu przed usunięciem stanu. Przetestować `--yes`, `--force`, zmianę nazwy i nieudane `exit`.
4. Usunąć I/O z `View` i operacje blokujące z `Update`. Test renderowania powinien wykonywać zero odczytów stanu i zero procesów SSH. Pomiar dla 10, 100 i 1000 wpisów powinien mierzyć osobno render i odświeżanie statusu.
5. Poprawić SFTP batch, import, formularze i kody wyjścia. Testować operacje z lokalnym OpenSSH, różne metody uwierzytelniania, cytowane ścieżki oraz małe terminale.
6. Uzupełnić CI o `go vet`, detektor wyścigów i powyższe testy regresji. Dodać okresowy skan `govulncheck`. Utrzymać test minimalnej deklarowanej wersji Go i obu systemów.
