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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/config"
	"github.com/kosttiik/BuddyGym/core-service/internal/grpcserver"
	"github.com/kosttiik/BuddyGym/core-service/internal/httpapi"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

//	@title			BuddyGym Core API
//	@version		1.0
//	@description	Core service of BuddyGym: auth, users, rooms, rewards, checkin proxy.
//	@BasePath		/api/v1

//	@securityDefinitions.apikey	TmaAuth
//	@in							header
//	@name						Authorization
//	@description				Telegram Mini App auth: "tma <initData>"

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
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	users := storage.NewUsers(pool)
	rooms := storage.NewRooms(pool)
	results := storage.NewResults(pool)

	api := httpapi.New(httpapi.Options{
		Users:    users,
		Rooms:    rooms,
		Checkins: checkin.NewClient(conn),
		BotToken: cfg.BotToken,
		AuthTTL:  cfg.AuthTTL,
		DBPing:   pool.Ping,
		RedisPing: func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		},
		Log: log,
	})

	root := http.NewServeMux()
	root.Handle("/", api.Handler())

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcSrv := grpc.NewServer()
	pbv1.RegisterCoreInternalServiceServer(grpcSrv, grpcserver.New(users, rooms, results, log))

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
