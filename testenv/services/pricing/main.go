package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "modernc.org/sqlite"

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
	pricingRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pricing_requests_total",
		Help: "Total pricing requests",
	}, []string{"endpoint", "status"})

	pricingDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pricing_request_duration_seconds",
		Help:    "Pricing request duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"endpoint"})
)

var (
	db     *sql.DB
	tracer trace.Tracer
	logger *slog.Logger
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	prometheus.MustRegister(pricingRequests, pricingDuration)

	pyroscopeURL := env("PYROSCOPE_SERVER_ADDRESS", "http://pyroscope.monitoring.svc.cluster.local:4040")
	pyroscope.Start(pyroscope.Config{
		ApplicationName: "pricing",
		ServerAddress:   pyroscopeURL,
		Tags:            map[string]string{"service_name": "pricing", "service_namespace": "demo"},
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU, pyroscope.ProfileAllocObjects, pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects, pyroscope.ProfileInuseSpace, pyroscope.ProfileGoroutines,
		},
	})

	tp, err := initTracer(ctx)
	if err != nil {
		logger.Error("tracer init failed", "error", err)
		os.Exit(1)
	}
	defer tp.Shutdown(ctx)
	tracer = otel.Tracer("pricing")

	dbPath := env("DATABASE_PATH", "/data/pricing.db")
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	seedPriceDatabase(db)

	mux := http.NewServeMux()
	mux.Handle("GET /api/prices/{id}", otelhttp.NewHandler(instrumentPricing("single", http.HandlerFunc(handleGetPrice)), "GET /api/prices/{id}"))
	mux.Handle("GET /api/prices/bulk", otelhttp.NewHandler(instrumentPricing("bulk", http.HandlerFunc(handleGetBulkPrices)), "GET /api/prices/bulk"))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db not ready", 503)
			return
		}
		w.WriteHeader(200)
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	port := env("PORT", "8083")
	srv := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		logger.Info("pricing service starting", "port", port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	srv.Shutdown(context.Background())
}

type PriceResponse struct {
	ProductID int64 `json:"product_id"`
	BasePrice int64 `json:"base_price_cents"`
	Price     int64 `json:"price_cents"`
	Discount  int   `json:"discount_pct"`
}

func handleGetPrice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, 400)
		return
	}

	_, span := tracer.Start(ctx, "GetPrice", trace.WithAttributes(attribute.Int64("product.id", id)))
	defer span.End()

	price, err := queryPrice(ctx, id)
	if err != nil {
		http.Error(w, `{"error":"price not found"}`, 404)
		return
	}
	writeJSON(w, price)
}

// handleGetBulkPrices returns prices for multiple products.
// N+1 QUERY BUG: Queries each product individually instead of using a batch query.
// Under load, this saturates the database with sequential queries.
// Visible in traces as N sequential GetSinglePrice spans inside GetBulkPrices.
// Visible in wall-clock profiles as time spent in database/sql.QueryRow.
func handleGetBulkPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		http.Error(w, `{"error":"missing 'ids' parameter"}`, 400)
		return
	}

	idStrs := strings.Split(idsParam, ",")
	_, span := tracer.Start(ctx, "GetBulkPrices", trace.WithAttributes(
		attribute.Int("products.count", len(idStrs)),
	))
	defer span.End()

	var prices []PriceResponse
	for _, idStr := range idStrs {
		id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if err != nil {
			continue
		}
		// N+1 BUG: One query per product instead of batch
		_, childSpan := tracer.Start(ctx, "GetSinglePrice", trace.WithAttributes(
			attribute.Int64("product.id", id),
		))
		price, err := queryPrice(ctx, id)
		childSpan.End()
		if err == nil {
			prices = append(prices, *price)
		}
	}

	span.SetAttributes(attribute.Int("prices.returned", len(prices)))
	writeJSON(w, prices)
}

func queryPrice(ctx context.Context, productID int64) (*PriceResponse, error) {
	var basePrice int64
	err := db.QueryRowContext(ctx, "SELECT base_price_cents FROM prices WHERE product_id = ?", productID).Scan(&basePrice)
	if err != nil {
		return nil, err
	}

	discount := 0
	if basePrice > 10000 {
		discount = 10
	}
	finalPrice := basePrice - (basePrice * int64(discount) / 100)

	return &PriceResponse{
		ProductID: productID,
		BasePrice: basePrice,
		Price:     finalPrice,
		Discount:  discount,
	}, nil
}

func seedPriceDatabase(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS prices (
		product_id INTEGER PRIMARY KEY,
		base_price_cents INTEGER NOT NULL
	)`)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM prices").Scan(&count)
	if count > 0 {
		return
	}

	logger.Info("seeding price database")
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO prices (product_id, base_price_cents) VALUES (?, ?)")
	for i := 1; i <= 2000; i++ {
		stmt.Exec(i, 500+rand.Intn(50000))
	}
	tx.Commit()
	logger.Info("seeded 2000 prices")
}

func instrumentPricing(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		duration := time.Since(start).Seconds()
		pricingDuration.WithLabelValues(name).Observe(duration)
		pricingRequests.WithLabelValues(name, strconv.Itoa(rec.status)).Inc()

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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
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
		semconv.ServiceName(env("OTEL_SERVICE_NAME", "pricing")),
		semconv.ServiceVersion("1.0.0"),
		attribute.String("service.namespace", "demo"),
	))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return tp, nil
}
