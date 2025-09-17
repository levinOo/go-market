package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/storage"
)

type UserRepository interface {
	Create(conn *pgxpool.Pool, secret, pepper string) (string, error)
	Check(conn *pgxpool.Pool, pepperKey, secretKey string) (string, error)
}

type OrderRepository interface {
	LoadOrder(conn *pgxpool.Pool, orderNum, retryNum int, accrualAddr string) (int, error)
	GetList(conn *pgxpool.Pool) ([]storage.Order, error)
}

type BalanceRepository interface {
	GetBalance(conn *pgxpool.Pool) ([]byte, error)
}

type WithdrawRepository interface {
	Create(conn *pgxpool.Pool) error
	GetWitthdrawsList(conn *pgxpool.Pool) ([]storage.Withdraw, error)
}
