package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/storage"
)

type AccrualModel struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual,omitempty"`
}

func newAccrualModel() *AccrualModel {
	return &AccrualModel{}
}

var (
	accrualMutex       sync.Mutex
	accrualBlocked     bool
	accrualUnblockTime time.Time
)

func AccrualRequest(conn *pgxpool.Pool, orderNum int, userID, accrualAddr string) {
	o := newAccrualModel()

	status := "PROCESSING"

	if err := storage.UpdateOrderStatus(conn, status, orderNum); err != nil {
		log.Printf("err to update order status: %v", err)
		return
	}

	uri := fmt.Sprintf("%s/api/orders/%v", accrualAddr, orderNum)

	for attempt := 0; attempt < 3; attempt++ {
		waitIfBlocked()

		if err := getAccrualResp(uri, o); err != nil {
			log.Printf("accrual request failed: %v", err)
			switch {
			case strings.Contains(err.Error(), "429"):
				continue
			case strings.Contains(err.Error(), "204"):
				return
			default:
				time.Sleep(time.Second)
				continue
			}
		}

		switch o.Status {
		case "PROCESSED":
			storage.UpdateProcessedStatus(conn, o.Status, o.Accrual, orderNum)
			storage.UpdateBalance(conn, o.Accrual, userID)
			return
		case "INVALID":
			storage.UpdateOrderStatus(conn, o.Status, orderNum)
		default:
			time.Sleep(time.Second)
		}
	}
}

func getAccrualResp(uri string, o *AccrualModel) error {
	resp, err := http.Get(uri)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return fmt.Errorf("accrual service: 204 No Content (order not registered)")
	case http.StatusTooManyRequests:
		retryDur := resp.Header.Get("Retry-After")
		if err := blockAccrualRequest(retryDur); err != nil {
			return fmt.Errorf("failed to block requests after 429: %w", err)
		}
		return fmt.Errorf("accrual service: 429 Too Many Requests (retry after %s)", retryDur)
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(o); err != nil {
			return fmt.Errorf("failed to decode accrual response: %w", err)
		}
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
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
