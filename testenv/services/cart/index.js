require('./tracing');

const express = require('express');
const Redis = require('ioredis');
const client = require('prom-client');
const { trace, context } = require('@opentelemetry/api');

function log(level, msg, extra = {}) {
  const span = trace.getSpan(context.active());
  const sc = span?.spanContext();
  console.log(JSON.stringify({
    level,
    msg,
    service: 'cart',
    trace_id: sc?.traceId || '',
    span_id: sc?.spanId || '',
    ...extra,
  }));
}

const app = express();
app.use(express.json());

const port = process.env.PORT || 8082;
const redis = new Redis(process.env.REDIS_URL || 'redis://redis:6379', {
  retryStrategy: (times) => Math.min(times * 500, 5000),
  maxRetriesPerRequest: 3,
  lazyConnect: false,
});
redis.on('error', (err) => console.error(JSON.stringify({ level: 'error', msg: 'redis connection error', error: err.message })));
redis.on('connect', () => console.log(JSON.stringify({ level: 'info', msg: 'redis connected' })));

client.collectDefaultMetrics();
const cartOps = new client.Counter({
  name: 'cart_operations_total',
  help: 'Cart operations',
  labelNames: ['operation'],
});

// Add item to cart
app.post('/api/cart/add', async (req, res) => {
  const { user_id, product_id, quantity } = req.body;
  if (!user_id || !product_id) {
    return res.status(400).json({ error: 'missing user_id or product_id' });
  }
  const key = `cart:${user_id}`;
  await redis.hset(key, product_id.toString(), quantity || 1);
  await redis.expire(key, 3600); // 1 hour TTL
  cartOps.inc({ operation: 'add' });
  res.json({ status: 'added', user_id, product_id, quantity: quantity || 1 });
});

// Get cart contents
app.post('/api/cart/get', async (req, res) => {
  const { user_id } = req.body;
  if (!user_id) {
    return res.status(400).json({ error: 'missing user_id' });
  }
  const items = await redis.hgetall(`cart:${user_id}`);
  const cart = Object.entries(items).map(([product_id, quantity]) => ({
    product_id: parseInt(product_id),
    quantity: parseInt(quantity),
  }));
  cartOps.inc({ operation: 'get' });
  res.json({ user_id, items: cart });
});

// Clear cart
app.post('/api/cart/clear', async (req, res) => {
  const { user_id } = req.body;
  if (!user_id) {
    return res.status(400).json({ error: 'missing user_id' });
  }
  await redis.del(`cart:${user_id}`);
  cartOps.inc({ operation: 'clear' });
  res.json({ status: 'cleared', user_id });
});

app.get('/healthz', (req, res) => res.sendStatus(200));
app.get('/readyz', async (req, res) => {
  try {
    await redis.ping();
    res.sendStatus(200);
  } catch {
    res.sendStatus(503);
  }
});
app.get('/metrics', async (req, res) => {
  res.set('Content-Type', client.register.contentType);
  res.send(await client.register.metrics());
});

app.listen(port, () => log('info', `cart service listening on :${port}`));
