// AccountHub: one Durable Object per account. Every signed-in device on
// screen keeps a WebSocket to it. The sockets hibernate, so an idle
// connection costs nothing and pings are answered without waking the hub.
//
// The hub tells devices when synced data changed, carries phone remote and
// Play on messages between devices on any network, keeps the inbox (airing
// alerts, shared titles, support replies) until a device takes it, and
// passes playback heartbeats on to the Discord bot.

import { DurableObject } from 'cloudflare:workers';
import { applyPush } from './sync.js';
import { HttpError, json, errorResponse, now, readJSON } from './util.js';

export const maxMessage = 64 * 1024;
export const maxInbox = 50;
export const inboxTTL = 14 * 24 * 60 * 60 * 1000;
const heartbeatEvery = 20 * 1000;

export class AccountHub extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('ping', 'pong'));
    this.lastBeat = new Map();
  }

  async fetch(request) {
    const url = new URL(request.url);
    const account = request.headers.get('x-account') || '';
    try {
      switch (url.pathname) {
        case '/live': return this.connect(request, account);
        case '/sync': {
          const body = await readJSON(request, 2 * 1024 * 1024);
          const result = await applyPush(this.env, account, body.docs);
          if (result.changed) this.broadcast({ t: 'sync', v: result.version, from: body.device }, body.device);
          return json(result);
        }
        case '/notify': {
          const msg = await readJSON(request, maxMessage);
          this.broadcast(msg, msg.from);
          return json({ ok: true });
        }
        case '/kick': {
          const { device, all } = await readJSON(request);
          if (all) {
            for (const ws of this.ctx.getWebSockets()) ws.close(4001, 'removed');
            await this.ctx.storage.deleteAll();
            return json({ ok: true });
          }
          for (const ws of this.ctx.getWebSockets(device)) ws.close(4001, 'removed');
          this.broadcast({ t: 'devices' });
          return json({ ok: true });
        }
        case '/inbox': {
          const { item } = await readJSON(request, 8192);
          return json(await this.addInbox(item));
        }
        case '/online': return json({ online: this.online() });
        default: return errorResponse(404, 'not found');
      }
    } catch (e) {
      if (e instanceof HttpError) return errorResponse(e.status, e.message, e.extra);
      throw e;
    }
  }

  async connect(request, account) {
    if (request.headers.get('upgrade') !== 'websocket') return errorResponse(426, 'websocket only');
    const device = request.headers.get('x-device') || '';
    const info = {
      account,
      device,
      name: request.headers.get('x-name') || 'Device',
      tv: request.headers.get('x-tv') === '1',
      discord: request.headers.get('x-discord') || '',
      os: request.headers.get('x-os') || '',
      remote: request.headers.get('x-remote') || '',
      host: request.headers.get('x-host') === '1',
    };
    // One socket per device: a reconnect replaces the old one.
    for (const old of this.ctx.getWebSockets(device)) old.close(4000, 'replaced');
    const [client, server] = Object.values(new WebSocketPair());
    this.ctx.acceptWebSocket(server, [device]);
    server.serializeAttachment(info);
    server.send(JSON.stringify({ t: 'hello', device, online: this.online() }));
    const inbox = await this.inbox();
    if (inbox.length) server.send(JSON.stringify({ t: 'inbox', items: inbox }));
    this.broadcast({ t: 'presence', online: this.online() }, device);
    return new Response(null, { status: 101, webSocket: client });
  }

  online() {
    const seen = new Map();
    for (const ws of this.ctx.getWebSockets()) {
      const a = ws.deserializeAttachment();
      if (a && !seen.has(a.device)) seen.set(a.device, { device: a.device, name: a.name, tv: a.tv, os: a.os, remote: a.remote, host: a.host });
    }
    return [...seen.values()];
  }

  /** Sends msg to every socket except the one of device skip. */
  broadcast(msg, skip) {
    const text = JSON.stringify(msg);
    for (const ws of this.ctx.getWebSockets()) {
      const a = ws.deserializeAttachment();
      if (skip && a && a.device === skip) continue;
      try { ws.send(text); } catch { /* closing */ }
    }
  }

  async webSocketMessage(ws, message) {
    if (typeof message !== 'string' || message.length > maxMessage) return;
    let msg;
    try { msg = JSON.parse(message); } catch { return; }
    if (!msg || typeof msg !== 'object') return;
    const me = ws.deserializeAttachment();
    if (!me) return;
    switch (msg.t) {
      case 'to': {
        // Phone remote, Play on, send to device, now-playing status.
        const targets = typeof msg.to === 'string' ? this.ctx.getWebSockets(msg.to) : [];
        if (!targets.length) {
          ws.send(JSON.stringify({ t: 'offline', to: msg.to, id: msg.id }));
          return;
        }
        const out = JSON.stringify({ t: 'msg', from: me.device, from_name: me.name, from_tv: me.tv, id: msg.id, m: msg.m });
        for (const t of targets) try { t.send(out); } catch { /* closing */ }
        return;
      }
      case 'presence':
        ws.send(JSON.stringify({ t: 'presence', online: this.online() }));
        return;
      case 'ack': {
        const ids = new Set(Array.isArray(msg.ids) ? msg.ids : []);
        const box = (await this.inbox()).filter((it) => !ids.has(it.id));
        await this.ctx.storage.put('inbox', box);
        return;
      }
      case 'hb':
        await this.heartbeat(me, msg.body);
        return;
      default:
    }
  }

  async webSocketClose(ws) {
    const me = ws.deserializeAttachment();
    // Other sockets learn this device left. The closing one is skipped.
    if (me) this.broadcast({ t: 'presence', online: this.online().filter((d) => d.device !== me.device) }, me.device);
  }

  async webSocketError(ws) {
    await this.webSocketClose(ws);
  }

  async inbox() {
    const box = (await this.ctx.storage.get('inbox')) || [];
    const fresh = box.filter((it) => now() - it.at < inboxTTL);
    if (fresh.length !== box.length) await this.ctx.storage.put('inbox', fresh);
    return fresh;
  }

  async addInbox(item) {
    if (!item || typeof item !== 'object') throw new HttpError(400, 'bad item');
    const stored = { ...item, id: crypto.randomUUID(), at: now() };
    const box = [...(await this.inbox()), stored].slice(-maxInbox);
    await this.ctx.storage.put('inbox', box);
    this.broadcast({ t: 'inbox', items: [stored] });
    return { id: stored.id, delivered: this.online().length };
  }

  /** Passes a playback heartbeat to the Discord bot, at most every 20 s. */
  async heartbeat(me, body) {
    if (!me.discord || !this.env.BOT_URL || !this.env.BOT_SECRET || !body || typeof body !== 'object') return;
    const last = this.lastBeat.get(me.device) || 0;
    if (now() - last < heartbeatEvery) return;
    this.lastBeat.set(me.device, now());
    try {
      await fetch(`${this.env.BOT_URL}/v1/heartbeat`, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'x-otakase-account': this.env.BOT_SECRET,
          'x-discord-user': me.discord,
          'x-otakase-device': me.device,
        },
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(5000),
      });
    } catch {
      /* The bot being offline only pauses Discord watch time. */
    }
  }
}
