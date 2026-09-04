# Poprawki po audycie sshuttlebox

Data: 2026-09-05. Zmiany lokalne względem `ede63430db1bf7e6b233c8bd96474b0d195ad5af`.
Repozytorium: `/Users/itaprac/Documents/dev/sshuttlebox`.

Wprowadzono poprawki do wszystkich 12 potwierdzonych problemów z [audytu](/Users/itaprac/Documents/dev/sshuttlebox/docs/audits/2026-09-05/audit.md), a także do opisanych tam błędów zapisu, zatrzymywania procesów, IPv6, powrotu do TUI i strony dokumentacji. Kod prób z pierwszego audytu pozostaje historycznym zapisem zachowania przed poprawkami.

## Zakres zmian

| Problem z audytu | Poprawka i weryfikacja |
| --- | --- |
| 1. Hasło trafia do sesji | Usunięto analizowanie wyjścia PTY. OpenSSH uruchamia prywatny helper `SSH_ASKPASS`, który pobiera hasło przez gniazdo Unix z katalogu `0700`. Hasło istnieje w pamięci; nie trafia do argumentów, zmiennych środowiska ani pliku helpera. Testy obejmują tekst aplikacji, pojedyncze użycie hasła, potwierdzenie klucza i OTP. |
| 2. Utrata zmian z innego terminala | Oddzielna blokada pliku oraz scalanie wpisów względem migawki z odczytu. Niezależne zmiany pozostają, konflikt tego samego wpisu blokuje zapis. Test uruchamia cztery równoległe procesy. Dodatkowa kontrola chroni zmianę hosta, któremu inny proces dodał nowy zależny tunel. |
| 3. Nadpisanie uszkodzonej konfiguracji | Trwały stan błędu odczytu blokuje operacje TUI. `ctrl+r` ponawia odczyt. Otwarcie formularza nie usuwa blokady. Walidacja obejmuje wersję, hosty, porty, tunele i referencje. |
| 4. Litera q zamyka formularz | Zwykłe znaki trafiają do pól. Testy obejmują wszystkie pola obu formularzy; dodatkowy test na prawdziwym PTY zapisał hosta o nazwie `q`. |
| 5. Usunięcie aktywnego wpisu bez stop | `--yes` nie omija blokady aktywnego tunelu. `--force` zatrzymuje go i potwierdza zakończenie. Blokada obejmuje stop i zapis konfiguracji. Stop działa również dla nazwy nieobecnej w konfiguracji. |
| 6. Zmiana aktywnego tunelu | Zmiana nazwy, parametrów lub używanego hosta wymaga zatrzymania tunelu. Blokady chronią obie nazwy przy zmianie. Dodanie tunelu nie może przejąć nazwy aktywnego procesu pozostawionego bez wpisu konfiguracji. |
| 7. SSH w renderowaniu TUI | `View` korzysta z zapamiętanych statusów i renderuje tylko widoczne wiersze. Sprawdzenia i mutacje działają w `tea.Cmd`, z limitami czasu. Statusy odświeżają się okresowo; stare wyniki są odrzucane. Maksymalnie cztery sprawdzenia SSH działają jednocześnie. Błędy mają status `unknown`. CLI list i doctor również korzystają z jednej migawki i nie usuwają stanu przy odczycie. |
| 8. Zbyt szerokie prawa konfiguracji | Atomowa podmiana prywatnego pliku `0600`. Kopie, eksport i przywracanie korzystają z tej samej polityki. Testy błędów zapisu potwierdzają zachowanie starej zawartości. |
| 9. Hasło w SFTP batch | Jawne `BatchMode=no` ma pierwszeństwo przed ustawieniem pochodzącym z `-b`. Kanał uwierzytelniania jest osobny od wejścia poleceń SFTP. Rzeczywisty OpenSSH przeszedł sześć testów z lokalnym serwerem. |
| 10. Import OpenSSH | Tokenizer zachowuje cytowane ścieżki i `key=value`. Import zapisuje alias oraz ścieżkę oryginalnego pliku, więc OpenSSH sam stosuje Include, Match, ProxyJump i pierwszeństwo ustawień. Eksport zachowuje nadpisania użytkownika i izoluje różne źródła warunkowymi Include. Nie pozwala nadpisać podlinkowanego pliku źródłowego, także przez symlink lub hardlink. Testy sprawdzają wynik systemowego `ssh -G`. |
| 11. Niewidoczne pola i błędy | Formularze przewijają się za aktywnym polem i rezerwują obszar na komunikat. Tryb dynamic ukrywa nieużywane pola; remote pokazuje właściwe nazwy celu. Type ma wybór klawiszami. Testy obejmują 80x15. |
| 12. Doctor zwraca sukces mimo FAIL | FAIL daje błąd i kod wyjścia 1. Dodano `doctor --json`. Nieudane sprawdzenie procesu nie jest przedstawiane jako martwy wpis. |

## Pozostałe poprawki

