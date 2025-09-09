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
	"github.com/jackc/pgx/v5"
	"github.com/levinOo/go-market/internal/config"
	"github.com/levinOo/go-market/internal/config/db"
	"github.com/levinOo/go-market/internal/models"
	"github.com/theplant/luhn"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey string

const userKey ctxKey = "user_id"

type User struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type WithdrawModel struct {
	Order string `json:"order"`
	Sum   string `json:"sum"`
}

func newUser() *User {
	return &User{}
}

func NewRouter(db *pgx.Conn, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	r.Group(func(r chi.Router) {
		r.Post("/api/user/register", registerHandler(db, cfg.PepperKey, cfg.SecretKey))
		r.Post("/api/user/login", loginHandler(db, cfg.PepperKey, cfg.SecretKey))
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(cfg.SecretKey))

		r.Post("/api/user/orders", loadOrderNumHandler(db))
		r.Get("/api/user/orders", getOrderList(db))
		r.Get("/api/user/balance", getCurBalance(db))
		r.Get("/api/user/withdrawals", withdrawReqHandler(db))
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
				ctx := context.WithValue(r.Context(), userKey, strconv.Itoa(int(userIDf)))
				r = r.WithContext(ctx)
			} else {
				http.Error(rw, "invalid user_id claim", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(rw, r)
		})
	}
}

func registerHandler(conn *pgx.Conn, pepperKey string, secretKey string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		u := newUser()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		err = json.Unmarshal(body, &u)
		if err != nil {
			log.Printf("failed to unmarshal JSON: %v", err)
			http.Error(rw, "invalid request format", http.StatusBadRequest)
			return
		}

		if u.Login == "" || u.Password == "" {
			log.Printf("login and password are empty")
			http.Error(rw, "login and password are required", http.StatusBadRequest)
			return
		}

		password, err := createHashPassword(u.Password, pepperKey)
		if err != nil {
			log.Printf("bcrypt hashing failed: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		userID, err := db.RegisterReq(u.Login, string(password), conn)
		if err != nil {
			if errors.Is(err, db.ErrLoginExists) {
				log.Printf("login already exists: %v", err)
				http.Error(rw, "login already exists", http.StatusConflict)
				return
			}
			log.Printf("failed to register user: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		token, err := buildJWT(userID, secretKey)
		if err != nil {
			log.Printf("failed to generate token: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(rw, &http.Cookie{
			Name:     "Authorization",
			Value:    token,
			Path:     "/",
			MaxAge:   3600,
			Domain:   "",
			Secure:   false,
			HttpOnly: true,
		})
		rw.WriteHeader(http.StatusOK)
	}
}

func loginHandler(conn *pgx.Conn, pepperKey string, secretKey string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		u := newUser()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		err = json.Unmarshal(body, &u)
		if err != nil {
			log.Printf("failed to unmarshal JSON: %v", err)
			http.Error(rw, "invalid request format", http.StatusBadRequest)
			return
		}

		if u.Login == "" || u.Password == "" {
			log.Printf("login and password are empty")
			http.Error(rw, "login and password are required", http.StatusBadRequest)
			return
		}

		userID, receivedPassword, err := db.AuthReq(conn, u.Login)
		if err != nil {
			if errors.Is(err, db.ErrUserNotExists) {
				log.Printf("unknown user: %v", err)
				http.Error(rw, "unknown user", http.StatusUnauthorized)
				return
			}
			log.Printf("failed to get password: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		err = checkPassword(u.Password, pepperKey, receivedPassword)
		if err != nil {
			log.Printf("invalid password: %v", err)
			http.Error(rw, "invalid login or password", http.StatusUnauthorized)
			return
		}

		token, err := buildJWT(userID, secretKey)
		if err != nil {
			log.Printf("failed to generate token: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(rw, &http.Cookie{
			Name:     "token",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false,
		})
		rw.WriteHeader(http.StatusOK)
	}
}

func loadOrderNumHandler(conn *pgx.Conn) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(string)
		if !ok || userID == "" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		orderNum, err := strconv.Atoi(string(body))
		if err != nil {
			log.Printf("failed to convert string to int: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		ok = luhn.Valid(orderNum)
		if !ok {
			log.Printf("order number is not valid: %v", err)
			http.Error(rw, "internal server error", http.StatusUnprocessableEntity)
			return
		}

		receivedUserID, err := db.CheckUniqOrder(orderNum, conn)
		if err != nil {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}

		switch receivedUserID {
		case "":
			err := db.AddOrder(orderNum, userID, conn)
			if err != nil {
				http.Error(rw, "bad request", http.StatusBadRequest)
				return
			}

			go models.AccrualRequest(conn, orderNum, userID)

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

func getOrderList(conn *pgx.Conn) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(userKey).(string)
		if !ok || userID == "" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		orders, err := db.GetOrdersList(conn, userID)
		if err != nil {
			log.Printf("couldn't get order list: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		if len(orders) == 0 {
			http.Error(rw, "нет данных для ответа", http.StatusNoContent)
			return
		}

		jsonData, err := json.MarshalIndent(orders, "", "    ")
		if err != nil {
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		rw.Header().Set("Content-Encoding", "gzip")
		rw.WriteHeader(http.StatusOK)

		gz := gzip.NewWriter(rw)
		defer gz.Close()
		_, err = gz.Write(jsonData)
		if err != nil {
			http.Error(rw, "internal serer error", http.StatusInternalServerError)
			return
		}
	}
}

func getCurBalance(conn *pgx.Conn) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(string)
		if !ok || userID == "" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		balance, err := db.GetUserBalance(conn, userID)
		if err != nil {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}

		jsonData, err := json.MarshalIndent(balance, "", "    ")
		if err != nil {
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		rw.WriteHeader(http.StatusOK)
		rw.Write(jsonData)
	}
}

func withdrawReqHandler(conn *pgx.Conn) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var w WithdrawModel
		userID, ok := r.Context().Value("user_id").(string)
		if !ok || userID == "" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		err = json.Unmarshal(body, &w)
		if err != nil {
			log.Printf("failed to read json: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		err = db.SuccessWithdraw(conn, userID, w.Order, w.Sum)
		if err != nil {
			if errors.Is(err, db.ErrInsufficientBalance) {
				http.Error(rw, "на счету недостаточно средств", http.StatusPaymentRequired)
				return
			}
			log.Printf("failed to process withdraw: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("OK"))
	}
}

func getWithdrawList(conn *pgx.Conn) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(string)
		if !ok || userID == "" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		withdraws, err := db.GetWitthdrawsList(conn, userID)
		if err != nil {
			log.Printf("couldn't get withdraw list: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		if len(withdraws) == 0 {
			http.Error(rw, "нет ни одного списания", http.StatusNoContent)
			return
		}

		jsonData, err := json.MarshalIndent(withdraws, "", "    ")
		if err != nil {
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		rw.Header().Set("Content-Encoding", "gzip")
		rw.WriteHeader(http.StatusOK)

		gz := gzip.NewWriter(rw)
		defer gz.Close()
		_, err = gz.Write(jsonData)
		if err != nil {
			http.Error(rw, "internal serer error", http.StatusInternalServerError)
			return
		}
	}
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
