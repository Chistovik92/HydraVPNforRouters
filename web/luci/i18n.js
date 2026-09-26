/*
 * HydraVPN for Router - Internationalization (i18n) System
 * Поддержка русского (основной) и английского языков
 * Support for Russian (primary) and English languages
 */

window.HydraVPN = window.HydraVPN || {};

HydraVPN.i18n = {
    // Current language
    currentLang: 'ru',
    
    // Available languages
    languages: {
        'ru': 'Русский',
        'en': 'English'
    },
    
    // Translations
    translations: {
        // Common / Общее
        'app.name': {
            'ru': 'HydraVPN for Router',
            'en': 'HydraVPN for Router'
        },
        'app.version': {
            'ru': 'Версия',
            'en': 'Version'
        },
        'app.initialized': {
            'ru': 'HydraVPN for Router UI v{version} инициализирован',
            'en': 'HydraVPN for Router UI v{version} initialized'
        },
        'loading': {
            'ru': 'Загрузка...',
            'en': 'Loading...'
        },
        'error': {
            'ru': 'Ошибка',
            'en': 'Error'
        },
        'success': {
            'ru': 'Успешно',
            'en': 'Success'
        },
        'warning': {
            'ru': 'Предупреждение',
            'en': 'Warning'
        },
        'info': {
            'ru': 'Информация',
            'en': 'Info'
        },
        'ok': {
            'ru': 'OK',
            'en': 'OK'
        },
        'cancel': {
            'ru': 'Отмена',
            'en': 'Cancel'
        },
        'save': {
            'ru': 'Сохранить',
            'en': 'Save'
        },
        'apply': {
            'ru': 'Применить',
            'en': 'Apply'
        },
        'reload': {
            'ru': 'Перезагрузить',
            'en': 'Reload'
        },
        'start': {
            'ru': 'Запустить',
            'en': 'Start'
        },
        'stop': {
            'ru': 'Остановить',
            'en': 'Stop'
        },
        'restart': {
            'ru': 'Перезапустить',
            'en': 'Restart'
        },
        'enable': {
            'ru': 'Включить',
            'en': 'Enable'
        },
        'disable': {
            'ru': 'Отключить',
            'en': 'Disable'
        },
        'delete': {
            'ru': 'Удалить',
            'en': 'Delete'
        },
        'edit': {
            'ru': 'Редактировать',
            'en': 'Edit'
        },
        'add': {
            'ru': 'Добавить',
            'en': 'Add'
        },
        'update': {
            'ru': 'Обновить',
            'en': 'Update'
        },
        'refresh': {
            'ru': 'Обновить',
            'en': 'Refresh'
        },
        'status': {
            'ru': 'Статус',
            'en': 'Status'
        },
        'settings': {
            'ru': 'Настройки',
            'en': 'Settings'
        },
        'configuration': {
            'ru': 'Конфигурация',
            'en': 'Configuration'
        },
        'dashboard': {
            'ru': 'Панель управления',
            'en': 'Dashboard'
        },
        'subscriptions': {
            'ru': 'Подписки',
            'en': 'Subscriptions'
        },
        'servers': {
            'ru': 'Серверы',
            'en': 'Servers'
        },
        'diagnostics': {
            'ru': 'Диагностика',
            'en': 'Diagnostics'
        },
        'logs': {
            'ru': 'Логи',
            'en': 'Logs'
        },
        'providers': {
            'ru': 'Провайдеры',
            'en': 'Providers'
        },
        'running': {
            'ru': 'Работает',
            'en': 'Running'
        },
        'stopped': {
            'ru': 'Остановлен',
            'en': 'Stopped'
        },
        'unknown': {
            'ru': 'Неизвестно',
            'en': 'Unknown'
        },
        'never': {
            'ru': 'Никогда',
            'en': 'Never'
        },
        'yes': {
            'ru': 'Да',
            'en': 'Yes'
        },
        'no': {
            'ru': 'Нет',
            'en': 'No'
        },
        'enabled': {
            'ru': 'Включено',
            'en': 'Enabled'
        },
        'disabled': {
            'ru': 'Отключено',
            'en': 'Disabled'
        },
        'actions': {
            'ru': 'Действия',
            'en': 'Actions'
        },
        'name': {
            'ru': 'Имя',
            'en': 'Name'
        },
        'url': {
            'ru': 'URL',
            'en': 'URL'
        },
        'type': {
            'ru': 'Тип',
            'en': 'Type'
        },
        'port': {
            'ru': 'Порт',
            'en': 'Port'
        },
        'address': {
            'ru': 'Адрес',
            'en': 'Address'
        },
        'protocol': {
            'ru': 'Протокол',
            'en': 'Protocol'
        },
        'last_update': {
            'ru': 'Последнее обновление',
            'en': 'Last Update'
        },
        'next_update': {
            'ru': 'Следующее обновление',
            'en': 'Next Update'
        },
        'outbounds': {
            'ru': 'Исходящие',
            'en': 'Outbounds'
        },
        'uptime': {
            'ru': 'Время работы',
            'en': 'Uptime'
        },
        'pid': {
            'ru': 'PID',
            'en': 'PID'
        },

        // Tabs / Вкладки
        'tab.dashboard': {
            'ru': 'Панель',
            'en': 'Dashboard'
        },
        'tab.settings': {
            'ru': 'Настройки',
            'en': 'Settings'
        },
        'tab.subscriptions': {
            'ru': 'Подписки',
            'en': 'Subscriptions'
        },
        'tab.servers': {
            'ru': 'Серверы',
            'en': 'Servers'
        },
        'tab.diagnostics': {
            'ru': 'Диагностика',
            'en': 'Diagnostics'
        },

        // Settings / Настройки
        'settings.dns': {
            'ru': 'DNS настройки',
            'en': 'DNS Settings'
        },
        'settings.dns_type': {
            'ru': 'Тип DNS',
            'en': 'DNS Type'
        },
        'settings.dns_servers': {
            'ru': 'DNS серверы',
            'en': 'DNS Servers'
        },
        'settings.bootstrap_dns': {
            'ru': 'Bootstrap DNS',
            'en': 'Bootstrap DNS'
        },
        'settings.dns_strategy': {
            'ru': 'Стратегия DNS',
            'en': 'DNS Strategy'
        },
        'settings.dns_detour': {
            'ru': 'DNS обход',
            'en': 'DNS Detour'
        },
        'settings.network': {
            'ru': 'Сеть',
            'en': 'Network'
        },
        'settings.source_interfaces': {
            'ru': 'Исходные интерфейсы',
            'en': 'Source Interfaces'
        },
        'settings.general': {
            'ru': 'Общие',
            'en': 'General'
        },
        'settings.enable_yacd': {
            'ru': 'Включить YACD',
            'en': 'Enable YACD'
        },
        'settings.disable_quic': {
            'ru': 'Отключить QUIC',
            'en': 'Disable QUIC'
        },
        'settings.latency_test_url': {
            'ru': 'URL теста задержки',
            'en': 'Latency Test URL'
        },
        'settings.log_level': {
            'ru': 'Уровень логирования',
            'en': 'Log Level'
        },
        'settings.save_success': {
            'ru': 'Настройки сохранены',
            'en': 'Settings saved'
        },
        'settings.save_error': {
            'ru': 'Ошибка сохранения: {error}',
            'en': 'Failed to save: {error}'
        },

        // Subscriptions / Подписки
        'subscriptions.section': {
            'ru': 'Секция',
            'en': 'Section'
        },
        'subscriptions.url': {
            'ru': 'URL',
            'en': 'URL'
        },
        'subscriptions.status': {
            'ru': 'Статус',
            'en': 'Status'
        },
        'subscriptions.last_update': {
            'ru': 'Последнее обновление',
            'en': 'Last Update'
        },
        'subscriptions.outbounds': {
            'ru': 'Исходящие',
            'en': 'Outbounds'
        },
        'subscriptions.update': {
            'ru': 'Обновить',
            'en': 'Update'
        },
        'subscriptions.add': {
            'ru': 'Добавить подписку',
            'en': 'Add Subscription'
        },
        'subscriptions.section_label': {
            'ru': 'Название секции',
            'en': 'Section Name'
        },
        'subscriptions.url_label': {
            'ru': 'URL подписки',
            'en': 'Subscription URL'
        },
        'subscriptions.auto_update': {
            'ru': 'Автообновление',
            'en': 'Auto Update'
        },
        'subscriptions.update_interval': {
            'ru': 'Интервал обновления',
            'en': 'Update Interval'
        },
        'subscriptions.updating': {
            'ru': 'Обновление подписки...',
            'en': 'Updating subscription...'
        },
        'subscriptions.updated': {
            'ru': 'Подписка обновлена',
            'en': 'Subscription updated'
        },
        'subscriptions.update_failed': {
            'ru': 'Ошибка обновления: {error}',
            'en': 'Update failed: {error}'
        },

        // Servers / Серверы
        'servers.add': {
            'ru': 'Добавить сервер',
            'en': 'Add Server'
        },
        'servers.name': {
            'ru': 'Имя',
            'en': 'Name'
        },
        'servers.label': {
            'ru': 'Метка',
            'en': 'Label'
        },
        'servers.protocol': {
            'ru': 'Протокол',
            'en': 'Protocol'
        },
        'servers.listen': {
            'ru': 'Слушать',
            'en': 'Listen'
        },
        'servers.port': {
            'ru': 'Порт',
            'en': 'Port'
        },
        'servers.security': {
            'ru': 'Безопасность',
            'en': 'Security'
        },
        'servers.routing_mode': {
            'ru': 'Режим маршрутизации',
            'en': 'Routing Mode'
        },

        // Diagnostics / Диагностика
        'diagnostics.run': {
            'ru': 'Запустить проверку',
            'en': 'Run Check'
        },
        'diagnostics.running': {
            'ru': 'Выполняется диагностика...',
            'en': 'Running diagnostics...'
        },
        'diagnostics.complete': {
            'ru': 'Диагностика завершена',
            'en': 'Diagnostics complete'
        },
        'diagnostics.failed': {
            'ru': 'Ошибка диагностики: {error}',
            'en': 'Diagnostics failed: {error}'
        },
        'diagnostics.check': {
            'ru': 'Проверка',
            'en': 'Check'
        },
        'diagnostics.status': {
            'ru': 'Статус',
            'en': 'Status'
        },
        'diagnostics.message': {
            'ru': 'Сообщение',
            'en': 'Message'
        },
        'diagnostics.duration': {
            'ru': 'Длительность',
            'en': 'Duration'
        },
        'diagnostics.select_checks': {
            'ru': 'Выберите проверки для запуска',
            'en': 'Select checks to run'
        },

        // Providers / Провайдеры
        'providers.singbox': {
            'ru': 'sing-box',
            'en': 'sing-box'
        },
        'providers.zapret': {
            'ru': 'zapret/zapret2',
            'en': 'zapret/zapret2'
        },
        'providers.byedpi': {
            'ru': 'ByeDPI',
            'en': 'ByeDPI'
        },
        'providers.status': {
            'ru': 'Статус провайдеров',
            'en': 'Providers Status'
        },

        // Notifications / Уведомления
        'notification.settings_saved': {
            'ru': 'Настройки сохранены',
            'en': 'Settings saved'
        },
        'notification.subscription_updated': {
            'ru': 'Подписка обновлена',
            'en': 'Subscription updated'
        },
        'notification.diagnostics_complete': {
            'ru': 'Диагностика завершена',
            'en': 'Diagnostics complete'
        },
        'notification.service_started': {
            'ru': 'Сервис запущен',
            'en': 'Service started'
        },
        'notification.service_stopped': {
            'ru': 'Сервис остановлен',
            'en': 'Service stopped'
        },
        'notification.config_reloaded': {
            'ru': 'Конфигурация перезагружена',
            'en': 'Configuration reloaded'
        },

        // Modal / Модальные окна
        'modal.close': {
            'ru': 'Закрыть',
            'en': 'Close'
        },
        'modal.confirm': {
            'ru': 'Подтвердить',
            'en': 'Confirm'
        },

        // Status messages / Статусы
        'status.starting': {
            'ru': 'Запуск...',
            'en': 'Starting...'
        },
        'status.stopping': {
            'ru': 'Остановка...',
            'en': 'Stopping...'
        },
        'status.running': {
            'ru': 'Работает',
            'en': 'Running'
        },
        'status.stopped': {
            'ru': 'Остановлен',
            'en': 'Stopped'
        },
        'status.error': {
            'ru': 'Ошибка',
            'en': 'Error'
        },

        // Time / Время
        'time.seconds': {
            'ru': 'с',
            'en': 's'
        },
        'time.minutes': {
            'ru': 'м',
            'en': 'm'
        },
        'time.hours': {
            'ru': 'ч',
            'en': 'h'
        },
        'time.days': {
            'ru': 'д',
            'en': 'd'
        },

        // Check types / Типы проверок
        'check.proxy': {
            'ru': 'Прокси соединение',
            'en': 'Proxy Connection'
        },
        'check.nft': {
            'ru': 'NFTables',
            'en': 'NFTables'
        },
        'check.singbox': {
            'ru': 'sing-box',
            'en': 'sing-box'
        },
        'check.inbounds': {
            'ru': 'Входящие соединения',
            'en': 'Inbound Connections'
        },
        'check.dns': {
            'ru': 'DNS',
            'en': 'DNS'
        },
        'check.fakeip': {
            'ru': 'FakeIP',
            'en': 'FakeIP'
        },
        'check.zapret': {
            'ru': 'zapret',
            'en': 'zapret'
        },
        'check.byedpi': {
            'ru': 'ByeDPI',
            'en': 'ByeDPI'
        },
        'check.all': {
            'ru': 'Все проверки',
            'en': 'All Checks'
        },

        // Language selector / Выбор языка
        'language': {
            'ru': 'Язык',
            'en': 'Language'
        },
        'language.ru': {
            'ru': 'Русский',
            'en': 'Russian'
        },
        'language.en': {
            'ru': 'Английский',
            'en': 'English'
        }
    },

    // Initialize i18n
    init: function() {
        // Detect language from localStorage or browser
        var savedLang = localStorage.getItem('hydravpn-lang');
        if (savedLang && this.translations[savedLang]) {
            this.currentLang = savedLang;
        } else {
            // Detect from browser
            var browserLang = navigator.language || navigator.userLanguage;
            if (browserLang.startsWith('ru')) {
                this.currentLang = 'ru';
            } else {
                this.currentLang = 'en';
            }
        }
        
        // Apply translations
        this.applyTranslations();
        
        // Create language selector
        this.createLanguageSelector();
    },

    // Get translation for key
    t: function(key, params) {
        var trans = this.translations[key];
        if (!trans) return key;
        
        var text = trans[this.currentLang] || trans['en'] || key;
        
        // Replace parameters
        if (params) {
            for (var param in params) {
                text = text.replace('{' + param + '}', params[param]);
            }
        }
        
        return text;
    },

    // Set language
    setLanguage: function(lang) {
        if (this.languages[lang]) {
            this.currentLang = lang;
            localStorage.setItem('hydravpn-lang', lang);
            this.applyTranslations();
        }
    },

    // Apply translations to DOM
    applyTranslations: function() {
        // Update elements with data-i18n attribute
        document.querySelectorAll('[data-i18n]').forEach(function(el) {
            var key = el.getAttribute('data-i18n');
            var text = HydraVPN.i18n.t(key);
            if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
                el.placeholder = text;
            } else {
                el.textContent = text;
            }
        });
        
        // Update elements with data-i18n-html attribute
        document.querySelectorAll('[data-i18n-html]').forEach(function(el) {
            var key = el.getAttribute('data-i18n-html');
            el.innerHTML = HydraVPN.i18n.t(key);
        });
        
        // Update document title
        document.title = this.t('app.name') + ' - ' + this.t('app.version');
    },

    // Create language selector dropdown
    createLanguageSelector: function() {
        var selector = document.getElementById('language-selector');
        if (!selector) return;
        
        selector.innerHTML = '';
        for (var code in this.languages) {
            var option = document.createElement('option');
            option.value = code;
            option.textContent = this.languages[code];
            if (code === this.currentLang) {
                option.selected = true;
            }
            selector.appendChild(option);
        }
        
        selector.addEventListener('change', function() {
            HydraVPN.i18n.setLanguage(this.value);
        });
    },

    // Format duration with localized units
    formatDuration: function(seconds) {
        if (typeof seconds === 'string') return seconds;
        
        var secs = Math.floor(seconds);
        var days = Math.floor(secs / 86400);
        secs %= 86400;
        var hours = Math.floor(secs / 3600);
        secs %= 3600;
        var mins = Math.floor(secs / 60);
        secs %= 60;
        
        var parts = [];
        if (days) parts.push(days + this.t('time.days'));
        if (hours) parts.push(hours + this.t('time.hours'));
        if (mins) parts.push(mins + this.t('time.minutes'));
        if (secs || parts.length === 0) parts.push(secs + this.t('time.seconds'));
        
        return parts.join(' ');
    },

    // Format date with locale
    formatDate: function(dateString) {
        var date = new Date(dateString);
        if (this.currentLang === 'ru') {
            return date.toLocaleString('ru-RU');
        }
        return date.toLocaleString('en-US');
    }
};

// Auto-initialize when DOM is ready
document.addEventListener('DOMContentLoaded', function() {
    HydraVPN.i18n.init();
});

// Export for module systems
if (typeof module !== 'undefined' && module.exports) {
    module.exports = HydraVPN.i18n;
}