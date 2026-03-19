# QoreChain Light Node

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

## Quick Start

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

### Initialize and Start

```bash
# Generate keys
lightnode-sx keys create --name operator

# Register on-chain
lightnode-sx register --node-type sx --version 1.15.0

# Start daemon
lightnode-sx start --config config.toml
```

## Configuration

See `config.example.toml` for all available options.

## License

Apache 2.0
