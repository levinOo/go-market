package handler

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/config"
	"github.com/levinOo/go-market/internal/models"
	"github.com/levinOo/go-market/internal/storage"
	"github.com/phedde/luhn-algorithm"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey string

const userContextKey ctxKey = "user_id"

type User struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type Withdraw struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

func (u *User) Create(conn *pgxpool.Pool, secretKey, pepperKey string) (string, error) {
	password, err := createHashPassword(u.Password, pepperKey)
	if err != nil {
		log.Printf("bcrypt hashing failed: %v", err)
		return "", err
	}

	userID, err := storage.RegisterReq(u.Login, string(password), conn)
	if err != nil {
		if errors.Is(err, storage.ErrLoginExists) {
			log.Printf("login already exists: %v", err)
			return "", storage.ErrLoginExists
		}
		log.Printf("failed to register user: %v", err)
		return "", err
	}

	err = storage.NewBalanceRecord(conn, userID)
	if err != nil {
		log.Printf("failed to create user balance: %v", err)
		return "", err
	}

	token, err := buildJWT(userID, secretKey)
	if err != nil {
		log.Printf("failed to generate token: %v", err)
		return "", err
	}

	return token, nil
}

func (u *User) Check(conn *pgxpool.Pool, pepperKey, secretKey string) (string, error) {
	userID, receivedPassword, err := storage.AuthReq(conn, u.Login)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotExists) {
			log.Printf("unknown user: %v", err)
			return "", storage.ErrUserNotExists
		}
		log.Printf("failed to get password: %v", err)
		return "", err
	}

	err = checkPassword(u.Password, pepperKey, receivedPassword)
	if err != nil {
		log.Printf("invalid password: %v", err)
		return "", storage.ErrInvalidPassword
	}

	token, err := buildJWT(userID, secretKey)
	if err != nil {
		log.Printf("failed to generate token: %v", err)
		return "", err
	}

	return token, nil
}

func (w *Withdraw) Create(conn *pgxpool.Pool, userID string) error {
	orderNum, err := strconv.Atoi(w.Order)
	if err != nil {
		log.Printf("failed to convert string to int: %v", err)
		return err
	}

	ok := luhn.IsValid(int64(orderNum))
	if !ok {
		log.Printf("неверный номер заказа: %v", err)
		return err
	}

	err = storage.TryWithdrawBalance(conn, w.Sum, userID)
	if err != nil {
		if errors.Is(err, storage.ErrInsufficientBalance) {
			log.Printf("на счету недостаточно средств: %v", err)
			return err
		}
		log.Printf("failed to process withdraw: %v", err)
		return err
	}

	err = storage.SumWithdrawBalance(conn, w.Sum, userID)
	if err != nil {
		log.Printf("failed to sum withdraw balance: %v", err)
		return err
	}

	processedAt := time.Now().Format(time.RFC3339)

	err = storage.SuccessWithdraw(conn, userID, w.Order, processedAt, w.Sum)
	if err != nil {
		log.Printf("couldn't add withdrawal entry: %v", err)
		return err
	}

	return nil
}

func newUser() *User {
	return &User{}
}

func newWithdrawModel() *Withdraw {
	return &Withdraw{}
}

func NewRouter(db *pgxpool.Pool, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	r.Group(func(r chi.Router) {
		r.Post("/api/user/register", registerHandler(db, cfg.PepperKey, cfg.SecretKey))
		r.Post("/api/user/login", loginHandler(db, cfg.PepperKey, cfg.SecretKey))
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(cfg.SecretKey))

		r.Post("/api/user/orders", loadOrderNumHandler(db, cfg.SystemAddr))
		r.Get("/api/user/orders", getOrderList(db))
		r.Get("/api/user/balance", getCurBalance(db))
		r.Get("/api/user/withdrawals", getWithdrawList(db))
		r.Post("/api/user/balance/withdraw", withdrawReqHandler(db))
	})

	return r
}

