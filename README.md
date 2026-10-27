# QoreChain Light Node

**v3.1.11** - **the node updates itself.** Every 6 hours it checks the release channel of its network; a new version is announced in the log, on the UX dashboard and at `/api/update`, and with `[update] mode = "auto"` (the default) installed and restarted into. It installs only a release whose checksums carry the QoreChain release signature (Ed25519, public key built into the binary), whose binary matches its signed checksum and which runs and reports the expected version; the previous binary stays next to it as `<name>.old`. In a container, or where the binary's folder is not writable, it only announces. `lightnode-sx update` (or `update --check`) does the same on demand. **Delegation alerts that work:** the node now warns when a validator you delegate to is jailed or leaves the active set, and when your bonded stake falls below the light node minimum, the two cases where the chain silently stops paying. The validator reputation check, which no network publishes yet, says so once instead of logging a warning every five minutes. **Testnet licence message** points to the self-service request on the dashboard (Tools -> Light Node, testnet selected), granted within minutes. 3.1.10 and earlier do not update themselves: install 3.1.11 by hand once.

**v3.1.10** - **`register` counts stake the way the chain does.** The chain counts only stake bonded to validators that are bonded now; `register` also counted delegations to jailed or unbonded validators, so it could pass its own check and then be refused on chain. **Licence messages follow the network:** on mainnet they point to the dashboard's Tools -> Light Node tab; on testnet, where licences are free and the dashboard sells nothing, to a free grant on request. Texts that still sent operators to create the account with qorechaind or to edit `operator_address` by hand now point to `keys add-operator`; the UX banner names `lightnode-ux`; the compose file defaults to the public RPC and marks the qorechaind mount as fallback only; the build is CGO-free and the UX port is 8420, as the docs now say. Found by the dashboard session while rewriting the public light node docs.

**v3.1.9** - **an account whose wallet already registered a post-quantum key can run a node.** The chain accepts only the key on the account, and most accounts set up in a wallet have one, derived from the same words; 3.1.8 then refused to register. `keys add-operator --recover` now derives that key from the words, exactly as qorechaind `tx pqc recover-key` does (the wallet-adapter derivation, and the dashboard's older one), and the node signs with it. An account with no key gets one derived from its words, so the words restore the node's key too; a key the words cannot reproduce is refused up front, with the advice to use a dedicated account. Verified on the v3.2.3 devnet: a key registered by qorechaind from the words, then recovery, delegation, registration and heartbeat from the node.

**v3.1.8** - **the node signs its own transactions, on every platform.** Until now every on-chain step went through the `qorechaind` CLI, which is published for linux/amd64 only, so on macOS, Windows and Linux arm64 a node could run but never register, heartbeat or claim. The hybrid post-quantum transaction (ML-DSA-87 over the body without the PQC extension, secp256k1 over the body with it) is now built in pure Go, from the chain's own contract, and verified on a v3.2.3 devnet with classical fallback disabled, as mainnet runs: key registration with proof of possession, delegation, registration, heartbeat and claim, from macOS arm64 and from Linux arm64 and amd64 containers, with no `qorechaind` installed. `keys add-operator` stores the operator account key (new, or `--recover` from the account's words, derived exactly as qorechaind and Keplr do), and `register` then does every step itself: attaches the node key as the account's post-quantum key, delegates the missing stake with `--delegate-to`, registers. The sign-bytes version is read from the network over every plan name that switched it, and the proof of possession is sent only where the chain knows the field. `qorechaind` remains the fallback for nodes whose operator key lives in a qorechaind keyring (`register --print`). Also in this release, the 3.1.7 fixes below, which were never published on their own.

**v3.1.7** (not published separately) - the release that makes registration work end to end, found by running the whole path on a v3.2.3 devnet: licence, post-quantum key, stake, register, heartbeat, claim. **Heartbeats and claims could not reach the public RPC:** the signer rewrote `https://rpc.qore.host` to `tcp://rpc.qore.host`, which `qorechaind` refuses, so a node pointed at it would have failed every heartbeat and been marked inactive within the hour. Nobody hit this only because no node was registered yet. **A transaction is now reported only once it is in a block**, with the block's verdict: a sync broadcast's code 0 means the mempool took it, and on the devnet a registration passed that check and then failed in the block. **The heartbeat and the claim no longer race:** they sign from one account, and built in the same minute the second was refused for a stale sequence (code 32). **`onboard` produced a node that could not register:** it saved the key without its public key, so `register` printed an empty `register-key-v2`; it named the key `validator` while `config.toml` said `operator`; it left `chain_id` on the testnet whatever endpoint was given; and without `QORE_LIGHTNODE_KEYRING_PASSPHRASE` it generated a key and then lost it. Keys saved by earlier versions are repaired when read. **`register` prints commands that run as printed:** each carries `--node`, the key is exported with `keys export --out` (a redirected export ended in a newline that `qorechaind` rejects), and it says when the operator account is below the chain's 1,000 QOR minimum stake instead of letting the registration fail. **The UX build has `onboard`, `keys` and `register`:** it told a new user to run `lightnode-ux onboard`, a command it did not have. Signing still needs `qorechaind`, which is published for linux/amd64 only (bundled in the SX Docker image); on macOS, Windows and Linux arm64 the node runs and verifies but cannot register yet.

