package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
)

func TestProbeSettingsMigration_PreservesExistingSettings(t *testing.T) {
	db, err := OpenDB(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE schema_migrations (version uint64 NOT NULL PRIMARY KEY, dirty bool NOT NULL); INSERT INTO schema_migrations VALUES (9,0); CREATE TABLE system_config (config_json TEXT NOT NULL); INSERT INTO system_config VALUES ('{"request_log_enabled":false,"max_consecutive_failures":7}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = MigrateStateDB(db); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = db.QueryRow("SELECT config_json FROM system_config").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cfg config.RuntimeConfig
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.RequestLogEnabled || cfg.MaxConsecutiveFailures != 7 || !cfg.ProbeEnabled || cfg.ProbeConcurrency != 32 || time.Duration(cfg.ProbeTimeout) != 15*time.Second || cfg.EgressAttributesProvider != "check-host" {
		t.Fatalf("settings migration lost data or defaults: %+v", cfg)
	}
}
