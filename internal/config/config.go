package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	Environment string
	Host        string
	Port        string

	DatabaseHost     string
	DatabasePort     string
	DatabaseName     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseSSLMode  string
	DatabaseSchema   string
	// DatabaseMaxConns caps the connection pool. 0 keeps pgx's default,
	// max(4, number of CPUs), which is small for a service that answers many
	// polling clients; mind the connection limit of the database (or its
	// pooler) before raising it, since every instance opens that many.
	DatabaseMaxConns int

	UserIDHeader string

	IconDir      string
	IconMaxBytes int64

	SupabaseStorageURL    string
	SupabaseStorageBucket string
	SupabaseServiceKey    string

	RabbitMQURL string

	// AdminEventsHMACSecret is shared with discordia-admin: it signs the
	// admin.user_suspended / admin.user_reactivated events and this service
	// drops any whose signature doesn't verify. Required when RabbitMQURL is set.
	AdminEventsHMACSecret string

	// BillingEventsHMACSecret is shared with billing: it signs the boost saga in
	// both directions. Required when RabbitMQURL is set.
	BillingEventsHMACSecret string
}

func Load() (Config, error) {
	c := Config{
		Environment: getenv("ENVIRONMENT", "development"),
		Host:        getenv("HOST", "0.0.0.0"),
		Port:        getenv("PORT", "8080"),

		DatabaseHost:     os.Getenv("DATABASE_HOST"),
		DatabasePort:     os.Getenv("DATABASE_PORT"),
		DatabaseName:     os.Getenv("DATABASE_NAME"),
		DatabaseUser:     os.Getenv("DATABASE_USER"),
		DatabasePassword: os.Getenv("DATABASE_PASSWORD"),
		DatabaseSSLMode:  getenv("DATABASE_SSLMODE", "disable"),
		DatabaseSchema:   os.Getenv("DATABASE_SCHEMA"),
		DatabaseMaxConns: int(getenvInt64("DATABASE_MAX_CONNS", 0)),

		UserIDHeader: getenv("USER_ID_HEADER", ""),

		IconDir:      getenv("ICON_DIR", "./data/icons"),
		IconMaxBytes: getenvInt64("ICON_MAX_BYTES", 5*1024*1024),

		SupabaseStorageURL:    os.Getenv("SUPABASE_STORAGE_URL"),
		SupabaseStorageBucket: os.Getenv("SUPABASE_STORAGE_BUCKET"),
		SupabaseServiceKey:    os.Getenv("SUPABASE_SERVICE_KEY"),

		RabbitMQURL: os.Getenv("RABBITMQ_URL"),

		AdminEventsHMACSecret:   os.Getenv("ADMIN_EVENTS_HMAC_SECRET"),
		BillingEventsHMACSecret: os.Getenv("BILLING_EVENTS_HMAC_SECRET"),
	}

	var missing []string
	for k, v := range map[string]string{
		"DATABASE_HOST":     c.DatabaseHost,
		"DATABASE_PORT":     c.DatabasePort,
		"DATABASE_NAME":     c.DatabaseName,
		"DATABASE_USER":     c.DatabaseUser,
		"DATABASE_PASSWORD": c.DatabasePassword,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if c.RabbitMQURL != "" && c.AdminEventsHMACSecret == "" {
		missing = append(missing, "ADMIN_EVENTS_HMAC_SECRET (required when RABBITMQ_URL is set)")
	}
	if c.RabbitMQURL != "" && c.BillingEventsHMACSecret == "" {
		missing = append(missing, "BILLING_EVENTS_HMAC_SECRET (required when RABBITMQ_URL is set)")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return c, nil
}

func (c Config) PostgresDSN() string {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		c.DatabaseHost, c.DatabasePort, c.DatabaseName, c.DatabaseUser, c.DatabasePassword, c.DatabaseSSLMode,
	)
	if c.DatabaseSchema != "" {
		dsn += fmt.Sprintf(" options='-c search_path=%s'", c.DatabaseSchema)
	}
	if c.DatabaseMaxConns > 0 {
		dsn += fmt.Sprintf(" pool_max_conns=%d", c.DatabaseMaxConns)
	}
	return dsn
}

func (c Config) IsProduction() bool { return c.Environment == "production" }

func (c Config) UseSupabaseStorage() bool {
	return c.SupabaseStorageURL != "" && c.SupabaseStorageBucket != "" && c.SupabaseServiceKey != ""
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
