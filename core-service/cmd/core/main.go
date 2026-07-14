// The core command runs the BuddyGym core service: REST API for the mini app
// and the internal gRPC endpoint for checkin-service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	_ "github.com/kosttiik/BuddyGym/core-service/docs"
	"github.com/kosttiik/BuddyGym/core-service/internal/avatar"
	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/config"
	"github.com/kosttiik/BuddyGym/core-service/internal/grpcserver"
	"github.com/kosttiik/BuddyGym/core-service/internal/httpapi"
	"github.com/kosttiik/BuddyGym/core-service/internal/ratelimit"
	roomreaper "github.com/kosttiik/BuddyGym/core-service/internal/rooms"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

//	@title			BuddyGym Core API
//	@version		1.0
//	@description	Core service of BuddyGym: auth, users, rooms, rewards, checkin proxy.
//	@BasePath		/api/v1

//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				JWT from POST /auth/telegram: "Bearer <token>"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := storage.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := storage.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("db ready")

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	conn, err := grpc.NewClient(cfg.CheckinAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(12*1024*1024),
			grpc.MaxCallSendMsgSize(12*1024*1024),
		),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	users := storage.NewUsers(pool)
	rooms := storage.NewRooms(pool)
	checkinClient := checkin.NewClient(conn)
	results := storage.NewResults(pool)

	// avatars are optional: without object storage the mini app falls back to initials.
	// these stay interface-typed so a disabled mirror is a nil interface, not a typed nil.
	var avatarStore httpapi.AvatarStore
	var avatarMirror httpapi.AvatarMirror
	if cfg.S3.Enabled() {
		store, err := avatar.NewStore(ctx, avatar.StoreConfig(cfg.S3))
		if err != nil {
			return err
		}
		avatarStore = store
		avatarMirror = avatar.NewMirror(users, avatar.NewTelegram(cfg.BotToken, nil), store, log)
		log.Info("avatar mirror ready", "bucket", cfg.S3.Bucket)
	} else {
		log.Warn("object storage not configured, avatars disabled")
	}

	api := httpapi.New(httpapi.Options{
		Users:          users,
		Rooms:          rooms,
		Checkins:       checkinClient,
		Avatars:        avatarStore,
		AvatarMirror:   avatarMirror,
		BotToken:       cfg.BotToken,
		AuthTTL:        cfg.AuthTTL,
		JWTSecret:      cfg.JWTSecret,
		JWTTTL:         cfg.JWTTTL,
		AuthLimiter:    ratelimit.New(rdb, "auth", 10, time.Minute, log),
		APILimiter:     ratelimit.New(rdb, "api", 120, time.Minute, log),
		CheckinLimiter: ratelimit.New(rdb, "checkin", 20, time.Hour, log),
		DBPing:         pool.Ping,
		RedisPing: func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		},
		Log: log,
	})

	root := http.NewServeMux()
	root.Handle("GET /api/v1/docs/", httpSwagger.Handler(
		httpSwagger.URL("/api/v1/docs/doc.json")))
	root.Handle("/", api.Handler())

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcSrv := grpc.NewServer()
	pbv1.RegisterCoreInternalServiceServer(grpcSrv, grpcserver.New(users, rooms, results, log))
	reflection.Register(grpcSrv)

	reaper := roomreaper.New(roomreaper.Options{
		Rooms:    rooms,
		Checkins: checkinClient,
		Log:      log,
	})
	go reaper.Run(ctx)

	errCh := make(chan error, 2)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			errCh <- err
			return
		}
		log.Info("grpc listening", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	grpcSrv.GracefulStop()
	return httpSrv.Shutdown(shutdownCtx)
}
