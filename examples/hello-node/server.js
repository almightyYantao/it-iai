const http = require('http');
const port = process.env.PORT || 3000;
const project = process.env.VIBEDEPLOY_PROJECT || 'unknown';
const url = process.env.VIBEDEPLOY_URL || '';

http.createServer((req, res) => {
  res.setHeader('Content-Type', 'text/html; charset=utf-8');
  res.end(`<!doctype html>
<html><body style="font-family:system-ui;max-width:640px;margin:40px auto;padding:0 16px">
  <h1>👋 Hello from ${project}</h1>
  <p>You reached <code>${req.url}</code>.</p>
  <p>Deployed at: <a href="${url}">${url || '(no URL)'}</a></p>
  <p>User: ${req.headers['x-vibedeploy-user-email'] || '(anonymous)'}</p>
</body></html>`);
}).listen(port, () => console.log(`hello-node listening on :${port}`));
