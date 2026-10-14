# QoreChain Light Node

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

```bash
# 1. Build (or pull the Docker image)
CGO_ENABLED=1 go build -o build/lightnode-sx ./cmd/lightnode-sx/

# 2. Run the onboarding wizard — PQC self-test + endpoint + key prompts
build/lightnode-sx onboard

# 3. Start the daemon
build/lightnode-sx start
```

The wizard asks for:
- **Chain RPC endpoint** — paste the official RPC URL when available, or leave blank to run in local-only mode while the chain itself is still being deployed.
- **Private key** — paste a hex-encoded Dilithium-5 key, or type `g` to generate a fresh one on this node.

If you leave the endpoint blank the daemon will start in **local-only mode** — the PQC stack is fully exercised and the web dashboard shows a banner explaining the state. Re-run `onboard` once the chain is live to point at a real RPC.

### Build from Source

```bash
# SX edition (server daemon)
CGO_ENABLED=1 go build -o build/lightnode-sx ./cmd/lightnode-sx/

# UX edition (dashboard + daemon)
CGO_ENABLED=1 go build -o build/lightnode-ux ./cmd/lightnode-ux/
```

### Docker

```bash
docker compose up lightnode-sx    # server edition
docker compose up lightnode-ux    # dashboard edition (port 8080)
```

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
