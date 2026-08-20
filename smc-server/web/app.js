const loginView = document.querySelector('#login-view');
const appView = document.querySelector('#app-view');
const loginForm = document.querySelector('#login-form');
const loginError = document.querySelector('#login-error');
const dashboardError = document.querySelector('#dashboard-error');
const directoryError = document.querySelector('#directory-error');
let directoryMode = 'players';
let disconnectTarget = null;

async function api(path, options = {}) {
  const response = await fetch(path, { credentials: 'same-origin', ...options });
  if (response.status === 401) { showLogin(); throw new Error('Authentication required'); }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  return response.status === 204 ? null : response.json();
}

function showLogin() { loginView.hidden = false; appView.hidden = true; }
function showApp(operator) {
  loginView.hidden = true; appView.hidden = false;
  document.querySelector('#operator-name').textContent = operator;
}

loginForm.addEventListener('submit', async event => {
  event.preventDefault(); loginError.textContent = '';
  const button = loginForm.querySelector('button'); button.disabled = true;
  try {
    const session = await api('/api/v1/session', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: loginForm.username.value, password: loginForm.password.value })
    });
    loginForm.password.value = ''; showApp(session.operator); await loadAll();
  } catch (error) { loginError.textContent = error.message; }
  finally { button.disabled = false; }
});

document.querySelector('#logout').addEventListener('click', async () => {
  try { await api('/api/v1/session', { method: 'DELETE' }); } finally { showLogin(); }
});
document.querySelector('#refresh').addEventListener('click', loadDashboard);

function renderModules(modules) {
  const nav = document.querySelector('#module-nav'); const grid = document.querySelector('#module-grid');
  nav.replaceChildren(); grid.replaceChildren();
  modules.forEach((module, index) => {
    const item = document.createElement('button'); item.className = `nav-item ${index === 0 ? 'active' : ''}`;
    item.disabled = module.status !== 'available'; item.dataset.module = module.id; item.append(document.createTextNode(module.name));
    if (module.status !== 'available') { const badge = document.createElement('small'); badge.textContent = module.status; item.append(badge); }
    if (module.status === 'available') item.addEventListener('click', () => openModule(module.id));
    nav.append(item);
    const card = document.createElement('article'); card.className = 'module-card';
    const name = document.createElement('strong'); name.textContent = module.name;
    const copy = document.createElement('p'); copy.textContent = module.description;
    const status = document.createElement('span'); status.className = `module-status ${module.status}`; status.textContent = module.status;
    card.append(name, copy, status); grid.append(card);
  });
}

function openModule(module) {
  document.querySelectorAll('.nav-item').forEach(item => item.classList.toggle('active', item.dataset.module === module));
  const overview = module === 'overview';
  document.querySelector('#overview-view').hidden = !overview;
  const directory = module === 'players' || module === 'accounts';
  document.querySelector('#directory-view').hidden = !directory;
  document.querySelector('#game-control-view').hidden = module !== 'game-control';
  document.querySelector('#administration-view').hidden = module !== 'administration';
  if (overview) { document.querySelector('#page-title').textContent = 'Server overview'; return; }
  if (directory) { openDirectory(module); return; }
  if (module === 'game-control') {
    document.querySelector('#page-title').textContent = 'Game control';
    document.querySelector('#broadcast-message').focus();
  }
  if (module === 'administration') {
    document.querySelector('#page-title').textContent = 'Administration';
    loadAuditEvents();
  }
}

function openDirectory(mode) {
  directoryMode = mode;
  const accounts = mode === 'accounts';
  document.querySelector('#page-title').textContent = accounts ? 'Account directory' : 'Player directory';
  document.querySelector('#directory-title').textContent = accounts ? 'Account inspector' : 'Player inspector';
  document.querySelector('#directory-copy').textContent = accounts ? 'Search persisted accounts, status, balances, and GM roles.' : 'Search persisted character records by character or account name.';
  document.querySelector('#directory-result-title').textContent = accounts ? 'Accounts' : 'Characters';
  document.querySelector('#directory-query').placeholder = accounts ? 'Enter an account name' : 'Enter a character or account name';
	  document.querySelector('#live-player-panel').hidden = accounts;
	  if (!accounts) loadLivePlayers();
  document.querySelector('#directory-query').focus();
}

