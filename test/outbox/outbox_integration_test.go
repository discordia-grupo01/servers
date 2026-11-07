package outbox_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS outbox_events`) })

	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS outbox_events`)
	for _, name := range []string{"0011_outbox_events.up.sql", "0033_outbox_traceparent.up.sql"} {
		schema, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}

func tracing(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return spans
}

func insert(t *testing.T, pool *pgxpool.Pool, ctx context.Context, routingKey string) {
	t.Helper()
	evt, err := outbox.New(routingKey, map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return outbox.Insert(ctx, tx, evt) }); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

func TestInsertStoresTheTraceparentOfTheRequest(t *testing.T) {
	tracing(t)
	pool := newPool(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "request")
	defer span.End()

	insert(t, pool, ctx, "servers.with_trace")

	var stored *string
	if err := pool.QueryRow(ctx, `SELECT traceparent FROM outbox_events WHERE routing_key = 'servers.with_trace'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	want := "00-" + span.SpanContext().TraceID().String() + "-" + span.SpanContext().SpanID().String() + "-01"
	if stored == nil || *stored != want {
		t.Errorf("traceparent guardado: %v, quería %q", stored, want)
	}
}

func TestInsertWithoutATraceStoresNull(t *testing.T) {
	tracing(t)
	pool := newPool(t)

	insert(t, pool, context.Background(), "servers.without_trace")

	var stored *string
	if err := pool.QueryRow(context.Background(), `SELECT traceparent FROM outbox_events WHERE routing_key = 'servers.without_trace'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != nil {
		t.Errorf("sin traza debería ser NULL, fue %q", *stored)
	}
}

type capturingPublisher struct {
	published chan trace.SpanContext
}

func (p capturingPublisher) PublishOrError(ctx context.Context, _ string, _ any) error {
	p.published <- trace.SpanContextFromContext(ctx)
	return nil
}

func TestPollerPublishesInsideTheTraceOfTheRequest(t *testing.T) {
	spans := tracing(t)
	pool := newPool(t)
	ctx, request := otel.Tracer("test").Start(context.Background(), "request")
	insert(t, pool, ctx, "servers.traced")
	request.End()

	publisher := capturingPublisher{published: make(chan trace.SpanContext, 1)}
	pollCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.NewPoller(pool, publisher).Start(pollCtx)

	select {
	case got := <-publisher.published:
		if got.TraceID() != request.SpanContext().TraceID() {
			t.Errorf("el publicador debería continuar la traza del pedido: %s", got.TraceID())
		}
	case <-time.After(2*outbox.PollInterval + 3*time.Second):
		t.Fatal("el poller no publicó el evento")
	}

	var producer sdktrace.ReadOnlySpan
	for _, span := range spans.Ended() {
		if span.Name() == "outbox publish servers.traced" {
			producer = span
		}
	}
	if producer == nil {
		t.Fatal("falta el span de publicación")
	}
	if producer.SpanKind() != trace.SpanKindProducer || producer.Parent().SpanID() != request.SpanContext().SpanID() {
		t.Errorf("el span de publicación debería colgar del pedido: %v", producer.Parent())
	}

	var publishedAt *time.Time
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = pool.QueryRow(context.Background(), `SELECT published_at FROM outbox_events WHERE routing_key = 'servers.traced'`).Scan(&publishedAt)
		if publishedAt != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if publishedAt == nil {
		t.Error("el evento debería quedar marcado como publicado")
	}
}

func TestPollerPublishesEventsThatHaveNoTrace(t *testing.T) {
	tracing(t)
	pool := newPool(t)
	insert(t, pool, context.Background(), "servers.untraced")

	publisher := capturingPublisher{published: make(chan trace.SpanContext, 1)}
	pollCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.NewPoller(pool, publisher).Start(pollCtx)

	select {
	case <-publisher.published:
	case <-time.After(2*outbox.PollInterval + 3*time.Second):
		t.Fatal("el poller no publicó un evento sin traza")
	}
}
