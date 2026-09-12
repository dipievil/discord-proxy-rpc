(function () {
    'use strict';

    const WS_RECONNECT_BASE = 1000;
    const WS_RECONNECT_MAX = 30000;
    const TIMESTAMP_INTERVAL = 1000;

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
    let timestampTimer = null;
    let currentActivity = null;
    let clientId = null;
    let toastTimer = null;

    function connect() {
        const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = protocol + '//' + location.host + '/ws';

        try {
            ws = new WebSocket(url);
        } catch (e) {
            console.error('[Dashboard] WebSocket constructor failed:', e);
            scheduleReconnect();
            return;
        }

        ws.onopen = function () {
            console.log('[Dashboard] Connected to proxy');
            reconnectDelay = WS_RECONNECT_BASE;
            updateConnectionStatus('connected');

            ws.send(JSON.stringify({
                type: 'subscribe',
                events: ['presence', 'state']
            }));
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

    function handleMessage(msg) {
        switch (msg.type) {
            case 'presence':
            case 'current':
                currentActivity = msg.payload;
                renderPresence(msg.payload);
                break;
            case 'state':
                updateConnectionStatus(msg.status);
                if (msg.client_id) {
                    clientId = msg.client_id;
                }
                break;
            default:
                console.warn('[Dashboard] Unknown message type:', msg.type);
        }
    }

    function renderPresence(activity) {
        if (!activity) return;

        setText('type', activityTypeLabel(activity.type));
        setText('details', activity.details || '\u2014');
        setText('state', activity.state || '\u2014');

        if (activity.timestamps) {
            startTimestampTimer(activity.timestamps);
        } else {
            stopTimestampTimer();
            setText('elapsed', '\u2014');
            setText('remaining', '\u2014');
        }

        if (activity.assets) {
            renderImage('large-image', activity.assets.large_image);
            renderImage('small-image', activity.assets.small_image);
            setText('large_text', activity.assets.large_text || '\u2014');
            setText('small_text', activity.assets.small_text || '\u2014');
        } else {
            renderImage('large-image', null);
            renderImage('small-image', null);
            setText('large_text', '\u2014');
            setText('small_text', '\u2014');
        }

        if (activity.party) {
            setText('party_id', activity.party.id || '\u2014');
            if (activity.party.size && activity.party.size.length >= 2) {
                setText('party_size', activity.party.size[0] + '/' + activity.party.size[1]);
            } else {
                setText('party_size', '\u2014');
            }
        } else {
            setText('party_id', '\u2014');
            setText('party_size', '\u2014');
        }

        renderButtons(activity.buttons);
    }

    function renderImage(elementId, imageId) {
        const img = document.getElementById(elementId);
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

    function startTimestampTimer(timestamps) {
        stopTimestampTimer();
        updateTimestamps(timestamps);
        timestampTimer = setInterval(function () {
            updateTimestamps(timestamps);
        }, TIMESTAMP_INTERVAL);
    }

    function stopTimestampTimer() {
        if (timestampTimer) {
            clearInterval(timestampTimer);
            timestampTimer = null;
        }
    }

    function updateTimestamps(timestamps) {
        const now = Math.floor(Date.now() / 1000);

        setText('elapsed', '\u2014');
        setText('remaining', '\u2014');

        if (timestamps.start) {
            const elapsed = now - timestamps.start;
            setText('elapsed', elapsed < 0 ? '\u2014' : formatDuration(elapsed));
        }

        if (timestamps.end) {
            const remaining = timestamps.end - now;
            if (remaining <= 0) {
                setText('remaining', 'Ended');
            } else {
                setText('remaining', formatDuration(remaining));
            }
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

    function renderButtons(buttons) {
        const list = document.getElementById('buttons-list');
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

    function setText(fieldId, text) {
        const el = document.querySelector('[data-field="' + fieldId + '"]');
        if (el) el.textContent = text;
    }

    function updateConnectionStatus(status) {
        const el = document.getElementById('status');
        const text = document.getElementById('status-text');
        if (!el || !text) return;

        el.className = 'status ' + status;
        text.textContent = status.charAt(0).toUpperCase() + status.slice(1);
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
            formatDuration: formatDuration
        };
    }
})();
