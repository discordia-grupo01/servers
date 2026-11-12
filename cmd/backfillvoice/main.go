package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/voicebackfill"
)

type countingPublisher struct{}

func (countingPublisher) Publish(context.Context, any) error { return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fs := flag.NewFlagSet("backfillvoice", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "cuenta eventos sin publicarlos")
	queue := fs.String("queue", "", "cola exacta de la réplica (por defecto VOICE_EVENTS_QUEUE.VOICE_INSTANCE_ID.replica)")
	instanceID := fs.String("instance-id", "", "identificador de la instancia de voice (sobrescribe VOICE_INSTANCE_ID)")
	fs.Parse(os.Args[1:])

	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitURL == "" && !*dryRun {
		fmt.Fprintln(os.Stderr, "backfillvoice: RABBITMQ_URL es obligatorio salvo con --dry-run")
		os.Exit(1)
	}

	pool, err := repository.NewPool(ctx, databaseDSN())
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfillvoice: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	targetQueue := resolveVoiceQueue(*queue, *instanceID)

	var publisher voicebackfill.Publisher = countingPublisher{}
	var amqpPublisher *voicebackfill.AMQPPublisher
	if !*dryRun {
		amqpPublisher, err = voicebackfill.NewAMQPPublisher(rabbitURL, targetQueue)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backfillvoice: %v\n", err)
			os.Exit(1)
		}
		defer amqpPublisher.Close()
		publisher = amqpPublisher
	}

	counts, err := voicebackfill.Run(ctx, pool, publisher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfillvoice: %v\n", err)
		os.Exit(1)
	}
	prefix := ""
	if *dryRun {
		prefix = "[dry-run] "
	}
	fmt.Printf("%scola %q: %d eventos (canales=%d roles=%d dueños=%d miembros=%d overrides=%d mutes=%d)\n",
		prefix, targetQueue, counts.Total(), counts.Channels, counts.Roles, counts.Owners,
		counts.Members, counts.Overrides, counts.Mutes)
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func resolveVoiceQueue(queue, instanceID string) string {
	if queue = strings.TrimSpace(queue); queue != "" {
		return queue
	}
	if instanceID = strings.TrimSpace(instanceID); instanceID == "" {
		instanceID = getenv("VOICE_INSTANCE_ID", "default")
	}
	return getenv("VOICE_EVENTS_QUEUE", "voice.signaling") + "." + instanceID + ".replica"
}

func databaseDSN() string {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		requiredEnv("DATABASE_HOST"),
		getenv("DATABASE_PORT", "5432"),
		getenv("DATABASE_NAME", "discordia_servicio"),
		getenv("DATABASE_USER", "discordia"),
		requiredEnv("DATABASE_PASSWORD"),
		getenv("DATABASE_SSLMODE", "disable"),
	)
	if schema := strings.TrimSpace(os.Getenv("DATABASE_SCHEMA")); schema != "" {
		dsn += fmt.Sprintf(" options='-c search_path=%s'", schema)
	}
	return dsn
}

func requiredEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		fmt.Fprintf(os.Stderr, "backfillvoice: falta %s\n", key)
		os.Exit(1)
	}
	return value
}
