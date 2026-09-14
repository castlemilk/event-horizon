# Dongle link runbook: absent hardware → live dish

Verified end to end 2026-09-12 (UGREEN AX900 / AIC8800D80, macOS, Starlink Gen 3).
Each step states how to confirm it — do not skip ahead on assumption.

## 1. Hardware present?

```
ioreg -p IOUSB -w0 | grep -iE 'ugreen|aic msc'
```

- **Nothing**: unplug the dongle ~10s, replug. Only a VBUS drop re-enters
  flashable ZeroCD mode; software cannot do this step.
- **"Aic MSC" / "Ugreen Storage Device"** (VID `0xa69c` PID `0x5723`): ZeroCD
  stage. No further replugs — it needs the bring-up, not another power cycle.
- **Ghost check**: a stale entry keeps its old id/busy-time while a fresh
  replug appears as a *new* node (e.g. `Aic MSC@02110000`, busy ~ms). Compare
  busy times before concluding anything about what is plugged in now.

## 2. Daemon up?

```
curl -s -m 8 http://127.0.0.1:8990/api/starlink/status
```

- **Connection refused**: the Event Horizon app launches it (admin prompt via
  the app's privileged launcher). The app auto-retries every ~20s once the
  flag below is honest; use its Restart item to force it.
- **Responds**: read `dongle_present` / `usb_stage` / `reason`, which are
  distinct states, not one message:
  - `NO_DONGLE` → back to step 1 (replug).
  - `DONGLE_ZEROCD` → step 3 (bring-up). No replug.
  - operational + unassociated → step 3.
  - `usb_stage: probing` → first pass still running; wait seconds, re-read.
    Never treat probing as absent.

## 3. Bring the link up

Daemon API (daemon already runs as root — no sudo needed):

```
curl -X POST http://127.0.0.1:8990/api/wifi/link \
  -H 'Content-Type: application/json' \
  -d '{"ssid":"<SSID>","pass":"<pass>","route":"192.168.100.1"}'
```

or CLI (needs sudo, blocks for the life of the link — leave it open):

```
sudo ./bin/usbwifi cmdctl link --ssid <SSID> --pass <pass> --route 192.168.100.1
```

Poll `GET /api/wifi/link` to `up` ("bridged on utun11 as …"). Flashing →
associating → handshaking → configuring takes a couple of minutes. A
`needs_replug` state mid-run means the firmware instance is used — replug,
then re-run.

## 4. Verify the dish (in order)

```
route -n get 192.168.100.1        # must say utun, not en0
nc -z -w5 192.168.100.1 9200      # port open
./bin/starlink-probe caps         # real terminal vs stub (starlink-sdk)
```

Then the starlink-sdk gateway picks it up itself; its `dish-unreachable`
alert resolves within ~30s of good ticks.

## Validation

- `go test ./pkg/api/ ./pkg/usb/ -count=1` (+ `-race` for the cache tests).
- `swift test` (app flag + daemon recovery).
- Live: the three `/api/starlink/status` states (unplug → `NO_DONGLE`;
  ZeroCD → `DONGLE_ZEROCD`; linked → associated + `usb_stage: operational`).
- UI truthfully shows "not connected" while the daemon is down
  (regression-tested: retained topology must not latch the flag).

## Reliability guards (added 2026-09-14)

- **Stale utun routes are replaced safely, not blindly.** `route add -host`
  fails with "File exists" against a route left by a dead link, and a bare
  delete could steal a LIVE link's route. `pkg/tun.EnsureHostRoute` checks
  the existing interface is a utun, proves its packet counters frozen over a
  short window, and only then replaces it. Non-utun routes (en0 etc.) are
  never touched. `AddStarlinkRoute` uses it, so the bridge no longer
  blackholes the dish after an unclean exit.
- **Sibling-VAP fallback on association refusal.** Guest APs rotate BSSIDs;
  a pinned BSSID that the AP withdrew fails with status_code=1 while a
  sibling on the same channel would accept. Each `link` run now attempts
  up to two sibling BSSIDs (strongest-first, same SSID or same channel)
  from a single in-session broad scan, without spending a firmware reset
  between attempts. Only an explicit refusal queues a fallback; silence
  still means "replug".
- **One link at a time, software-enforced.** `cmdctl link` refuses to start
  when a linkstate record's writer is still alive (PID check + freshness);
  the daemon's LinkService already serialized the API side. The CLI guard
  exists because killing a healthy link by starting a second one is a real
  failure mode, and the two processes then fight over the radio with no
  way to report it coherently.
- **Linkstate file as the daemon's window.** `~/.event-horizon/linkstate.json`
  is written by the CLI at bridge-up and removed on every exit; the daemon
  reads it as a fallback when its bus scan is empty (the CLI holds the USB
  claim exclusively, so the scan reads NO_DONGLE precisely when the link is
  working). Dead-PID or >24h entries read as gone, never live.

## Operator notes (earned 2026-09-12, Uncle Rad-Guest session)

- **sudo needs the absolute binary path + clean env.** The sudoers rule
  allows `/Users/benebsworth/projects/event-horizon/bin/usbwifi` exactly —
  `./bin/usbwifi` fails — and a `TERMINFO` in the environment is refused.
  Canonical form from scripts/agents:
  `env -u TERMINFO sudo -n /Users/benebsworth/projects/event-horizon/bin/usbwifi …`
- **Run `link` detached.** It blocks for the life of the bridge; a tool
  timeout (or closed terminal) kills it and drops the bridge with it.
  Background it to a log file and verify via route/dish, not via command exit.
- **association `status_code=1` means the AP said no — not deafness, not the
  password** (password is only used post-association). Checklist in order:
  live beacon with matching SSID on the scanned channel, current BSSID
  (multi-VAP guest APs rotate them; `:50:`/`:b0:`/`:e0:` were all seen in one
  night), then instance freshness. A hidden SSID needs SSID + BSSID pinned.
- **Guest APs deauth idle clients** (`reason=4 inactivity` after ~25 min here)
  and may hide the SSID / drop VAPs overnight. If every VAP refuses on a
  known-good instance, check from a second device (phone/Mac joining settles
  AP-side vs dongle-side in seconds) before burning resets.
- **Resets are a budget, not a retry loop.** Each `link` run spends MM_RESET;
  past one per flash the radio goes deaf while still answering CFM. Repeated
  `--skip-flash` runs end at `LIBUSB_ERROR_TIMEOUT` on bulk OUT — fully wedged,
  replug is then the only way out. One clean flash + one association per power
  cycle; stop after two failed associations and replug.
- **Ghost USB nodes are real.** A dead `Ugreen Storage Device` node can linger
  for hours beside the fresh `Aic MSC`/`AIC 8800D80` node. Verify replugs by
  *new* node + low busy-time, never by name presence alone.

## Troubleshooting

| Symptom | Likely cause | Action |
|---|---|---|
| `/api/starlink/status`, `/api/hardware/topology` hang | libusb enumeration stalled on a half-enumerated ZeroCD (ghost + fresh node) | Should not happen post-cache fix; if it does, the stall is in `enumerateUSB`, check `ioreg` for ghost nodes |
| App green but daemon dead | stale `isDaemonConnected` (fixed: flag follows status fetch only) | rebuild app, relaunch, approve prompt |
| Daemon died, log ends in registers/`SIGABRT` | libusb `pthread_key_create` (`usbi_tls_key_create`) crash — root cause still open | relaunch via app; capture log if it recurs |
| UI shows wrong network ("on X" while unassociated) | scanner `IsSelected` reflects last *selected*, not *associated* | trust `connectedSSID`/daemon `associated`, never the highlight; fixed by actually associating |
| "Authorized, but the daemon never answered" (supervisor.log) | privileged spawn succeeded, daemon crashed or is still scanning | read `/var/log/eventhorizon-daemon.log` (binary-safe: `grep -a`) |
| Auth prompt every launch | no sudoers rule + no LaunchDaemon | app's Install Daemon Service authorizes once, launchd owns it after |
| Tests pass alone, fail together (`SFH` leak) | `usb.SetDongleConnected` is process-global, set by connect tests | reset it in the test (`SetDongleConnected("")`) |
| Go test binary hangs with new cache tests | test stub holding a mutex across a blocking stub call | count under lock, invoke outside it; wait for in-flight passes before restoring seam vars |

## Gotchas for future sessions

- `grep` on `/var/log/eventhorizon-daemon.log` needs `-a` (NUL bytes in the log).
- The daemon binary the app launches is `build/Event Horizon.app/Contents/Resources/usbwifi`
  (+ `Contents/MacOS/usbwifi`, + repo `bin/usbwifi`) — rebuild Go, copy to all
  three, exactly like `scripts/package_app.sh` lines 31–34. The app's
  fingerprint check replaces stale daemons, but only when it runs `ensure`.
- `~/Library/Logs/EventHorizon/supervisor.log` shows every privileged spawn
  attempt and its outcome — first place to look when the app looks alive and
  does nothing.
- The app must be rebuilt + relaunched for Swift changes; Go daemon changes
  only need the binary copy + app Restart (auth prompt).
- `bin/usbwifi-mcp` in the repo root listing is unrelated third-party runtime
  state; `.agents/mcp_config.json` was already dirty — neither is ours.
