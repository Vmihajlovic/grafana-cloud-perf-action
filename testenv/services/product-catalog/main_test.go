package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	injectErrorRate = 0
	injectLatencyMs = 0
	injectDBLatency = 0
	os.Exit(m.Run())
}

func TestSearchProducts(t *testing.T) {
	products := testProducts()
	tests := []struct {
		name    string
		query   string
		wantMin int
	}{
		{"single word match", "electronics", 50},
		{"no match", "xyznonexistent", 0},
		{"case insensitive", "PREMIUM", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := searchProducts(tt.query, products)
			if len(results) < tt.wantMin {
				t.Errorf("got %d results, want >= %d", len(results), tt.wantMin)
			}
		})
	}
}

func BenchmarkSearchProducts(b *testing.B) {
	products := testProducts()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		searchProducts("electronics", products)
	}
}

func TestHandleSearchProducts_MissingQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/products/search", nil)
	w := httptest.NewRecorder()
	handleSearchProducts(w, req)
	if w.Result().StatusCode != 400 {
		t.Errorf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestMaybeInjectFailure_NoInjection(t *testing.T) {
	injectErrorRate = 0
	injectLatencyMs = 0
	w := httptest.NewRecorder()
	if maybeInjectFailure(w) {
		t.Error("should not inject when rates are 0")
	}
}

func TestMaybeInjectFailure_AlwaysFail(t *testing.T) {
	injectErrorRate = 1.0
	w := httptest.NewRecorder()
	if !maybeInjectFailure(w) {
		t.Error("should inject when rate is 1.0")
	}
	injectErrorRate = 0
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"ok": "true"})
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("wrong content type")
	}
	var m map[string]string
	json.NewDecoder(w.Result().Body).Decode(&m)
	if m["ok"] != "true" {
		t.Error("unexpected body")
	}
}

func TestSeedDatabase(t *testing.T) {
	testDB, _ := sql.Open("sqlite", ":memory:")
	defer testDB.Close()
	db = testDB
	seedDatabase(testDB)

	var count int
	testDB.QueryRow("SELECT COUNT(*) FROM products").Scan(&count)
	if count != 2000 {
		t.Errorf("expected 2000 products, got %d", count)
	}
}

func testProducts() []Product {
	categories := []string{"electronics", "clothing", "books", "home", "sports", "toys", "food", "tools"}
	var products []Product
	for i := 0; i < 500; i++ {
		products = append(products, Product{
			ID:       int64(i + 1),
			Name:     "Premium " + categories[i%len(categories)] + " Widget",
			Description: "High-quality widget for " + categories[i%len(categories)] + " enthusiasts",
			PriceCents:  int64(1000 + i*37),
			Category: categories[i%len(categories)],
		})
	}
	return products
}
