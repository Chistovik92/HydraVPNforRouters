/* 
 * HydraVPN for Router LuCI Web UI
 * Main entry point for the LuCI interface
 * 
 * HydraVPN for Router - веб-интерфейс LuCI
 * Главная точка входа для интерфейса LuCI
 */

// HydraVPN namespace / Пространство имен HydraVPN
window.HydraVPN = window.HydraVPN || {};

HydraVPN.version = '1.0.6';
HydraVPN.apiBase = '/cgi-bin/luci/rpc/hydravpn-router';

// Initialize when DOM is ready / Инициализация при готовности DOM
document.addEventListener('DOMContentLoaded', function() {
    HydraVPN.init();
});

HydraVPN.init = function() {
    console.log(HydraVPN.i18n.t('app.initialized', {version: HydraVPN.version}));
    
    // Check if we're on the HydraVPN for Router page / Проверка страницы
    if (!document.body.classList.contains('hydravpn-router')) {
        return;
    }
    
    // Initialize tabs / Инициализация вкладок
    HydraVPN.tabs.init();
    
    // Initialize dashboard / Инициализация панели управления
    HydraVPN.dashboard.init();
    
    // Initialize settings / Инициализация настроек
    HydraVPN.settings.init();
    
    // Initialize subscriptions / Инициализация подписок
    HydraVPN.subscriptions.init();
    
    // Initialize servers / Инициализация серверов
    HydraVPN.servers.init();
    
    // Initialize diagnostics / Инициализация диагностики
    HydraVPN.diagnostics.init();
    
    // Start status polling / Запуск опроса статуса
    HydraVPN.status.startPolling();
};

// API helper / Помощник API
HydraVPN.api = {
    call: function(method, params) {
        return fetch(HydraVPN.apiBase, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-LuCI-RPC': 'hydravpn-router'
            },
            body: JSON.stringify({
                jsonrpc: '2.0',
                id: Date.now(),
                method: method,
                params: params || []
            })
        }).then(function(response) {
            return response.json();
        }).then(function(data) {
            if (data.error) {
                throw new Error(data.error.message || 'RPC error');
            }
            return data.result;
        });
    },
    
    getStatus: function() {
        return this.call('get_status');
    },
    
    getConfig: function() {
        return this.call('get_config');
    },
    
    setConfig: function(config) {
        return this.call('set_config', [config]);
    },
    
    reload: function() {
        return this.call('reload');
    },
    
    start: function() {
        return this.call('start');
    },
    
    stop: function() {
        return this.call('stop');
    },
    
    getProvidersStatus: function() {
        return this.call('get_providers_status');
    },
    
    getSubscriptions: function() {
        return this.call('get_subscriptions');
    },
    
    updateSubscription: function(section, url) {
        return this.call('update_subscription', [section, url]);
    },
    
    getServers: function() {
        return this.call('get_servers');
    },
    
    addServer: function(server) {
        return this.call('add_server', [server]);
    },
    
    updateServer: function(name, server) {
        return this.call('update_server', [name, server]);
    },
    
    deleteServer: function(name) {
        return this.call('delete_server', [name]);
    },
    
    runDiagnostics: function(checks) {
        return this.call('run_diagnostics', [checks]);
    },
    
    generateRealityKeypair: function() {
        return this.call('generate_reality_keypair');
    }
};

// Tab management / Управление вкладками
HydraVPN.tabs = {
    init: function() {
        var tabButtons = document.querySelectorAll('.hydravpn-tabs .tab-button');
        var tabPanels = document.querySelectorAll('.hydravpn-tab-panel');
        
        tabButtons.forEach(function(button) {
            button.addEventListener('click', function() {
                var tabId = this.dataset.tab;
                
                // Update buttons / Обновление кнопок
                tabButtons.forEach(function(btn) {
                    btn.classList.remove('active');
                });
                this.classList.add('active');
                
                // Update panels / Обновление панелей
                tabPanels.forEach(function(panel) {
                    panel.classList.remove('active');
                });
                document.getElementById('tab-' + tabId).classList.add('active');
                
                // Trigger tab-specific initialization / Запуск инициализации вкладки
                if (HydraVPN[tabId] && HydraVPN[tabId].onTabActivate) {
                    HydraVPN[tabId].onTabActivate();
                }
            });
        });
    }
};

