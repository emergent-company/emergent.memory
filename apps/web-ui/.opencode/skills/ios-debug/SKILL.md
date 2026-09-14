---
name: ios-debug
description: Debug the Memory iOS app runtime (logs, crashes, screenshots, backtraces) via the Mac build machine. Use when asked to debug, inspect, or verify iOS app behavior.
---

# iOS Debug

Debug the Memory iOS app runtime on the Mac build machine (`mcj-mini`, reachable via `ssh mcj-mini`, no password). App process name is `Memory`, bundle id `com.emergent.memory`. Project checkout on Mac: `~/code/alftred` (rsync'd from `/root/alfred`).

## 1. When to use

- App crashes, hangs, or misbehaves on the iOS simulator.
- Need app stdout, os_log output, crash reports, screenshots, or backtraces.
- Need to verify the built app actually runs after an iOS change.

## 2. `tools/ios-debug-mac.sh` subcommands

All run over `ssh "$MAC_HOST" ...`; env overrides: `MEMORY_MAC_HOST`, `MEMORY_MAC_PATH`, `SCHEME`, `BUNDLE_ID`, `UDID`, `APP_PATH`, `VERBOSE`.

- `boot` — boot simulator (auto-detects UDID) + wait for bootstatus. `tools/ios-debug-mac.sh boot`
- `run [--no-sync]` — rsync, xcodebuild Debug build, install, launch with `--console-pty` (streams app stdout; Ctrl-C stops). `tools/ios-debug-mac.sh run`
- `log [predicate]` — live os_log stream for process "Memory". `tools/ios-debug-mac.sh log 'process == "Memory" AND messageType == 16'`
- `logs [predicate] [--last 10m]` — historical os_log. `tools/ios-debug-mac.sh logs --last 30m`
- `screenshot [outfile]` — PNG; with a local path it scp's back, with `-` or omitted it streams raw bytes to stdout. `tools/ios-debug-mac.sh screenshot /tmp/memory.png`
- `crash [--latest N]` — list newest `.ips` crash reports. `tools/ios-debug-mac.sh crash --latest 10`
- `crash --symbolicate <path>` — symbolicate a `.ips` against `Memory.app.dSYM` (warns if dSYM missing). `tools/ios-debug-mac.sh crash --symbolicate ~/Library/Logs/DiagnosticReports/Memory-2026-08-30.ips`
- `bt [name|pid]` — LLDB batch backtrace of the running app (launches with wait-for-debugger if not running). `tools/ios-debug-mac.sh bt Memory`
- `terminate` — kill the app on the booted simulator. `tools/ios-debug-mac.sh terminate`
- `app` — print resolved config + simulator state. `tools/ios-debug-mac.sh app`

## 3. XcodeBuildMCP MCP server

Wired in `opencode.json` (`mcp.xcodebuild`), a local MCP that runs `ssh mcj-mini npx -y xcodebuildmcp@latest mcp`. It is a structured build/run/test tool, NOT a debugger. Key tools: `build_run_sim` (build + install + run on simulator), `build_xcrun` (wrap xcrun/simctl commands). Use it for build/test/run automation; use `ios-debug-mac.sh` for runtime diagnosis.

## 4. Two-channel logging gotcha

- `print()` / stdout → only visible via `simctl launch --console-pty` (i.e. `ios-debug-mac.sh run`).
- `os_log` → only visible via `log stream` / `log show` (i.e. `log` / `logs` subcommands).
- These are different channels. Missing output in one does not mean the app is silent; check the right one.

## 5. Crash + symbolication

- Crash reports: `~/Library/Logs/DiagnosticReports/*.ips` and `~/Library/Developer/CoreSimulator/Devices/<UDID>/data/Library/Logs/CrashReporter/*.ips`.
- Symbolicate: `xcrun symbolicatecrash <path> Memory.app.dSYM` — dSYM lives next to the app in `build/DerivedData/Build/Products/Debug-iphonesimulator/`. The dSYM must be preserved from the same build as the crash; rebuild with `run` (Debug build) to restore symbols.
- Use `crash --symbolicate` to run this on the Mac.

## 6. LLDB attach

A simulator app runs as a HOST process on the Mac (not inside a VM), so attaching by PID works remotely over ssh: `lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'`. Use `bt` subcommand; the `$(pgrep ...)` must expand on the Mac (escaped locally).

## 7. Raw CLI reference (run manually when needed)

```bash
# device / boot
xcrun simctl list devices available
xcrun simctl boot <UDID> && xcrun simctl bootstatus <UDID> -b

# install / launch / kill
xcrun simctl install booted <APP_PATH>
xcrun simctl launch --console-pty booted com.emergent.memory   # streams stdout
xcrun simctl terminate booted com.emergent.memory

# logs
xcrun simctl spawn booted log stream --predicate 'process == "Memory"' --style compact
xcrun simctl spawn booted log show --last 10m --predicate 'process == "Memory"'

# screenshot
xcrun simctl io booted screenshot /tmp/memory.png

# crashes
ls -t ~/Library/Logs/DiagnosticReports/*.ips | head -5
ls -t ~/Library/Developer/CoreSimulator/Devices/<UDID>/data/Library/Logs/CrashReporter/*.ips | head -5
xcrun symbolicatecrash <path.ips> <path>/Memory.app.dSYM

# lldb backtrace (attach by host pid; $(pgrep) expands on the Mac)
lldb --batch -o 'process attach --pid $(pgrep -f Memory)' -o 'bt all' -o 'detach' -o 'quit'
```
