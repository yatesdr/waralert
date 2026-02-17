// WarAlert - Alert System JavaScript
var WarAlert = (function() {
    'use strict';

    // --- SSE Factory ---

    function createSSE(url, handlers) {
        var es = null;
        var reconnectTimeout = null;
        var connected = false;

        function connect() {
            if (es) es.close();
            es = new EventSource(url || '/events');

            es.addEventListener('connected', function(e) {
                connected = true;
                if (handlers.onConnected) handlers.onConnected(JSON.parse(e.data));
            });

            Object.keys(handlers).forEach(function(key) {
                if (key === 'onConnected' || key === 'onError') return;
                var eventType = key.replace(/^on/, '').replace(/([A-Z])/g, function(m) {
                    return '-' + m.toLowerCase();
                }).replace(/^-/, '');
                es.addEventListener(eventType, function(e) {
                    try { handlers[key](JSON.parse(e.data)); }
                    catch (err) { console.error('SSE ' + eventType + ' error:', err); }
                });
            });

            es.onerror = function() {
                connected = false;
                es.close();
                if (handlers.onError) handlers.onError();
                if (reconnectTimeout) clearTimeout(reconnectTimeout);
                reconnectTimeout = setTimeout(connect, 1000);
            };
        }

        function disconnect() {
            if (reconnectTimeout) clearTimeout(reconnectTimeout);
            if (es) { es.close(); es = null; }
            connected = false;
        }

        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', connect);
        } else {
            connect();
        }

        return {
            connect: connect,
            disconnect: disconnect,
            isConnected: function() { return connected; }
        };
    }

    // --- Modal Helpers ---

    function showModal(id) {
        var modal = document.getElementById(id);
        if (modal) modal.style.display = 'flex';
    }

    function hideModal(id) {
        var modal = document.getElementById(id);
        if (modal) modal.style.display = 'none';
        if (id === 'chain-edit-modal') {
            stopGatePoller();
            // Restart chain if it was running before editing (cancel case)
            if (_chainWasRunning && _chainEditName) {
                api.post('/htmx/chains/' + encodeURIComponent(_chainEditName) + '/start').then(function() {
                    htmx.ajax('GET', '/htmx/chains', '#chain-list');
                }).catch(function() {});
                _chainWasRunning = false;
            }
        }
    }

    // --- API Wrappers ---

    var api = {
        get: function(url) {
            return fetch(url).then(function(resp) {
                if (!resp.ok) return resp.text().then(function(msg) { throw new Error(msg); });
                return resp.json();
            });
        },
        post: function(url, data) {
            return fetch(url, {
                method: 'POST',
                headers: data ? {'Content-Type': 'application/json'} : {},
                body: data ? JSON.stringify(data) : undefined
            }).then(function(resp) {
                if (!resp.ok) return resp.text().then(function(msg) { throw new Error(msg); });
                var ct = resp.headers.get('content-type');
                if (ct && ct.indexOf('application/json') >= 0) return resp.json();
                return null;
            });
        },
        put: function(url, data) {
            return fetch(url, {
                method: 'PUT',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(data)
            }).then(function(resp) {
                if (!resp.ok) return resp.text().then(function(msg) { throw new Error(msg); });
                var ct = resp.headers.get('content-type');
                if (ct && ct.indexOf('application/json') >= 0) return resp.json();
                return null;
            });
        },
        del: function(url) {
            return fetch(url, { method: 'DELETE' }).then(function(resp) {
                if (!resp.ok) return resp.text().then(function(msg) { throw new Error(msg); });
                return null;
            });
        }
    };

    // --- Toast Notifications ---

    var toastContainer = null;

    function ensureToastContainer() {
        if (!toastContainer) {
            toastContainer = document.createElement('div');
            toastContainer.className = 'toast-container';
            document.body.appendChild(toastContainer);
        }
    }

    function toast(message, type) {
        ensureToastContainer();
        type = type || 'info';
        var el = document.createElement('div');
        el.className = 'toast toast-' + type;
        el.textContent = message;
        toastContainer.appendChild(el);
        requestAnimationFrame(function() { el.classList.add('toast-show'); });
        setTimeout(function() {
            el.classList.remove('toast-show');
            setTimeout(function() { if (el.parentNode) el.parentNode.removeChild(el); }, 300);
        }, 3000);
    }

    // --- Theme Management ---

    function getStoredTheme() { return localStorage.getItem('theme'); }

    function getEffectiveTheme() {
        var stored = getStoredTheme();
        if (stored === 'light' || stored === 'dark') return stored;
        return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }

    function applyTheme() {
        var effective = getEffectiveTheme();
        document.documentElement.dataset.theme = effective;
        var btn = document.querySelector('.theme-toggle');
        if (!btn) return;
        var stored = getStoredTheme();
        if (stored === 'dark') {
            btn.textContent = '\u263D';
            btn.title = 'Theme: dark (click for system)';
        } else if (!stored) {
            btn.textContent = '\u25D0';
            btn.title = 'Theme: system (click for light)';
        } else {
            btn.textContent = '\u2600';
            btn.title = 'Theme: light (click for dark)';
        }
    }

    function toggleTheme() {
        var stored = getStoredTheme();
        if (stored === 'light') localStorage.setItem('theme', 'dark');
        else if (stored === 'dark') localStorage.removeItem('theme');
        else localStorage.setItem('theme', 'light');
        applyTheme();
    }

    function initTheme() {
        applyTheme();
        window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function() {
            if (!getStoredTheme()) applyTheme();
        });
    }

    // --- HTML Escaping ---

    function escapeHTML(str) {
        if (str == null) return '';
        return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    function escapeAttr(str) {
        if (str == null) return '';
        return String(str).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }

    // --- Action Functions ---

    function createChain() {
        var name = document.getElementById('chain-name').value.trim();
        if (!name) { toast('Chain name is required', 'error'); return; }
        api.post('/htmx/chains', {
            name: name,
            enabled: document.getElementById('chain-enabled').checked,
            debounce_sec: parseInt(document.getElementById('chain-debounce').value) || 0,
            cooldown_sec: parseInt(document.getElementById('chain-cooldown').value) || 0,
            blocks: []
        }).then(function() {
            hideModal('chain-modal');
            toast('Chain created', 'success');
            htmx.ajax('GET', '/htmx/chains', '#chain-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function createSource() {
        var sourceType = document.getElementById('source-type').value;
        var name = document.getElementById('source-name').value.trim();
        if (!name) { toast('Source name is required', 'error'); return; }
        var data = {
            type: sourceType,
            name: name,
            enabled: document.getElementById('source-enabled').checked
        };
        if (sourceType === 'warlink') {
            data.url = document.getElementById('source-url').value.trim();
        } else if (sourceType === 'ping') {
            data.host = document.getElementById('source-host').value.trim();
            data.port = parseInt(document.getElementById('source-port').value) || 0;
            var secs = parseInt(document.getElementById('source-ping-interval').value) || 30;
            data.ping_interval = secs * 1000000000; // nanoseconds
        } else {
            data.address = document.getElementById('source-address').value.trim();
            data.family = document.getElementById('source-family').value;
            data.slot = parseInt(document.getElementById('source-slot').value) || 0;
        }
        api.post('/htmx/sources', data).then(function() {
            hideModal('source-modal');
            toast('Source added', 'success');
            htmx.ajax('GET', '/htmx/sources', '#source-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function toggleSourceType(selectEl) {
        var val = selectEl.value;
        var prefix = selectEl.id.indexOf('edit') >= 0 ? 'source-edit-' : 'source-';
        var warlinkFields = document.getElementById(prefix + 'warlink-fields');
        var plcFields = document.getElementById(prefix + 'plc-fields');
        var pingFields = document.getElementById(prefix + 'ping-fields');
        if (warlinkFields) warlinkFields.style.display = val === 'warlink' ? '' : 'none';
        if (plcFields) plcFields.style.display = val === 'plc' ? '' : 'none';
        if (pingFields) pingFields.style.display = val === 'ping' ? '' : 'none';
    }

    function editSource(name) {
        api.get('/htmx/sources/' + encodeURIComponent(name)).then(function(src) {
            document.getElementById('source-edit-type').value = src.type;
            document.getElementById('source-edit-name').value = src.name;
            document.getElementById('source-edit-enabled').checked = !!src.enabled;

            var warlinkFields = document.getElementById('source-edit-warlink-fields');
            var plcFields = document.getElementById('source-edit-plc-fields');
            var pingFields = document.getElementById('source-edit-ping-fields');

            warlinkFields.style.display = 'none';
            plcFields.style.display = 'none';
            pingFields.style.display = 'none';

            if (src.type === 'warlink') {
                warlinkFields.style.display = '';
                document.getElementById('source-edit-url').value = src.url || '';
            } else if (src.type === 'ping') {
                pingFields.style.display = '';
                document.getElementById('source-edit-host').value = src.host || '';
                document.getElementById('source-edit-port').value = src.port || 0;
                var intervalSecs = src.ping_interval ? Math.round(src.ping_interval / 1000000000) : 30;
                document.getElementById('source-edit-ping-interval').value = intervalSecs;
            } else {
                plcFields.style.display = '';
                document.getElementById('source-edit-address').value = src.address || '';
                document.getElementById('source-edit-family').value = src.family || 'logix';
                document.getElementById('source-edit-slot').value = src.slot || 0;
            }
            showModal('source-edit-modal');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function saveSource() {
        var sourceType = document.getElementById('source-edit-type').value;
        var name = document.getElementById('source-edit-name').value;
        var data = {
            type: sourceType,
            name: name,
            enabled: document.getElementById('source-edit-enabled').checked
        };
        if (sourceType === 'warlink') {
            data.url = document.getElementById('source-edit-url').value.trim();
        } else if (sourceType === 'ping') {
            data.host = document.getElementById('source-edit-host').value.trim();
            data.port = parseInt(document.getElementById('source-edit-port').value) || 0;
            var secs = parseInt(document.getElementById('source-edit-ping-interval').value) || 30;
            data.ping_interval = secs * 1000000000; // nanoseconds
        } else {
            data.address = document.getElementById('source-edit-address').value.trim();
            data.family = document.getElementById('source-edit-family').value;
            data.slot = parseInt(document.getElementById('source-edit-slot').value) || 0;
        }
        api.put('/htmx/sources/' + encodeURIComponent(name), data).then(function() {
            hideModal('source-edit-modal');
            toast('Source updated', 'success');
            htmx.ajax('GET', '/htmx/sources', '#source-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function createSubscriber() {
        var phone = document.getElementById('sub-phone').value.trim();
        if (!phone) { toast('Phone number is required', 'error'); return; }
        var topics = getCheckedValues('sub-topics-group');
        api.post('/htmx/subscribers', {
            phone: phone,
            name: document.getElementById('sub-name').value.trim(),
            topics: topics,
            active: true
        }).then(function() {
            hideModal('sub-modal');
            toast('Subscriber added', 'success');
            htmx.ajax('GET', '/htmx/subscribers', '#subscriber-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function getCheckedValues(groupId) {
        var group = document.getElementById(groupId);
        if (!group) return [];
        var boxes = group.querySelectorAll('input[type="checkbox"]:checked');
        var vals = [];
        for (var i = 0; i < boxes.length; i++) vals.push(boxes[i].value);
        return vals;
    }

    function saveSMSProvider() {
        var smsType = document.getElementById('sms-type').value;
        var data = {
            type: smsType,
            enabled: document.getElementById('sms-enabled').checked,
            global_rate_per_min: parseInt(document.getElementById('sms-rate').value) || 30
        };
        if (smsType === 'smsgate') {
            data.mode = document.getElementById('sms-mode').value;
            data.base_url = document.getElementById('sms-base-url').value;
            data.username = document.getElementById('sms-username').value;
            data.password = document.getElementById('sms-password').value;
            data.webhook_secret = document.getElementById('sms-webhook-secret').value;
        } else {
            data.account_sid = document.getElementById('sms-account-sid').value;
            data.auth_token = document.getElementById('sms-auth-token').value;
            data.from_number = document.getElementById('sms-from-number').value;
        }
        api.post('/htmx/providers/sms', data).then(function() {
            toast('SMS provider saved', 'success');
            setTimeout(function() { window.location.reload(); }, 500);
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function toggleSMSType() {
        var isTwilio = document.getElementById('sms-type').value === 'twilio';
        document.getElementById('smsgate-fields').style.display = isTwilio ? 'none' : '';
        document.getElementById('twilio-fields').style.display = isTwilio ? '' : 'none';
        var connBtn = document.getElementById('sms-test-conn-btn');
        if (connBtn) connBtn.style.display = isTwilio ? 'none' : '';
        var regBtn = document.getElementById('sms-register-webhook-btn');
        if (regBtn) regBtn.style.display = isTwilio ? 'none' : '';
    }

    function toggleSMSMode() {
        var mode = document.getElementById('sms-mode');
        if (!mode) return;
        var isLocal = mode.value === 'local';
        var hint = document.getElementById('sms-mode-hint');
        if (hint) {
            hint.textContent = isLocal
                ? 'Connect directly to an Android phone running SMS-Gate on your local network.'
                : 'Connect to the SMS-Gate cloud (api.sms-gate.app) or your own private server.';
        }
        var urlInput = document.getElementById('sms-base-url');
        if (urlInput) {
            urlInput.placeholder = isLocal ? 'http://192.168.1.100:8080' : 'https://api.sms-gate.app';
        }
    }

    function testSMSConnection() {
        var data = {
            mode: document.getElementById('sms-mode').value,
            base_url: document.getElementById('sms-base-url').value,
            username: document.getElementById('sms-username').value,
            password: document.getElementById('sms-password').value
        };
        if (!data.base_url) { toast('Base URL is required', 'error'); return; }
        toast('Testing connection...', 'info');
        fetch('/htmx/providers/sms/test-connection', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(data)
        }).then(function(resp) {
            return resp.text().then(function(text) {
                if (!resp.ok) throw new Error(text);
                return text;
            });
        }).then(function(msg) {
            toast('SMS-Gate: ' + msg, 'success');
        }).catch(function(err) {
            toast('SMS-Gate: ' + err.message, 'error');
        });
    }

    function saveEmailProvider() {
        api.post('/htmx/providers/email', {
            enabled: document.getElementById('email-enabled').checked,
            host: document.getElementById('email-host').value,
            port: parseInt(document.getElementById('email-port').value) || 587,
            username: document.getElementById('email-username').value,
            password: document.getElementById('email-password').value,
            from: document.getElementById('email-from').value,
            use_tls: document.getElementById('email-tls').checked
        }).then(function() {
            toast('Email provider saved', 'success');
            setTimeout(function() { window.location.reload(); }, 500);
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function testSMS() {
        var phone = prompt('Enter phone number for test SMS (e.g. +15551234567):');
        if (!phone) return;
        toast('Sending test SMS to ' + phone + '...', 'info');
        api.post('/htmx/providers/sms/test', { phone: phone, message: 'WarAlert test message' })
            .then(function() { toast('Test SMS sent successfully to ' + phone, 'success'); })
            .catch(function(err) { toast('SMS send failed: ' + err.message, 'error'); });
    }

    function testEmail() {
        var to = prompt('Enter email address for test:');
        if (!to) return;
        toast('Sending test email to ' + to + '...', 'info');
        api.post('/htmx/providers/email/test', { to: [to] })
            .then(function() { toast('Test email sent successfully to ' + to, 'success'); })
            .catch(function(err) { toast('Email send failed: ' + err.message, 'error'); });
    }

    function copyWebhookURL() {
        var input = document.getElementById('sms-webhook-url');
        if (!input) return;
        if (navigator.clipboard) {
            navigator.clipboard.writeText(input.value).then(function() {
                toast('Webhook URL copied', 'success');
            }).catch(function() {
                input.select();
                document.execCommand('copy');
                toast('Webhook URL copied', 'success');
            });
        } else {
            input.select();
            document.execCommand('copy');
            toast('Webhook URL copied', 'success');
        }
    }

    function registerSMSWebhook() {
        toast('Registering webhook...', 'info');
        api.post('/htmx/providers/sms/register-webhook').then(function() {
            toast('Webhook registered with SMS-gate', 'success');
            checkWebhookStatus();
        }).catch(function(err) {
            toast('Register webhook failed: ' + err.message, 'error');
        });
    }

    function checkWebhookStatus() {
        var badge = document.getElementById('sms-webhook-status-badge');
        if (!badge) return;
        var smsType = document.getElementById('sms-type');
        if (smsType && smsType.value === 'twilio') {
            badge.textContent = 'N/A';
            badge.className = 'badge badge-muted';
            return;
        }
        fetch('/htmx/providers/sms/webhook-status').then(function(resp) {
            if (!resp.ok) return resp.text().then(function(msg) { throw new Error(msg); });
            return resp.json();
        }).then(function(data) {
            if (data.registered) {
                badge.textContent = 'Registered';
                badge.className = 'badge badge-success';
            } else {
                badge.textContent = 'Not Registered';
                badge.className = 'badge badge-muted';
            }
        }).catch(function() {
            badge.textContent = 'Error';
            badge.className = 'badge badge-muted';
        });
    }

    // --- Topic Management ---

    function createTopic() {
        var name = document.getElementById('topic-name').value.trim();
        if (!name) { toast('Topic name is required', 'error'); return; }
        api.post('/htmx/topics', { topic: name }).then(function() {
            hideModal('topic-modal');
            document.getElementById('topic-name').value = '';
            toast('Topic added', 'success');
            htmx.ajax('GET', '/htmx/topics', '#topic-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function createTopicInline() {
        var input = document.getElementById('topic-inline-name');
        if (!input) return;
        var name = input.value.trim().toUpperCase();
        if (!name) { toast('Topic name is required', 'error'); return; }
        api.post('/htmx/topics', { topic: name }).then(function() {
            input.value = '';
            toast('Topic "' + name + '" created', 'success');
            htmx.ajax('GET', '/htmx/subscribers', '#subscriber-list');
            refreshTopicBadges();
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function deleteTopic(name) {
        if (!confirm('Delete topic "' + name + '"? Subscribers will lose this topic assignment.')) return;
        api.del('/htmx/topics/' + encodeURIComponent(name)).then(function() {
            toast('Topic "' + name + '" deleted', 'success');
            htmx.ajax('GET', '/htmx/subscribers', '#subscriber-list');
            refreshTopicBadges();
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function refreshTopicBadges() {
        // Reload the page to get updated topic lists in modals
        setTimeout(function() { window.location.reload(); }, 500);
    }

    // --- User Management ---

    function createUser() {
        var username = document.getElementById('user-add-username').value.trim();
        var password = document.getElementById('user-add-password').value;
        if (!username || !password) { toast('Username and password are required', 'error'); return; }
        api.post('/htmx/users', {
            username: username,
            password: password,
            role: document.getElementById('user-add-role').value
        }).then(function() {
            hideModal('user-add-modal');
            document.getElementById('user-add-username').value = '';
            document.getElementById('user-add-password').value = '';
            toast('User created', 'success');
            htmx.ajax('GET', '/htmx/users', '#user-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function editUser(username, role) {
        document.getElementById('user-edit-username').value = username;
        document.getElementById('user-edit-password').value = '';
        document.getElementById('user-edit-role').value = role;
        showModal('user-edit-modal');
    }

    function saveUser() {
        var username = document.getElementById('user-edit-username').value;
        var data = { role: document.getElementById('user-edit-role').value };
        var pw = document.getElementById('user-edit-password').value;
        if (pw) data.password = pw;
        api.put('/htmx/users/' + encodeURIComponent(username), data).then(function() {
            hideModal('user-edit-modal');
            toast('User updated', 'success');
            htmx.ajax('GET', '/htmx/users', '#user-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    // --- Tag Picker ---
    // Cached tags per PLC: { plcName: [{name, type}, ...] }
    var _tagCache = {};

    function loadTagsForPLC(plcName, cb) {
        if (!plcName) { if (cb) cb([]); return; }
        if (_tagCache[plcName]) { if (cb) cb(_tagCache[plcName]); return; }
        api.get('/htmx/plc-tags/' + encodeURIComponent(plcName)).then(function(tags) {
            _tagCache[plcName] = tags || [];
            if (cb) cb(_tagCache[plcName]);
        }).catch(function() {
            _tagCache[plcName] = [];
            if (cb) cb([]);
        });
    }

    function invalidateTagCache(plcName) {
        if (plcName) delete _tagCache[plcName];
        else _tagCache = {};
    }

    // Render a filterable tag picker dropdown.
    // pickerId: unique id for this picker instance
    // plcName: PLC to load tags for
    // selectedTag: currently selected tag name
    // onSelectExpr: JS expression string called with the tag name, e.g. "WarAlert.updateConditionField(0,0,'tag',TAG)"
    //   TAG will be replaced with the actual quoted value
    function tagPickerHTML(pickerId, plcName, selectedTag, onSelectExpr) {
        var html = '<div class="tag-picker" id="' + pickerId + '">';
        html += '<input type="text" class="tag-picker-input" value="' + escapeAttr(selectedTag || '') + '"';
        html += ' placeholder="Search tags..." autocomplete="off"';
        html += ' data-plc="' + escapeAttr(plcName || '') + '"';
        html += ' data-onselect="' + escapeAttr(onSelectExpr) + '"';
        html += ' onfocus="WarAlert.openTagPicker(this)"';
        html += ' oninput="WarAlert.filterTagPicker(this)"';
        html += ' onblur="setTimeout(function(){WarAlert.closeTagPicker(\'' + escapeAttr(pickerId) + '\')},200)"';
        html += '>';
        html += '<div class="tag-picker-dropdown" style="display:none"></div>';
        html += '</div>';
        return html;
    }

    function openTagPicker(inputEl) {
        var picker = inputEl.closest('.tag-picker');
        var dropdown = picker.querySelector('.tag-picker-dropdown');
        var plcName = inputEl.dataset.plc;

        if (!plcName) {
            dropdown.innerHTML = '<div class="tag-picker-empty">Select a PLC first</div>';
            dropdown.style.display = '';
            return;
        }

        loadTagsForPLC(plcName, function(tags) {
            renderTagDropdown(picker, tags, inputEl.value);
            dropdown.style.display = '';
        });
    }

    function filterTagPicker(inputEl) {
        var picker = inputEl.closest('.tag-picker');
        var plcName = inputEl.dataset.plc;
        var tags = _tagCache[plcName] || [];
        renderTagDropdown(picker, tags, inputEl.value);

        // Also update the model value as user types (allows free-text)
        var onSelect = inputEl.dataset.onselect;
        if (onSelect) {
            var expr = onSelect.replace('TAG', JSON.stringify(inputEl.value));
            try { (new Function(expr))(); } catch(e) {}
        }
    }

    function renderTagDropdown(picker, tags, filter) {
        var dropdown = picker.querySelector('.tag-picker-dropdown');
        var q = (filter || '').toLowerCase();
        var filtered = tags.filter(function(t) {
            return !q || t.name.toLowerCase().indexOf(q) >= 0 || (t.type && t.type.toLowerCase().indexOf(q) >= 0);
        });

        if (filtered.length === 0) {
            dropdown.innerHTML = '<div class="tag-picker-empty">' + (tags.length === 0 ? 'No tags available' : 'No matching tags') + '</div>';
            return;
        }

        var html = '';
        for (var i = 0; i < filtered.length; i++) {
            var t = filtered[i];
            html += '<div class="tag-picker-item" onmousedown="WarAlert.selectTagPickerItem(this)"';
            html += ' data-tag="' + escapeAttr(t.name) + '">';
            html += '<span class="tag-picker-name">' + escapeHTML(t.name) + '</span>';
            if (t.type) html += '<span class="tag-picker-type">' + escapeHTML(t.type) + '</span>';
            html += '</div>';
        }
        dropdown.innerHTML = html;
    }

    function selectTagPickerItem(itemEl) {
        var picker = itemEl.closest('.tag-picker');
        var input = picker.querySelector('.tag-picker-input');
        var tagName = itemEl.dataset.tag;

        input.value = tagName;
        picker.querySelector('.tag-picker-dropdown').style.display = 'none';

        var onSelect = input.dataset.onselect;
        if (onSelect) {
            var expr = onSelect.replace('TAG', JSON.stringify(tagName));
            try { (new Function(expr))(); } catch(e) {}
        }
    }

    function closeTagPicker(pickerId) {
        var picker = document.getElementById(pickerId);
        if (picker) {
            var dropdown = picker.querySelector('.tag-picker-dropdown');
            if (dropdown) dropdown.style.display = 'none';
        }
    }

    // --- PLC Tag Management ---

    var _currentTagPLC = '';

    var _addTagSelection = '';

    function manageTags(plcName) {
        _currentTagPLC = plcName;
        _addTagSelection = '';
        document.getElementById('plc-tags-name').textContent = plcName;
        // Render tag picker into container
        var container = document.getElementById('plc-tag-picker-container');
        if (container) {
            var onSelect = 'WarAlert._setAddTagSelection(TAG)';
            container.innerHTML = tagPickerHTML('tp-add-tag', plcName, '', onSelect);
        }
        // Invalidate cache so we get fresh tags, then pre-load
        invalidateTagCache(plcName);
        loadTagsForPLC(plcName);
        refreshTagList(plcName);
        showModal('plc-tags-modal');
    }

    function _setAddTagSelection(tagName) {
        _addTagSelection = tagName;
    }

    function refreshTagList(plcName) {
        api.get('/htmx/plcs/' + encodeURIComponent(plcName) + '/tags').then(function(tags) {
            var container = document.getElementById('plc-tag-list');
            if (!tags || tags.length === 0) {
                container.innerHTML = '<div class="empty-state">No tags configured for this PLC.</div>';
                return;
            }
            var html = '<table class="table"><thead><tr><th>Tag Name</th><th>Enabled</th><th>Actions</th></tr></thead><tbody>';
            for (var i = 0; i < tags.length; i++) {
                var t = tags[i];
                html += '<tr><td>' + escapeHTML(t.name) + '</td>';
                html += '<td>' + (t.enabled ? '<span class="badge badge-success">Yes</span>' : '<span class="badge badge-muted">No</span>') + '</td>';
                html += '<td class="actions"><button class="btn btn-sm btn-danger" onclick="WarAlert.removePLCTag(\'' + escapeAttr(_currentTagPLC) + '\', \'' + escapeAttr(t.name) + '\')">Remove</button></td></tr>';
            }
            html += '</tbody></table>';
            container.innerHTML = html;
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function addPLCTag() {
        // Read from the tag picker input
        var picker = document.getElementById('tp-add-tag');
        var input = picker ? picker.querySelector('.tag-picker-input') : null;
        var tagName = (input ? input.value : _addTagSelection).trim();
        if (!tagName) { toast('Tag name is required', 'error'); return; }
        api.post('/htmx/plcs/' + encodeURIComponent(_currentTagPLC) + '/tags', {
            name: tagName,
            enabled: document.getElementById('plc-tag-enabled').checked
        }).then(function() {
            if (input) input.value = '';
            _addTagSelection = '';
            toast('Tag added', 'success');
            invalidateTagCache(_currentTagPLC);
            refreshTagList(_currentTagPLC);
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function removePLCTag(plcName, tagName) {
        api.del('/htmx/plcs/' + encodeURIComponent(plcName) + '/tags/' + encodeURIComponent(tagName)).then(function() {
            toast('Tag removed', 'success');
            refreshTagList(plcName);
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    // --- Subscriber Edit ---

    function editSubTopicBadgeHTML(topic) {
        return '<span class="badge badge-success" data-topic="' + escapeAttr(topic) + '" style="display:inline-flex;align-items:center;gap:0.25rem">'
            + escapeHTML(topic)
            + '<button type="button" style="background:none;border:none;cursor:pointer;font-size:14px;line-height:1;padding:0 2px;color:inherit;opacity:0.7" onclick="WarAlert.removeEditSubTopic(this)" title="Remove">&times;</button>'
            + '</span>';
    }

    function addEditSubTopic(selectEl) {
        var topic = selectEl.value;
        if (!topic) return;
        selectEl.value = '';
        var container = document.getElementById('sub-edit-topics-selected');
        if (!container) return;
        if (container.querySelector('[data-topic="' + topic + '"]')) return;
        container.insertAdjacentHTML('beforeend', editSubTopicBadgeHTML(topic));
    }

    function removeEditSubTopic(btnEl) {
        btnEl.parentElement.remove();
    }

    function getEditSubTopics() {
        var container = document.getElementById('sub-edit-topics-selected');
        if (!container) return [];
        var badges = container.querySelectorAll('[data-topic]');
        var topics = [];
        for (var i = 0; i < badges.length; i++) topics.push(badges[i].dataset.topic);
        return topics;
    }

    function editSubscriber(phone, name, topicsStr) {
        document.getElementById('sub-edit-phone').value = phone;
        document.getElementById('sub-edit-name').value = name;
        var topics = topicsStr ? topicsStr.split(',') : [];
        var container = document.getElementById('sub-edit-topics-selected');
        if (container) {
            var html = '';
            for (var i = 0; i < topics.length; i++) {
                if (topics[i]) html += editSubTopicBadgeHTML(topics[i]);
            }
            container.innerHTML = html;
        }
        var picker = document.getElementById('sub-edit-topic-picker');
        if (picker) picker.value = '';
        showModal('sub-edit-modal');
    }

    function saveSubscriber() {
        var phone = document.getElementById('sub-edit-phone').value;
        var name = document.getElementById('sub-edit-name').value.trim();
        var topics = getEditSubTopics();
        api.put('/htmx/subscribers/' + encodeURIComponent(phone), {
            name: name,
            topics: topics
        }).then(function() {
            hideModal('sub-edit-modal');
            toast('Subscriber updated', 'success');
            htmx.ajax('GET', '/htmx/subscribers', '#subscriber-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    // --- Chain Block Editor ---

    var _chainBlocks = [];
    var _chainEditName = '';
    var _chainWasRunning = false;
    var _gatePoller = null;

    function testFireChain(name) {
        api.get('/htmx/chains/' + encodeURIComponent(name)).then(function(cfg) {
            var topics = [];
            var blocks = cfg.blocks || [];
            for (var i = 0; i < blocks.length; i++) {
                var b = blocks[i];
                if (b.type === 'action') {
                    if (b.action_type === 'sms' && b.topic) topics.push(b.topic);
                    if (b.action_type === 'email' && b.to && b.to.length) topics.push(b.to.join(', '));
                    if (b.action_type === 'webhook' && b.url) topics.push(b.url);
                }
            }
            var target = topics.length > 0 ? topics.join(', ') : 'configured subscribers';
            if (!confirm('This will send a test alert to all subscribers for: ' + target + '\n\nAre you sure?')) return;

            api.post('/htmx/chains/' + encodeURIComponent(name) + '/test')
                .then(function(result) {
                    if (!result) {
                        toast('Test fire completed (no response)', 'warning');
                        return;
                    }
                    var msgs = [];
                    msgs.push(result.actions_fired + ' action(s) fired');
                    if (result.gates_passed) msgs.push(result.gates_passed + ' gate(s) passed');
                    if (result.gates_failed) msgs.push(result.gates_failed + ' gate(s) blocked');

                    if (result.errors && result.errors.length > 0) {
                        toast('Test fire: ' + msgs.join(', ') + '. Errors: ' + result.errors.join('; '), 'warning');
                    } else {
                        toast('Test fire successful: ' + msgs.join(', '), 'success');
                    }
                    htmx.ajax('GET', '/htmx/chains', '#chain-list');
                })
                .catch(function(err) { toast('Test fire failed: ' + err.message, 'error'); });
        }).catch(function(err) { toast('Failed to load chain: ' + err.message, 'error'); });
    }

    function editChain(name) {
        _chainEditName = name;
        api.get('/htmx/chains/' + encodeURIComponent(name)).then(function(cfg) {
            _chainWasRunning = !!cfg.enabled;
            // Stop the chain while editing to prevent alerts during configuration
            if (_chainWasRunning) {
                api.post('/htmx/chains/' + encodeURIComponent(name) + '/stop').catch(function() {});
            }
            document.getElementById('chain-edit-title').textContent = name;
            document.getElementById('chain-edit-debounce').value = cfg.debounce_sec || 0;
            document.getElementById('chain-edit-cooldown').value = cfg.cooldown_sec || 0;
            document.getElementById('chain-edit-enabled').checked = !!cfg.enabled;
            _chainBlocks = cfg.blocks || [];
            renderBlocks();
            showModal('chain-edit-modal');
            startGatePoller();
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function startGatePoller() {
        stopGatePoller();
        pollGates();
        _gatePoller = setInterval(pollGates, 2000);
    }

    function stopGatePoller() {
        if (_gatePoller) { clearInterval(_gatePoller); _gatePoller = null; }
    }

    function pollGates() {
        if (!_chainEditName) return;
        api.post('/htmx/chains/' + encodeURIComponent(_chainEditName) + '/gates', _chainBlocks).then(function(results) {
            var container = document.getElementById('chain-blocks-container');
            if (!container) return;
            var blocks = container.querySelectorAll('.chain-block');
            for (var i = 0; i < blocks.length; i++) {
                var info = results[String(i)];
                blocks[i].classList.remove('gate-true', 'gate-waiting', 'gate-false');
                // Remove any existing countdown
                var existing = blocks[i].querySelector('.gate-countdown');
                if (existing) existing.remove();

                if (!info) continue;
                var status = info.status;
                if (status === 'true') {
                    blocks[i].classList.add('gate-true');
                } else if (status === 'waiting') {
                    blocks[i].classList.add('gate-waiting');
                    // Show countdown in header
                    if (info.remaining_sec > 0) {
                        var mins = Math.floor(info.remaining_sec / 60);
                        var secs = info.remaining_sec % 60;
                        var text = mins > 0 ? (mins + ' min ' + secs + ' sec remaining') : (secs + ' sec remaining');
                        var span = document.createElement('span');
                        span.className = 'gate-countdown';
                        span.style.cssText = 'font-size:12px;color:var(--warning,#ffc107);margin-left:0.25rem';
                        span.textContent = '(' + text + ')';
                        var header = blocks[i].querySelector('.chain-block-header');
                        if (header) {
                            var btnSpan = header.querySelector('span[style*="margin-left:auto"]');
                            if (btnSpan) header.insertBefore(span, btnSpan);
                            else header.appendChild(span);
                        }
                    }
                } else if (status === 'false') {
                    blocks[i].classList.add('gate-false');
                }
            }
        }).catch(function() {});
    }

    function saveChain() {
        var existingTopics = window._waralertTopics || [];
        var newTopics = [];

        var data = {
            name: _chainEditName,
            enabled: document.getElementById('chain-edit-enabled').checked,
            debounce_sec: parseInt(document.getElementById('chain-edit-debounce').value) || 0,
            cooldown_sec: parseInt(document.getElementById('chain-edit-cooldown').value) || 0,
            blocks: _chainBlocks.map(function(b) {
                var block = JSON.parse(JSON.stringify(b));
                // Ensure condition values are properly typed
                if (block.conditions) {
                    for (var i = 0; i < block.conditions.length; i++) {
                        var c = block.conditions[i];
                        if (c.type === 'plc_tag' && c.value != null) {
                            var num = Number(c.value);
                            if (!isNaN(num) && String(c.value).trim() !== '') {
                                c.value = num;
                            }
                        }
                    }
                }
                // Collect new topics from action blocks
                if (block.type === 'action' && block.topic) {
                    var t = block.topic.trim().toUpperCase();
                    block.topic = t;
                    if (t && existingTopics.indexOf(t) < 0 && newTopics.indexOf(t) < 0) {
                        newTopics.push(t);
                    }
                }
                return block;
            })
        };

        // Auto-create new topics, then save the chain
        var topicPromises = newTopics.map(function(t) {
            return api.post('/htmx/topics', { topic: t }).catch(function() {});
        });

        Promise.all(topicPromises).then(function() {
            // Update local topic list
            for (var i = 0; i < newTopics.length; i++) {
                if (existingTopics.indexOf(newTopics[i]) < 0) {
                    existingTopics.push(newTopics[i]);
                }
            }
            window._waralertTopics = existingTopics;

            return api.put('/htmx/chains/' + encodeURIComponent(_chainEditName), data);
        }).then(function() {
            _chainWasRunning = false; // PUT handler restarts if enabled
            hideModal('chain-edit-modal');
            toast('Chain saved', 'success');
            htmx.ajax('GET', '/htmx/chains', '#chain-list');
        }).catch(function(err) { toast(err.message, 'error'); });
    }

    function renderBlocks() {
        var container = document.getElementById('chain-blocks-container');
        if (!container) return;
        if (_chainBlocks.length === 0) {
            container.innerHTML = '<div class="empty-state">No blocks. Add a Gate or Action block.</div>';
            return;
        }
        var html = '';
        for (var i = 0; i < _chainBlocks.length; i++) {
            html += renderBlockHTML(_chainBlocks[i], i);
        }
        container.innerHTML = html;
    }

    function renderBlockHTML(block, idx) {
        var typeClass = block.type === 'gate' ? 'block-type-gate' : 'block-type-action';
        var typeLabel = block.type === 'gate' ? 'GATE' : 'ACTION';
        var html = '<div class="chain-block">';
        html += '<div class="chain-block-header">';
        html += '<span class="block-type-badge ' + typeClass + '">' + typeLabel + '</span>';
        if (block.type === 'gate') {
            html += '<select onchange="WarAlert.updateBlockField(' + idx + ', \'logic_mode\', this.value)" style="width:auto;font-size:13px;padding:2px 6px">';
            html += '<option value="and"' + (block.logic_mode !== 'or' ? ' selected' : '') + '>AND</option>';
            html += '<option value="or"' + (block.logic_mode === 'or' ? ' selected' : '') + '>OR</option>';
            html += '</select>';
            html += '<span style="font-size:12px;color:var(--text-muted,#888)" title="All conditions in this gate must remain continuously true for this duration before the gate activates. 0 = immediate.">Duration:</span>';
            html += '<input type="number" value="' + (block.duration_min || 0) + '" min="0" style="width:70px;font-size:13px;padding:2px 6px" title="All conditions in this gate must remain continuously true for this duration before the gate activates. 0 = immediate." onchange="WarAlert.updateBlockField(' + idx + ', \'duration_min\', parseInt(this.value)||0)">';
            html += '<span style="font-size:12px;color:var(--text-muted,#888)">min</span>';
        } else {
            html += '<select onchange="WarAlert.updateBlockField(' + idx + ', \'action_type\', this.value); WarAlert.renderBlocks()" style="width:auto;font-size:13px;padding:2px 6px">';
            html += '<option value="sms"' + (block.action_type === 'sms' || !block.action_type ? ' selected' : '') + '>Send SMS</option>';
            html += '<option value="email"' + (block.action_type === 'email' ? ' selected' : '') + '>Send Email</option>';
            html += '<option value="webhook"' + (block.action_type === 'webhook' ? ' selected' : '') + '>Webhook</option>';
            html += '</select>';
        }
        html += '<span style="margin-left:auto;display:flex;gap:0.25rem">';
        html += '<button class="btn btn-sm" onclick="WarAlert.moveBlock(' + idx + ', -1)" ' + (idx === 0 ? 'disabled' : '') + '>&uarr;</button>';
        html += '<button class="btn btn-sm" onclick="WarAlert.moveBlock(' + idx + ', 1)" ' + (idx === _chainBlocks.length - 1 ? 'disabled' : '') + '>&darr;</button>';
        html += '<button class="btn btn-sm btn-danger" onclick="WarAlert.removeBlock(' + idx + ')">Remove</button>';
        html += '</span>';
        html += '</div>';
        html += '<div class="chain-block-body">';
        if (block.type === 'gate') {
            html += renderGateBody(block, idx);
        } else {
            html += renderActionBody(block, idx);
        }
        html += '</div></div>';
        return html;
    }

    // --- Gate Rendering ---

    function renderGateBody(block, idx) {
        var html = '';
        var conditions = block.conditions || [];
        for (var i = 0; i < conditions.length; i++) {
            html += renderConditionRow(idx, i, conditions[i]);
        }
        html += '<button class="btn btn-sm" onclick="WarAlert.addCondition(' + idx + ')">+ Add Condition</button>';
        return html;
    }

    function renderConditionRow(blockIdx, condIdx, cond) {
        var html = '<div class="condition-row">';
        html += '<select onchange="WarAlert.updateConditionType(' + blockIdx + ', ' + condIdx + ', this.value)" style="width:auto">';
        html += '<option value="plc_tag"' + (cond.type === 'plc_tag' ? ' selected' : '') + '>PLC Tag</option>';
        html += '<option value="ping"' + (cond.type === 'ping' ? ' selected' : '') + '>Ping Monitor</option>';
        html += '<option value="time_between"' + (cond.type === 'time_between' ? ' selected' : '') + '>Time Between</option>';
        html += '<option value="weekday"' + (cond.type === 'weekday' ? ' selected' : '') + '>Weekday</option>';
        html += '</select>';

        if (cond.type === 'plc_tag') {
            html += renderPLCTagFields(blockIdx, condIdx, cond);
        } else if (cond.type === 'ping') {
            html += renderPingFields(blockIdx, condIdx, cond);
        } else if (cond.type === 'time_between') {
            html += renderTimeBetweenFields(blockIdx, condIdx, cond);
        } else if (cond.type === 'weekday') {
            html += renderWeekdayFields(blockIdx, condIdx, cond);
        }

        html += '<button class="btn btn-sm btn-danger" onclick="WarAlert.removeCondition(' + blockIdx + ', ' + condIdx + ')" style="align-self:center">&times;</button>';
        html += '</div>';
        return html;
    }

    function renderPLCTagFields(bi, ci, cond) {
        var plcNames = window._waralertPLCNames || [];
        var html = '<select onchange="WarAlert.updateConditionPLC(' + bi + ', ' + ci + ', this.value)" style="width:auto">';
        html += '<option value="">-- PLC --</option>';
        for (var i = 0; i < plcNames.length; i++) {
            html += '<option value="' + escapeAttr(plcNames[i]) + '"' + (cond.plc === plcNames[i] ? ' selected' : '') + '>' + escapeHTML(plcNames[i]) + '</option>';
        }
        html += '</select>';
        // Filterable tag picker
        var pickerId = 'tp-' + bi + '-' + ci;
        var onSelect = 'WarAlert.updateConditionField(' + bi + ',' + ci + ',\'tag\',TAG)';
        html += tagPickerHTML(pickerId, cond.plc, cond.tag, onSelect);
        html += '<select onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'operator\', this.value)" style="width:auto">';
        var ops = ['==', '!=', '>', '<', '>=', '<='];
        for (var j = 0; j < ops.length; j++) {
            html += '<option value="' + ops[j] + '"' + (cond.operator === ops[j] ? ' selected' : '') + '>' + escapeHTML(ops[j]) + '</option>';
        }
        html += '</select>';
        html += '<input type="text" value="' + escapeAttr(cond.value != null ? cond.value : '') + '" placeholder="Value" onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'value\', this.value)" style="width:80px">';

        // Pre-load tags for this PLC if set
        if (cond.plc) loadTagsForPLC(cond.plc);

        return html;
    }

    function renderPingFields(bi, ci, cond) {
        var pingNames = window._waralertPingNames || [];
        var html = '<select onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'plc\', this.value)" style="width:auto">';
        html += '<option value="">-- Monitor --</option>';
        for (var i = 0; i < pingNames.length; i++) {
            html += '<option value="' + escapeAttr(pingNames[i]) + '"' + (cond.plc === pingNames[i] ? ' selected' : '') + '>' + escapeHTML(pingNames[i]) + '</option>';
        }
        html += '</select>';
        html += '<select onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'operator\', this.value)" style="width:auto">';
        html += '<option value="=="' + (cond.operator === '==' ? ' selected' : '') + '>==</option>';
        html += '<option value="!="' + (cond.operator === '!=' ? ' selected' : '') + '>!=</option>';
        html += '</select>';
        html += '<select onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'value\', Number(this.value))" style="width:auto">';
        var valNum = (cond.value === true || cond.value === 1) ? 1 : 0;
        html += '<option value="1"' + (valNum === 1 ? ' selected' : '') + '>Online</option>';
        html += '<option value="0"' + (valNum === 0 ? ' selected' : '') + '>Offline</option>';
        html += '</select>';
        return html;
    }

    function updateConditionPLC(blockIdx, condIdx, plcName) {
        _chainBlocks[blockIdx].conditions[condIdx].plc = plcName;
        _chainBlocks[blockIdx].conditions[condIdx].tag = '';
        // Pre-load tags then re-render to update the picker
        if (plcName) {
            loadTagsForPLC(plcName, function() { renderBlocks(); });
        } else {
            renderBlocks();
        }
    }

    function renderTimeBetweenFields(bi, ci, cond) {
        var html = '<input type="time" value="' + escapeAttr(cond.time_from || '') + '" onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'time_from\', this.value)" style="width:auto">';
        html += '<span style="align-self:center">to</span>';
        html += '<input type="time" value="' + escapeAttr(cond.time_to || '') + '" onchange="WarAlert.updateConditionField(' + bi + ', ' + ci + ', \'time_to\', this.value)" style="width:auto">';
        return html;
    }

    function renderWeekdayFields(bi, ci, cond) {
        var days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
        var selected = cond.days || [];
        var html = '<div class="weekday-checkboxes">';
        for (var i = 0; i < days.length; i++) {
            var checked = selected.indexOf(days[i]) >= 0 ? ' checked' : '';
            html += '<label><input type="checkbox" value="' + days[i] + '"' + checked + ' onchange="WarAlert.updateConditionDays(' + bi + ', ' + ci + ')">' + days[i] + '</label>';
        }
        html += '</div>';
        return html;
    }

    // --- Action Rendering ---

    function renderActionBody(block, idx) {
        var html = '<div class="action-fields">';
        var aType = block.action_type || 'sms';
        if (aType === 'sms') {
            html += renderSMSFields(block, idx);
        } else if (aType === 'email') {
            html += renderEmailFields(block, idx);
        } else if (aType === 'webhook') {
            html += renderWebhookFields(block, idx);
        }
        html += '</div>';
        return html;
    }

    function renderSMSFields(block, idx) {
        var topics = window._waralertTopics || [];
        var pickerId = 'topic-picker-' + idx;
        var html = '<div class="form-group"><label>Topic</label>';
        html += '<div class="tag-picker" id="' + pickerId + '">';
        html += '<input type="text" class="tag-picker-input" value="' + escapeAttr(block.topic || '') + '"';
        html += ' placeholder="Select or type a topic..."';
        html += ' data-picker-type="topic" data-block-idx="' + idx + '"';
        html += ' onfocus="WarAlert.openTopicPicker(this)"';
        html += ' oninput="WarAlert.filterTopicPicker(this)"';
        html += ' onblur="setTimeout(function(){WarAlert.closeTagPicker(\'' + escapeAttr(pickerId) + '\')},200)"';
        html += ' onchange="WarAlert.updateBlockField(' + idx + ', \'topic\', this.value.trim().toUpperCase())"';
        html += '>';
        html += '<div class="tag-picker-dropdown" style="display:none"></div>';
        html += '</div>';
        html += '<small style="display:block;margin-top:0.25rem;color:var(--text-muted,#888)">Format: <strong>{TOPIC}</strong> or <strong>{TOPIC}-{SUBTOPIC}</strong> (e.g. FIRE-ZONE1). Subscribers to FIRE receive all FIRE-* alerts.</small>';
        html += '</div>';
        html += '<div class="form-group"><label>Message</label>';
        html += '<textarea onchange="WarAlert.updateBlockField(' + idx + ', \'message\', this.value)">' + escapeHTML(block.message || '') + '</textarea></div>';
        return html;
    }

    function openTopicPicker(inputEl) {
        var picker = inputEl.closest('.tag-picker');
        var dropdown = picker.querySelector('.tag-picker-dropdown');
        var topics = window._waralertTopics || [];
        renderTopicDropdown(picker, topics, inputEl.value);
        dropdown.style.display = '';
    }

    function filterTopicPicker(inputEl) {
        var picker = inputEl.closest('.tag-picker');
        var topics = window._waralertTopics || [];
        renderTopicDropdown(picker, topics, inputEl.value);
        // Update model as user types
        var idx = parseInt(inputEl.dataset.blockIdx);
        if (!isNaN(idx)) {
            _chainBlocks[idx].topic = inputEl.value.trim().toUpperCase();
        }
    }

    function renderTopicDropdown(picker, topics, filter) {
        var dropdown = picker.querySelector('.tag-picker-dropdown');
        var q = (filter || '').toLowerCase();
        var filtered = topics.filter(function(t) {
            return !q || t.toLowerCase().indexOf(q) >= 0;
        });

        var html = '';
        // Show "Create new" option if typed text doesn't match an existing topic
        var upperQ = (filter || '').trim().toUpperCase();
        if (upperQ && topics.indexOf(upperQ) < 0) {
            html += '<div class="tag-picker-item tag-picker-create" onmousedown="WarAlert.selectTopicPickerItem(this)" data-tag="' + escapeAttr(upperQ) + '">';
            html += '<span class="tag-picker-name">+ Create &quot;' + escapeHTML(upperQ) + '&quot;</span>';
            html += '</div>';
        }

        for (var i = 0; i < filtered.length; i++) {
            html += '<div class="tag-picker-item" onmousedown="WarAlert.selectTopicPickerItem(this)" data-tag="' + escapeAttr(filtered[i]) + '">';
            html += '<span class="tag-picker-name">' + escapeHTML(filtered[i]) + '</span>';
            html += '</div>';
        }

        if (!html) {
            html = '<div class="tag-picker-empty">Type a topic name</div>';
        }
        dropdown.innerHTML = html;
    }

    function selectTopicPickerItem(itemEl) {
        var picker = itemEl.closest('.tag-picker');
        var input = picker.querySelector('.tag-picker-input');
        var topicName = itemEl.dataset.tag;

        input.value = topicName;
        picker.querySelector('.tag-picker-dropdown').style.display = 'none';

        var idx = parseInt(input.dataset.blockIdx);
        if (!isNaN(idx)) {
            _chainBlocks[idx].topic = topicName;
        }
    }

    function renderEmailFields(block, idx) {
        var html = '<div class="form-group"><label>To (comma-separated)</label>';
        html += '<input type="text" value="' + escapeAttr((block.to || []).join(', ')) + '" onchange="WarAlert.updateBlockField(' + idx + ', \'to\', this.value.split(\',\').map(function(s){return s.trim()}).filter(Boolean))"></div>';
        html += '<div class="form-group"><label>Subject</label>';
        html += '<input type="text" value="' + escapeAttr(block.subject || '') + '" onchange="WarAlert.updateBlockField(' + idx + ', \'subject\', this.value)"></div>';
        html += '<div class="form-group"><label>Body</label>';
        html += '<textarea onchange="WarAlert.updateBlockField(' + idx + ', \'body\', this.value)">' + escapeHTML(block.body || '') + '</textarea></div>';
        return html;
    }

    function renderWebhookFields(block, idx) {
        var html = '<div class="form-row">';
        html += '<div class="form-group"><label>Method</label>';
        html += '<select onchange="WarAlert.updateBlockField(' + idx + ', \'method\', this.value)">';
        var methods = ['POST', 'GET', 'PUT', 'PATCH', 'DELETE'];
        for (var i = 0; i < methods.length; i++) {
            html += '<option value="' + methods[i] + '"' + (block.method === methods[i] ? ' selected' : '') + '>' + methods[i] + '</option>';
        }
        html += '</select></div>';
        html += '<div class="form-group"><label>Content-Type</label>';
        html += '<input type="text" value="' + escapeAttr(block.content_type || 'application/json') + '" onchange="WarAlert.updateBlockField(' + idx + ', \'content_type\', this.value)"></div>';
        html += '</div>';

        html += '<div class="form-group"><label>URL</label>';
        html += '<input type="url" value="' + escapeAttr(block.url || '') + '" onchange="WarAlert.updateBlockField(' + idx + ', \'url\', this.value)"></div>';

        html += '<div class="form-group"><label>Body</label>';
        html += '<textarea onchange="WarAlert.updateBlockField(' + idx + ', \'body\', this.value)">' + escapeHTML(block.body || '') + '</textarea></div>';

        // Auth
        var auth = block.auth || {};
        html += '<div class="form-group"><label>Auth Type</label>';
        html += '<select onchange="WarAlert.updateBlockAuth(' + idx + ', \'type\', this.value); WarAlert.renderBlocks()" style="width:auto">';
        html += '<option value="">None</option>';
        html += '<option value="bearer"' + (auth.type === 'bearer' ? ' selected' : '') + '>Bearer Token</option>';
        html += '<option value="basic"' + (auth.type === 'basic' ? ' selected' : '') + '>Basic Auth</option>';
        html += '<option value="custom_header"' + (auth.type === 'custom_header' ? ' selected' : '') + '>Custom Header</option>';
        html += '</select></div>';

        if (auth.type === 'bearer') {
            html += '<div class="form-group"><label>Token</label>';
            html += '<input type="text" value="' + escapeAttr(auth.token || '') + '" onchange="WarAlert.updateBlockAuth(' + idx + ', \'token\', this.value)"></div>';
        } else if (auth.type === 'basic') {
            html += '<div class="form-row">';
            html += '<div class="form-group"><label>Username</label>';
            html += '<input type="text" value="' + escapeAttr(auth.username || '') + '" onchange="WarAlert.updateBlockAuth(' + idx + ', \'username\', this.value)"></div>';
            html += '<div class="form-group"><label>Password</label>';
            html += '<input type="password" value="' + escapeAttr(auth.password || '') + '" onchange="WarAlert.updateBlockAuth(' + idx + ', \'password\', this.value)"></div>';
            html += '</div>';
        } else if (auth.type === 'custom_header') {
            html += '<div class="form-row">';
            html += '<div class="form-group"><label>Header Name</label>';
            html += '<input type="text" value="' + escapeAttr(auth.header_name || '') + '" onchange="WarAlert.updateBlockAuth(' + idx + ', \'header_name\', this.value)"></div>';
            html += '<div class="form-group"><label>Header Value</label>';
            html += '<input type="text" value="' + escapeAttr(auth.header_value || '') + '" onchange="WarAlert.updateBlockAuth(' + idx + ', \'header_value\', this.value)"></div>';
            html += '</div>';
        }

        return html;
    }

    // --- Block manipulation ---

    function addBlock(type) {
        var block = { type: type, name: '' };
        if (type === 'gate') {
            block.logic_mode = 'and';
            block.conditions = [];
        } else {
            block.action_type = 'sms';
        }
        _chainBlocks.push(block);
        renderBlocks();
    }

    function removeBlock(idx) {
        _chainBlocks.splice(idx, 1);
        renderBlocks();
    }

    function moveBlock(idx, direction) {
        var newIdx = idx + direction;
        if (newIdx < 0 || newIdx >= _chainBlocks.length) return;
        var tmp = _chainBlocks[idx];
        _chainBlocks[idx] = _chainBlocks[newIdx];
        _chainBlocks[newIdx] = tmp;
        renderBlocks();
    }

    function updateBlockName(idx, value) {
        _chainBlocks[idx].name = value;
    }

    function updateBlockField(idx, field, value) {
        _chainBlocks[idx][field] = value;
    }

    function updateBlockAuth(idx, field, value) {
        if (!_chainBlocks[idx].auth) _chainBlocks[idx].auth = {};
        _chainBlocks[idx].auth[field] = value;
    }

    // --- Condition manipulation ---

    function addCondition(blockIdx) {
        if (!_chainBlocks[blockIdx].conditions) _chainBlocks[blockIdx].conditions = [];
        _chainBlocks[blockIdx].conditions.push({ type: 'plc_tag', operator: '==' });
        renderBlocks();
    }

    function removeCondition(blockIdx, condIdx) {
        _chainBlocks[blockIdx].conditions.splice(condIdx, 1);
        renderBlocks();
    }

    function updateConditionType(blockIdx, condIdx, newType) {
        var cond = _chainBlocks[blockIdx].conditions[condIdx];
        cond.type = newType;
        // Reset type-specific fields
        if (newType === 'plc_tag') {
            cond.plc = ''; cond.tag = ''; cond.operator = '=='; cond.value = '';
            delete cond.time_from; delete cond.time_to; delete cond.days;
        } else if (newType === 'ping') {
            cond.plc = ''; cond.operator = '=='; cond.value = 1;
            delete cond.tag; delete cond.time_from; delete cond.time_to; delete cond.days;
        } else if (newType === 'time_between') {
            cond.time_from = ''; cond.time_to = '';
            delete cond.plc; delete cond.tag; delete cond.operator; delete cond.value; delete cond.days;
        } else if (newType === 'weekday') {
            cond.days = [];
            delete cond.plc; delete cond.tag; delete cond.operator; delete cond.value;
            delete cond.time_from; delete cond.time_to;
        }
        renderBlocks();
    }

    function updateConditionField(blockIdx, condIdx, field, value) {
        _chainBlocks[blockIdx].conditions[condIdx][field] = value;
    }

    function updateConditionDays(blockIdx, condIdx) {
        // Read checked days from the DOM
        var container = document.getElementById('chain-blocks-container');
        var blocks = container.querySelectorAll('.chain-block');
        if (!blocks[blockIdx]) return;
        var rows = blocks[blockIdx].querySelectorAll('.condition-row');
        if (!rows[condIdx]) return;
        var checkboxes = rows[condIdx].querySelectorAll('.weekday-checkboxes input[type="checkbox"]');
        var days = [];
        for (var i = 0; i < checkboxes.length; i++) {
            if (checkboxes[i].checked) days.push(checkboxes[i].value);
        }
        _chainBlocks[blockIdx].conditions[condIdx].days = days;
    }

    // Auto-init theme
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initTheme);
    } else {
        initTheme();
    }

    // --- Public API ---

    return {
        createSSE: createSSE,
        showModal: showModal,
        hideModal: hideModal,
        api: api,
        toast: toast,
        toggleTheme: toggleTheme,

        // Chains
        createChain: createChain,
        editChain: editChain,
        saveChain: saveChain,
        testFireChain: testFireChain,
        renderBlocks: renderBlocks,

        // Block editor
        addBlock: addBlock,
        removeBlock: removeBlock,
        moveBlock: moveBlock,
        updateBlockName: updateBlockName,
        updateBlockField: updateBlockField,
        updateBlockAuth: updateBlockAuth,
        addCondition: addCondition,
        removeCondition: removeCondition,
        updateConditionType: updateConditionType,
        updateConditionField: updateConditionField,
        updateConditionPLC: updateConditionPLC,
        updateConditionDays: updateConditionDays,

        // Tag picker
        openTagPicker: openTagPicker,
        filterTagPicker: filterTagPicker,
        selectTagPickerItem: selectTagPickerItem,
        closeTagPicker: closeTagPicker,
        _setAddTagSelection: _setAddTagSelection,

        // Topic picker
        openTopicPicker: openTopicPicker,
        filterTopicPicker: filterTopicPicker,
        selectTopicPickerItem: selectTopicPickerItem,

        // Sources
        createSource: createSource,
        editSource: editSource,
        saveSource: saveSource,
        toggleSourceType: toggleSourceType,

        // PLC Tags
        manageTags: manageTags,
        addPLCTag: addPLCTag,
        removePLCTag: removePLCTag,

        // Subscribers
        createSubscriber: createSubscriber,
        editSubscriber: editSubscriber,
        saveSubscriber: saveSubscriber,
        addEditSubTopic: addEditSubTopic,
        removeEditSubTopic: removeEditSubTopic,

        // Topics
        createTopic: createTopic,
        createTopicInline: createTopicInline,
        deleteTopic: deleteTopic,

        // Users
        createUser: createUser,
        editUser: editUser,
        saveUser: saveUser,

        // Providers
        saveSMSProvider: saveSMSProvider,
        saveEmailProvider: saveEmailProvider,
        toggleSMSType: toggleSMSType,
        toggleSMSMode: toggleSMSMode,
        testSMSConnection: testSMSConnection,
        testSMS: testSMS,
        testEmail: testEmail,
        copyWebhookURL: copyWebhookURL,
        registerSMSWebhook: registerSMSWebhook,
        checkWebhookStatus: checkWebhookStatus
    };
})();
