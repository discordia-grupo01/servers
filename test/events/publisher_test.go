package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/discordia-grupo01/servers/internal/events"
)

// unreachableURL points at a port nothing listens on, so amqp.Dial fails
// fast with "connection refused" instead of hanging.
const unreachableURL = "amqp://guest:guest@127.0.0.1:1/"

// CA: RABBITMQ_URL vacia -> publisher no-op desde el arranque, Publish no
// hace nada y no devuelve error.
func TestNewAMQPPublisher_NoURL_IsNoOp(t *testing.T) {
	p := events.NewAMQPPublisher("")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := p.Publish(ctx, "servers.member_joined", map[string]string{"a": "b"}); err != nil {
		t.Fatalf("Publish() error = %v, want nil (no-op)", err)
	}
}

// CA: fallo de conexion al construir el publisher no es fatal -- el
// constructor devuelve un publisher usable en vez de abortar el proceso.
func TestNewAMQPPublisher_ConnectFailure_NotFatal(t *testing.T) {
	p := events.NewAMQPPublisher(unreachableURL)
	if p == nil {
		t.Fatal("NewAMQPPublisher() = nil, want a usable publisher even if the initial connect failed")
	}
}

// CA: fallo de conexion/publish puntual se loguea, la llamada sigue
// devolviendo nil (best-effort) para no afectar al caller.
func TestPublish_ConnectionFailure_NeverErrors(t *testing.T) {
	p := events.NewAMQPPublisher(unreachableURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := p.Publish(ctx, "servers.member_left", map[string]string{"server_id": "s1"}); err != nil {
		t.Fatalf("Publish() error = %v, want nil (best-effort)", err)
	}
}

// CA: reconexion simple -- tras un fallo, la siguiente llamada a Publish
// vuelve a intentar conectar en lugar de quedar en un estado roto para
// siempre (no debe paniquear ni bloquear).
func TestPublish_RetriesConnectionOnNextCall(t *testing.T) {
	p := events.NewAMQPPublisher(unreachableURL)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		if err := p.Publish(ctx, "servers.member_joined", map[string]int{"attempt": i}); err != nil {
			t.Fatalf("Publish() call %d error = %v, want nil", i, err)
		}
	}
}
