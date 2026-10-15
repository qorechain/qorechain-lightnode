# QoreChain Light Node for Android

The same Go light node the server (SX) and desktop (UX) builds run, bound into an
Android app with `gomobile`. Not a reimplementation: `../mobile` wraps
`../internal/lightclient` and `../internal/client` directly.

Verified 2026-10-15 on an Android 15 (API 35) arm64 emulator against live
mainnet: the app syncs headers in a foreground service and shows the chain
height, the reward pool and the heartbeat window read from `qorechain-vladi`.

## What it does, and what it does not

| | Android build |
|---|---|
| Verify block headers | yes, in-process |
| Cross-check against witnesses | supported, but none configured (see below) |
| Show the reward pool and per-node ceiling | yes |
| Register, heartbeat, claim | **no** |
| Earn light-node rewards | **no** |

The app says so on its first line. An operator must not discover it from an
empty rewards screen.

### Why it cannot earn yet

On desktop the daemon signs by driving the bundled `qorechaind` CLI
(`internal/daemon/signer.go`). That does not port:

- `qorechaind` has no Android build. It needs CGO plus `libwasmvm`,
  `libqorepqc` and `libqoresvm`.
- Android 10+ refuses to `exec` a binary from app storage anyway.

QoreX cannot sign for it either. Its spec requires StrongBox Keystore and
biometric confirmation on every signature, and it exposes no signing API to
other apps. A heartbeat is due every ~54 minutes forever, so that is the right
rule for a wallet and the wrong one for a node.

The agreed direction, from the QoreX spec owner: a separate heartbeat-only key
on the device running the node; registering, claiming and withdrawing stay in
QoreX. That needs `MsgHeartbeat` built and signed in Go. The ML-DSA-87 half
exists (`internal/pqc`, pure Go). The sign-bytes and PQC-attachment contract
does not, and it will be taken from the chain code by the Net Development
session after v3.2.3, not guessed. A transaction built from a guessed contract
is rejected by the chain at best; at worst it is a key that can do more than
heartbeat.

### Why a foreground service

The chain marks a node inactive after `heartbeat_interval +
heartbeat_grace_period` blocks without a beat. On mainnet that is 1,100 blocks,
about **58 minutes** at the October 2026 cadence of ~3.2 s/block. Below
`min_uptime_for_rewards` (0.8) it earns nothing.

Doze can defer ordinary background work on a stationary, unplugged phone for
longer than that, so a periodic `WorkManager` job cannot be relied on. The
service is `dataSync`-typed with a persistent notification. This build does not
heartbeat, but the shape is already the one a heartbeating build would need.

### No witnesses by default

The node runs with no witness endpoints and the screen shows
`trusted-single-source`. Every value comes from `rpc.qore.host`, which could
fabricate any of them. A witness only means something if somebody else runs it,
and there is no independently operated public mainnet RPC to point at today.
Every spelling of loopback counts as one witness, so pointing two at the phone
proves nothing.

## Build

Needs Go, the Android SDK with an NDK, and JDK 17.

```bash
./build-aar.sh
```

```bash
JAVA_HOME=$(/usr/libexec/java_home -v 17) ./gradlew assembleDebug
```

- Release builds (`./gradlew assembleRelease`) are signed with the QoreX
  Android upload key, the owner's choice. The credentials come from
  `QOREX_UPLOAD_*` in `~/.gradle/gradle.properties`, exactly as QoreX reads them,
  and are never committed. Without them the release APK is left unsigned
  (`app-release-unsigned.apk`) rather than signed with the debug key, so a
  debug-signed build cannot be mistaken for a release. Signing certificate
  SHA-256: `1d50d063b945ce38145c8388e60f86a61a48c12d42419448fe4c43d083dcdf1f`
  (`CN=QoreX Upload, O=QoreChain, C=RO`).
- `build-aar.sh` rebuilds `libs/qorelightnode.aar` from `../mobile`. Run it
  whenever `../internal` or `../mobile` changes, or the app ships a stale node.
- The binding is its own Go module (`../mobile/go.mod`). `gomobile` needs
  `golang.org/x/mobile` in the module graph, and the light node proper is a
  minimal-dependency client. `go build ./...` at the repo root builds exactly
  what it did before.
- The Gradle wrapper is pinned to 9.3.1. AGP 8.x uses an internal API that
  Gradle 9.6 removed.
- `compileSdk` is 36 because `androidx.core:core-ktx` 1.17 requires it.
  `targetSdk` stays 35.
- The AAR carries arm64 only, which covers every current Android phone. To run
  on an x86_64 emulator, add `android/amd64` to `build-aar.sh` and to
  `abiFilters` in `app/build.gradle.kts`.

## Known limitations

- **UI.** The UI is plain Views (AppCompat). QoreX and the wallet skeleton use
  Compose. This is a status screen of four text blocks, and a minimal-dependency
  client should not pull in a UI toolkit for that.
- **Running state.** Whether the service runs is tracked in a process-static
  flag. If the process dies while the service keeps running, the button can
  show the wrong label until the next toggle.
- **Edge-to-edge.** It is opted out on API 35 (`values-v35/themes.xml`). Forced
  edge-to-edge drew the content under the action bar and hid the "cannot earn"
  notice. Handling insets properly would let the opt-out go.
- **Network.** Mainnet only, hardcoded in `NodeHolder`. There is no settings
  screen yet for diana or a custom RPC.
