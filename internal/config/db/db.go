package db

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ConnectDB(DBAddr string) (*pgxpool.Pool, error) {

	// подключение к БД
	conn, err := pgxpool.New(context.Background(), DBAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to db: %w", err)
	}

	// миграции БД
	migrationsPath := "file://migrations"
	m, err := migrate.New(
		migrationsPath,
		DBAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create migrate instance: %w", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return conn, nil
}
