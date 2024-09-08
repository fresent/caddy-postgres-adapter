package postgresadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	_ "github.com/lib/pq"
)

func init() {
	caddyconfig.RegisterAdapter("postgres", Adapter{})
}

type PostgresAdapterConfig struct {
	QueryTimeout     time.Duration `json:"query_timeout,omitempty"`
	LockTimeout      time.Duration `json:"lock_timeout,omitempty"`
	Hosts            string        `json:"hosts,omitempty"`
	Port             string        `json:"port,omitempty"`
	User             string        `json:"user,omitempty"`
	Password         string        `json:"password,omitempty"`
	DBname           string        `json:"dbname,omitempty"`
	SSLmode          string        `json:"sslmode,omitempty"`
	ConnectionString string        `json:"connection_string,omitempty"`
	DisableDDL       bool          `json:"disable_ddl,omitempty"`
	TableNamePrefix  string        `json:"table_name_prefix,omitempty"`
	RefreshInterval  int64         `json:"refresh_interval,omitempty"`
}

var (
	dbs         []*sql.DB
	currentDBMu sync.Mutex
	currentDB   *sql.DB
)

var createTableSQL = `
CREATE TABLE IF NOT EXISTS %s (
	id SERIAL PRIMARY KEY,
	key VARCHAR(255) NOT NULL,
	value TEXT,
	enable BOOLEAN NOT NULL DEFAULT TRUE,
	created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS %s_key_idx ON %s (key);
`

var tableName = ""

var config_version = "0"

type Adapter struct{}

func getDb(postgresAdapterConfig PostgresAdapterConfig) ([]*sql.DB, error) {
	if len(dbs) > 0 {
		return dbs, nil
	}

	hosts := strings.Split(postgresAdapterConfig.Hosts, ",")
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		var connStr string
		if postgresAdapterConfig.ConnectionString != "" {
			connStr = strings.Replace(postgresAdapterConfig.ConnectionString, "{host}", host, 1)
		} else {
			connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
				host,
				postgresAdapterConfig.Port,
				postgresAdapterConfig.User,
				postgresAdapterConfig.Password,
				postgresAdapterConfig.DBname,
				postgresAdapterConfig.SSLmode)
		}

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			caddy.Log().Named("adapters.postgres.config").Error(fmt.Sprintf("Failed to open database connection to %s: %v", host, err))
			continue
		}

		// Test the connection
		ctx, cancel := context.WithTimeout(context.Background(), postgresAdapterConfig.QueryTimeout)
		if err = db.PingContext(ctx); err != nil {
			cancel()
			caddy.Log().Named("adapters.postgres.config").Error(fmt.Sprintf("Failed to ping database at %s: %v", host, err))
			db.Close()
			continue
		}
		cancel()

		db.SetConnMaxLifetime(time.Minute * 3)
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)

		dbs = append(dbs, db)
	}

	if len(dbs) == 0 {
		return nil, fmt.Errorf("failed to connect to any database host")
	}

	// Randomly select an initial database connection
	currentDB = dbs[rand.Intn(len(dbs))]

	tableName = postgresAdapterConfig.TableNamePrefix + "_CONFIG"

	if !postgresAdapterConfig.DisableDDL {
		ctx, cancel := context.WithTimeout(context.Background(), postgresAdapterConfig.QueryTimeout)
		defer cancel()

		_, err := currentDB.ExecContext(ctx, fmt.Sprintf(createTableSQL, tableName, tableName, tableName))
		if err != nil {
			caddy.Log().Named("adapters.postgres.config").Error(fmt.Sprintf("Create Table Error: %v", err))
			return dbs, fmt.Errorf("failed to create table: %w", err)
		}
	}

	return dbs, nil
}

func getNextDB() *sql.DB {
	currentDBMu.Lock()
	defer currentDBMu.Unlock()

	for i := 0; i < len(dbs); i++ {
		nextDB := dbs[(i+1)%len(dbs)]
		if err := nextDB.Ping(); err == nil {
			currentDB = nextDB
			return currentDB
		}
	}

	// If all databases fail, return the current one (which might also fail)
	return currentDB
}

func executeQuery(query string, args ...interface{}) (*sql.Rows, error) {
	var err error
	var rows *sql.Rows

	for attempts := 0; attempts < len(dbs); attempts++ {
		rows, err = currentDB.Query(query, args...)
		if err == nil {
			return rows, nil
		}

		caddy.Log().Named("adapters.postgres.query").Error(fmt.Sprintf("Query failed: %v. Trying next database.", err))
		currentDB = getNextDB()
	}

	return nil, fmt.Errorf("query failed on all database hosts: %w", err)
}

func executeQueryRow(query string, args ...interface{}) *sql.Row {
	return currentDB.QueryRow(query, args...)
}

func getValueFromDb(key string) (string, error) {
	var value string
	err := executeQueryRow("SELECT value FROM "+tableName+" WHERE key = $1 AND enable = TRUE ORDER BY created DESC LIMIT 1", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get value for key %s: %w", key, err)
	}
	return value, nil
}

