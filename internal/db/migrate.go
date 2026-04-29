package db

import (
	"fmt"
)

func (l *LocalDB) Migrate() error {
	currentVersion, err := l.getSchemaVersion()
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	if currentVersion >= SchemaVersion {
		return nil
	}

	// Fresh install: apply full schema
	if currentVersion == 0 {
		if _, err := l.Exec(SchemaSQL); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		if err := l.setSchemaVersion(SchemaVersion); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		return nil
	}

	// Incremental migrations
	if currentVersion == 1 {
		if _, err := l.Exec(MigrationV1ToV2); err != nil {
			return fmt.Errorf("migrate v1 to v2: %w", err)
		}
		currentVersion = 2
	}
	if currentVersion == 2 {
		if _, err := l.Exec(MigrationV2ToV3); err != nil {
			return fmt.Errorf("migrate v2 to v3: %w", err)
		}
		currentVersion = 3
	}
	if currentVersion == 3 {
		if _, err := l.Exec(MigrationV3ToV4); err != nil {
			return fmt.Errorf("migrate v3 to v4: %w", err)
		}
		currentVersion = 4
	}
	if currentVersion == 4 {
		if _, err := l.Exec(MigrationV4ToV5); err != nil {
			return fmt.Errorf("migrate v4 to v5: %w", err)
		}
	}

	return nil
}

func (l *LocalDB) getSchemaVersion() (int, error) {
	var tableExists int
	err := l.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='schema_version'
	`).Scan(&tableExists)
	if err != nil {
		return 0, err
	}

	if tableExists == 0 {
		return 0, nil
	}

	var version int
	err = l.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

func (l *LocalDB) setSchemaVersion(version int) error {
	_, err := l.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version)
	return err
}
