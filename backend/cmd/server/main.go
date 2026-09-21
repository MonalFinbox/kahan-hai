// Command server runs the kahan-hai comparison API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/blinkit"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/instamart"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/minutes"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/zepto"
	"github.com/monalbarse/kahan-hai/backend/internal/api"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
	"github.com/monalbarse/kahan-hai/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var (
		addr        = env("ADDR", ":8080")
		dbPath      = env("DB_PATH", "./data/kahan-hai.db")
		chromeURL   = env("CHROME_URL", "") // set in docker compose; empty launches local Chrome
		headless    = env("HEADLESS", "true") == "true"
		lat         = envFloat("DEFAULT_LAT", 28.6315)
		lon         = envFloat("DEFAULT_LON", 77.2167)
		perAdapterT = time.Duration(envInt("ADAPTER_TIMEOUT_SECONDS", 45)) * time.Second
	)

	st, err := store.Open(dbPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	pool := browser.NewPool(chromeURL, headless)
	defer pool.Close()

	reg := adapter.NewRegistry(perAdapterT,
		blinkit.New(pool),
		instamart.New(pool),
		zepto.New(pool),
		minutes.New(pool),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(reg, st, log, adapter.Location{Lat: lat, Lon: lon}).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous: a search fans out to real browsers.
		WriteTimeout: 3 * time.Minute,
	}

	go func() {
		log.Info("listening", "addr", addr, "chrome", orLocal(chromeURL), "db", dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func orLocal(s string) string {
	if s == "" {
		return "local"
	}
	return s
}
