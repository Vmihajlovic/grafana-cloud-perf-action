package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "modernc.org/sqlite"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "product_catalog_requests_total",
		Help: "Total requests by endpoint and status",
	}, []string{"endpoint", "status"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "product_catalog_request_duration_seconds",
		Help:    "Request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"endpoint"})

	productsServed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "product_catalog_products_served_total",
		Help: "Total products returned across all requests",
	})
)

var (
	db     *sql.DB
	tracer trace.Tracer
	logger *slog.Logger

	injectErrorRate float64
	injectLatencyMs int
	injectDBLatency int
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

	injectErrorRate, _ = strconv.ParseFloat(os.Getenv("INJECT_ERROR_RATE"), 64)
	injectLatencyMs, _ = strconv.Atoi(os.Getenv("INJECT_LATENCY_MS"))
	injectDBLatency, _ = strconv.Atoi(os.Getenv("INJECT_DB_LATENCY_MS"))

	prometheus.MustRegister(requestsTotal, requestDuration, productsServed)

	pyroscopeURL := env("PYROSCOPE_SERVER_ADDRESS", "http://pyroscope.monitoring.svc.cluster.local:4040")
	_, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: "product-catalog",
		ServerAddress:   pyroscopeURL,
		Tags:            map[string]string{"service_name": "product-catalog", "service_namespace": "demo"},
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU, pyroscope.ProfileAllocObjects, pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects, pyroscope.ProfileInuseSpace, pyroscope.ProfileGoroutines,
		},
	})
	if err != nil {
		logger.Warn("pyroscope init failed", "error", err)
	}

	tp, err := initTracer(ctx)
	if err != nil {
		logger.Error("tracer init failed", "error", err)
		os.Exit(1)
	}
	defer tp.Shutdown(ctx)
	tracer = otel.Tracer("product-catalog")

	dbPath := env("DATABASE_PATH", "/data/product-catalog.db")
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	seedDatabase(db)

	mux := http.NewServeMux()
	mux.Handle("GET /api/products", otelhttp.NewHandler(instrument("list", http.HandlerFunc(handleListProducts)), "GET /api/products"))
	mux.Handle("GET /api/products/search", otelhttp.NewHandler(instrument("search", http.HandlerFunc(handleSearchProducts)), "GET /api/products/search"))
	mux.Handle("GET /api/products/{id}", otelhttp.NewHandler(instrument("get", http.HandlerFunc(handleGetProduct)), "GET /api/products/{id}"))
	mux.Handle("GET /api/categories", otelhttp.NewHandler(instrument("categories", http.HandlerFunc(handleListCategories)), "GET /api/categories"))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db not ready", 503)
			return
		}
		w.WriteHeader(200)
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	port := env("PORT", "8081")
	srv := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		logger.Info("product-catalog starting", "port", port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	srv.Shutdown(context.Background())
}

// ─── Handlers ────────────────────────────────────────────────

func handleListProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if maybeInjectFailure(w) {
		return
	}

	limit := intParam(r, "limit", 50)
	offset := intParam(r, "offset", 0)
	category := r.URL.Query().Get("category")

	_, span := tracer.Start(ctx, "ListProducts", trace.WithAttributes(
		attribute.Int("limit", limit), attribute.Int("offset", offset),
	))
	defer span.End()

	maybeDBLatency()

	var rows *sql.Rows
	var err error
	if category != "" {
		rows, err = db.QueryContext(ctx, "SELECT id, name, description, price_cents, category, image_url, stock FROM products WHERE category = ? ORDER BY id LIMIT ? OFFSET ?", category, limit, offset)
	} else {
		rows, err = db.QueryContext(ctx, "SELECT id, name, description, price_cents, category, image_url, stock FROM products ORDER BY id LIMIT ? OFFSET ?", limit, offset)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	products := scanProducts(rows)
	productsServed.Add(float64(len(products)))
	writeJSON(w, products)
}

func handleSearchProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if maybeInjectFailure(w) {
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error":"missing query parameter 'q'"}`, 400)
		return
	}

	_, span := tracer.Start(ctx, "SearchProducts", trace.WithAttributes(
		attribute.String("search.query", query),
	))
	defer span.End()

	maybeDBLatency()

	rows, err := db.QueryContext(ctx, "SELECT id, name, description, price_cents, category, image_url, stock FROM products")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	allProducts := scanProducts(rows)

	// searchProducts uses regex-based relevance scoring.
	// This implementation compiles a new regex for each word x each product,
	// which is correct but has a performance problem under load.
	results := searchProducts(query, allProducts)

	span.SetAttributes(attribute.Int("search.results", len(results)))
	productsServed.Add(float64(len(results)))
	writeJSON(w, results)
}

func handleGetProduct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if maybeInjectFailure(w) {
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid product id"}`, 400)
		return
	}

	_, span := tracer.Start(ctx, "GetProduct", trace.WithAttributes(attribute.Int64("product.id", id)))
	defer span.End()

	maybeDBLatency()

	p := Product{}
	err = db.QueryRowContext(ctx, "SELECT id, name, description, price_cents, category, image_url, stock FROM products WHERE id = ?", id).Scan(&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Category, &p.ImageURL, &p.Stock)
	if err != nil {
		http.Error(w, `{"error":"product not found"}`, 404)
		return
	}
	writeJSON(w, p)
}

func handleListCategories(w http.ResponseWriter, r *http.Request) {
	if maybeInjectFailure(w) {
		return
	}
	rows, err := db.QueryContext(r.Context(), "SELECT DISTINCT category FROM products ORDER BY category")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var cats []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		cats = append(cats, c)
	}
	writeJSON(w, cats)
}

