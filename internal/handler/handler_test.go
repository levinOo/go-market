package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/levinOo/go-market/internal/repository"
	"github.com/stretchr/testify/require"
)

type fakeUser struct {
	CheckFn  func(conn *pgxpool.Pool, pepperKey, secretKey string) (string, error)
	CreateFn func(conn *pgxpool.Pool, secret, pepper string) (string, error)
}

func (f *fakeUser) Check(conn *pgxpool.Pool, pepperKey, secretKey string) (string, error) {
	return f.CheckFn(conn, pepperKey, secretKey)
}

func (f *fakeUser) Create(conn *pgxpool.Pool, secret, pepper string) (string, error) {
	return f.CreateFn(conn, secret, pepper)
}

func TestRegisterHandler(t *testing.T) {

	tests := []struct {
		name           string
		reqBody        string
		mockCreateFunc func() (string, error)
		expectedStatus int
		expectCookie   bool
	}{
		{
			name:    "succes",
			reqBody: `{"login":"user1","password":"pass"}`,
			mockCreateFunc: func() (string, error) {
				return "test-token", nil
			},
			expectedStatus: http.StatusOK,
			expectCookie:   true,
		},
		{
			name:    "Invalid JSON",
			reqBody: `{"login:"user1","password":"pass"}`,
			mockCreateFunc: func() (string, error) {
				return "test-token", nil
			},
			expectedStatus: http.StatusBadRequest,
			expectCookie:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NewUser = func() repository.UserRepository {
				return &fakeUser{
					CreateFn: func(_ *pgxpool.Pool, _, _ string) (string, error) {
						return tt.mockCreateFunc()
					},
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(tt.reqBody))
			w := httptest.NewRecorder()

			handler := registerHandler(nil, "pepper", "secret")
			handler(w, req)

			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, tt.expectedStatus, res.StatusCode)

			if tt.expectCookie {
				cookies := res.Cookies()
				require.NotEmpty(t, cookies, "expected cookie but got none")
				require.Equal(t, "Authorization", cookies[0].Name)
				require.Equal(t, "test-token", cookies[0].Value)
			}
		})
	}
}

func Test_loginHandler(t *testing.T) {
	tests := []struct {
		name           string
		reqBody        string
		mockCheckFunc  func() (string, error)
		expectedStatus int
		expectCookie   bool
	}{
		{
			name:    "succes",
			reqBody: `{"login":"user","password":"test"}`,
			mockCheckFunc: func() (string, error) {
				return "token", nil
			},
			expectedStatus: http.StatusOK,
			expectCookie:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NewUser = func() repository.UserRepository {
				return &fakeUser{
					CheckFn: func(_ *pgxpool.Pool, _, _ string) (string, error) {
						return "token", nil
					},
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(tt.reqBody))
			w := httptest.NewRecorder()

			handler := loginHandler(nil, "pepper", "secret")

			handler(w, req)

			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, tt.expectedStatus, res.StatusCode)

			if tt.expectCookie {
				cookies := res.Cookies()
				require.NotEmpty(t, cookies, "expected cookie but got none")
				require.Equal(t, "Authorization", cookies[0].Name)
				require.Equal(t, "token", cookies[0].Value)
			}
		})
	}
}
