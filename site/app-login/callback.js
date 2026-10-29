(() => {
  'use strict';
  const params = new URLSearchParams(location.search);
  for (const [key, value] of new URLSearchParams(location.hash.slice(1))) params.set(key, value);
  history.replaceState(null, '', location.pathname);
  const message = document.getElementById('message');
  const button = document.getElementById('continue');
  try {
    const rawState = params.get('state');
    if (!rawState || rawState.length > 4096) throw new Error();
    const encoded = rawState.replace(/-/g, '+').replace(/_/g, '/');
    const state = JSON.parse(atob(encoded.padEnd(Math.ceil(encoded.length / 4) * 4, '=')));
    if (!['AniList', 'MAL'].includes(state.service) || typeof state.nonce !== 'string' || state.nonce.length < 32) throw new Error();
    if (params.has('error')) {
      message.textContent = 'Login was declined. Start again from Accounts in Otakase.';
      return;
    }
    if (!(state.service === 'AniList' ? params.get('access_token') : params.get('code'))) throw new Error();
    const transfer = new URLSearchParams();
    for (const key of ['state', 'access_token', 'token_type', 'expires_in', 'code']) {
      if (params.has(key)) transfer.set(key, params.get(key));
    }
    let target = 'otakase://login?' + transfer.toString();
    const tv = Object.hasOwn(state, 'host');
    if (tv) {
      const parts = typeof state.host === 'string' ? state.host.split('.').map(Number) : [];
      const privateIP = parts.length === 4 && parts.every(p => Number.isInteger(p) && p >= 0 && p <= 255) &&
        (parts[0] === 10 || (parts[0] === 192 && parts[1] === 168) || (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31));
      if (!privateIP || !/^\d+\.\d+\.\d+\.\d+$/.test(state.host) || !Number.isInteger(state.port) || state.port < 1024 || state.port > 65535) throw new Error();
      target = 'http://' + state.host + ':' + state.port + '/login?' + transfer.toString();
    }
    button.hidden = false;
    button.textContent = tv ? 'Finish sign in on your TV' : 'Open Otakase';
    button.onclick = () => location.assign(target);
    message.textContent = tv
      ? 'Keep your phone and TV on the same home network. Tap below to finish sign in on your TV.'
      : 'Opening Otakase. Tap below if your browser asks you to continue.';
    if (!tv) location.assign(target);
  } catch (_) {
    message.textContent = 'This login is incomplete or expired. Start again from Accounts in Otakase.';
  }
})();
