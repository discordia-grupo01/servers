package config_test

import (
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/config"
)

func setDB(t *testing.T) {
	t.Setenv("DATABASE_HOST", "db")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_NAME", "svc")
	t.Setenv("DATABASE_USER", "u")
	t.Setenv("DATABASE_PASSWORD", "p")
	// CI exports RABBITMQ_URL for the integration tests; Load would then
	// demand ADMIN_EVENTS_HMAC_SECRET too.
	t.Setenv("RABBITMQ_URL", "")
}

func TestLoad_OK(t *testing.T) {
	setDB(t)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if c.Environment != "development" || c.Port != "8080" || c.Host != "0.0.0.0" {
		t.Errorf("operational defaults wrong: %+v", c)
	}
	if c.UserIDHeader != "" || c.IconMaxBytes != 5*1024*1024 {
		t.Errorf("config = %+v", c)
	}
	dsn := c.PostgresDSN()
	for _, want := range []string{"host=db", "dbname=svc", "user=u", "password=p", "sslmode=disable"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN %q missing %q", dsn, want)
		}
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	setDB(t)
	t.Setenv("DATABASE_PASSWORD", "")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_PASSWORD") {
		t.Fatalf("expected error mentioning DATABASE_PASSWORD, got: %v", err)
	}
}

func TestLoad_Overrides(t *testing.T) {
	setDB(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("PORT", "9000")
	t.Setenv("ICON_MAX_BYTES", "123")
	t.Setenv("USER_ID_HEADER", "X-Uid")

	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsProduction() || c.Port != "9000" || c.IconMaxBytes != 123 || c.UserIDHeader != "X-Uid" {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoad_InvalidIconMaxBytesFallsBackToDefault(t *testing.T) {
	setDB(t)
	t.Setenv("ICON_MAX_BYTES", "abc")
	c, _ := config.Load()
	if c.IconMaxBytes != 5*1024*1024 {
		t.Errorf("IconMaxBytes = %d", c.IconMaxBytes)
	}
}

func TestLoad_RabbitMQWithoutAdminSecretFails(t *testing.T) {
	setDB(t)
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ADMIN_EVENTS_HMAC_SECRET", "")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "ADMIN_EVENTS_HMAC_SECRET") {
		t.Fatalf("expected error mentioning ADMIN_EVENTS_HMAC_SECRET, got: %v", err)
	}
}

func TestLoad_RabbitMQWithAdminSecret(t *testing.T) {
	setDB(t)
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ADMIN_EVENTS_HMAC_SECRET", "s3cret")
	t.Setenv("BILLING_EVENTS_HMAC_SECRET", "b1lling")
	c, err := config.Load()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if c.AdminEventsHMACSecret != "s3cret" {
		t.Errorf("AdminEventsHMACSecret = %q", c.AdminEventsHMACSecret)
	}
	if c.BillingEventsHMACSecret != "b1lling" {
		t.Errorf("BillingEventsHMACSecret = %q", c.BillingEventsHMACSecret)
	}
}

func TestLoad_RabbitMQWithoutBillingSecretFails(t *testing.T) {
	setDB(t)
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ADMIN_EVENTS_HMAC_SECRET", "s3cret")
	t.Setenv("BILLING_EVENTS_HMAC_SECRET", "")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "BILLING_EVENTS_HMAC_SECRET") {
		t.Fatalf("expected error mentioning BILLING_EVENTS_HMAC_SECRET, got: %v", err)
	}
}

func TestLoad_NoRabbitMQDoesNotNeedAdminSecret(t *testing.T) {
	setDB(t)
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("ADMIN_EVENTS_HMAC_SECRET", "")
	if _, err := config.Load(); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestLoad_DatabaseMaxConns_DefaultsToPgxAndCapsThePoolWhenSet(t *testing.T) {
	setDB(t)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if c.DatabaseMaxConns != 0 || strings.Contains(c.PostgresDSN(), "pool_max_conns") {
		t.Errorf("unset must leave pgx's default: %+v dsn=%q", c, c.PostgresDSN())
	}

	t.Setenv("DATABASE_MAX_CONNS", "12")
	c, err = config.Load()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(c.PostgresDSN(), "pool_max_conns=12") {
		t.Errorf("DSN %q missing pool_max_conns=12", c.PostgresDSN())
	}
}
