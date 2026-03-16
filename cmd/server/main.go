package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/api"
	"goscribe/internal/worker"
	"goscribe/pkg/config"
)

func main() {
	cfg := loadConfig()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.redisAddr})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis connection failed: %v", err)
	}
	log.Printf("connected to Redis at %s", cfg.redisAddr)

	postActions := loadPostActions(cfg.configFile)
	log.Printf("loaded %d post-processing action(s)", len(postActions))

	redisOpt := asynq.RedisClientOpt{Addr: cfg.redisAddr}
	client := asynq.NewClient(redisOpt)
	defer client.Close()

	var httpSrv *http.Server
	var asynqSrv *asynq.Server

	switch cfg.mode {
	case "api":
		httpSrv = buildHTTPServer(cfg, client, rdb, postActions)
	case "worker":
		asynqSrv = buildAsynqServer(cfg, redisOpt)
	default:
		httpSrv = buildHTTPServer(cfg, client, rdb, postActions)
		asynqSrv = buildAsynqServer(cfg, redisOpt)
	}

	errCh := make(chan error, 2)

	if httpSrv != nil {
		go func() {
			log.Printf("HTTP server listening on :%s (mode=%s)", cfg.port, cfg.mode)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("http: %w", err)
			}
		}()
	}

	if asynqSrv != nil {
		mux := asynq.NewServeMux()
		proc := worker.NewProcessor(worker.Config{
			RDB:         rdb,
			OpenAIKey:   cfg.openAIKey,
			GeminiKey:   cfg.geminiKey,
			GeminiModel: cfg.geminiModel,
			Provider:    cfg.provider,
			ResultTTL:   cfg.resultTTL,
			PostActions: postActions,
		})
		mux.HandleFunc(worker.TaskTypeProcess, proc.ProcessTask)
		go func() {
			log.Println("worker started")
			if err := asynqSrv.Run(mux); err != nil {
				errCh <- fmt.Errorf("worker: %w", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		log.Printf("fatal error: %v", err)
	case sig := <-quit:
		log.Printf("received %v, shutting down (timeout=%s)...", sig, cfg.shutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()

	if httpSrv != nil {
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown error: %v", err)
		}
	}
	if asynqSrv != nil {
		asynqSrv.Shutdown()
	}

	log.Println("shutdown complete")
}

type serverConfig struct {
	mode            string
	port            string
	redisAddr       string
	openAIKey       string
	geminiKey       string
	geminiModel     string
	provider        string
	configFile      string
	resultTTL       time.Duration
	maxUploadBytes  int64
	shutdownTimeout time.Duration
	uploadsDir      string
}

func loadConfig() serverConfig {
	redisURL := getenv("REDIS_URL", "redis://redis:6379")
	redisAddr := parseRedisAddr(redisURL)

	resultTTLHours, _ := strconv.Atoi(getenv("RESULT_TTL_HOURS", "24"))
	maxUploadMB, _ := strconv.ParseInt(getenv("MAX_UPLOAD_MB", "100"), 10, 64)
	shutdownSecs, _ := strconv.Atoi(getenv("SHUTDOWN_TIMEOUT_SECONDS", "30"))

	return serverConfig{
		mode:            getenv("MODE", "all"),
		port:            getenv("PORT", "8080"),
		redisAddr:       redisAddr,
		openAIKey:       getenv("OPENAI_API_KEY", ""),
		geminiKey:       getenv("GEMINI_API_KEY", ""),
		geminiModel:     getenv("GEMINI_MODEL", "gemini-2.0-flash"),
		provider:        getenv("GOSCRIBE_PROVIDER", "openai"),
		configFile:      getenv("GOSCRIBE_CONFIG_FILE", ""),
		resultTTL:       time.Duration(resultTTLHours) * time.Hour,
		maxUploadBytes:  maxUploadMB << 20,
		shutdownTimeout: time.Duration(shutdownSecs) * time.Second,
		uploadsDir:      getenv("UPLOADS_DIR", "/tmp/goscribe-uploads"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseRedisAddr(redisURL string) string {
	u, err := url.Parse(redisURL)
	if err != nil {
		return redisURL
	}
	host := u.Host
	if host == "" {
		return redisURL
	}
	return host
}

func loadPostActions(configFile string) []config.PostAction {
	if configFile != "" {
		cfg, err := config.LoadConfig(configFile)
		if err == nil && len(cfg.PostActions) > 0 {
			return cfg.PostActions
		}
		log.Printf("warning: could not load config file %q: %v — using built-in defaults", configFile, err)
	}
	return config.DefaultPostActions()
}

func buildHTTPServer(cfg serverConfig, client *asynq.Client, rdb *redis.Client, actions []config.PostAction) *http.Server {
	h := api.NewHandler(api.HandlerConfig{
		Enqueuer:        client,
		RDB:             rdb,
		PostActions:     actions,
		ResultTTL:       cfg.resultTTL,
		MaxUploadBytes:  cfg.maxUploadBytes,
		UploadsDir:      cfg.uploadsDir,
		DefaultProvider: cfg.provider,
	})
	return &http.Server{
		Addr:         ":" + cfg.port,
		Handler:      api.NewRouter(h),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func buildAsynqServer(cfg serverConfig, opt asynq.RedisClientOpt) *asynq.Server {
	return asynq.NewServer(opt, asynq.Config{
		Concurrency:     5,
		Queues:          map[string]int{"default": 1},
		ShutdownTimeout: cfg.shutdownTimeout,
	})
}
