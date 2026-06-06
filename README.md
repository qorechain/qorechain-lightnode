# QoreChain Light Node

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
