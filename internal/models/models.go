package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/levinOo/go-market/internal/config/db"
)

type AccrualModel struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual,omitempty"`
}

func AccrualRequest(conn *pgx.Conn, orderNum int, userID, accrualAddr string) {
	var o AccrualModel
	status := "PROCESSING"

	err := db.UpdateOrderStatus(conn, status, o.Accrual, orderNum, userID)
	if err != nil {
		log.Printf("err to update order status: %v", err)
	}

	uri := fmt.Sprintf("%s/api/orders/%v", accrualAddr, orderNum)

	// реализовать кол-во повторений // retryable 1 3 5 секунд
	for i := 0; i < 3; i++ {
		resp, err := http.Get(uri)
		if err != nil {
			log.Printf("%v", err)
		}
		defer resp.Body.Close()

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
			break
		} else {
			time.Sleep(time.Second)
		}
	}
}
