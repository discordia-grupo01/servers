package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/config"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
	"github.com/discordia-grupo01/servers/internal/repository/channel_repo"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/ownership_transfer_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

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

	svc := service.New(serverRepo, icons, publisher)
	roleSvc := role_service.New(serverRepo, roleRepo)
	memberRoleSvc := member_role_service.New(serverRepo, roleRepo, memberRepo)
	membershipSvc := membership_service.New(serverRepo, memberRepo, publisher)
	transferSvc := ownership_transfer_service.New(serverRepo, memberRepo, transferRepo)
	go ownership_transfer_service.NewExpirer(transferRepo).Start(ctx)
	invSvc := invitation.NewService(repository.NewInvitationRepository(pool), publisher)
	categorySvc := category_service.New(serverRepo, categoryRepo)
	channelSvc := channel_service.New(serverRepo, categoryRepo, channelRepo)

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
		Auth:               middleware.AuthConfig{UserIDHeader: cfg.UserIDHeader},
		DB:                 pool,
	})

	addr := cfg.Host + ":" + cfg.Port
	log.Printf("listening on %s", addr)
	log.Fatal(router.Run(addr))
}