// Dashboard / Панель управления
HydraVPN.dashboard = {
    init: function() {
        this.refresh();
        this.setupAutoRefresh();
    },
    
    onTabActivate: function() {
        this.refresh();
    },
    
    refresh: function() {
        HydraVPN.api.getStatus().then(function(status) {
            HydraVPN.dashboard.render(status);
        }).catch(function(err) {
            console.error('Failed to fetch status:', err);
            HydraVPN.dashboard.showError(HydraVPN.i18n.t('error') + ': ' + err.message);
        });
    },
    
    setupAutoRefresh: function() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
        }
        this.refreshInterval = setInterval(function() {
            HydraVPN.dashboard.refresh();
        }, 30000); // 30 seconds / 30 секунд
    },
    
    render: function(status) {
        // Update service status / Обновление статуса сервиса
        var statusEl = document.getElementById('service-status');
        if (statusEl) {
            var state = status.state || 'unknown';
            statusEl.textContent = HydraVPN.i18n.t('status.' + state);
            statusEl.className = 'status-badge ' + state;
        }
        
        // Update uptime / Обновление времени работы
        var uptimeEl = document.getElementById('uptime');
        if (uptimeEl && status.uptime) {
            uptimeEl.textContent = HydraVPN.i18n.formatDuration(status.uptime);
        }
        
        // Update providers / Обновление провайдеров
        var providersEl = document.getElementById('providers-status');
        if (providersEl && status.providers) {
            providersEl.innerHTML = HydraVPN.dashboard.renderProviders(status.providers);
        }
        
        // Update stats / Обновление статистики
        HydraVPN.dashboard.renderStats(status);
    },
    
    renderProviders: function(providers) {
        var html = '<div class="providers-grid">';
        for (var name in providers) {
            var provider = providers[name];
            var running = provider.running ? 'running' : 'stopped';
            var statusText = HydraVPN.i18n.t(running);
            html += '<div class="provider-card ' + running + '">';
            html += '<h4>' + name.toUpperCase() + '</h4>';
            html += '<span class="provider-status ' + running + '">' + statusText + '</span>';
            if (provider.pid) {
                html += '<div class="provider-detail">' + HydraVPN.i18n.t('pid') + ': ' + provider.pid + '</div>';
            }
            if (provider.uptime) {
                html += '<div class="provider-detail">' + HydraVPN.i18n.t('uptime') + ': ' + HydraVPN.i18n.formatDuration(provider.uptime) + '</div>';
            }
            html += '</div>';
        }
        html += '</div>';
        return html;
    },
    
    renderStats: function(status) {
        // Traffic stats, connection counts, etc. / Статистика трафика, подключений и т.д.
        var statsEl = document.getElementById('dashboard-stats');
        if (statsEl && status.stats) {
            // Render stats / Рендеринг статистики
        }
    },
    
    showError: function(message) {
        var container = document.getElementById('dashboard-container');
        if (container) {
            container.innerHTML = '<div class="error-message">' + message + '</div>';
        }
    }
};

// Settings / Настройки
HydraVPN.settings = {
    init: function() {
        this.load();
        this.bindEvents();
    },
    
    onTabActivate: function() {
        this.load();
    },
    
    load: function() {
        HydraVPN.api.getConfig().then(function(config) {
            HydraVPN.settings.render(config);
        }).catch(function(err) {
            console.error('Failed to load config:', err);
        });
    },
    
    render: function(config) {
        // Fill form fields with config values / Заполнение полей формы значениями конфига
        HydraVPN.settings.populateForm(config);
    },
    
    populateForm: function(config) {
        // DNS settings / DNS настройки
        HydraVPN.setSelectValue('dns-type', config.settings.dns_type);
        HydraVPN.setTextareaValue('dns-servers', config.settings.dns_server.join('\n'));
        HydraVPN.setTextareaValue('bootstrap-dns', config.settings.bootstrap_dns_server.join('\n'));
        HydraVPN.setSelectValue('dns-strategy', config.settings.dns_strategy);
        HydraVPN.setCheckboxValue('dns-detour', config.settings.dns_detour_enabled);
        
        // Network settings / Сетевые настройки
        HydraVPN.setTextareaValue('source-interfaces', config.settings.source_network_interfaces.join('\n'));
        
        // General settings / Общие настройки
        HydraVPN.setCheckboxValue('enable-yacd', config.settings.enable_yacd);
        HydraVPN.setCheckboxValue('disable-quic', config.settings.disable_quic);
        HydraVPN.setInputValue('latency-test-url', config.settings.latency_test_url);
        HydraVPN.setSelectValue('log-level', config.settings.log_level);
    },
    
    bindEvents: function() {
        var form = document.getElementById('settings-form');
        if (form) {
            form.addEventListener('submit', function(e) {
                e.preventDefault();
                HydraVPN.settings.save();
            });
        }
    },
    
    save: function() {
        var config = HydraVPN.settings.collectForm();
        HydraVPN.api.setConfig(config).then(function() {
            HydraVPN.showNotification(HydraVPN.i18n.t('notification.settings_saved'), 'success');
            HydraVPN.api.reload();
        }).catch(function(err) {
            HydraVPN.showNotification(HydraVPN.i18n.t('settings.save_error', {error: err.message}), 'error');
        });
    },
    
    collectForm: function() {
        return {
            settings: {
                dns_type: HydraVPN.getSelectValue('dns-type'),
                dns_server: HydraVPN.getTextareaValue('dns-servers').split('\n').filter(function(s) { return s.trim(); }),
                bootstrap_dns_server: HydraVPN.getTextareaValue('bootstrap-dns').split('\n').filter(function(s) { return s.trim(); }),
                dns_strategy: HydraVPN.getSelectValue('dns-strategy'),
                dns_detour_enabled: HydraVPN.getCheckboxValue('dns-detour'),
                source_network_interfaces: HydraVPN.getTextareaValue('source-interfaces').split('\n').filter(function(s) { return s.trim(); }),
                enable_yacd: HydraVPN.getCheckboxValue('enable-yacd'),
                disable_quic: HydraVPN.getCheckboxValue('disable-quic'),
                latency_test_url: HydraVPN.getInputValue('latency-test-url'),
                log_level: HydraVPN.getSelectValue('log-level')
            }
        };
    }
};

