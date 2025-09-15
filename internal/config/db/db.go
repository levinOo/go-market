package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrLoginExists           = errors.New("login already exists")
	ErrUserNotExists         = errors.New("user not exists")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrInvalidPassword       = errors.New("invalid password")
	ErrGetUserID             = errors.New("couldn't get the userID")
	ErrUnUnprocessableEntity = errors.New("invalid order number")
)

type Order struct {
	Number   string    `json:"number"`
	Status   string    `json:"status"`
	Accrual  *float64  `json:"accrual,omitempty"`
	Uploaded time.Time `json:"uploaded_at"`
}

type Withdraw struct {
	Order       string    `json:"order"`
	Sum         float64   `json:"sum"`
	ProcessedAt time.Time `json:"processed_at"`
}

type UserBalance struct {
	Current  float64 `json:"current"`
	Withdraw float64 `json:"withdraw"`
}

// при не подключеении retry
func ConnectDB(DBAddr string) (*pgxpool.Pool, error) {

	conn, err := pgxpool.New(context.Background(), DBAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to db: %w", err)
	}

	return conn, nil
}

func RunMigrations(connString string) error {
	migrationsPath := "file://migrations"
	m, err := migrate.New(
		migrationsPath,
		connString,
	)
	if err != nil {
		return fmt.Errorf("could not create migrate instance: %w", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// ____________________Регистрация пользователя:

func RegisterReq(login string, password string, conn *pgxpool.Pool) (int, error) {
	var userID int

	err := conn.QueryRow(context.Background(), `
        INSERT INTO users (login, password)
        VALUES ($1, $2)
        ON CONFLICT (login) DO NOTHING
        RETURNING id;
    `, login, password).Scan(&userID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrLoginExists
		}
		return 0, err
	}

	return userID, nil
}

func NewBalanceRecord(conn *pgxpool.Pool, userID int) error {
	_, err := conn.Exec(context.Background(), `
        INSERT INTO balance (user_id)
        VALUES ($1);
    `, userID)
	if err != nil {
		return err
	}
	return nil
}

// ____________________Аутентификация пользователя:

func AuthReq(conn *pgxpool.Pool, login string) (int, string, error) {
	var (
		password string
		userID   int
	)

	err := conn.QueryRow(context.Background(), `
        SELECT password, id
        FROM users
        WHERE login = $1
    `, login).Scan(&password, &userID)
	if err != nil {
		return 0, "", err
	}

	return userID, password, nil
}

func GetPassword(login string, conn *pgx.Conn) (string, error) {
	var password string

	err := conn.QueryRow(context.Background(), `
        SELECT password
        FROM users
        WHERE login = $1
    `, login).Scan(&password)

	if err != nil {
		return "", err
	}

	return password, nil
}

// ____________________Загрузка номера заказа:

func CheckUniqOrder(orderNum int, conn *pgxpool.Pool) (string, error) {
	var receivedUserID string

	err := conn.QueryRow(context.Background(), `
		SELECT user_id
		FROM orders
		WHERE order_number = $1
	`, orderNum).Scan(&receivedUserID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}

	return receivedUserID, nil
}

func AddOrder(conn *pgxpool.Pool, orderNum int, userID, status, uploadedAt string) error {
	_, err := conn.Exec(
		context.Background(),
		`INSERT INTO orders (user_id, order_number, uploaded_at, status) VALUES ($1, $2, $3, $4)`,
		userID, orderNum, uploadedAt, status,
	)

	return err
}

func UpdateBalance(conn *pgxpool.Pool, accrual float64, userID string) error {
	_, err := conn.Exec(context.Background(), `
	UPDATE balance
	SET current = current + $1
	WHERE user_id = $2
	`, accrual, userID)

	if err != nil {
		return err
	}

	return nil
}

func UpdateProcessedStatus(conn *pgxpool.Pool, status string, accrual float64, orderNum int) error {
	_, err := conn.Exec(context.Background(), `
		UPDATE orders
		SET status = $1, accrual = $2
		WHERE order_number = $3
		`, status, accrual, orderNum)

	if err != nil {
		return err
	}

	return nil
}

func UpdateOrderStatus(conn *pgxpool.Pool, status string, orderNum int) error {
	_, err := conn.Exec(context.Background(), `
		UPDATE orders
		SET status = $1
		WHERE order_number = $2
	`, status, orderNum)

	if err != nil {
		return err
	}

	return nil
}

// ____________________Получение списка загруженных номеров заказов:

func GetOrdersList(conn *pgxpool.Pool, userID string) ([]Order, error) {
	var orders []Order

	rows, err := conn.Query(context.Background(), `
        SELECT order_number, status, accrual, uploaded_at
        FROM orders
        WHERE user_id = $1
        ORDER BY uploaded_at DESC
    `, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.Number, &o.Status, &o.Accrual, &o.Uploaded); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return orders, nil
}

// ____________________Получение текущего баланса пользователя:

func GetUserBalance(conn *pgxpool.Pool, userID string) (UserBalance, error) {
	var u UserBalance

	err := conn.QueryRow(context.Background(), `
		SELECT current, withdraw
		FROM balance
		WHERE user_id = $1
	`, userID).Scan(&u.Current, &u.Withdraw)

	if err != nil {
		return UserBalance{}, err
	}

	return u, nil
}

// ____________________Запрос на списание средств:

func TryWithdrawBalance(conn *pgxpool.Pool, amount float64, userID string) error {
	res, err := conn.Exec(context.Background(), `
        UPDATE balance
        SET current = current - $1
        WHERE user_id = $2 AND current >= $1
    `, amount, userID)
	if err != nil {
		return err
	}

	if res.RowsAffected() == 0 {
		return ErrInsufficientBalance
	}

	return nil
}

func SumWithdrawBalance(conn *pgxpool.Pool, amount float64, userID string) error {
	_, err := conn.Exec(context.Background(), `
		UPDATE balance
    	SET withdraw = withdraw + $1
   		WHERE user_id = $2
		`, amount, userID)
	if err != nil {
		return err
	}

	return nil
}

func SuccessWithdraw(conn *pgxpool.Pool, userID, orderNum, processedAt string, amount float64) error {
	_, err := conn.Exec(context.Background(), `
        INSERT INTO withdraw (user_id, order_number, amount, processed_at)
        VALUES ($1, $2, $3, $4)
    `, userID, orderNum, amount, processedAt)
	if err != nil {
		return err
	}

	return nil
}

// ____________________Получение информации о выводе средств:

func GetWitthdrawsList(conn *pgxpool.Pool, userID string) ([]Withdraw, error) {
	var withdraw []Withdraw

	rows, err := conn.Query(context.Background(), `
	SELECT order_number, amount, processed_at
	FROM withdraw
	WHERE user_id = $1
	ORDER BY processed_at DESC
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var w Withdraw
		if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, err
		}
		withdraw = append(withdraw, w)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return withdraw, nil
}
