# Windows service — manual verification checklist

Run on a real Windows 10/11 machine with Docker Desktop before promoting the
first service-capable release. Automated coverage ends where the SCM, NTFS
rename semantics, Defender, and Docker Desktop's login-scoped engine begin —
these must be exercised on hardware.

## Install

1. From an elevated prompt: `reagent.exe service install -config <flock> -start`.
   Verify in `services.msc`: delayed auto-start, recovery tab shows
   restart 5s / 30s / 120s with a 1-day reset; event log source `reagent`
   exists; Task Scheduler shows "IronFlock Agent Repair" (ONSTART, SYSTEM);
   `icacls %ProgramData%\IronFlock\Reagent` shows SYSTEM + Administrators only,
   with the Users grant on `apps\`.
2. Non-elevated install attempt → clear "elevated prompt" error.
3. Machine with an existing `C:\Users\<x>\reagent\reagent.db` → install without
   `-agentDir` aborts with the two-choice migration message; re-run with
   `-agentDir` pointing at the old dir keeps existing app data working.

## Boot / Docker

4. Reboot WITHOUT logging in → service runs, device shows CONFIGURING in
   Studio (WAMP endpoints reachable, remote debugging works). Log in → Docker
   Desktop starts → device goes CONNECTED, apps deploy. Confirm SYSTEM can
   reach `\\.\pipe\docker_engine` (`psexec -s docker version`) and that
   `docker.exe` is on the MACHINE PATH (service sees no user PATH).
5. Compose app full lifecycle: install → RUNNING → reboot → RUNNING again;
   app `/data` contents survive both the reboot and an agent self-update.

## Self-update

6. Studio "update agent": progress events arrive, `reagent-prev.exe` appears,
   service restarts within ~15s, `reagent.exe -version` reports the new
   version, `update-state.json` disappears ~2 min later (probation passed).
7. Bad update (binary that exits immediately, staged as `reagent-v<ver>.exe`
   + forced activation): 3 failed starts → rollback to prev → marker phase
   `rolledback` → the same version is NOT re-activated on the next update
   check (blacklist holds, no ping-pong).
8. Vacancy: stop the service, delete `reagent.exe` (leave `reagent-prev.exe`),
   reboot → repair task restores the binary and starts the service.
9. Defender real-time protection ON during an update: swap succeeds (retry
   logic absorbs scan-handle sharing violations). Note any quarantine events —
   if Defender flags the unsigned exe, code signing moves from follow-up to
   prerequisite.

## Supervision / control

10. `taskkill /f /im reagent.exe` → SCM restarts after 5s (then 30s/120s on
    repeated kills within a day). `reagent service stop` → clean stop, NO
    restart. `sc query reagent` matches `reagent service status`.
11. Kill the agent mid compose build → the `docker compose` child processes
    disappear with it (job object).
12. Second instance: with the service running, `reagent.exe -config ...` in a
    console → refuses with the single-instance message. FlockFlasher
    "Test Device" on the same machine also refuses (expected, documented).
13. From Studio: `system_restart_agent` (service restarts), `system_reboot`,
    `system_shutdown` (machine reboots/shuts down after ~5s), device terminal
    → clean "not supported on Windows" error.

## Environment

14. Proxy machine: install with `-proxy http://host:port` → OTA download and
    the wss connection both work under LocalSystem; machine-store corporate
    CA is accepted (Go uses the Windows system store).