async function loadLivePlayers() {
  const results = document.querySelector('#live-player-results');
  const count = document.querySelector('#live-player-count');
  results.replaceChildren(); count.textContent = 'Checking channel…';
  try {
    const snapshot = await api('/api/v1/online-players');
    if (!snapshot.available) {
      count.textContent = 'Channel disabled';
      const message = document.createElement('p'); message.className = 'empty-state'; message.textContent = 'The private Agent management channel is not configured. Persisted search remains available below.'; results.append(message); return;
    }
    count.textContent = `${snapshot.players.length} online`;
    if (!snapshot.players.length) { const empty = document.createElement('p'); empty.className = 'empty-state'; empty.textContent = 'No players are currently in the Agent world.'; results.append(empty); return; }
    snapshot.players.forEach(player => {
      const card = document.createElement('article'); card.className = 'live-player-card';
      const name = document.createElement('strong'); name.textContent = player.name;
      const meta = document.createElement('small'); meta.textContent = `Level ${player.level} · Region ${player.regionId} · UID ${player.uniqueId}`;
      const vitals = document.createElement('span'); vitals.textContent = `${number(player.hp)} HP · ${number(player.mp)} MP`;
      const disconnect = document.createElement('button'); disconnect.type = 'button'; disconnect.textContent = 'Disconnect'; disconnect.addEventListener('click', () => openDisconnect(player));
      card.append(name, meta, vitals, disconnect); results.append(card);
    });
  } catch (error) {
    count.textContent = 'Channel unavailable';
    const message = document.createElement('p'); message.className = 'empty-state'; message.textContent = error.message; results.append(message);
  }
}

function openDisconnect(player) {
  disconnectTarget = player;
  document.querySelector('#disconnect-target').textContent = `${player.name} · Character #${player.characterId} · Runtime UID ${player.uniqueId}`;
  document.querySelector('#disconnect-reason').value = '';
  document.querySelector('#disconnect-error').textContent = '';
  document.querySelector('#disconnect-modal').hidden = false;
  document.querySelector('#disconnect-reason').focus();
}

function closeDisconnect() {
  disconnectTarget = null;
  document.querySelector('#disconnect-modal').hidden = true;
}

document.querySelector('#disconnect-cancel').addEventListener('click', closeDisconnect);
document.querySelector('#disconnect-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!disconnectTarget) return;
  const reason = document.querySelector('#disconnect-reason').value.trim();
  const error = document.querySelector('#disconnect-error'); error.textContent = '';
  const button = event.currentTarget.querySelector('.danger-button'); button.disabled = true;
  try {
    await api('/api/v1/player-disconnects', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({characterId:disconnectTarget.characterId, expectedUniqueId:disconnectTarget.uniqueId, reason})});
    closeDisconnect(); await loadLivePlayers();
  } catch (failure) { error.textContent = failure.message; }
  finally { button.disabled = false; }
});

const broadcastMessage = document.querySelector('#broadcast-message');
broadcastMessage.addEventListener('input', () => {
  document.querySelector('#broadcast-count').textContent = `${[...broadcastMessage.value].length} / 200`;
  document.querySelector('#broadcast-status').textContent = '';
});

document.querySelector('#broadcast-form').addEventListener('submit', event => {
  event.preventDefault();
  const message = broadcastMessage.value.trim();
  if (!message) return;
  document.querySelector('#broadcast-preview').textContent = message;
  document.querySelector('#broadcast-error').textContent = '';
  document.querySelector('#broadcast-modal').hidden = false;
  document.querySelector('#broadcast-confirm-form .broadcast-button').focus();
});

function closeBroadcast() { document.querySelector('#broadcast-modal').hidden = true; }
document.querySelector('#broadcast-cancel').addEventListener('click', closeBroadcast);
document.querySelector('#broadcast-confirm-form').addEventListener('submit', async event => {
  event.preventDefault();
  const message = broadcastMessage.value.trim();
  const error = document.querySelector('#broadcast-error'); error.textContent = '';
  const button = event.currentTarget.querySelector('.broadcast-button'); button.disabled = true;
  try {
    const result = await api('/api/v1/broadcasts', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({message})});
    closeBroadcast(); broadcastMessage.value = ''; document.querySelector('#broadcast-count').textContent = '0 / 200';
    document.querySelector('#broadcast-status').textContent = `Broadcast accepted by ${result.recipientCount} online session${result.recipientCount === 1 ? '' : 's'}.`;
  } catch (failure) { error.textContent = failure.message; }
  finally { button.disabled = false; }
});

