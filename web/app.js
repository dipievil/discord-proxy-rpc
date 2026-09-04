(function () {
    'use strict';

    var WS_RECONNECT_BASE = 1000;
    var WS_RECONNECT_MAX = 30000;
    var TIMESTAMP_INTERVAL = 1000;

    var ws = null;
    var reconnectDelay = WS_RECONNECT_BASE;
    var reconnectTimer = null;
    var timestampTimer = null;
    var currentActivity = null;
    var clientId = null;

    function connect() {
        var protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        var url = protocol + '//' + location.host + '/ws';

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
                var msg = JSON.parse(event.data);
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
        var img = document.getElementById(elementId);
        if (!img || !imageId) {
            if (img) img.hidden = true;
            return;
        }

        if (imageId.indexOf('http') === 0) {
            img.src = imageId;
        } else if (clientId) {
            img.src = 'https://cdn.discordapp.com/app-assets/' + clientId + '/' + imageId + '.png';
        } else {
            img.hidden = true;
            return;
        }

        img.hidden = false;
        img.onerror = function () { img.hidden = true; };
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
        var now = Math.floor(Date.now() / 1000);

        if (timestamps.start) {
            var elapsed = now - timestamps.start;
            setText('elapsed', formatDuration(elapsed));
        }

        if (timestamps.end) {
            var remaining = timestamps.end - now;
            if (remaining <= 0) {
                setText('remaining', 'Ended');
            } else {
                setText('remaining', formatDuration(remaining));
            }
        }
    }

    function formatDuration(seconds) {
        if (seconds < 0) seconds = 0;
        var h = Math.floor(seconds / 3600);
        var m = Math.floor((seconds % 3600) / 60);
        var s = seconds % 60;
        if (h > 0) return h + 'h ' + m + 'm ' + s + 's';
        if (m > 0) return m + 'm ' + s + 's';
        return s + 's';
    }

    function renderButtons(buttons) {
        var list = document.getElementById('buttons-list');
        if (!list) return;

        if (!buttons || buttons.length === 0) {
            list.innerHTML = '<li class="empty">No buttons</li>';
            return;
        }
        list.innerHTML = buttons.map(function (b) {
            return '<li><a href="' + escapeHtml(b.url) + '" target="_blank" rel="noopener">' + escapeHtml(b.label) + '</a></li>';
        }).join('');
    }

    function escapeHtml(str) {
        var div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    function setText(fieldId, text) {
        var el = document.querySelector('[data-field="' + fieldId + '"]');
        if (el) el.textContent = text;
    }

    function updateConnectionStatus(status) {
        var el = document.getElementById('status');
        var text = document.getElementById('status-text');
        if (!el || !text) return;

        el.className = 'status ' + status;
        text.textContent = status.charAt(0).toUpperCase() + status.slice(1);
    }

    function activityTypeLabel(type) {
        var labels = { 0: 'Playing', 1: 'Streaming', 2: 'Listening', 3: 'Watching', 4: 'Custom', 5: 'Competing' };
        return labels[type] || 'Unknown';
    }

    function setupCopyJson() {
        var btn = document.getElementById('copy-json');
        if (!btn) return;

        btn.addEventListener('click', function () {
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

    function fallbackCopy(text) {
        var ta = document.createElement('textarea');
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
        var toast = document.getElementById('toast');
        if (!toast) return;
        toast.textContent = message;
        toast.hidden = false;
        setTimeout(function () { toast.hidden = true; }, 2000);
    }

    function init() {
        setupCopyJson();
        connect();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
