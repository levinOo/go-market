package service

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/levinOo/go-market/internal/config"
	"github.com/levinOo/go-market/internal/config/db"
	"github.com/levinOo/go-market/internal/handler"
)

func Serve(cfg config.Config) error {
	dbConn, err := db.ConnectDB(cfg.DataBaseAddr)
	if err != nil {
		log.Fatalf("DB connection failed: %v", err)
	}

	err = db.RunMigrations(cfg.DataBaseAddr)
	if err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	router := handler.NewRouter(dbConn, cfg)

	serverErr := make(chan error, 1)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
	}

	go func() {
		log.Printf("Starting server on %s", cfg.Addr)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	return shutdownServer(srv, dbConn, serverErr)
}

func shutdownServer(srv *http.Server, db *pgx.Conn, serverErr <-chan error) error {
	shutdown, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		log.Printf("Server error: %v", err)
		return err
	case <-shutdown.Done():
		log.Println("Received shutdown signal, gracefully shutting down...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			log.Printf("Error during server shutdown: %v", err)
			return err
		}

		err = db.Close(context.Background())
		if err != nil {
			log.Printf("Error closing database connection: %v", err)
			return err
		}

		log.Println("Server shutdown complete")
		return nil

	}
}
