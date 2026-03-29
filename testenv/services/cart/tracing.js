const { NodeSDK } = require('@opentelemetry/sdk-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-http');
const { ExpressInstrumentation } = require('@opentelemetry/instrumentation-express');
const { HttpInstrumentation } = require('@opentelemetry/instrumentation-http');
const { IORedisInstrumentation } = require('@opentelemetry/instrumentation-ioredis');
const { Resource } = require('@opentelemetry/resources');
const { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION } = require('@opentelemetry/semantic-conventions');

const sdk = new NodeSDK({
  resource: new Resource({
    [ATTR_SERVICE_NAME]: process.env.OTEL_SERVICE_NAME || 'cart',
    [ATTR_SERVICE_VERSION]: '1.0.0',
    'service.namespace': 'demo',
  }),
  traceExporter: new OTLPTraceExporter({
    url: `http://${process.env.OTEL_EXPORTER_OTLP_ENDPOINT || 'alloy.monitoring.svc.cluster.local:4318'}/v1/traces`,
  }),
  instrumentations: [new HttpInstrumentation(), new ExpressInstrumentation(), new IORedisInstrumentation()],
});

sdk.start();
process.on('SIGTERM', () => sdk.shutdown());
