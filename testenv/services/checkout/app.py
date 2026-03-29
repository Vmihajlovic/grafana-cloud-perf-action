"""Checkout service — aggregates cart + pricing into an order."""

import json
import logging
import os
import sys
from urllib.request import urlopen, Request
from urllib.error import URLError

from flask import Flask, jsonify, request
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.instrumentation.urllib import URLLibInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from prometheus_client import Counter, Histogram, make_wsgi_app, REGISTRY
from werkzeug.middleware.dispatcher import DispatcherMiddleware


class TraceLogFormatter(logging.Formatter):
    """JSON log formatter that includes trace_id and span_id."""
    def format(self, record):
        span = trace.get_current_span()
        ctx = span.get_span_context() if span else None
        log_entry = {
            "level": record.levelname,
            "msg": record.getMessage(),
            "service": "checkout",
            "trace_id": format(ctx.trace_id, '032x') if ctx and ctx.trace_id else "",
            "span_id": format(ctx.span_id, '016x') if ctx and ctx.span_id else "",
        }
        return json.dumps(log_entry)


handler = logging.StreamHandler(sys.stdout)
handler.setFormatter(TraceLogFormatter())
logging.basicConfig(level=logging.INFO, handlers=[handler])
logger = logging.getLogger(__name__)

checkout_requests = Counter("checkout_requests_total", "Checkout requests", ["status"])
checkout_duration = Histogram("checkout_duration_seconds", "Checkout duration")

# OTel setup
resource = Resource.create({
    "service.name": os.getenv("OTEL_SERVICE_NAME", "checkout"),
    "service.version": "1.0.0",
    "service.namespace": "demo",
})
provider = TracerProvider(resource=resource)
otlp_endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy.monitoring.svc.cluster.local:4318")
exporter = OTLPSpanExporter(endpoint=f"http://{otlp_endpoint}/v1/traces")
provider.add_span_processor(BatchSpanProcessor(exporter))
trace.set_tracer_provider(provider)
tracer = trace.get_tracer("checkout")

CART_URL = os.getenv("CART_URL", "http://cart:8082")
PRICING_URL = os.getenv("PRICING_URL", "http://pricing:8083")

app = Flask(__name__)
FlaskInstrumentor().instrument_app(app)
URLLibInstrumentor().instrument()

# Mount Prometheus metrics at /metrics
app.wsgi_app = DispatcherMiddleware(app.wsgi_app, {"/metrics": make_wsgi_app()})


def fetch_json(url, method="GET", body=None):
    """Fetch JSON from an internal service."""
    req = Request(url, method=method)
    req.add_header("Content-Type", "application/json")
    if body:
        data = json.dumps(body).encode()
    else:
        data = None
    try:
        with urlopen(req, data, timeout=10) as resp:
            return json.loads(resp.read())
    except URLError as e:
        logger.error(f"Failed to fetch {url}: {e}")
        return None


@app.route("/api/checkout", methods=["POST"])
def checkout():
    data = request.get_json()
    user_id = data.get("user_id")
    if not user_id:
        return jsonify({"error": "missing user_id"}), 400

    with tracer.start_as_current_span("Checkout", attributes={"user.id": user_id}):
        # Get cart
        with tracer.start_as_current_span("GetCart"):
            cart = fetch_json(f"{CART_URL}/api/cart/get", method="POST", body={"user_id": user_id})
            if not cart or not cart.get("items"):
                return jsonify({"error": "cart is empty"}), 400

        items = cart["items"]
        product_ids = [str(item["product_id"]) for item in items]

        # Get prices
        with tracer.start_as_current_span("GetPrices", attributes={"products.count": len(product_ids)}):
            ids_param = ",".join(product_ids)
            prices = fetch_json(f"{PRICING_URL}/api/prices/bulk?ids={ids_param}")

        if not prices:
            return jsonify({"error": "pricing service unavailable"}), 502

        # Build price lookup
        price_map = {p["product_id"]: p for p in prices}

        # Calculate total
        total = 0
        order_items = []
        for item in items:
            pid = item["product_id"]
            qty = item["quantity"]
            price_info = price_map.get(pid, {})
            unit_price = price_info.get("price_cents", 0)
            line_total = unit_price * qty
            total += line_total
            order_items.append({
                "product_id": pid,
                "quantity": qty,
                "unit_price_cents": unit_price,
                "line_total_cents": line_total,
            })

        # Clear cart after checkout
        with tracer.start_as_current_span("ClearCart"):
            fetch_json(f"{CART_URL}/api/cart/clear", method="POST", body={"user_id": user_id})

        order = {
            "user_id": user_id,
            "items": order_items,
            "total_cents": total,
            "status": "completed",
        }

        logger.info(f"Order completed for user {user_id}: {total} cents, {len(order_items)} items")
        return jsonify(order)


@app.route("/healthz")
def healthz():
    return "ok"


@app.route("/readyz")
def readyz():
    return "ok"


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8084"))
    app.run(host="0.0.0.0", port=port)