document.querySelector('#audit-filter-form').addEventListener('submit', event => {
  event.preventDefault(); loadAuditEvents();
});

async function loadAuditEvents() {
  const action = document.querySelector('#audit-action').value;
  const limit = document.querySelector('#audit-limit').value;
  const results = document.querySelector('#audit-results');
  const error = document.querySelector('#audit-error');
  error.textContent = ''; results.replaceChildren();
  const loading = document.createElement('p'); loading.className = 'empty-state'; loading.textContent = 'Loading durable audit records.'; results.append(loading);
  try {
    const query = new URLSearchParams({limit}); if (action) query.set('action', action);
    const payload = await api(`/api/v1/audit-events?${query}`);
    renderAuditEvents(payload.items);
  } catch (failure) {
    results.replaceChildren(); error.textContent = failure.message;
    document.querySelector('#audit-count').textContent = 'Unavailable';
  }
}

function renderAuditEvents(events) {
  const results = document.querySelector('#audit-results'); results.replaceChildren();
  document.querySelector('#audit-count').textContent = `${events.length} record${events.length === 1 ? '' : 's'}`;
  if (!events.length) { const empty = document.createElement('p'); empty.className = 'empty-state'; empty.textContent = 'No audit records match this filter.'; results.append(empty); return; }
  events.forEach(event => {
    const row = document.createElement('article'); row.className = 'audit-row';
    const identity = document.createElement('div'); identity.className = 'audit-identity';
    const action = document.createElement('strong'); action.textContent = event.action;
    const meta = document.createElement('small'); meta.textContent = `${new Date(event.timestamp).toLocaleString()} · ${event.operator || 'system'} · ${event.target || 'control plane'}`;
    identity.append(action, meta);
    const outcome = document.createElement('span'); outcome.className = `audit-outcome ${event.outcome}`; outcome.textContent = event.outcome;
    const details = document.createElement('code'); details.textContent = event.details ? JSON.stringify(event.details) : '—';
    row.append(identity, outcome, details); results.append(row);
  });
}

document.querySelector('#directory-form').addEventListener('submit', async event => {
  event.preventDefault(); directoryError.textContent = '';
  const query = document.querySelector('#directory-query').value.trim();
  if (!query) return;
  const button = event.currentTarget.querySelector('button'); button.disabled = true;
  try {
    const endpoint = directoryMode === 'accounts' ? '/api/v1/accounts' : '/api/v1/characters';
    const result = await api(`${endpoint}?q=${encodeURIComponent(query)}&limit=25`);
    renderDirectory(result.items);
  } catch (error) { directoryError.textContent = error.message; }
  finally { button.disabled = false; }
});

function field(label, value) {
  const wrapper = document.createElement('div'); wrapper.className = 'directory-field';
  const caption = document.createElement('small'); caption.textContent = label.toUpperCase();
  const content = document.createElement('strong'); content.textContent = value; wrapper.append(caption, content); return wrapper;
}

function renderDirectory(items) {
  const results = document.querySelector('#directory-results'); results.replaceChildren();
  document.querySelector('#directory-count').textContent = `${items.length} result${items.length === 1 ? '' : 's'}`;
  if (!items.length) { const empty = document.createElement('p'); empty.className = 'empty-state'; empty.textContent = 'No matching records found.'; results.append(empty); return; }
  items.forEach(item => {
    const row = document.createElement('article'); row.className = 'directory-row';
    const primary = document.createElement('div'); primary.className = 'directory-primary';
    const name = document.createElement('strong'); name.textContent = directoryMode === 'accounts' ? item.username : item.name;
    const meta = document.createElement('small'); meta.textContent = directoryMode === 'accounts' ? `Account #${item.id} · ${item.email || 'No email'}` : `${item.username} · Character #${item.id}`; primary.append(name, meta);
    if (directoryMode === 'accounts') {
      row.append(primary, field('Silk', number(item.silk)), field('Gift Silk', number(item.giftSilk)), field('Silk Points', number(item.silkPoint)));
      const status = document.createElement('span'); status.className = `status-chip ${item.status === 1 ? '' : 'inactive'}`; status.textContent = item.gameMaster ? 'Game master' : item.status === 1 ? 'Active' : 'Inactive'; row.append(status);
    } else {
      row.append(primary, field('Level', number(item.level)), field('Gold', number(item.gold)), field('Region', item.regionId), field('Shard', item.shardId));
    }
    results.append(row);
  });
}