// Subscriptions / Подписки
HydraVPN.subscriptions = {
    init: function() {
        this.load();
        this.bindEvents();
    },
    
    onTabActivate: function() {
        this.load();
    },
    
    load: function() {
        HydraVPN.api.getSubscriptions().then(function(subs) {
            HydraVPN.subscriptions.render(subs);
        }).catch(function(err) {
            console.error('Failed to load subscriptions:', err);
        });
    },
    
    render: function(subscriptions) {
        var container = document.getElementById('subscriptions-list');
        if (!container) return;
        
        var html = '<table class="hydravpn-table"><thead><tr>';
        html += '<th data-i18n="subscriptions.section"></th>';
        html += '<th data-i18n="subscriptions.url"></th>';
        html += '<th data-i18n="subscriptions.status"></th>';
        html += '<th data-i18n="subscriptions.last_update"></th>';
        html += '<th data-i18n="subscriptions.outbounds"></th>';
        html += '<th data-i18n="actions"></th>';
        html += '</tr></thead><tbody>';
        
        for (var key in subscriptions) {
            var sub = subscriptions[key];
            var statusClass = sub.last_error ? 'error' : 'ok';
            var statusText = sub.last_error ? HydraVPN.i18n.t('error') : HydraVPN.i18n.t('ok');
            
            html += '<tr data-section="' + sub.section + '" data-url="' + sub.url + '">';
            html += '<td>' + sub.section + '</td>';
            html += '<td><code>' + sub.url + '</code></td>';
            html += '<td><span class="status-badge ' + statusClass + '">' + statusText + '</span></td>';
            html += '<td>' + (sub.last_update ? HydraVPN.i18n.formatDate(sub.last_update) : HydraVPN.i18n.t('never')) + '</td>';
            html += '<td>' + sub.outbounds + '</td>';
            html += '<td><button class="btn btn-sm btn-primary update-sub" data-section="' + sub.section + '" data-url="' + sub.url + '" data-i18n="subscriptions.update"></button></td>';
            html += '</tr>';
        }
        
        html += '</tbody></table>';
        container.innerHTML = html;
        
        // Apply translations to new elements / Применение переводов к новым элементам
        HydraVPN.i18n.applyTranslations();
        
        // Bind update buttons / Привязка кнопок обновления
        container.querySelectorAll('.update-sub').forEach(function(btn) {
            btn.addEventListener('click', function() {
                HydraVPN.subscriptions.update(this.dataset.section, this.dataset.url);
            });
        });
    },
    
    update: function(section, url) {
        HydraVPN.showNotification(HydraVPN.i18n.t('subscriptions.updating'), 'info');
        HydraVPN.api.updateSubscription(section, url).then(function() {
            HydraVPN.showNotification(HydraVPN.i18n.t('notification.subscription_updated'), 'success');
            HydraVPN.subscriptions.load();
        }).catch(function(err) {
            HydraVPN.showNotification(HydraVPN.i18n.t('subscriptions.update_failed', {error: err.message}), 'error');
        });
    },
    
    bindEvents: function() {
        var addBtn = document.getElementById('add-subscription');
        if (addBtn) {
            addBtn.addEventListener('click', function() {
                HydraVPN.subscriptions.showAddDialog();
            });
        }
    },
    
    showAddDialog: function() {
        // Show modal dialog for adding subscription / Показ модального окна для добавления подписки
        HydraVPN.modal.show({
            title: HydraVPN.i18n.t('subscriptions.add'),
            content: '<form id="add-sub-form">' +
                '<div class="form-group"><label data-i18n="subscriptions.section_label"></label><input type="text" name="section" required></div>' +
                '<div class="form-group"><label data-i18n="subscriptions.url_label"></label><input type="url" name="url" required></div>' +
                '<div class="form-group"><label data-i18n="subscriptions.auto_update"></label><input type="checkbox" name="auto_update" checked></div>' +
                '<div class="form-group"><label data-i18n="subscriptions.update_interval"></label><input type="text" name="interval" value="1h"></div>' +
            '</form>',
            onSubmit: function(data) {
                // Add subscription via API / Добавление подписки через API
                HydraVPN.showNotification(HydraVPN.i18n.t('notification.subscription_updated'), 'success');
                HydraVPN.subscriptions.load();
            }
        });
    }
};

