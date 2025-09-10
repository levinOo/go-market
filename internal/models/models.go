package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/config/db"
)

type AccrualModel struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual,omitempty"`
}

var (
	accrualMutex       sync.Mutex
	accrualBlocked     bool
	accrualUnblockTime time.Time
)

func AccrualRequest(conn *pgxpool.Pool, orderNum int, userID, accrualAddr string) {
	var o AccrualModel
	status := "PROCESSING"

	err := db.UpdateOrderStatus(conn, status, o.Accrual, orderNum, userID)
	if err != nil {
		log.Printf("err to update order status: %v", err)
	}

	waitIfBlocked()

	uri := fmt.Sprintf("%s/api/orders/%v", accrualAddr, orderNum)

	for i := 0; i < 3; i++ {
		resp, err := http.Get(uri)
		if err != nil {
			log.Printf("%v", err)
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusNoContent:
			log.Printf("сервис accrual прислал код 204: заказ не зарегистрирован в системе расчёта.")

			return
		case http.StatusTooManyRequests:
			log.Printf("сервис accrual прислал код 429: превышено количество запросов к сервису.")

			_, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("failed to read resp body: %v", err)
				return
			}

			retryDur := resp.Header.Get("Retry-After")

			err = blockAccrualRequest(retryDur)
			if err != nil {
				log.Printf("failed to block all requests: %v", err)
			}

			return
		default:
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("%v", err)
			}

			err = json.Unmarshal(data, &o)
			if err != nil {
				log.Printf("%v", err)
				continue
			}

			if o.Status == "INVALID" || o.Status == "PROCESSED" {
				db.UpdateOrderStatus(conn, o.Status, o.Accrual, orderNum, userID)

				return
			} else {
				time.Sleep(time.Second)
			}
		}
	}
}

func blockAccrualRequest(t string) error {
	accrualMutex.Lock()
	defer accrualMutex.Unlock()

	dur, err := strconv.Atoi(t)
	if err != nil {
		return err
	}

	accrualBlocked = true
	accrualUnblockTime = time.Now().Add(time.Duration(dur) * time.Second)
	return nil
}

func waitIfBlocked() {
	accrualMutex.Lock()
	defer accrualMutex.Unlock()

	if accrualBlocked && time.Now().Before(accrualUnblockTime) {
		sleepTime := time.Until(accrualUnblockTime)
		accrualMutex.Unlock()

		time.Sleep(sleepTime)

		accrualMutex.Lock()
		accrualBlocked = false

	} else if accrualBlocked {
		accrualBlocked = false
	}
}
