package db

const schemaVersion = 1

func (d *DB) migrate() error {
	// Create schema version table
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		)
	`)
	if err != nil {
		return err
	}

	var current int
	row := d.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	row.Scan(&current)

	if current < 1 {
		if err := d.migrateV1(); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) migrateV1() error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		// Verified headers from light client
		`CREATE TABLE IF NOT EXISTS headers (
			height INTEGER PRIMARY KEY,
			hash TEXT NOT NULL,
			time TEXT NOT NULL,
			validator_hash TEXT NOT NULL
		)`,

		// Delegation state
		`CREATE TABLE IF NOT EXISTS delegations (
			validator TEXT PRIMARY KEY,
			amount TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,

		// Reward history
		`CREATE TABLE IF NOT EXISTS rewards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			amount TEXT NOT NULL,
			height INTEGER NOT NULL,
			claimed_at TEXT
		)`,

		// Validator telemetry
		`CREATE TABLE IF NOT EXISTS telemetry_validators (
			address TEXT PRIMARY KEY,
			moniker TEXT,
			uptime REAL NOT NULL DEFAULT 0,
			reputation_composite REAL NOT NULL DEFAULT 0,
			reputation_stake REAL NOT NULL DEFAULT 0,
			reputation_perf REAL NOT NULL DEFAULT 0,
			reputation_contrib REAL NOT NULL DEFAULT 0,
			reputation_time REAL NOT NULL DEFAULT 0,
			pool TEXT NOT NULL DEFAULT '',
			jailed INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,

		// Network / RL consensus telemetry
		`CREATE TABLE IF NOT EXISTS telemetry_network (
			height INTEGER PRIMARY KEY,
			timestamp TEXT NOT NULL,
			block_time_ms INTEGER,
			tx_count INTEGER,
			validator_count INTEGER,
			active_set_size INTEGER,
			total_stake TEXT,
			inflation_rate TEXT,
			gas_price TEXT,
			rl_block_size INTEGER,
			rl_gas_limit INTEGER,
			rl_reward REAL,
			rl_epoch INTEGER
		)`,

		// Bridge status
		`CREATE TABLE IF NOT EXISTS telemetry_bridge (
			chain TEXT PRIMARY KEY,
			chain_type TEXT NOT NULL,
			status TEXT NOT NULL,
			pending_transfers INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,

		// Tokenomics
		`CREATE TABLE IF NOT EXISTS telemetry_tokenomics (
			height INTEGER PRIMARY KEY,
			total_burned TEXT,
			inflation_rate TEXT,
			xqore_tvl TEXT,
			total_supply TEXT,
			staking_ratio TEXT,
			updated_at TEXT NOT NULL
		)`,

		// Slashing events
		`CREATE TABLE IF NOT EXISTS slashing_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			validator TEXT NOT NULL,
			height INTEGER NOT NULL,
			type TEXT NOT NULL,
			amount TEXT,
			detected_at TEXT NOT NULL
		)`,

		// Key-value store for light node state
		`CREATE TABLE IF NOT EXISTS light_node_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,

		// Record schema version
		`INSERT INTO schema_version (version) VALUES (1)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	return tx.Commit()
}