// Servers / Серверы
HydraVPN.servers = {
    init: function() {
        this.load();
        this.bindEvents();
    },
    
    onTabActivate: function() {
        this.load();
    },
    
    load: function() {
        HydraVPN.api.getServers().then(function(servers) {
            HydraVPN.servers.render(servers);
        }).catch(function(err) {
            console.error('Failed to load servers:', err);
        });
    },
    
    render: function(servers) {
        var container = document.getElementById('servers-list');
        if (!container) return;
        
        // Render server list / Рендеринг списка серверов
    },
    
    bindEvents: function() {
        // Add server button, etc. / Кнопка добавления сервера и т.д.
    }
};

// Diagnostics / Диагностика
HydraVPN.diagnostics = {
    init: function() {
        this.bindEvents();
    },
    
    onTabActivate: function() {
        // Load diagnostics history / Загрузка истории диагностики
    },
    
    bindEvents: function() {
        var runBtn = document.getElementById('run-diagnostics');
        if (runBtn) {
            runBtn.addEventListener('click', function() {
                HydraVPN.diagnostics.run();
            });
        }
    },
    
    run: function() {
        var checks = [];
        document.querySelectorAll('#diagnostics-checks input[type="checkbox"]:checked').forEach(function(cb) {
            checks.push(cb.value);
        });
        
        if (checks.length === 0) {
            checks = ['proxy', 'nft', 'singbox', 'dns', 'fakeip'];
        }
        
        HydraVPN.showNotification(HydraVPN.i18n.t('diagnostics.running'), 'info');
        
        HydraVPN.api.runDiagnostics(checks).then(function(results) {
            HydraVPN.diagnostics.renderResults(results);
            HydraVPN.showNotification(HydraVPN.i18n.t('notification.diagnostics_complete'), 'success');
        }).catch(function(err) {
            HydraVPN.showNotification(HydraVPN.i18n.t('diagnostics.failed', {error: err.message}), 'error');
        });
    },
    
    renderResults: function(results) {
        var container = document.getElementById('diagnostics-results');
        if (!container) return;
        
        var html = '<table class="hydravpn-table"><thead><tr>';
        html += '<th data-i18n="diagnostics.check"></th>';
        html += '<th data-i18n="diagnostics.status"></th>';
        html += '<th data-i18n="diagnostics.message"></th>';
        html += '<th data-i18n="diagnostics.duration"></th>';
        html += '</tr></thead><tbody>';
        
        results.forEach(function(result) {
            html += '<tr class="result-' + result.status + '">';
            html += '<td>' + HydraVPN.i18n.t('check.' + result.name) + '</td>';
            html += '<td><span class="status-badge ' + result.status + '">' + HydraVPN.i18n.t(result.status) + '</span></td>';
            html += '<td>' + result.message + '</td>';
            html += '<td>' + HydraVPN.i18n.formatDuration(result.duration) + '</td>';
            html += '</tr>';
        });
        
        html += '</tbody></table>';
        container.innerHTML = html;
        
        // Apply translations to new elements / Применение переводов к новым элементам
        HydraVPN.i18n.applyTranslations();
    }
};