func getValuesFromDb(key string) ([]string, error) {
	rows, err := executeQuery("SELECT value FROM "+tableName+" WHERE key = $1 AND enable = TRUE ORDER BY created DESC", key)
	if err != nil {
		return nil, fmt.Errorf("failed to query values for key %s: %w", key, err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("failed to scan value for key %s: %w", key, err)
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows for key %s: %w", key, err)
	}
	return values, nil
}

func getConfiguration() ([]byte, error) {
	config := caddy.Config{}

	configSections := []struct {
		key      string
		target   interface{}
		isRawMsg bool
	}{
		{"config", &config, false},
		{"config.admin", &config.Admin, false},
		{"config.logging", &config.Logging, false},
		{"config.storage", &config.StorageRaw, true},
		{"config.apps", &config.AppsRaw, true},
	}

	for _, section := range configSections {
		value, err := getValueFromDb(section.key)
		if err != nil {
			return nil, fmt.Errorf("error getting %s: %w", section.key, err)
		}
		if value != "" {
			if section.isRawMsg {
				err = json.Unmarshal([]byte(value), section.target)
			} else if section.target == nil {
				err = json.Unmarshal([]byte(value), &section.target)
			} else {
				err = json.Unmarshal([]byte(value), section.target)
			}
			if err != nil {
				return nil, fmt.Errorf("error unmarshaling %s: %w", section.key, err)
			}
		}
	}

	if config.AppsRaw != nil {
		if httpAppConfig, ok := config.AppsRaw["http"]; ok {
			var httpApp caddyhttp.App
			if err := json.Unmarshal(httpAppConfig, &httpApp); err != nil {
				return nil, fmt.Errorf("error unmarshaling http app config: %w", err)
			}

			httpAppChanged := false
			for serverKey, server := range httpApp.Servers {
				values, err := getValuesFromDb("config.apps.http.servers." + serverKey + ".routes")
				if err != nil {
					return nil, fmt.Errorf("error getting routes for server %s: %w", serverKey, err)
				}
				if len(values) > 0 {
					server.Routes = make([]caddyhttp.Route, 0, len(values))
					for _, routeJSON := range values {
						var route caddyhttp.Route
						if err := json.Unmarshal([]byte(routeJSON), &route); err != nil {
							return nil, fmt.Errorf("error unmarshaling route for server %s: %w", serverKey, err)
						}
						server.Routes = append(server.Routes, route)
					}
					httpApp.Servers[serverKey] = server
					httpAppChanged = true
				}
			}

			if httpAppChanged {
				var warnings []caddyconfig.Warning
				config.AppsRaw["http"] = caddyconfig.JSON(&httpApp, &warnings)
				if len(warnings) > 0 {
					caddy.Log().Named("adapters.postgres.config").Warn(fmt.Sprintf("warnings during JSON encoding of HTTP app: %v", warnings))
				}
			}
		}
	}

	return json.Marshal(config)
}

func (a Adapter) Adapt(body []byte, options map[string]interface{}) ([]byte, []caddyconfig.Warning, error) {
	postgresAdapterConfig := PostgresAdapterConfig{
		QueryTimeout:    time.Second * 3,
		LockTimeout:     time.Minute,
		TableNamePrefix: "CADDY",
		RefreshInterval: 100,
		SSLmode:         "disable",
	}

	if err := json.Unmarshal(body, &postgresAdapterConfig); err != nil {
		return nil, nil, fmt.Errorf("error unmarshaling adapter config: %w", err)
	}

	if postgresAdapterConfig.ConnectionString == "" && postgresAdapterConfig.Hosts == "" {
		return nil, nil, fmt.Errorf("either ConnectionString or Hosts must be provided")
	}

	var err error
	_, err = getDb(postgresAdapterConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting database connections: %w", err)
	}

	config, err := getConfiguration()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting configuration: %w", err)
	}

	config_version_new := getConfigVersion()
	config_version = config_version_new

	runCheckLoop(postgresAdapterConfig)
	return config, nil, nil
}

func getConfigVersion() string {
	caddy.Log().Named("adapters.postgres.checkloop").Debug("getConfigVersion")

	var version string
	err := executeQueryRow("SELECT value FROM "+tableName+" WHERE key = 'version' AND enable = TRUE ORDER BY created DESC LIMIT 1").Scan(&version)
	if err != nil {
		if err != sql.ErrNoRows {
			caddy.Log().Named("adapters.postgres.load").Error(fmt.Sprintf("Error getting config version: %v", err))
		}
		return config_version
	}
	return version
}

func refreshConfig(config_version_new string) {
	config, err := getConfiguration()
	if err != nil {
		caddy.Log().Named("adapters.postgres.refreshConfig").Error(fmt.Sprintf("Error refreshing config: %v", err))
		return
	}
	config_version = config_version_new
	if err := caddy.Load(config, false); err != nil {
		caddy.Log().Named("adapters.postgres.refreshConfig").Error(fmt.Sprintf("Error loading new config: %v", err))
	}
}

func checkAndRefreshConfig(postgresAdapterConfig PostgresAdapterConfig) {
	config_version_new := getConfigVersion()
	if config_version_new != config_version {
		refreshConfig(config_version_new)
	}
	caddy.Log().Named("adapters.postgres.checkloop").Debug(fmt.Sprintf("checkAndRefreshConfig config_version_new %s", config_version_new))
}

func runCheckLoop(postgresAdapterConfig PostgresAdapterConfig) {
	ticker := time.NewTicker(time.Second * time.Duration(postgresAdapterConfig.RefreshInterval))
	go func() {
		for {
			select {
			case <-ticker.C:
				caddy.Log().Named("adapters.postgres.checkloop").Debug(fmt.Sprintf("Checking config version %s", config_version))
				checkAndRefreshConfig(postgresAdapterConfig)
			case <-caddy.Context().Done():
				caddy.Log().Named("adapters.postgres.checkloop").Debug("Shutting down check loop")
				ticker.Stop()
				return
			}
		}
	}()
}

var _ caddyconfig.Adapter = (*Adapter)(nil)