**v3.1.6** - fixes found by pairing QoreX with a real 3.1.5 node. **Staking rewards and delegations were read for the wrong account:** the node used its keyring key's address, which for a post-quantum key is not a chain account, so `/api/rewards` had no `staking_rewards` and `/api/delegation` was empty for an operator holding real stake; both now use `operator_address`. **The delegation table was empty in every release:** the API sent `Validator`/`Amount`, while the node's own web dashboard and QoreX read `validator`/`amount`. Delegations are also synced at start instead of five minutes later, ordered by amount instead of as text, and `staking_rewards` is reported in whole uqor. **The SX node can serve the dashboard API** (`[dashboard] enabled = true`, same token and exposure rules as UX), so QoreX can pair with the nodes that register and earn. `config.example.toml` had `log_level`/`log_format` under `[dashboard]`, where they were ignored; they are top-level now. No migration.

**v3.1.5** - compatible with the chain through v3.2.3, and three fixes that change what an operator sees. **A registered node was reported as unregistered:** the client decoded the light-node queries from shapes the chain never sends (it answers `{"node":...,"found":...}`, not `{"light_node":...}`), so `status` said "not registered" and `auto_claim` never found anything to claim. **`/api/rewards` reported the staking yield** under the field QoreX shows as light-node earnings; `total_rewards` is now the light-node accrual and `staking_rewards` carries the staking figure, along with `heartbeat_age_blocks`, `auto_claim` and `last_claimed_at`, which QoreX expected and never received. **The dashboard ignored the access token** QoreX sends: set `[dashboard] token` and every `/api` route and `/ws` require it (nothing changes if you leave it empty). Also: plaintext witnesses must be real loopback addresses, not names that merely start with `127.` (QSR-2026-0020); an expired licence is reported as expired rather than suspended; and the new `/api/rewardpool` shows what is earnable, with the per-node bound as a monthly figure and which bound the chain is applying (the monthly one from chain v3.2.3). An Android build that verifies the chain, but does not register or earn, is in `android/`. No config, key or data migration: `git pull && docker compose up -d --build lightnode-sx`.

**v3.1.4** - the SX image's bundled signer moves to `qorechaind` v3.2.0, the release mainnet runs since height 2,981,742 (25 September 2026). From that height the chain verifies only the v2 hybrid sign-bytes, and the v3.1.95 signer that 3.1.3 shipped still signed v1, so every heartbeat and reward claim from a 3.1.3 container is refused with code 21 (`pqc`). The v3.2.0 `tx pqc cosign` asks the node which form the network expects (`--sign-bytes auto`, its default), so the same image signs correctly on mainnet and on testnet. Nothing else changes: keys, config and data volumes carry over. The compose file builds the image from this repository, so update with `git pull && docker compose up -d --build lightnode-sx`; a plain `docker compose up` or `restart` reuses the old image and keeps the old signer. If you run `lightnode-sx` outside Docker with your own `qorechaind`, upgrade that binary to v3.2.0 from https://download.qore.host/mainnet/latest.json.

**v3.1.3** — the release that lets an SX node actually live on chain. Registration is licence-gated by the tool as well as by the chain: `register` and `status` ask the chain for the `lightnode_operator` licence on `operator_address` and, without one, say so and where it is bought (dashboard, Tools -> Buy License) instead of printing commands the chain would refuse. Heartbeats are on by default and paced by the chain's own `last_heartbeat`; the daemon drives the `qorechaind` signer (generate, then post-quantum cosign), which the SX Docker image now bundles from the release channel with a pinned digest, so a container node stays active without a second install. The "auto-compound" loop, which signed a document the chain could never verify and logged a failure every hour, is replaced by an opt-in `auto_claim` of light-node rewards through the same signer; nothing re-delegates from the server. Both keyrings can sign with the key they hold (it was a stub). Also: the operator wallet explained by the tool (`keys list`/`create`/`show`, `operator_address` validated), `api_addr`, and `QORECHAIN_RPC_ADDR` finally honoured.