- Wydzielono zapis do `internal/store`, argumenty do `ssh_args.go`, uwierzytelnianie do `ssh_auth.go`, wspólne operacje tuneli do `tunnel_service.go`, a asynchroniczne operacje i układ TUI do oddzielnych plików. CLI i TUI używają wspólnych blokad i operacji procesu.
- Stan procesu zawiera osobny cel SSH. Istniejące wpisy z tekstowym Command zachowują zgodność odczytu. Sterowanie gniazdem nie odczytuje konfiguracji użytkownika, która mogłaby zmienić wynik kontroli.
- Zapis modelu TUI następuje po udanym zapisie danych. Błąd lub konflikt zachowuje formularz i poprzedni model.
- Start sprawdza aktualność parametrów po uzyskaniu blokady. Równoczesne starty nie tworzą dwóch masterów. Niepowodzenie sprzątania po błędzie zapisu stanu jest jawnie raportowane wraz z poleceniem odzyskania kontroli.
- Stop nie wysyła sygnału do niezidentyfikowanego procesu ze starego wpisu zawierającego tylko PID. Zgłasza potrzebę ręcznego zakończenia i późniejszego prune, zamiast ryzykować zabicie innego procesu.
- Powrót z sesji SSH/SFTP zachowuje wybór, filtr i ustawienia widoku. Wyjście podczas mutacji czeka na zakończenie ograniczonej czasowo operacji.
- Poprawiono IPv6 w SFTP i przekierowaniach. Bufory diagnostyczne przechowują najwyżej ostatnie 64 KiB.
- Strona dokumentacji nie pokazuje Copied po błędzie schowka i respektuje ograniczenie ruchu. Sprawdzono obsługę sukcesu, błędu, fallbacku i ograniczenia animacji w Node VM.
- CI obejmuje Go 1.22 i stable, macOS i Linux, `vet`, `-race`, skan podatności oraz rzeczywiste operacje klienta OpenSSH przeciw lokalnemu serwerowi testowemu.

## Wyniki weryfikacji

- Dodano 55 funkcji testowych w nowych plikach Go. Część zawiera kilka przypadków. Zaktualizowano istniejące testy pod nowe, bezpieczne zasady zapisu i sterowania.
- `go test -race -cover ./...`: wynik pozytywny na macOS arm64, Go 1.27.0.
- `go vet ./...`: wynik pozytywny.
- `git diff --check`: wynik pozytywny.
- `GOTOOLCHAIN=go1.22.12 go test -race -ldflags=-linkmode=external ./...`: wynik pozytywny. Na bieżącym macOS wewnętrzny linker Go 1.22 tworzy plik odrzucany przez loader z powodu braku LC_UUID. Wariant CI dla Go 1.22 na macOS stosuje zewnętrzny linker. Zewnętrzny linker emituje ostrzeżenia LC_DYSYMTAB, ale testy kończą się sukcesem.
- `GOOS=linux GOARCH=amd64 go build ... ./cmd/shbx`: wynik pozytywny. Testy Go 1.22 i stable na macOS i Linuxie, skan podatności oraz integracja SSH/SFTP przeszły również w [CI dla commita 85c821a](https://github.com/itaprac/sshuttlebox/actions/runs/33927874754).
- `govulncheck`: zero podatności osiągalnych przez kod i zero w importowanych pakietach. Skan wykazał jedną podatność wyłącznie w nieużywanym module dla Windows, GO-2026-5024 w `golang.org/x/sys/windows`. Projekt wspiera macOS i Linux. Nie podniesiono minimalnej wersji Go tylko z powodu nieużywanej ścieżki Windows.
- Rzeczywisty OpenSSH i lokalny Paramiko 5.0.0: `ls`, `get`, `put`, keyboard-interactive z hasłem, błędne hasło i nieznany klucz serwera. Wszystkie sześć przypadków przeszło. Poprawne transfery sprawdzały zawartość plików; błędne hasło kończyło się po jednej próbie, nieznany klucz przed wysłaniem hasła. Test zapisano w `tests/integration/ssh_password.py` i dodano do CI.
- Rzeczywisty PTY 80x15: otwarcie formularza, wpisanie `q`, zapis nowego hosta i wyjście z ekranu głównego zakończyły się poprawnie.

Pokrycie instrukcji po pełnym przebiegu: CLI 73,4%, config 80,2%, tunnelstate 78,8%. Pokrycie własnych testów pakietu store wynosi 25,8%; jego mechanizmy scalania są dodatkowo wykonywane w testach pakietów config i tunnelstate. Nie jest to pomiar z `-coverpkg=./...`.

Benchmark renderowania na Apple M1 dla 10, 100 i 1000 tuneli: odpowiednio 0,413 ms, 0,427 ms i 0,555 ms. Liczba alokacji pozostaje niemal stała. Pomiar dotyczy renderowania zapisanej migawki, bez procesów SSH. Osobny pomiar odświeżania statusów bez aktywnych procesów wyniósł 0,011 ms, 0,026 ms i 0,313 ms. Nie przedstawia opóźnień rzeczywistych połączeń.

## Zmiany widoczne dla użytkownika

Importowane aliasy zależą od oryginalnego pliku OpenSSH. Import wypisuje jego ścieżkę i ostrzega o wzorcach, których nie da się zamienić na listę nazw. Eksport aliasu przemianowanego w shbx wymaga użycia pierwotnej nazwy, aby zachować warunki Host w źródle.

Hasła nadal mogą być zapisane w prywatnym JSON, zgodnie z istniejącą funkcją aplikacji. Poprawka zmienia bezpieczne przekazywanie hasła do OpenSSH i prawa plików. Nie dodaje systemowego magazynu poświadczeń.

Katalog `docs/prototypes` był obecny przed pracą. Nie jest częścią poprawek ani commita z tego audytu.
