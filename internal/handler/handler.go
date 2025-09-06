package handler

import (
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
	// r.Groupe почитать что такое для реализации SOLID midlleware
	r := chi.NewRouter()

	r.Route("/api/user", func(r chi.Router) {
		r.Post("/register", registerHandler(db, cfg.PepperKey, cfg.SecretKey))
		r.Post("/login", loginHandler(db, cfg.PepperKey, cfg.SecretKey))
		r.Post("/orders", authMiddleware(loadOrderNumHandler(db), cfg.SecretKey))
		r.Get("/orders", authMiddleware(getOrderList(db), cfg.SecretKey))

		r.Route("/balance", func(r chi.Router) {
			r.Get("/", authMiddleware(getCurBalance(db), cfg.SecretKey))
			r.Get("/withdraw", authMiddleware(withdrawReqHandler(db), cfg.SecretKey))
		})

		r.Get("/withdrawals", authMiddleware(getWithdrawList(db), cfg.SecretKey))
	})

	return r
}

func authMiddleware(next http.HandlerFunc, key string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(rw, "missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

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

		if userID, ok := claims["user_id"].(float64); ok {
			ctx := context.WithValue(r.Context(), "user_id", strconv.Itoa(int(userID)))
			r = r.WithContext(ctx)
		}
		// проверка а что если не flot64

		next.ServeHTTP(rw, r)
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
		defer r.Body.Close()

		err = json.Unmarshal(body, &u)
		if err != nil {
			log.Printf("failed to unmarshal JSON: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		password, err := createHashPassword(u.Password, pepperKey)
		if err != nil {
			log.Printf("bcrypt hashing failed: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		userId, err := db.RegisterReq(u.Login, string(password), conn)
		if err != nil {
			if errors.Is(err, db.ErrLoginExists) {
				log.Printf("login already exists: %v", err)
				http.Error(rw, "login already exists", http.StatusConflict)
				return
			}
			log.Printf("failed to register user: %v", err)
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}

		token, err := buildJWT(userId, secretKey)
		if err != nil {
			log.Printf("failed to generate token: %v", err)
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		rw.Header().Add("Authorization", "Bearer "+token)
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("user registered"))
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
			http.Error(rw, "internal server error", http.StatusInternalServerError)
			return
		}

		userID, receivedPassword, err := db.AuthReq(conn, u.Login)
		if err != nil {
			if errors.Is(err, db.ErrUserNotExists) {
				log.Printf("unknown user: %v", err)
				http.Error(rw, "invalid login or password", http.StatusUnauthorized)
				return
			}
			log.Printf("failed to get password: %v", err)
			http.Error(rw, "bad request", http.StatusBadRequest)
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

		rw.Header().Add("Authorization", "Bearer "+token)
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("user is Autorized"))

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

		receivedUserId, err := db.CheckUniqOrder(orderNum, conn)
		if err != nil {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}

		switch receivedUserId {
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
		userID, ok := r.Context().Value("user_id").(string)
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

		// сжатие данных

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		rw.Write(jsonData)
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

		//сжатие данных

		rw.WriteHeader(http.StatusOK)
		rw.Write(jsonData)
	}
}

func buildJWT(userId int, key string) (string, error) {

	claims := jwt.MapClaims{
		"user_id": userId,
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
