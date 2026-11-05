package admin_consumer

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/admin"
	"github.com/discordia-grupo01/events-contract/envelope"
)

// ServerStore is where the moderation state of a server is written. Apply must
// store it only if version is newer than the one stored, and report whether it
// did. suspendedAt is when the server was suspended; nil means reactivated.
type ServerStore interface {
	ApplyServerModeration(ctx context.Context, serverID uuid.UUID, suspendedAt *time.Time, version int64) (bool, error)
}

// ServerDeleter runs the deletion that staff asked for. It must be idempotent:
// a server that no longer exists is not an error.
type ServerDeleter interface {
	DeleteServerByStaff(ctx context.Context, serverID uuid.UUID) error
}

// ServerCache is told that some server's state changed, so whatever caches it
// (the server suspension middleware) forgets it.
type ServerCache interface {
	Reset()
}

// ServerHandlers is what the consumer needs to act on admin.server_* events.
// Cache is optional.
type ServerHandlers struct {
	Store   ServerStore
	Deleter ServerDeleter
	Cache   ServerCache
}

// serverRoutingKeys are the admin.server_* events this consumer binds when it
// is given ServerHandlers.
var serverRoutingKeys = []string{
	admin.RoutingKeyServerSuspended,
	admin.RoutingKeyServerReactivated,
	admin.RoutingKeyServerDeletionRequested,
}

func isServerRoutingKey(key string) bool {
	return slices.Contains(serverRoutingKeys, key)
}

// ShouldRequeueServerEvent processes one admin.server_* message and reports
// whether it must be retried (true) or acknowledged (false). Like
// ShouldRequeue, a message that can never succeed (unknown routing key,
// malformed JSON, missing or invalid signature, a server_id that is not a UUID,
// version < 1) is dropped with a log and changes nothing; only a database
// failure is retried. The signature is checked before anything else, because a
// forged admin.server_deletion_requested would delete a server for good.
//
// A stale or duplicate suspension, and a deletion of a server that no longer
// exists, are no-ops.
func ShouldRequeueServerEvent(ctx context.Context, h ServerHandlers, secret, routingKey string, body []byte) bool {
	var (
		meta     envelope.Meta
		rawID    string
		version  int64
		verified bool
	)
	switch routingKey {
	case admin.RoutingKeyServerSuspended:
		var evt admin.ServerSuspended
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("admin_consumer: malformed payload, dropping message: %v", err)
			return false
		}
		meta, rawID, version, verified = evt.Meta, evt.ServerID, evt.Version, evt.Verify(secret)
	case admin.RoutingKeyServerReactivated:
		var evt admin.ServerReactivated
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("admin_consumer: malformed payload, dropping message: %v", err)
			return false
		}
		meta, rawID, version, verified = evt.Meta, evt.ServerID, evt.Version, evt.Verify(secret)
	case admin.RoutingKeyServerDeletionRequested:
		var evt admin.ServerDeletionRequested
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("admin_consumer: malformed payload, dropping message: %v", err)
			return false
		}
		meta, rawID, version, verified = evt.Meta, evt.ServerID, evt.Version, evt.Verify(secret)
	default:
		log.Printf("admin_consumer: unknown routing key %q, dropping message", routingKey)
		return false
	}

	if !verified {
		log.Printf("admin_consumer: WARN invalid or missing signature, routing_key=%s event_id=%s server_id=%s, dropping message", routingKey, meta.EventID, rawID)
		return false
	}
	serverID, err := uuid.Parse(rawID)
	if err != nil {
		log.Printf("admin_consumer: invalid server_id %q, event_id=%s, dropping message", rawID, meta.EventID)
		return false
	}
	if version < 1 {
		log.Printf("admin_consumer: invalid version %d, event_id=%s server_id=%s, dropping message", version, meta.EventID, serverID)
		return false
	}

	if routingKey == admin.RoutingKeyServerDeletionRequested {
		return deleteServer(ctx, h, serverID, meta)
	}
	return applyModeration(ctx, h, routingKey, serverID, version, meta)
}

func deleteServer(ctx context.Context, h ServerHandlers, serverID uuid.UUID, meta envelope.Meta) bool {
	if err := h.Deleter.DeleteServerByStaff(ctx, serverID); err != nil {
		log.Printf("admin_consumer: failed to delete server, event_id=%s server_id=%s, retrying: %v", meta.EventID, serverID, err)
		return true
	}
	log.Printf("admin_consumer: processed %s, event_id=%s server_id=%s", admin.RoutingKeyServerDeletionRequested, meta.EventID, serverID)
	return false
}

func applyModeration(ctx context.Context, h ServerHandlers, routingKey string, serverID uuid.UUID, version int64, meta envelope.Meta) bool {
	var suspendedAt *time.Time
	if routingKey == admin.RoutingKeyServerSuspended {
		at := meta.OccurredAt.UTC()
		suspendedAt = &at
	}

	applied, err := h.Store.ApplyServerModeration(ctx, serverID, suspendedAt, version)
	if err != nil {
		log.Printf("admin_consumer: failed to store server moderation, event_id=%s server_id=%s, retrying: %v", meta.EventID, serverID, err)
		return true
	}
	if !applied {
		log.Printf("admin_consumer: ignored stale or unknown-server event, event_id=%s server_id=%s version=%d", meta.EventID, serverID, version)
		return false
	}
	if h.Cache != nil {
		h.Cache.Reset()
	}
	log.Printf("admin_consumer: applied %s, event_id=%s server_id=%s version=%d", routingKey, meta.EventID, serverID, version)
	return false
}
