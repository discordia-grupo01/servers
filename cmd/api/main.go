package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/config"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/events/admin_consumer"
	"github.com/discordia-grupo01/servers/internal/events/profile_consumer"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channel_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channelperm_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/ownership_transfer_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
	"github.com/discordia-grupo01/servers/internal/repository/server_moderation_repo"
	"github.com/discordia-grupo01/servers/internal/repository/suspension_repo"
	"github.com/discordia-grupo01/servers/internal/repository/user_profile_repo"
	"github.com/discordia-grupo01/servers/internal/repository/word_filter_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
	"github.com/discordia-grupo01/servers/internal/service/mention_service"
	"github.com/discordia-grupo01/servers/internal/service/moderation_service"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
	"github.com/discordia-grupo01/servers/internal/service/word_filter_service"
)

// @title        Discordia - Servers
// @version      1.0.0
// @description  Servers service: servers, channels, categories, roles, member roles, membership, invitations, ownership transfer, mentions.
// @description  All /v1/** routes require identity: the JWT is validated by Kong at the gateway, and this service takes the user id from the X-User-Id header (or from the "sub" claim of the token Kong forwards).
// @description  A user suspended at platform level gets 403 with details.error_code ACCOUNT_SUSPENDED on every /v1/** route (including GET /v1/channels/{channelId}/voice-access) until reactivated.
// @description  A server suspended by the platform staff answers 403 with details.error_code SERVER_SUSPENDED, to everyone including its owner, on every /v1/** route that acts on it, its channels, categories, roles or invitations (including GET /v1/channels/{channelId}/voice-access). The only exceptions are POST and GET /v1/servers and a member leaving the server (DELETE /v1/servers/{serverId}/members/{their own id}). GET /v1/servers keeps listing it, with suspended=true and no channels or categories.
// @BasePath     /
// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
// @description                 Type "Bearer" followed by a space and the JWT (already validated by Kong; this service trusts the forwarded identity).
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	ctx := context.Background()

	pool, err := repository.NewPool(ctx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	var icons iconstore.Store
	switch {
	case cfg.UseSupabaseStorage():
		icons = iconstore.NewSupabaseStore(
			cfg.SupabaseStorageURL, cfg.SupabaseStorageBucket, cfg.SupabaseServiceKey, cfg.IconMaxBytes,
		)
	case cfg.IsProduction():
		log.Fatalf("iconstore: SUPABASE_STORAGE_URL, SUPABASE_STORAGE_BUCKET and SUPABASE_SERVICE_KEY are required in production")
	default:
		icons, err = iconstore.NewLocalStore(cfg.IconDir, cfg.IconMaxBytes)
		if err != nil {
			log.Fatalf("iconstore: %v", err)
		}
	}

	publisher := events.NewAMQPPublisher(cfg.RabbitMQURL)

	serverRepo := repository.NewServerRepository(pool)
	roleRepo := role_repo.NewRoleRepository(pool)
	memberRepo := member_repo.NewMemberRepository(pool)
	transferRepo := ownership_transfer_repo.NewRepository(pool)
	categoryRepo := category_repo.NewCategoryRepository(pool)
	channelRepo := channel_repo.NewChannelRepository(pool)
	moderationRepo := moderation_repo.NewRepository(pool)
	channelPermRepo := channelperm_repo.NewRepository(pool)

	svc := service.New(serverRepo, icons, memberRepo, channelPermRepo)
	roleSvc := role_service.New(serverRepo, roleRepo, memberRepo)
	memberRoleSvc := member_role_service.New(serverRepo, roleRepo, memberRepo)
	membershipSvc := membership_service.New(serverRepo, memberRepo)
	transferSvc := ownership_transfer_service.New(serverRepo, memberRepo, transferRepo)
	go ownership_transfer_service.NewExpirer(transferRepo).Start(ctx)
	go outbox.NewPoller(pool, publisher).Start(ctx)
	suspensionRepo := suspension_repo.NewSuspensionRepository(pool)
	suspensionGuard := middleware.NewSuspensionGuard(suspensionRepo, middleware.DefaultSuspensionCacheTTL)
	serverModerationRepo := server_moderation_repo.NewRepository(pool)
	serverSuspensionGuard := middleware.NewServerSuspensionGuard(serverModerationRepo, middleware.DefaultSuspensionCacheTTL)
	if cfg.RabbitMQURL != "" {
		go profile_consumer.New(cfg.RabbitMQURL, user_profile_repo.NewUserProfileRepository(pool)).Start(ctx)
		go admin_consumer.New(cfg.RabbitMQURL, cfg.AdminEventsHMACSecret, admin_consumer.WithInvalidation(suspensionRepo, suspensionGuard)).
			WithServerModeration(admin_consumer.ServerHandlers{Store: serverModerationRepo, Deleter: svc, Cache: serverSuspensionGuard}).
			Start(ctx)
	} else {
		log.Printf("profile_consumer: RABBITMQ_URL not set, member profiles won't be replicated")
		log.Printf("admin_consumer: RABBITMQ_URL not set, platform suspensions and server moderation won't be applied")
	}
	invSvc := invitation.NewService(repository.NewInvitationRepository(pool), serverRepo, memberRepo)
	categorySvc := category_service.New(serverRepo, categoryRepo, memberRepo)
	channelSvc := channel_service.New(serverRepo, categoryRepo, channelRepo, memberRepo, channelPermRepo, moderationRepo, user_profile_repo.NewUserProfileRepository(pool))
	moderationSvc := moderation_service.New(serverRepo, memberRepo, moderationRepo, moderationRepo)
	go moderation_service.NewMuteExpirer(moderationRepo).Start(ctx)
	wordFilterSvc := word_filter_service.New(serverRepo, memberRepo, word_filter_repo.NewRepository(pool))
	channelPermSvc := channelperm_service.New(serverRepo, channelRepo, roleRepo, memberRepo, channelPermRepo)
	mentionSvc := mention_service.New(serverRepo, channelRepo, memberRepo, roleRepo)

	router := handler.NewRouter(handler.Deps{
		Servers:            svc,
		Icons:              icons,
		Roles:              roleSvc,
		MemberRoles:        memberRoleSvc,
		Membership:         membershipSvc,
		OwnershipTransfers: transferSvc,
		Invitations:        invSvc,
		Channels:           channelSvc,
		Categories:         categorySvc,
		Moderation:         moderationSvc,
		WordFilter:         wordFilterSvc,
		ChannelPermissions: channelPermSvc,
		VoiceAccess:        channelSvc,
		Mentions:           mentionSvc,
		Auth:               middleware.AuthConfig{UserIDHeader: cfg.UserIDHeader},
		RejectSuspended:    suspensionGuard.RejectSuspended(),

		RejectSuspendedServer: serverSuspensionGuard.RejectSuspendedServer(),
		DB:                    pool,
	})

	addr := cfg.Host + ":" + cfg.Port
	log.Printf("listening on %s", addr)
	log.Fatal(router.Run(addr))
}
