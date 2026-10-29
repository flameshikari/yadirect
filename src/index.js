import { createApp } from './app.js';

const port = Number(process.env.PORT ?? 3000);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error('PORT must be an integer between 1 and 65535');
}

const server = createApp().listen(port, '0.0.0.0', () => {
  console.log(`Listening on port ${port}`);
});
server.on('error', (error) => {
  console.error('Unable to start server:', error.message);
  process.exit(1);
});

for (const signal of ['SIGTERM', 'SIGINT']) {
  process.once(signal, () => {
    server.close(() => process.exit(0));
    setTimeout(() => process.exit(1), 10_000).unref();
  });
}
