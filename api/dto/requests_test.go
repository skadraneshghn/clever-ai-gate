package dto

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateTenantRequest_UnmarshalLargeNumbers(t *testing.T) {
	tests := []struct {
		name            string
		jsonInput       string
		expectedName    string
		expectedBalance int64
		expectedRPM     int
	}{
		{
			name:            "Overflow 10^20 as number",
			jsonInput:       `{"name":"test-tenant","token_balance":100000000000000000000,"rate_limit_rpm":100000000000000000000}`,
			expectedName:    "test-tenant",
			expectedBalance: math.MaxInt64,
			expectedRPM:     math.MaxInt32,
		},
		{
			name:            "19 nines as number",
			jsonInput:       `{"name":"test-tenant","token_balance":9999999999999999999,"rate_limit_rpm":9999999999999999999}`,
			expectedName:    "test-tenant",
			expectedBalance: math.MaxInt64,
			expectedRPM:     math.MaxInt32,
		},
		{
			name:            "Scientific notation 1e20",
			jsonInput:       `{"name":"test-tenant","token_balance":1e20,"rate_limit_rpm":1e10}`,
			expectedName:    "test-tenant",
			expectedBalance: math.MaxInt64,
			expectedRPM:     math.MaxInt32,
		},
		{
			name:            "Huge numbers as string",
			jsonInput:       `{"name":"test-tenant","token_balance":"99999999999999999999999","rate_limit_rpm":"99999999999"}`,
			expectedName:    "test-tenant",
			expectedBalance: math.MaxInt64,
			expectedRPM:     math.MaxInt32,
		},
		{
			name:            "Normal numbers within range",
			jsonInput:       `{"name":"normal-tenant","token_balance":5000000,"rate_limit_rpm":120}`,
			expectedName:    "normal-tenant",
			expectedBalance: 5000000,
			expectedRPM:     120,
		},
		{
			name:            "Omitted balance and rpm",
			jsonInput:       `{"name":"default-tenant"}`,
			expectedName:    "default-tenant",
			expectedBalance: 0,
			expectedRPM:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req CreateTenantRequest
			err := json.Unmarshal([]byte(tc.jsonInput), &req)
			if err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
			if req.Name != tc.expectedName {
				t.Errorf("expected name %q, got %q", tc.expectedName, req.Name)
			}
			if req.TokenBalance != tc.expectedBalance {
				t.Errorf("expected balance %d, got %d", tc.expectedBalance, req.TokenBalance)
			}
			if req.RateLimitRPM != tc.expectedRPM {
				t.Errorf("expected rpm %d, got %d", tc.expectedRPM, req.RateLimitRPM)
			}
		})
	}
}

func TestUpdateTenantRequest_UnmarshalLargeNumbers(t *testing.T) {
	jsonInput := `{"name":"updated-tenant","token_balance":100000000000000000000,"rate_limit_rpm":999999999999,"is_active":true}`
	var req UpdateTenantRequest
	err := json.Unmarshal([]byte(jsonInput), &req)
	if err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if req.Name != "updated-tenant" {
		t.Errorf("expected name updated-tenant, got %s", req.Name)
	}
	if req.TokenBalance != math.MaxInt64 {
		t.Errorf("expected balance clamped to math.MaxInt64 (%d), got %d", int64(math.MaxInt64), req.TokenBalance)
	}
	if req.RateLimitRPM != math.MaxInt32 {
		t.Errorf("expected rpm clamped to math.MaxInt32 (%d), got %d", math.MaxInt32, req.RateLimitRPM)
	}
	if !req.IsActive {
		t.Errorf("expected is_active=true, got false")
	}
}

func TestGinShouldBindJSON_CreateTenantRequest(t *testing.T) {
	// Test that Gin's ShouldBindJSON works with custom UnmarshalJSON and validator
	gin.SetMode(gin.TestMode)

	t.Run("Valid with huge number", func(t *testing.T) {
		body := `{"name":"Acme Corp","token_balance":100000000000000000000,"rate_limit_rpm":999999999999}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		var req CreateTenantRequest
		err := c.ShouldBindJSON(&req)
		if err != nil {
			t.Fatalf("expected ShouldBindJSON to succeed, got error: %v", err)
		}
		if req.Name != "Acme Corp" {
			t.Errorf("expected Acme Corp, got %s", req.Name)
		}
		if req.TokenBalance != math.MaxInt64 {
			t.Errorf("expected TokenBalance math.MaxInt64, got %d", req.TokenBalance)
		}
		if req.RateLimitRPM != math.MaxInt32 {
			t.Errorf("expected RateLimitRPM math.MaxInt32, got %d", req.RateLimitRPM)
		}
	})

	t.Run("Validation failure on missing name", func(t *testing.T) {
		body := `{"token_balance":1000000000}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		var req CreateTenantRequest
		err := c.ShouldBindJSON(&req)
		if err == nil {
			t.Fatalf("expected ShouldBindJSON to fail due to missing required name field")
		}
	})
}
