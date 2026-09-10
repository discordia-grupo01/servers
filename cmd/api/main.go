package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/config"
	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
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

	icons, err := iconstore.NewLocalStore(cfg.IconDir, cfg.IconMaxBytes)
	if err != nil {
		log.Fatalf("iconstore: %v", err)
	}

	serverRepo := repository.NewServerRepository(pool)
	roleRepo := role_repo.NewRoleRepository(pool)
	memberRepo := member_repo.NewMemberRepository(pool)

	svc := service.New(serverRepo, icons)
	roleSvc := role_service.New(serverRepo, roleRepo)
	memberRoleSvc := member_role_service.New(serverRepo, roleRepo, memberRepo)

	router := handler.NewRouter(handler.Deps{
		Servers:     svc,
		Roles:       roleSvc,
		MemberRoles: memberRoleSvc,
		Auth:        middleware.AuthConfig{UserIDHeader: cfg.UserIDHeader},
		DB:          pool,
	})

	addr := cfg.Host + ":" + cfg.Port
	log.Printf("listening on %s", addr)
	log.Fatal(router.Run(addr))
}
