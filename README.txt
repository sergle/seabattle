===============================================================================
 SEA BATTLE — local LAN two-player Battleship
===============================================================================

A small Go server that hosts one Battleship game for exactly two players. Each
player opens the game in a phone (or desktop) browser over the local network.
No accounts, no internet, no app install.

The interface is BILINGUAL: English and Ukrainian. A language dropdown with
flags sits in the top-right corner — pick a language at any time. The default
is taken from your browser's language; your choice is remembered. Button labels
below use their English text (e.g. "Start"); the Ukrainian UI mirrors them.

-------------------------------------------------------------------------------
 REQUIREMENTS
-------------------------------------------------------------------------------
- Go 1.22 or newer (built and tested with Go 1.25).
- Two devices (phones/laptops) on the SAME Wi-Fi / LAN as the server.
- A modern browser on each device.

-------------------------------------------------------------------------------
 BUILD
-------------------------------------------------------------------------------
From the project directory:

    go build -o seabattle .

This produces a single self-contained binary named `seabattle`. The web client
(HTML/CSS/JS) is embedded inside the binary — nothing else to copy.

-------------------------------------------------------------------------------
 RUN THE SERVER
-------------------------------------------------------------------------------
    ./seabattle

Or run without building:

    go run .

Choose a different port (default 8080):

    ./seabattle -port 9000

On start the server prints how to reach it, e.g.:

    Sea Battle on :8080
      this device : http://localhost:8080
      other phone : http://192.168.1.42:8080   (same WiFi)

Note the "other phone" URL — that is the address both phones use.

FIREWALL: the host machine must allow incoming TCP connections on the chosen
port (8080 by default), or the phones will not connect.

-------------------------------------------------------------------------------
 CONNECT THE PLAYERS
-------------------------------------------------------------------------------
1. Start the server on one machine (it can be one of the two phones, a laptop,
   or any computer on the network).
2. On EACH phone, open the "other phone" URL shown in the banner
   (e.g. http://192.168.1.42:8080).
3. The first time, each player is asked for a name. Type it and tap "Join".
4. The banner at the top shows "You vs <opponent>" with a dot:
   green = opponent online, red = offline.
5. Change your name any time: tap your OWN name in the banner, type a new one,
   confirm. The opponent's banner updates immediately.

Only two players can join. A third browser that connects sees "Game is full".

-------------------------------------------------------------------------------
 HOW TO PLAY
-------------------------------------------------------------------------------
FLEET (each player places the same 10 ships):
    1 ship  of 4 cells
    2 ships of 3 cells
    3 ships of 2 cells
    4 ships of 1 cell
Total: 10 ships, 20 cells.

PLACEMENT RULES (enforced — you cannot break them):
    - Ships are straight, horizontal or vertical.
    - Ships may not overlap.
    - Ships may not touch each other — not side by side, not even at a corner.
      Every ship is surrounded by at least one empty cell. Shaded cells around
      a placed ship show this "no-go" zone.

PLACING YOUR SHIPS:
    1. The next ship to place is selected for you automatically (starting with
       the first). You can just tap cells on YOUR grid to drop ships one after
       another — no need to pick from the palette each time.
       (To place out of order, tap a different ship in the palette first.)
    2. Tap a cell on YOUR grid to drop the selected ship there.
    3. Move a placed ship: press and drag it. A green outline means a legal
       spot, red means illegal — release on red and the ship snaps back.
    4. Rotate a ship: double-tap it (toggles horizontal / vertical).
    5. A placed ship turns grey in the palette.
    6. When all 10 ships are placed, the "Start" button lights up. Tap it.

STARTING:
    After BOTH players tap their own "Start", the server randomly picks who
    shoots first. The screen shows "Your turn" or "Opponent's turn".

SHOOTING:
    - On your turn, tap a cell on the ENEMY WATERS grid to fire.
    - Result:
        miss  - a small dot. Your turn ends; the opponent shoots next.
        hit   - a red X. You HIT a ship: you get ANOTHER shot.
        sunk  - the whole ship turns dark red. The empty cells around it are
                auto-marked as misses (no ships can be there). You shoot again.
    - So: keep hitting, keep shooting. Miss, and the turn passes.
    - On Android phones the device VIBRATES on hits, sinks, and game over
      (haptic feedback). iPhones do not vibrate (browser limitation).

WINNING:
    Sink all 20 of the opponent's ship cells. The winner sees "YOU WIN",
    the loser sees "YOU LOSE".

PLAY AGAIN (REMATCH):
    On the result screen each player has a "New Game" button. When BOTH players
    tap it, the boards reset and you return to ship placement with the same
    names and slots. While you wait for the other player, the button shows
    "Waiting for opponent".

-------------------------------------------------------------------------------
 RECONNECTING
-------------------------------------------------------------------------------
If a phone's screen sleeps, the browser reloads, or Wi-Fi drops briefly, just
reopen the same URL. You rejoin your own side with the board and whose-turn
state restored — the game is paused while you are away and resumes when you
return. Your slot is held for you; nobody else can take it.

While disconnected, the status line turns RED and shows "Reconnecting" until
the connection is restored.

(The game lives in memory only. If the SERVER process is restarted, the match
is gone and a fresh game begins.)

-------------------------------------------------------------------------------
 TROUBLESHOOTING
-------------------------------------------------------------------------------
- "Connecting" / "Reconnecting" never clears:
    Phone is not on the same network, wrong IP, or the host firewall is
    blocking the port. Verify the banner IP and that both devices share Wi-Fi.

- "Game is full":
    Two players are already connected. If one of them is you on a stale tab,
    close the extra tab; your original slot reconnects via the same browser.

- Page won't load at all:
    Make sure you used the "other phone" http://<ip>:<port> address, not
    "localhost" (localhost only works on the machine running the server).

-------------------------------------------------------------------------------
 DEVELOPER NOTES
-------------------------------------------------------------------------------
Run the test suite:

    go test ./... -race

Project documents:
    docs/architecture.md  - overall design and domain rules
    docs/protocol.md      - WebSocket message format (canonical)
    docs/server.md        - server implementation plan
    docs/ui.md            - browser client plan
    docs/server-tests.md  - server test cases

Layout:
    main.go              startup, embeds web/, prints banner
    internal/game/       pure game rules (board, fleet, turns) — no networking
    internal/server/     hub, sessions, WebSocket + HTTP handlers
    internal/netutil/    LAN IP detection for the banner
    web/                 browser client (HTML/CSS/JS), embedded into the binary
===============================================================================
