(function () {
    'use strict';

    const WS_RECONNECT_BASE = 1000;
    const WS_RECONNECT_MAX = 30000;
    const TIMESTAMP_INTERVAL = 1000;

    const DEFAULT_WS_PATH = '/ws';
    const SUBPROTOCOL_TOKEN_PREFIX = 'proxy-token-';
    const TOKEN_STORAGE_KEY = 'proxy-rpc-token';
    const B64URL_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

    const ACTIVITY_TYPE_LABELS = {
        0: 'Playing',
        1: 'Streaming',
        2: 'Listening',
        3: 'Watching',
        4: 'Custom',
        5: 'Competing'
    };

    const DISCORD_CDN_HOST = 'cdn.discordapp.com';

    let ws = null;
    let reconnectDelay = WS_RECONNECT_BASE;
    let reconnectTimer = null;
    let currentActivity = null;
    let clientId = null;
    let toastTimer = null;
    let authToken = null;
    let singlePanel = null;
    let basePanelRoot = null;
    let panels = {};
    let primaryAccount = null;
    let multiAccount = false;
    let accountEntries = [];

    function getWsPath() {
        try {
            const meta = document.querySelector('meta[name="proxy-ws-path"]');
            if (meta && meta.content) return meta.content;
        } catch (e) {
            console.warn('[Dashboard] Could not read configured WebSocket path:', e);
        }
        return DEFAULT_WS_PATH;
    }

    // The browser WebSocket API cannot set an Authorization header, so the
    // bearer token is sent as a subprotocol value, base64url-encoded to stay
    // within the header's token grammar.
    function buildSubprotocols(token) {
        if (!token) return [];
        return [SUBPROTOCOL_TOKEN_PREFIX + base64UrlEncode(token)];
    }

    function base64UrlEncode(value) {
        const bytes = utf8Bytes(value);
        let encoded = '';

        for (let i = 0; i < bytes.length; i += 3) {
            const b0 = bytes[i];
            const b1 = i + 1 < bytes.length ? bytes[i + 1] : null;
            const b2 = i + 2 < bytes.length ? bytes[i + 2] : null;

            encoded += B64URL_ALPHABET[b0 >> 2];
            encoded += B64URL_ALPHABET[((b0 & 0x03) << 4) | ((b1 === null ? 0 : b1) >> 4)];
            if (b1 === null) break;
            encoded += B64URL_ALPHABET[((b1 & 0x0f) << 2) | ((b2 === null ? 0 : b2) >> 6)];
            if (b2 === null) break;
            encoded += B64URL_ALPHABET[b2 & 0x3f];
        }

        return encoded;
    }

    function utf8Bytes(value) {
        const bytes = [];
        for (let i = 0; i < value.length; i++) {
            const c = value.charCodeAt(i);
            if (c < 0x80) {
                bytes.push(c);
            } else if (c < 0x800) {
                bytes.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
            } else {
                bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
            }
        }
        return bytes;
    }

    function resolveToken() {
        const stored = readStoredToken();
        if (stored) return stored;

        const fromUrl = readUrlToken();
        if (!fromUrl) return null;

        storeToken(fromUrl);
        stripTokenFromUrl();
        return fromUrl;
    }

    function readStoredToken() {
        try {
            if (typeof window !== 'undefined' && window.sessionStorage) {
                return window.sessionStorage.getItem(TOKEN_STORAGE_KEY);
            }
        } catch (e) {
            console.warn('[Dashboard] Could not read stored token:', e);
        }
        return null;
    }

    function storeToken(token) {
        try {
            if (typeof window !== 'undefined' && window.sessionStorage) {
                window.sessionStorage.setItem(TOKEN_STORAGE_KEY, token);
            }
        } catch (e) {
            console.warn('[Dashboard] Could not persist token:', e);
        }
    }

    function readUrlToken() {
        try {
            if (typeof URLSearchParams === 'undefined') return null;
            if (typeof location === 'undefined' || !location.search) return null;
            return new URLSearchParams(location.search).get('token');
        } catch (e) {
            console.warn('[Dashboard] Could not read token from URL:', e);
            return null;
        }
    }

    function stripTokenFromUrl() {
        try {
            if (typeof history === 'undefined' || !history.replaceState) return;
            const url = new URL(location.href);
            url.searchParams.delete('token');
            history.replaceState(null, '', url.pathname + url.search + url.hash);
        } catch (e) {
            console.warn('[Dashboard] Could not clean token from URL:', e);
        }
    }

    function connect() {
        const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = protocol + '//' + location.host + getWsPath();
        const subprotocols = buildSubprotocols(authToken);

        try {
            ws = subprotocols.length ? new WebSocket(url, subprotocols) : new WebSocket(url);
        } catch (e) {
            console.error('[Dashboard] WebSocket constructor failed:', e);
            scheduleReconnect();
            return;
        }

        ws.onopen = function () {
            console.log('[Dashboard] Connected to proxy');
            reconnectDelay = WS_RECONNECT_BASE;
            startSession();
        };

        ws.onmessage = function (event) {
            try {
                const msg = JSON.parse(event.data);
                handleMessage(msg);
            } catch (e) {
                console.error('[Dashboard] Failed to parse message:', e);
            }
        };

        ws.onclose = function () {
            console.log('[Dashboard] Disconnected');
            updateConnectionStatus('disconnected');
            scheduleReconnect();
        };

        ws.onerror = function (err) {
            console.error('[Dashboard] WebSocket error:', err);
        };
    }

    function scheduleReconnect() {
        if (reconnectTimer) clearTimeout(reconnectTimer);
        reconnectTimer = setTimeout(function () {
            reconnectDelay = Math.min(reconnectDelay * 2, WS_RECONNECT_MAX);
            connect();
        }, reconnectDelay);
    }

    // startSession learns which accounts the proxy tracks before subscribing,
    // so a multi-account proxy gets one panel and one get_current per account.
    // A single-account proxy, or an older server without /api/accounts, sees
    // exactly the frames it always did: no account field anywhere.
    function startSession() {
        loadAccounts(function (view) {
            applyAccounts(view);

            // The status indicator follows the proxy's IPC link, not the
            // WebSocket: an open socket does not mean Discord is reachable.
            // The state snapshot that answers this subscription sets it.
            ws.send(JSON.stringify({
                type: 'subscribe',
                events: ['presence', 'state']
            }));

            if (multiAccount) {
                view.accounts.forEach(function (entry) {
                    ws.send(JSON.stringify({ type: 'get_current', account: entry.account }));
                });
                return;
            }
            ws.send(JSON.stringify({ type: 'get_current' }));
        });
    }

    function loadAccounts(done) {
        if (typeof fetch !== 'function') {
            done(accountsView(null));
            return;
        }

        fetch('/api/accounts', {
            credentials: 'same-origin',
            headers: { Accept: 'application/json' }
        }).then(function (resp) {
            if (!resp.ok) throw new Error('GET /api/accounts: ' + resp.status);
            return resp.json();
        }).then(function (payload) {
            done(accountsView(payload));
        }).catch(function (err) {
            console.warn('[Dashboard] Could not load accounts, assuming a single account:', err);
            done(accountsView(null));
        });
    }

    // accountsView normalizes GET /api/accounts into the panel plan: the
    // ordered account list and whether more than one account is tracked. An
    // empty or malformed response means a single-account proxy.
    function accountsView(payload) {
        const raw = payload && Array.isArray(payload.accounts) ? payload.accounts : [];
        const accounts = [];

        raw.forEach(function (entry) {
            if (!entry || typeof entry.account !== 'string' || entry.account === '') return;
            if (accounts.some(function (known) { return known.account === entry.account; })) return;
            accounts.push({
                account: entry.account,
                state: typeof entry.state === 'string' ? entry.state : ''
            });
        });

        return {
            multi: accounts.length > 1,
            accounts: accounts
        };
    }

    function handleMessage(msg) {
        switch (msg.type) {
            case 'presence':
            case 'current': {
                if (msg.client_id) {
                    clientId = msg.client_id;
                }
                noteAccount(msg.account);
                const panel = panelFor(msg.account);
                if (!panel) break;
                panel.activity = msg.payload;
                currentActivity = msg.payload;
                const view = presenceView(msg.payload);
                updateSourceNotice(panel, view.source, view.hasActivity);
                renderPresence(panel, view.activity);
                break;
            }
            case 'state':
                if (msg.client_id) {
                    clientId = msg.client_id;
                }
                if (msg.account) {
                    noteAccount(msg.account);
                    updateAccountStatus(msg.account, msg.status);
                    // The header follows the primary account; every panel keeps
                    // its own indicator for the rest.
                    if (multiAccount && msg.account !== primaryAccount) break;
                }
                updateConnectionStatus(msg.status);
                break;
            default:
                console.warn('[Dashboard] Unknown message type:', msg.type);
        }
    }

    // noteAccount adds a panel for an account a frame names but /api/accounts
    // did not report, so a failed account listing can never make two accounts
    // share one panel.
    function noteAccount(account) {
        if (typeof account !== 'string' || account === '') return;
        if (accountEntries.some(function (entry) { return entry.account === account; })) return;

        accountEntries = accountEntries.concat([{ account: account, state: '' }]);
        applyAccounts({ multi: accountEntries.length > 1, accounts: accountEntries });
    }

    function makePanel(root) {
        return { root: root, timer: null, activity: null, state: null };
    }

    function basePanel() {
        if (!basePanelRoot) {
            basePanelRoot = document.getElementById('presence-card');
        }
        if (basePanelRoot && !singlePanel) {
            singlePanel = makePanel(basePanelRoot);
        }
        return singlePanel;
    }

    // panelFor routes a frame to the panel of the account it names. A frame
    // without an account belongs to the single/primary panel.
    function panelFor(account) {
        if (account && panels[account]) return panels[account];
        return singlePanel || basePanel();
    }

    // applyAccounts renders one panel per account when the proxy tracks more
    // than one, and keeps the single panel otherwise. It is idempotent, so a
    // reconnect reuses existing panels and only adds or removes what changed.
    function applyAccounts(view) {
        const entries = view && Array.isArray(view.accounts) ? view.accounts : [];
        const base = basePanel();
        const container = document.getElementById('accounts');

        accountEntries = entries.slice();
        multiAccount = Boolean(view && view.multi) && entries.length > 1;
        primaryAccount = entries.length ? entries[0].account : null;

        if (!multiAccount) {
            Object.keys(panels).forEach(function (name) {
                if (panels[name] && panels[name] !== singlePanel) removePanel(panels[name]);
            });
            panels = {};
            if (base) setAccountMeta(base, null, null);
            if (container) container.classList.remove('multi');
            return;
        }

        if (container) container.classList.add('multi');

        const seen = {};
        entries.forEach(function (entry, index) {
            let panel = panels[entry.account];
            if (!panel) {
                const root = index === 0 && base ? base.root : clonePanelRoot();
                panel = makePanel(root);
                if (root) root.setAttribute('data-account', entry.account);
            }
            panels[entry.account] = panel;
            seen[entry.account] = true;
            if (container && panel.root) container.appendChild(panel.root);
            setAccountMeta(panel, entry.account, entry.state);
        });

        Object.keys(panels).forEach(function (name) {
            if (seen[name]) return;
            removePanel(panels[name]);
            delete panels[name];
        });
    }

    function clonePanelRoot() {
        if (!basePanelRoot || typeof basePanelRoot.cloneNode !== 'function') return null;

        const clone = basePanelRoot.cloneNode(true);
        clone.removeAttribute('id');
        clone.removeAttribute('data-account');

        const identified = typeof clone.querySelectorAll === 'function' ? clone.querySelectorAll('[id]') : [];
        Array.prototype.forEach.call(identified, function (el) {
            el.removeAttribute('id');
        });
        resetPanel(clone);
        return clone;
    }

    function resetPanel(root) {
        if (!root || typeof root.querySelectorAll !== 'function') return;

        Array.prototype.forEach.call(root.querySelectorAll('[data-field]'), function (el) {
            const name = el.getAttribute('data-field');
            if (name === 'account' || name === 'account_state') return;
            el.textContent = '\u2014';
        });
        Array.prototype.forEach.call(root.querySelectorAll('[data-asset]'), function (img) {
            img.hidden = true;
            img.removeAttribute('src');
            const placeholder = img.nextElementSibling;
            if (placeholder && placeholder.classList && placeholder.classList.contains('asset-placeholder')) {
                placeholder.hidden = false;
            }
        });
        renderButtons(makePanel(root), null);
    }

    function removePanel(panel) {
        if (!panel) return;
        stopTimestampTimer(panel);
        if (panel.root && panel.root !== basePanelRoot && panel.root.parentNode) {
            panel.root.parentNode.removeChild(panel.root);
        }
    }

    function setAccountMeta(panel, account, state) {
        if (!panel || !panel.root || typeof panel.root.querySelector !== 'function') return;

        const meta = panel.root.querySelector('[data-account-meta]');
        if (!meta) return;
        if (!account) {
            meta.hidden = true;
            return;
        }
        meta.hidden = false;
        setField(panel, 'account', account);
        if (state) updateAccountState(panel, state);
    }

    function updateAccountState(panel, state) {
        if (!panel || !panel.root || typeof panel.root.querySelector !== 'function') return;

        panel.state = state;
        const el = panel.root.querySelector('[data-field="account_state"]');
        if (!el) return;
        el.textContent = capitalize(state);
        el.className = 'account-state ' + state;
    }

    function updateAccountStatus(account, status) {
        const panel = panels[account];
        if (!panel) return;
        updateAccountState(panel, status);
    }

    function capitalize(value) {
        return value.charAt(0).toUpperCase() + value.slice(1);
    }

    // presenceView normalizes a presence/current payload into the fields the
    // dashboard renders. A source of "none" means the proxy has no activity
    // source at all, so the (null) activity must not be rendered as an empty
    // activity -- the UI shows an explicit notice instead.
    function presenceView(payload) {
        const snapshot = payload || {};
        const source = snapshot.source || 'none';
        const hasActivity = source !== 'none' && snapshot.activity != null;
        return {
            source: source,
            hasActivity: hasActivity,
            activity: hasActivity ? snapshot.activity : null
        };
    }

    function updateSourceNotice(panel, source, hasActivity) {
        const notice = panel && panel.root ? panel.root.querySelector('[data-notice]') : null;
        if (!notice) return;

        if (hasActivity) {
            notice.hidden = true;
            return;
        }

        notice.hidden = false;
        notice.textContent = source === 'none'
            ? 'No activity source: Discord IPC exposes no command to read another app\u2019s Rich Presence, so this proxy cannot observe activity yet.'
            : 'No activity reported by the source right now.';
    }

    function renderPresence(panel, activity) {
        if (!panel || !activity) return;

        setField(panel, 'type', activityTypeLabel(activity.type));
        setField(panel, 'details', activity.details || '\u2014');
        setField(panel, 'state', activity.state || '\u2014');

        if (activity.timestamps) {
            startTimestampTimer(panel, activity.timestamps);
        } else {
            stopTimestampTimer(panel);
            setField(panel, 'elapsed', '\u2014');
            setField(panel, 'remaining', '\u2014');
        }

        if (activity.assets) {
            renderImage(panel, 'large', activity.assets.large_image);
            renderImage(panel, 'small', activity.assets.small_image);
            setField(panel, 'large_text', activity.assets.large_text || '\u2014');
            setField(panel, 'small_text', activity.assets.small_text || '\u2014');
        } else {
            renderImage(panel, 'large', null);
            renderImage(panel, 'small', null);
            setField(panel, 'large_text', '\u2014');
            setField(panel, 'small_text', '\u2014');
        }

        if (activity.party) {
            setField(panel, 'party_id', activity.party.id || '\u2014');
            if (activity.party.size && activity.party.size.length >= 2) {
                setField(panel, 'party_size', activity.party.size[0] + '/' + activity.party.size[1]);
            } else {
                setField(panel, 'party_size', '\u2014');
            }
        } else {
            setField(panel, 'party_id', '\u2014');
            setField(panel, 'party_size', '\u2014');
        }

        renderButtons(panel, activity.buttons);
    }

    function renderImage(panel, role, imageId) {
        if (!panel || !panel.root || typeof panel.root.querySelector !== 'function') return;

        const img = panel.root.querySelector('[data-asset="' + role + '"]');
        if (!img) return;

        const placeholder = img.nextElementSibling;
        const src = resolveAssetSrc(imageId, clientId);

        if (!src) {
            showAssetPlaceholder(img, placeholder);
            return;
        }

        img.src = src;
        hideAssetPlaceholder(img, placeholder);
        img.onerror = function () {
            showAssetPlaceholder(img, placeholder);
        };
    }

    function resolveAssetSrc(imageId, clientId) {
        if (!imageId) return null;

        if (isSafeImageUrl(imageId)) {
            return imageId;
        }

        // Reject any URL-shaped value that is not a trusted CDN URL so it
        // falls back to the placeholder instead of becoming an asset id.
        if (isUrlLike(imageId)) {
            return null;
        }

        if (clientId) {
            return 'https://' + DISCORD_CDN_HOST + '/app-assets/' + clientId + '/' + imageId + '.png';
        }

        return null;
    }

    function isUrlLike(value) {
        return /^[a-z][a-z0-9+.-]*:\/\//i.test(value);
    }

    function showAssetPlaceholder(img, placeholder) {
        img.hidden = true;
        if (placeholder && placeholder.classList.contains('asset-placeholder')) {
            placeholder.hidden = false;
        }
    }

    function hideAssetPlaceholder(img, placeholder) {
        img.hidden = false;
        if (placeholder && placeholder.classList.contains('asset-placeholder')) {
            placeholder.hidden = true;
        }
    }

    function isSafeImageUrl(url) {
        try {
            const u = new URL(url);
            return (u.protocol === 'https:' || u.protocol === 'http:') &&
                u.hostname === DISCORD_CDN_HOST;
        } catch (e) {
            return false;
        }
    }

    function startTimestampTimer(panel, timestamps) {
        stopTimestampTimer(panel);
        updateTimestamps(panel, timestamps);
        if (!panel) return;
        panel.timer = setInterval(function () {
            updateTimestamps(panel, timestamps);
        }, TIMESTAMP_INTERVAL);
    }

    function stopTimestampTimer(panel) {
        if (panel && panel.timer) {
            clearInterval(panel.timer);
            panel.timer = null;
        }
    }

    // Discord IPC timestamps are Unix milliseconds; compare them against the
    // same unit and convert to whole seconds only for display.
    function computeTimestamps(timestamps, nowMs) {
        const result = { elapsed: null, remaining: null, ended: false };

        if (timestamps.start) {
            const elapsed = Math.floor((nowMs - timestamps.start) / 1000);
            if (elapsed >= 0) result.elapsed = elapsed;
        }

        if (timestamps.end) {
            const remaining = Math.floor((timestamps.end - nowMs) / 1000);
            if (remaining <= 0) {
                result.ended = true;
            } else {
                result.remaining = remaining;
            }
        }

        return result;
    }

    function updateTimestamps(panel, timestamps) {
        const computed = computeTimestamps(timestamps, Date.now());

        setField(panel, 'elapsed', computed.elapsed === null ? '\u2014' : formatDuration(computed.elapsed));

        if (computed.ended) {
            setField(panel, 'remaining', 'Ended');
        } else {
            setField(panel, 'remaining', computed.remaining === null ? '\u2014' : formatDuration(computed.remaining));
        }
    }

    function formatDuration(seconds) {
        if (seconds < 0) seconds = 0;
        const h = Math.floor(seconds / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        const s = seconds % 60;
        if (h > 0) return h + 'h ' + m + 'm ' + s + 's';
        if (m > 0) return m + 'm ' + s + 's';
        return s + 's';
    }

    function renderButtons(panel, buttons) {
        const list = panel && panel.root ? panel.root.querySelector('[data-buttons]') : null;
        if (!list) return;

        list.textContent = '';

        if (!buttons || buttons.length === 0) {
            const empty = document.createElement('li');
            empty.className = 'empty';
            empty.textContent = 'No buttons';
            list.appendChild(empty);
            return;
        }

        buttons.forEach(function (b) {
            if (!isSafeUrl(b.url)) return;

            const li = document.createElement('li');
            const a = document.createElement('a');
            a.href = b.url;
            a.target = '_blank';
            a.rel = 'noopener';
            a.textContent = b.label;
            li.appendChild(a);
            list.appendChild(li);
        });
    }

    function isSafeUrl(url) {
        try {
            const u = new URL(url, window.location.href);
            return u.protocol === 'http:' || u.protocol === 'https:';
        } catch (e) {
            return false;
        }
    }

    function setField(panel, fieldId, text) {
        if (!panel || !panel.root || typeof panel.root.querySelector !== 'function') return;
        const el = panel.root.querySelector('[data-field="' + fieldId + '"]');
        if (el) el.textContent = text;
    }

    function updateConnectionStatus(status) {
        const el = document.getElementById('status');
        const text = document.getElementById('status-text');
        if (!el || !text) return;

        el.className = 'status ' + status;
        text.textContent = capitalize(status);
    }

    function activityTypeLabel(type) {
        return ACTIVITY_TYPE_LABELS[type] || 'Unknown';
    }

    function setupCopyJson() {
        const btn = document.getElementById('copy-json');
        if (!btn) return;

        btn.addEventListener('click', function () {
            if (!currentActivity) return;

            const json = JSON.stringify(currentActivity, null, 2);
            if (navigator.clipboard && navigator.clipboard.writeText) {
                navigator.clipboard.writeText(json).then(function () {
                    showToast('Copied!');
                }).catch(function () {
                    fallbackCopy(json);
                });
            } else {
                fallbackCopy(json);
            }
        });
    }

    function fallbackCopy(text) {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.left = '-9999px';
        document.body.appendChild(ta);
        ta.select();
        try {
            document.execCommand('copy');
            showToast('Copied!');
        } catch (e) {
            console.error('[Dashboard] Copy failed:', e);
        }
        document.body.removeChild(ta);
    }

    function showToast(message) {
        const toast = document.getElementById('toast');
        if (!toast) return;
        toast.textContent = message;
        toast.hidden = false;
        if (toastTimer) clearTimeout(toastTimer);
        toastTimer = setTimeout(function () { toast.hidden = true; }, 2000);
    }

    function isCopyShortcut(e) {
        return (e.ctrlKey || e.metaKey) && e.altKey && e.code === 'KeyC';
    }

    function setupKeyboardShortcut() {
        document.addEventListener('keydown', function (e) {
            if (!isCopyShortcut(e)) return;
            e.preventDefault();
            if (!currentActivity) return;

            var json = JSON.stringify(currentActivity, null, 2);
            if (navigator.clipboard && navigator.clipboard.writeText) {
                navigator.clipboard.writeText(json).then(function () {
                    showToast('Copied!');
                }).catch(function () {
                    fallbackCopy(json);
                });
            } else {
                fallbackCopy(json);
            }
        });
    }

    function init() {
        authToken = resolveToken();
        basePanel();
        setupCopyJson();
        setupKeyboardShortcut();
        connect();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

    // Expose pure helpers for automated testing (no-op in production).
    if (typeof window !== 'undefined') {
        window.__dashboard = {
            isCopyShortcut: isCopyShortcut,
            resolveAssetSrc: resolveAssetSrc,
            isSafeImageUrl: isSafeImageUrl,
            formatDuration: formatDuration,
            computeTimestamps: computeTimestamps,
            buildSubprotocols: buildSubprotocols,
            base64UrlEncode: base64UrlEncode,
            getWsPath: getWsPath,
            resolveToken: resolveToken,
            presenceView: presenceView,
            accountsView: accountsView,
            capitalize: capitalize
        };
    }
})();