func authMiddleware(key string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("Authorization")
			if err != nil {
				http.Error(rw, "missing token", http.StatusUnauthorized)
				return
			}

			tokenString := cookie.Value
			tokenString = strings.TrimSpace(tokenString)
			if tokenString == "" {
				http.Error(rw, "empty token", http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(key), nil
			})
			if err != nil || !token.Valid {
				http.Error(rw, "invalid token", http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(rw, "invalid token claims", http.StatusUnauthorized)
				return
			}

			if userIDf, ok := claims["user_id"].(float64); ok {
				ctx := context.WithValue(r.Context(), userContextKey, strconv.Itoa(int(userIDf)))
				r = r.WithContext(ctx)
			} else {
				http.Error(rw, "invalid user_id claim", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(rw, r)
		})
	}
}

func registerHandler(conn *pgxpool.Pool, pepperKey string, secretKey string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		u := newUser()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		err = json.Unmarshal(body, &u)
		if err != nil {
			log.Printf("failed to read JSON: %v", err)
			http.Error(rw, "invalid request format", http.StatusBadRequest)
			return
		}

		// повторяющаяся логика
		if u.Login == "" || u.Password == "" {
			log.Printf("login and password are empty")
			http.Error(rw, "login and password are required", http.StatusBadRequest)
			return
		}

		token, err := u.Create(conn, secretKey, pepperKey)
		if err != nil {
			if errors.Is(err, storage.ErrLoginExists) {
				http.Error(rw, "login already exists", http.StatusConflict)
			}
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		http.SetCookie(rw, &http.Cookie{
			Name:     "Authorization",
			Value:    token,
			Path:     "/",
			MaxAge:   3600,
			Domain:   "",
			HttpOnly: true,
			Secure:   false,
		})
		rw.WriteHeader(http.StatusOK)
	}
}

