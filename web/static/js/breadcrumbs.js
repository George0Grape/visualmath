(function () {
  var LABELS = {
    'dashboard':     'Дашборд',
    'profile':       'Профиль',
    'modules':       'Модули',
    'create':        'Создание',
    'edit':          'Редактирование',
    'view':          'Просмотр',
    'lectures':      'Лекции',
    'launch':        'Запуск',
    'classroom':     'Аудитория',
    'subjects':      'Предметы',
    'assignments':   'Задания',
    'submissions':   'Решения',
    'annotate':      'Аннотация',
    'join':          'Вступить',
    'student':       'Студент',
    'teacher':       'Преподаватель',
    'session':       'Сессия',
    'results':       'Результаты',
    'login':         'Вход',
    'register':      'Регистрация',
  };

  // эти сегменты не имеют собственной страницы — не делаем ссылку
  var NO_LINK = { 'classroom': true };

  var CSS = `
    .vm-breadcrumbs {
      display: flex;
      align-items: center;
      gap: 4px;
      font-size: 12px;
      color: var(--muted, #6b7280);
      margin-top: 6px;
      margin-bottom: 0;
      flex-wrap: wrap;
    }
    .vm-breadcrumbs a {
      color: var(--muted, #6b7280);
      text-decoration: none;
      transition: color .15s;
    }
    .vm-breadcrumbs a:hover { color: var(--accent, #2563eb); }
    .vm-breadcrumbs .bc-sep { opacity: .4; font-size: 10px; }
    .vm-breadcrumbs .bc-current { color: var(--text, #111); font-weight: 500; }
  `;

  function label(seg) { return LABELS[seg.toLowerCase()] || seg; }
  function isID(s) { return /^[0-9a-f\-]{4,}$/i.test(s) || /^\d+$/.test(s); }

  function build() {
    var parts = location.pathname.replace(/^\/|\/$/g, '').split('/').filter(Boolean);

    // считаем только не-ID сегменты
    var realParts = parts.filter(function(s) { return !isID(s); });

    // показываем только если 4+ смысловых сегмента
    if (realParts.length < 4) return;

    var crumbs = [{ text: 'Главная', href: '/dashboard' }];
    var acc = '';
    for (var i = 0; i < parts.length; i++) {
      var seg = parts[i];
      if (isID(seg)) { acc += '/' + seg; continue; }
      acc += '/' + seg;
      var isLast = i === parts.length - 1;
      var noLink = NO_LINK[seg.toLowerCase()];
      crumbs.push({
        text: label(seg),
        href: (isLast || noLink) ? null : acc
      });
    }

    if (crumbs.length < 2) return;

    var styleEl = document.createElement('style');
    styleEl.textContent = CSS;
    document.head.appendChild(styleEl);

    var nav = document.createElement('nav');
    nav.className = 'vm-breadcrumbs';
    nav.setAttribute('aria-label', 'Навигация');

    crumbs.forEach(function (c, idx) {
      if (idx > 0) {
        var sep = document.createElement('span');
        sep.className = 'bc-sep';
        sep.textContent = '›';
        nav.appendChild(sep);
      }
      if (!c.href) {
        var span = document.createElement('span');
        span.className = idx === crumbs.length - 1 ? 'bc-current' : '';
        span.textContent = c.text;
        nav.appendChild(span);
      } else {
        var a = document.createElement('a');
        a.href = c.href;
        a.textContent = c.text;
        nav.appendChild(a);
      }
    });

    // вставляем ПОСЛЕ .page-title или первого h1/h2
    var target = document.querySelector('.page-title, h1, h2');
    if (target && target.parentNode) {
      target.parentNode.insertBefore(nav, target.nextSibling);
    } else {
      var main = document.querySelector('.main, main');
      if (main) main.appendChild(nav);
    }
  }

  document.addEventListener('DOMContentLoaded', build);
})();