**v3.1.1** — aligned with `qorechain-core@v3.0.2`. Adds a PQC regression test suite (keygen, sign, verify, and tamper-detection) that guards the v3.0.2 signature-verification fix, and runs it in CI. No runtime behaviour change from v3.1.0.

Light node client for the QoreChain network. Provides two editions:

- **SX (Server eXperience)** — headless daemon with CLI for server deployments
- **UX (User eXperience)** — embedded web dashboard with CLI for desktop use

## Features

- Header verification via skipping verification light client
- Delegated staking with multi-validator split
- Auto-compound rewards with configurable intervals
- Reputation-aware validator rebalancing
- Real-time network telemetry (validators, consensus, bridge, tokenomics)
- On-chain registration with heartbeat liveness proofs
- 3% block reward eligibility for active light nodes
- Post-quantum cryptography support (Dilithium-5)
- **Interactive onboarding wizard** that runs a PQC self-test, accepts the chain RPC endpoint, and imports or generates a Dilithium-5 validator key
- **Local-only mode** so the node can prove its PQC stack works even before the chain itself is deployed
- **Live PQC self-test** — `lightnode-sx selftest` runs keygen -> sign -> verify -> tamper-detection in under a second

## Quick Start

### First-time setup

Registering a node needs a `lightnode_operator` licence on the operator account (bought on the dashboard, granted on chain), at least 1,000 QOR delegated from that account to any bonded validator, and a few QOR for fees. Nothing else needs installing: the node signs its own transactions on Linux, macOS and Windows.

```bash
# 1. The keys are stored encrypted; keep this set for every command below
export QORE_LIGHTNODE_KEYRING_PASSPHRASE='choose-a-passphrase'

# 2. Self-test, endpoint (mainnet: https://rpc.qore.host), node key
lightnode-sx onboard

# 3. The operator account: the one the licence is for (--recover reads its
#    words from standard input), or a new one (its 24 words are shown once)
lightnode-sx keys add-operator --recover

# 4. Attaches the node key as the account's post-quantum key, delegates the
#    missing stake if you name a validator, registers. Each step is confirmed
#    in a block before the next.
lightnode-sx register --delegate-to qorvaloper1...

# 5. Heartbeats run on their own from here, every ~1,000 blocks
lightnode-sx start
lightnode-sx status
```

`register` checks the licence first and says where to get one if it is missing. The UX build (`lightnode-ux`) has the same commands and shares the same home directory. An operator whose account key stays in a qorechaind keyring can skip step 3 and run `register --print` for the equivalent qorechaind commands.

The wizard asks for:
- **Chain RPC endpoint** - the chain id is read from it. Leave it blank to run in local-only mode.
- **Private key** - paste a hex-encoded Dilithium-5 key, or type `g` to generate a fresh one on this node.

### Build from Source

```bash
# SX edition (server daemon)
CGO_ENABLED=0 go build -o build/lightnode-sx ./cmd/lightnode-sx/

# UX edition (dashboard + daemon)
CGO_ENABLED=0 go build -o build/lightnode-ux ./cmd/lightnode-ux/
```

### Docker

```bash
docker compose up lightnode-sx    # server edition
docker compose up lightnode-ux    # dashboard edition (port 8420)
```

### Updates

A running node follows its network's releases on its own (`[update] mode` in `config.toml`: `auto`, `notify` or `off`). To check or update by hand:

```bash
lightnode-sx update --check   # is there a newer release?
lightnode-sx update           # verify, install, keep the old binary as .old; then restart the service
```

Docker nodes are told about a new release but do not modify the image: pull the new one and recreate the container.

### Verify the PQC stack any time

```bash
lightnode-sx selftest
```

Runs 5 checks: keygen, sign, verify-valid, reject-tampered-sig, reject-tampered-msg.

## Additional Documentation

- [Installation Guide](./INSTALLATION_GUIDE.md)
- [FAQ](./FAQ.md)
- [Community Notes](./COMMUNITY_NOTES.md)
- [Operator Journal](./OPERATOR_JOURNAL.md)
- [Example Configuration](./config.example.toml)

## Configuration

See `config.example.toml` for available example options. Always verify network values against current official QoreChain sources before using them in production.

## License

Apache 2.0
