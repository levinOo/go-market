package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/config/db"
)

type AccrualModel struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual,omitempty"`
}

func AccrualRequest(conn *pgxpool.Pool, orderNum int, userID, accrualAddr string) {
	var o AccrualModel
	status := "PROCESSING"

	err := db.UpdateOrderStatus(conn, status, o.Accrual, orderNum, userID)
	if err != nil {
		log.Printf("err to update order status: %v", err)
	}

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
		default:
			if resp.StatusCode == http.StatusNoContent {
				time.Sleep(time.Second)
				return
			}

			data, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("%v", err)
			}

			err = json.Unmarshal(data, &o)
			if err != nil {
				log.Printf("%v", err)
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
