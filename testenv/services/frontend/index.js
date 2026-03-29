require('./tracing');

const express = require('express');
const http = require('http');
const client = require('prom-client');
const { trace, context } = require('@opentelemetry/api');

function log(level, msg, extra = {}) {
  const span = trace.getSpan(context.active());
  const sc = span?.spanContext();
  console.log(JSON.stringify({
    level,
    msg,
    service: 'frontend',
    trace_id: sc?.traceId || '',
    span_id: sc?.spanId || '',
    ...extra,
  }));
}

const app = express();
const port = process.env.PORT || 8080;

const CATALOG_URL = process.env.CATALOG_URL || 'http://product-catalog:8081';
const PRICING_URL = process.env.PRICING_URL || 'http://pricing:8083';
const CART_URL = process.env.CART_URL || 'http://cart:8082';
const CHECKOUT_URL = process.env.CHECKOUT_URL || 'http://checkout:8084';

const requestCounter = new client.Counter({
  name: 'frontend_requests_total',
  help: 'Total frontend requests',
  labelNames: ['route', 'status'],
});

client.collectDefaultMetrics();

// Proxy helper
function proxy(targetUrl, res) {
  return new Promise((resolve, reject) => {
    http.get(targetUrl, (upstream) => {
      let data = '';
      upstream.on('data', (chunk) => (data += chunk));
      upstream.on('end', () => {
        res.status(upstream.statusCode).set('Content-Type', 'application/json').send(data);
        resolve();
      });
    }).on('error', (err) => {
      res.status(502).json({ error: `upstream error: ${err.message}` });
      reject(err);
    });
  });
}

// Routes — proxy to backend services

app.get('/api/products', (req, res) => {
  const qs = req.url.split('?')[1] || '';
  proxy(`${CATALOG_URL}/api/products?${qs}`, res)
    .then(() => requestCounter.inc({ route: 'products', status: '200' }))
    .catch(() => requestCounter.inc({ route: 'products', status: '502' }));
});

app.get('/api/products/search', (req, res) => {
  const qs = req.url.split('?')[1] || '';
  proxy(`${CATALOG_URL}/api/products/search?${qs}`, res)
    .then(() => requestCounter.inc({ route: 'search', status: '200' }))
    .catch(() => requestCounter.inc({ route: 'search', status: '502' }));
});

app.get('/api/products/:id', (req, res) => {
  proxy(`${CATALOG_URL}/api/products/${req.params.id}`, res)
    .then(() => requestCounter.inc({ route: 'product', status: '200' }))
    .catch(() => requestCounter.inc({ route: 'product', status: '502' }));
});

app.get('/api/categories', (req, res) => {
  proxy(`${CATALOG_URL}/api/categories`, res)
    .then(() => requestCounter.inc({ route: 'categories', status: '200' }))
    .catch(() => requestCounter.inc({ route: 'categories', status: '502' }));
});

app.get('/api/prices/bulk', (req, res) => {
  const qs = req.url.split('?')[1] || '';
  proxy(`${PRICING_URL}/api/prices/bulk?${qs}`, res)
    .then(() => requestCounter.inc({ route: 'prices', status: '200' }))
    .catch(() => requestCounter.inc({ route: 'prices', status: '502' }));
});

app.post('/api/cart/:action', express.json(), (req, res) => {
  const opts = {
    hostname: new URL(CART_URL).hostname,
    port: new URL(CART_URL).port || 8082,
    path: `/api/cart/${req.params.action}`,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  };
  const upstream = http.request(opts, (upRes) => {
    let data = '';
    upRes.on('data', (chunk) => (data += chunk));
    upRes.on('end', () => res.status(upRes.statusCode).set('Content-Type', 'application/json').send(data));
  });
  upstream.on('error', (err) => res.status(502).json({ error: err.message }));
  upstream.write(JSON.stringify(req.body));
  upstream.end();
  requestCounter.inc({ route: 'cart', status: '200' });
});

app.post('/api/checkout', express.json(), (req, res) => {
  const opts = {
    hostname: new URL(CHECKOUT_URL).hostname,
    port: new URL(CHECKOUT_URL).port || 8084,
    path: '/api/checkout',
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  };
  const upstream = http.request(opts, (upRes) => {
    let data = '';
    upRes.on('data', (chunk) => (data += chunk));
    upRes.on('end', () => res.status(upRes.statusCode).set('Content-Type', 'application/json').send(data));
  });
  upstream.on('error', (err) => res.status(502).json({ error: err.message }));
  upstream.write(JSON.stringify(req.body));
  upstream.end();
  requestCounter.inc({ route: 'checkout', status: '200' });
});

app.get('/healthz', (req, res) => res.sendStatus(200));
app.get('/metrics', async (req, res) => {
  res.set('Content-Type', client.register.contentType);
  res.send(await client.register.metrics());
});

app.listen(port, () => log('info', `frontend listening on :${port}`));
