package events_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/discordia-grupo01/servers/internal/events"
)

// unreachableURL points at a port nothing listens on, so amqp.Dial fails
// fast with "connection refused" instead of hanging.
const unreachableURL = "amqp://guest:guest@127.0.0.1:1/"

// blackHoleURL points at a TEST-NET-1 address (RFC 5737, 192.0.2.0/24):
// reserved for documentation/testing, guaranteed to never be routed to a
// real host. Unlike unreachableURL (which fails fast with "connection
// refused"), connecting here goes silent -- no RST, no response -- which is
// exactly the kind of network partition that makes an unbounded amqp.Dial
// hang instead of failing quickly.
const blackHoleURL = "amqp://guest:guest@192.0.2.1:5672/"

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

// RF4 / CA3: if RabbitMQ is unreachable in a way that doesn't fail fast
// (a network partition, not a refused connection), Publish must still
// return within a bounded, short time -- not stall the caller's HTTP
// response for however long the OS's default TCP connect timeout is.
func TestPublish_NetworkPartition_BoundedLatency(t *testing.T) {
	p := events.NewAMQPPublisher(blackHoleURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	if err := p.Publish(ctx, "servers.member_joined", map[string]string{"a": "b"}); err != nil {
		t.Fatalf("Publish() error = %v, want nil (best-effort)", err)
	}
	elapsed := time.Since(start)

	// The dial timeout inside connect() is 3s; give it generous headroom
	// (well under the 30s that amqp.Dial's own default would allow) so the
	// test isn't flaky on a slow CI runner while still catching a
	// regression back to the unbounded default.
	const maxAcceptable = 8 * time.Second
	if elapsed > maxAcceptable {
		t.Errorf("Publish() took %v against an unresponsive host, want <= %v", elapsed, maxAcceptable)
	}
}

// CB: concurrent Publish calls against a shared AMQPPublisher must not race
// on its internal connection/channel state, and each call must still return
// nil (best-effort) independently of the others. Run with -race to catch
// data races on the mutex-protected conn/ch fields.
func TestPublish_ConcurrentCalls_NoRace(t *testing.T) {
	p := events.NewAMQPPublisher(unreachableURL)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs[i] = p.Publish(ctx, "servers.member_joined", map[string]string{
				"user_id": fmt.Sprintf("user-%d", i),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Publish() call %d error = %v, want nil", i, err)
		}
	}
}