// ─── Search ──────────────────────────────────────────────────

// searchProducts performs regex-based multi-field relevance search.
// It compiles a regex pattern for each search term against each product,
// scores matches across name/description/category, then sorts by score.
//
// PERFORMANCE NOTE: This function compiles a new regex per word per product.
// With 2000 products and a single-word query, that's 2000 regex compilations
// per request. Under concurrent load, this becomes CPU-bound and is the
// primary bottleneck visible in continuous profiling.
func searchProducts(query string, products []Product) []Product {
	type scored struct {
		product Product
		score   int
	}
	var results []scored

	words := strings.Fields(query)
	if len(words) == 0 {
		words = []string{query}
	}

	for _, p := range products {
		score := 0
		for _, word := range words {
			pattern := fmt.Sprintf("(?i)\\b%s", regexp.QuoteMeta(word))
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			if re.MatchString(p.Name) {
				score += 10
			}
			if re.MatchString(p.Description) {
				score += 5
			}
			if re.MatchString(p.Category) {
				score += 3
			}
		}
		if score > 0 {
			h := sha256.New()
			h.Write([]byte(fmt.Sprintf("%s:%s:%d", p.Name, query, p.ID)))
			_ = hex.EncodeToString(h.Sum(nil))
			results = append(results, scored{product: p, score: score})
		}
	}

	// Sort by relevance score (bubble sort — O(n^2))
	for i := 0; i < len(results); i++ {
		for j := 0; j < len(results)-i-1; j++ {
			if results[j].score < results[j+1].score {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}

	out := make([]Product, 0, len(results))
	for _, r := range results {
		out = append(out, r.product)
	}
	return out
}

// ─── Data Model & DB ─────────────────────────────────────────

type Product struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
	Category    string `json:"category"`
	ImageURL    string `json:"image_url"`
	Stock       int    `json:"stock"`
}

func scanProducts(rows *sql.Rows) []Product {
	var products []Product
	for rows.Next() {
		p := Product{}
		rows.Scan(&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Category, &p.ImageURL, &p.Stock)
		products = append(products, p)
	}
	if products == nil {
		products = []Product{}
	}
	return products
}

func seedDatabase(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS products (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		price_cents INTEGER NOT NULL,
		category TEXT NOT NULL,
		image_url TEXT DEFAULT '',
		stock INTEGER DEFAULT 0
	)`)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM products").Scan(&count)
	if count > 0 {
		logger.Info("database already seeded", "products", count)
		return
	}

	logger.Info("seeding database")
	categories := []string{"electronics", "clothing", "books", "home", "sports", "toys", "food", "tools"}
	adjectives := []string{"Premium", "Essential", "Classic", "Modern", "Eco-Friendly", "Professional", "Compact", "Deluxe"}
	nouns := []string{"Widget", "Gadget", "Kit", "Set", "Pack", "Bundle", "Collection", "System"}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO products (name, description, price_cents, category, image_url, stock) VALUES (?, ?, ?, ?, ?, ?)")
	for i := 0; i < 2000; i++ {
		cat := categories[i%len(categories)]
		adj := adjectives[i%len(adjectives)]
		noun := nouns[(i/len(adjectives))%len(nouns)]
		name := fmt.Sprintf("%s %s %s %d", adj, cat, noun, i+1)
		desc := fmt.Sprintf("High-quality %s %s for %s enthusiasts. Model %d with advanced features and durable construction.", adj, noun, cat, i+1)
		price := 500 + rand.Intn(50000)
		stock := rand.Intn(200)
		stmt.Exec(name, desc, price, cat, fmt.Sprintf("/images/products/%d.jpg", i+1), stock)
	}
	tx.Commit()
	logger.Info("seeded 2000 products")
}

// ─── Helpers ─────────────────────────────────────────────────

func instrument(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		duration := time.Since(start).Seconds()
		requestDuration.WithLabelValues(name).Observe(duration)
		requestsTotal.WithLabelValues(name, strconv.Itoa(rec.status)).Inc()

		// Structured log with trace context for log-to-trace correlation
		span := trace.SpanFromContext(r.Context())
		sc := span.SpanContext()
		logger.Info("request completed",
			"endpoint", name,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", int(duration*1000),
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func maybeInjectFailure(w http.ResponseWriter) bool {
	if injectLatencyMs > 0 {
		time.Sleep(time.Duration(injectLatencyMs) * time.Millisecond)
	}
	if injectErrorRate > 0 && rand.Float64() < injectErrorRate {
		http.Error(w, `{"error":"injected failure"}`, 500)
		return true
	}
	return false
}

func maybeDBLatency() {
	if injectDBLatency > 0 {
		time.Sleep(time.Duration(injectDBLatency) * time.Millisecond)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func intParam(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
	endpoint := env("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy.monitoring.svc.cluster.local:4318")
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, err
	}

	res, _ := sdkresource.New(ctx, sdkresource.WithAttributes(
		semconv.ServiceName(env("OTEL_SERVICE_NAME", "product-catalog")),
		semconv.ServiceVersion("1.0.0"),
		attribute.String("service.namespace", "demo"),
	))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Wrap with otelpyroscope to add pyroscope.profile.id to each span,
	// enabling the trace-to-profile link icon in Grafana
	otelTp := otelpyroscope.NewTracerProvider(tp,
		otelpyroscope.WithAppName("product-catalog"),
		otelpyroscope.WithRootSpanOnly(true),
		otelpyroscope.WithAddSpanName(true),
	)
	otel.SetTracerProvider(otelTp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return tp, nil
}
