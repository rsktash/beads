import { Hono } from 'hono';
import { login, logout } from '../auth.js';

export function authRouter(auth) {
  const r = new Hono();

  r.post('/login', async (c) => {
    if (!auth.enabled) return c.json({ error: 'auth disabled' }, 400);
    const body = await c.req.json().catch(() => ({}));
    const { username, password } = body;
    if (!username || !password) {
      return c.json({ error: 'username and password are required' }, 400);
    }
    const result = login(auth, username, password);
    if (!result) return c.json({ error: 'invalid credentials' }, 401);
    return c.json(result);
  });

  r.post('/logout', async (c) => {
    if (!auth.enabled) return c.json({ ok: true });
    const token = c.get('token');
    if (token) logout(auth, token);
    return c.json({ ok: true });
  });

  return r;
}