func loginHandler(conn *pgxpool.Pool, pepperKey string, secretKey string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		u := newUser()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		err = json.Unmarshal(body, &u)
		if err != nil {
			log.Printf("failed to read JSON: %v", err)
			http.Error(rw, "invalid request format", http.StatusBadRequest)
			return
		}

		// повторяющаяся логика
		if u.Login == "" || u.Password == "" {
			log.Printf("login and password are empty")
			http.Error(rw, "login and password are required", http.StatusBadRequest)
			return
		}

		token, err := u.Check(conn, pepperKey, secretKey)
		if err != nil {
			switch err {
			case storage.ErrUserNotExists:
				http.Error(rw, "unknown user", http.StatusUnauthorized)
				return
			case storage.ErrInvalidPassword:
				http.Error(rw, "invalid login or password", http.StatusUnauthorized)
				return
			default:
				http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		http.SetCookie(rw, &http.Cookie{
			Name:     "Authorization",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false,
		})
		rw.WriteHeader(http.StatusOK)
	}
}

func loadOrderNumHandler(conn *pgxpool.Pool, accrualAddr string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, err := getUserIDFromContext(r)
		if err != nil {
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		orderNum, err := strconv.Atoi(string(body))
		if err != nil {
			log.Printf("failed to convert string to int: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		ok := luhn.IsValid(int64(orderNum))
		if !ok {
			log.Printf("order number is not valid: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusUnprocessableEntity)
			return
		}

		receivedUserID, err := storage.CheckUniqOrder(orderNum, conn)
		if err != nil {
			log.Printf("не удалось получить userId из бд: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		switch receivedUserID {
		case "":
			uploadedAt := time.Now().Format(time.RFC3339)
			status := "NEW"

			err := storage.AddOrder(conn, orderNum, userID, status, uploadedAt)
			if err != nil {
				log.Printf("не удалось добавить заказа в orders: %v", err)
				http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			go models.AccrualRequest(conn, orderNum, userID, accrualAddr)

			rw.WriteHeader(http.StatusAccepted)
			rw.Write([]byte("новый номер заказа принят в обработку"))
		case userID:
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("номер заказа уже был загружен этим пользователем"))
		default:
			rw.WriteHeader(http.StatusConflict)
			rw.Write([]byte("номер заказа уже был загружен другим пользователем"))
		}

	}
}

func getOrderList(conn *pgxpool.Pool) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")

		userID, err := getUserIDFromContext(r)
		if err != nil {
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		orders, err := storage.GetOrdersList(conn, userID)
		if err != nil {
			log.Printf("couldn't get order list: %v", err)
			http.Error(rw, `{"error":"Internal Server Error"}`, http.StatusInternalServerError)
			return
		}

		if len(orders) == 0 {
			log.Printf("orders is empty")
			rw.WriteHeader(http.StatusNoContent)
			return
		}

		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			rw.Header().Set("Content-Encoding", "gzip")
			rw.WriteHeader(http.StatusOK)

			gz := gzip.NewWriter(rw)
			defer gz.Close()

			if err := json.NewEncoder(gz).Encode(orders); err != nil {
				log.Printf("failed to encode gzipped response: %v", err)
				return
			}
		} else {
			rw.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(rw).Encode(orders); err != nil {
				log.Printf("failed to encode response: %v", err)
				return
			}
		}
	}
}

func getCurBalance(conn *pgxpool.Pool) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")

		userID, err := getUserIDFromContext(r)
		if err != nil {
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		balance, err := storage.GetUserBalance(conn, userID)
		if err != nil {
			log.Printf("couldn't get current list: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		jsonData, err := json.MarshalIndent(balance, "", "    ")
		if err != nil {
			log.Printf("failed to marshal balance response: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		rw.WriteHeader(http.StatusOK)
		rw.Write(jsonData)
	}
}

func withdrawReqHandler(conn *pgxpool.Pool) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		w := newWithdrawModel()

		userID, err := getUserIDFromContext(r)
		if err != nil {
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		err = json.Unmarshal(body, &w)
		if err != nil {
			log.Printf("failed to read json: %v", err)
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if err = w.Create(conn, userID); err != nil {
			switch err {
			case storage.ErrInsufficientBalance:
				http.Error(rw, http.StatusText(http.StatusPaymentRequired), http.StatusPaymentRequired)
				return
			case storage.ErrUnUnprocessableEntity:
				http.Error(rw, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
				return
			default:
				http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("OK"))
	}
}

func getWithdrawList(conn *pgxpool.Pool) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, err := getUserIDFromContext(r)
		if err != nil {
			rw.Header().Set("Content-Type", "application/json")
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		withdraws, err := storage.GetWitthdrawsList(conn, userID)
		if err != nil {
			log.Printf("couldn't get withdraw list: %v", err)
			http.Error(rw, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		if len(withdraws) == 0 {
			log.Printf("нет ни одного списания")
			rw.WriteHeader(http.StatusNoContent)
			return
		}

		rw.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			rw.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(rw)
			defer gz.Close()
			if err := json.NewEncoder(gz).Encode(withdraws); err != nil {
				log.Printf("failed to encode gzipped response: %v", err)
			}
			return
		}

		if err := json.NewEncoder(rw).Encode(withdraws); err != nil {
			log.Printf("failed to encode response: %v", err)
		}
	}
}

func getUserIDFromContext(r *http.Request) (string, error) {
	userID, ok := r.Context().Value(userContextKey).(string)
	if !ok || userID == "" {
		log.Printf("не удалось получить userID: %v", userID)
		return "", storage.ErrGetUserID
	}

	return userID, nil
}

func buildJWT(userID int, key string) (string, error) {

	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(key))
}

func checkPassword(inputPassword, pepperKey, storedHash string) error {
	passwordWithPepper := inputPassword + pepperKey

	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(passwordWithPepper))
}

func createHashPassword(password string, pepperKey string) ([]byte, error) {
	password = password + pepperKey

	return bcrypt.GenerateFromPassword([]byte(password), 10)
}