function number(value) { return new Intl.NumberFormat().format(value ?? 0); }
function renderDashboard(data) {
  const online = data.shards.reduce((total, shard) => total + shard.onlinePlayers, 0);
  const allReachable = data.services.every(service => service.state === 'reachable');
  document.querySelector('#platform-state').textContent = allReachable ? 'Platform reachable' : 'Platform attention required';
  document.querySelector('#generated-at').textContent = `Snapshot ${new Date(data.generatedAt).toLocaleString()}`;
  document.querySelector('#shard-count').textContent = number(data.shards.length);
  document.querySelector('#online-count').textContent = number(online);
  const labels = { accounts: 'Accounts', activeAccounts: 'Active', gameMasters: 'Game masters', characters: 'Characters', guilds: 'Guilds', items: 'Items', notices: 'Notices' };
  const summary = document.querySelector('#summary-grid'); summary.replaceChildren();
  Object.entries(labels).forEach(([key, label]) => {
    const card = document.createElement('article'); card.className = 'summary-card';
    const title = document.createElement('small'); title.textContent = label.toUpperCase();
    const value = document.createElement('strong'); value.textContent = number(data.totals[key]); card.append(title, value); summary.append(card);
  });
  const services = document.querySelector('#service-list'); services.replaceChildren();
  data.services.forEach(service => {
    const row = document.createElement('div'); row.className = 'service-row';
    const icon = document.createElement('span'); icon.className = 'service-icon'; icon.textContent = service.name.slice(0, 1);
    const copy = document.createElement('div'); copy.className = 'service-copy';
    const name = document.createElement('strong'); name.textContent = service.name;
    const address = document.createElement('small'); address.textContent = `${service.address} · ${service.latencyMs} ms`; copy.append(name, address);
    const state = document.createElement('span'); state.className = `state ${service.state}`; state.textContent = service.state;
    row.append(icon, copy, state); services.append(row);
  });
  const shards = document.querySelector('#shard-list'); shards.replaceChildren();
  data.shards.forEach(shard => {
    const percent = shard.capacity > 0 ? Math.min(100, Math.round(shard.onlinePlayers / shard.capacity * 100)) : 0;
    const row = document.createElement('div'); row.className = 'shard-row';
    const copy = document.createElement('div'); copy.className = 'shard-copy';
    const name = document.createElement('strong'); name.textContent = shard.name;
    const meta = document.createElement('small'); meta.textContent = `Shard ${shard.id} · Content ${shard.contentId} · ${shard.status === 1 ? 'Operating' : 'Offline'}`; copy.append(name, meta);
    const capacity = document.createElement('div'); capacity.className = 'capacity';
    const label = document.createElement('div'); label.className = 'capacity-label'; label.innerHTML = `<span>${number(shard.onlinePlayers)} online</span><span>${percent}%</span>`;
    const track = document.createElement('div'); track.className = 'capacity-track'; const fill = document.createElement('div'); fill.className = 'capacity-fill'; fill.style.width = `${percent}%`; track.append(fill); capacity.append(label, track);
    row.append(copy, capacity); shards.append(row);
  });
}

async function loadDashboard() {
  dashboardError.textContent = '';
  try { renderDashboard(await api('/api/v1/dashboard')); }
  catch (error) { if (appView.hidden === false) dashboardError.textContent = error.message; }
}
async function loadAll() {
  const [{ modules }] = await Promise.all([api('/api/v1/modules'), loadDashboard()]); renderModules(modules);
}
async function bootstrap() {
  try { const session = await api('/api/v1/session'); showApp(session.operator); await loadAll(); }
  catch (_) { showLogin(); }
}
setInterval(() => { document.querySelector('#clock').textContent = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }, 1000);
bootstrap();
