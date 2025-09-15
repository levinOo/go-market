package handler

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Test_registerHandler(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn      *pgxpool.Pool
		pepperKey string
		secretKey string
		want      http.HandlerFunc
	}{

		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registerHandler(tt.conn, tt.pepperKey, tt.secretKey)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("registerHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loginHandler(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn      *pgxpool.Pool
		pepperKey string
		secretKey string
		want      http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loginHandler(tt.conn, tt.pepperKey, tt.secretKey)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("loginHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loadOrderNumHandler(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn        *pgxpool.Pool
		accrualAddr string
		retryNum    string
		want        http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadOrderNumHandler(tt.conn, tt.accrualAddr, tt.retryNum)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("loadOrderNumHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getOrderList(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn *pgxpool.Pool
		want http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getOrderList(tt.conn)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("getOrderList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getCurBalance(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn *pgxpool.Pool
		want http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCurBalance(tt.conn)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("getCurBalance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_withdrawReqHandler(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn *pgxpool.Pool
		want http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withdrawReqHandler(tt.conn)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("withdrawReqHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getWithdrawList(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		conn *pgxpool.Pool
		want http.HandlerFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getWithdrawList(tt.conn)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("getWithdrawList() = %v, want %v", got, tt.want)
			}
		})
	}
}