// Status polling / Опрос статуса
HydraVPN.status = {
    polling: false,
    interval: null,
    
    startPolling: function() {
        if (this.polling) return;
        this.polling = true;
        this.poll();
        this.interval = setInterval(this.poll.bind(this), 10000);
    },
    
    stopPolling: function() {
        this.polling = false;
        if (this.interval) {
            clearInterval(this.interval);
            this.interval = null;
        }
    },
    
    poll: function() {
        HydraVPN.api.getStatus().then(function(status) {
            HydraVPN.status.updateUI(status);
        }).catch(function(err) {
            console.warn('Status poll failed:', err);
        });
    },
    
    updateUI: function(status) {
        // Update status indicators across all tabs / Обновление индикаторов статуса во всех вкладках
        var indicator = document.getElementById('global-status-indicator');
        if (indicator) {
            var state = status.state || 'unknown';
            indicator.className = 'status-indicator ' + state;
            indicator.title = HydraVPN.i18n.t('status.' + state);
        }
    }
};

// Utility functions / Вспомогательные функции
HydraVPN.formatDuration = function(seconds) {
    return HydraVPN.i18n.formatDuration(seconds);
};

HydraVPN.formatDate = function(dateString) {
    return HydraVPN.i18n.formatDate(dateString);
};

HydraVPN.getInputValue = function(id) {
    var el = document.getElementById(id);
    return el ? el.value : '';
};

HydraVPN.setInputValue = function(id, value) {
    var el = document.getElementById(id);
    if (el) el.value = value;
};

HydraVPN.getSelectValue = function(id) {
    var el = document.getElementById(id);
    return el ? el.value : '';
};

HydraVPN.setSelectValue = function(id, value) {
    var el = document.getElementById(id);
    if (el) el.value = value;
};

HydraVPN.getCheckboxValue = function(id) {
    var el = document.getElementById(id);
    return el ? el.checked : false;
};

HydraVPN.setCheckboxValue = function(id, value) {
    var el = document.getElementById(id);
    if (el) el.checked = value;
};

HydraVPN.getTextareaValue = function(id) {
    var el = document.getElementById(id);
    return el ? el.value : '';
};

HydraVPN.setTextareaValue = function(id, value) {
    var el = document.getElementById(id);
    if (el) el.value = value;
};

// Notification system / Система уведомлений
HydraVPN.showNotification = function(message, type) {
    var container = document.getElementById('notifications');
    if (!container) {
        container = document.createElement('div');
        container.id = 'notifications';
        container.className = 'notifications-container';
        document.body.appendChild(container);
    }
    
    var notification = document.createElement('div');
    notification.className = 'notification notification-' + (type || 'info');
    notification.textContent = message;
    
    container.appendChild(notification);
    
    setTimeout(function() {
        notification.classList.add('show');
    }, 10);
    
    setTimeout(function() {
        notification.classList.remove('show');
        setTimeout(function() {
            notification.remove();
        }, 300);
    }, 5000);
};

// Modal dialog / Модальное окно
HydraVPN.modal = {
    show: function(options) {
        var modal = document.createElement('div');
        modal.className = 'hydravpn-modal';
        modal.innerHTML = '<div class="modal-overlay"></div>' +
            '<div class="modal-dialog">' +
            '<div class="modal-header"><h3>' + options.title + '</h3><button class="modal-close">&times;</button></div>' +
            '<div class="modal-body">' + options.content + '</div>' +
            '<div class="modal-footer"><button class="btn btn-secondary modal-cancel" data-i18n="cancel"></button><button class="btn btn-primary modal-submit" data-i18n="ok"></button></div>' +
            '</div>';
        
        document.body.appendChild(modal);
        
        // Apply translations to modal / Применение переводов к модальному окну
        HydraVPN.i18n.applyTranslations();
        
        // Focus management / Управление фокусом
        var firstInput = modal.querySelector('input, select, textarea');
        if (firstInput) firstInput.focus();
        
        // Event handlers / Обработчики событий
        modal.querySelector('.modal-close, .modal-cancel, .modal-overlay').forEach(function(el) {
            el.addEventListener('click', function() {
                HydraVPN.modal.hide(modal);
            });
        });
        
        modal.querySelector('.modal-submit').addEventListener('click', function() {
            var form = modal.querySelector('form');
            var data = {};
            if (form) {
                new FormData(form).forEach(function(value, key) {
                    data[key] = value;
                });
            }
            HydraVPN.modal.hide(modal);
            if (options.onSubmit) options.onSubmit(data);
        });
        
        // Show animation / Анимация показа
        setTimeout(function() {
            modal.classList.add('show');
        }, 10);
    },
    
    hide: function(modal) {
        modal.classList.remove('show');
        setTimeout(function() {
            modal.remove();
        }, 300);
    }
};

// Export for module systems / Экспорт для модульных систем
if (typeof module !== 'undefined' && module.exports) {
    module.exports = HydraVPN;
}