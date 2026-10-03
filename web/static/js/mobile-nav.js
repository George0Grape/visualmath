(function () {
  var style = document.createElement('style');
  style.textContent = `
    /* ── A11y: видимый фокус ТОЛЬКО при навигации с клавиатуры ──
       :focus-visible не срабатывает на мышиный клик, поэтому дизайн
       не меняется для обычных пользователей, а keyboard-навигация
       получает чёткий контур. !important перебивает локальные outline:none. */
    :focus-visible {
      outline: 2px solid var(--accent, #2563eb) !important;
      outline-offset: 2px !important;
      border-radius: 4px;
    }
    /* Текст только для скринридеров (визуально скрыт) */
    .vm-sr-only {
      position: absolute !important; width: 1px; height: 1px;
      padding: 0; margin: -1px; overflow: hidden;
      clip: rect(0,0,0,0); white-space: nowrap; border: 0;
    }
    /* Skip-link: появляется при фокусе (Tab сразу после загрузки) */
    .vm-skip-link {
      position: fixed; top: 8px; left: 8px; z-index: 3000;
      background: var(--accent, #2563eb); color: #fff;
      padding: 10px 16px; border-radius: 8px;
      font-size: 14px; font-weight: 600; text-decoration: none;
      transform: translateY(-150%); transition: transform .15s ease;
    }
    .vm-skip-link:focus { transform: translateY(0); }
    /* Уважаем системную настройку «меньше движения» */
    @media (prefers-reduced-motion: reduce) {
      *, *::before, *::after {
        animation-duration: .001ms !important;
        animation-iteration-count: 1 !important;
        transition-duration: .001ms !important;
        scroll-behavior: auto !important;
      }
    }

    .vm-hamburger {
      display: none;
      align-items: center; justify-content: center;
      width: 36px; height: 36px;
      border: 1.5px solid var(--border);
      border-radius: 8px; background: transparent;
      color: var(--text); cursor: pointer;
      flex-shrink: 0;
    }
    .vm-overlay {
      display: none;
      position: fixed; inset: 0; z-index: 1190;
      background: rgba(0,0,0,.35);
      backdrop-filter: blur(2px);
    }
    .vm-overlay.open { display: block; }

    @media (max-width: 700px) {
      .vm-hamburger { display: flex; }

      /* Корень обрезки контента справа: переполняющие топбар/таблицы элементы
         раздувают scrollWidth страницы. Жёстко гасим горизонтальный скролл —
         топбар у нас position:fixed, поэтому sticky-эффекты это не ломает. */
      html, body { overflow-x: hidden !important; max-width: 100%; }
      /* Мобильный drawer: ВСЕГДА position:fixed (вне потока), чтобы на flex-страницах
         (например classroom/subjects) сайдбар не занимал место и не ломал раскладку.
         display:flex!important перебивает старое .sidebar{display:none} из шаблонов.
         z-index 1200 > overlay 1190 — drawer никогда не затемняется поверх себя. */
      .sidebar {
        display: flex !important;
        position: fixed !important;
        top: var(--topbar-h, 58px); left: 0; bottom: 0;
        width: min(300px, 85vw);
        z-index: 1200;
        overflow-y: auto; overflow-x: hidden;
        transform: translateX(-100%) !important;
        transition: transform .25s ease;
      }
      .sidebar.open { transform: translateX(0) !important; box-shadow: 4px 0 24px rgba(0,0,0,.15); }

      /* Топбар: компактнее на мобильном + детям разрешаем ужиматься */
      .topbar { padding: 0 12px; gap: 8px; }
      .topbar > * { min-width: 0; }
      .topbar-right, .topbar-actions { gap: 8px; min-width: 0; }
      /* Логотип чуть меньше, чтобы освободить место под действия */
      .topbar-logo, .topbar-brand { font-size: 17px; }

      /* Второстепенное в топбаре прячем: крошки, текстовые «секции» бренда
         (напр. «Classroom») и разделители — на узком экране они переполняют строку */
      .vm-breadcrumbs, .brand-section, .brand-sep, .topbar-crumb, .topbar-sep,
      .topbar .bc-sep, .topbar .tb-sep, .topbar .sep { display: none !important; }

      /* Текстовые «призрачные» кнопки в топбаре (обычно back-link «← Дашборд»)
         дублируют пункты бокового меню — на мобильном прячем, освобождая строку */
      .topbar .tb-btn-ghost { display: none !important; }

      /* Classroom-страницы (subject-detail и т.п.) имеют свой топбар без сайдбара:
         back-link «← Classroom», разделитель и длинное название предмета. Сжимаем:
         back-link → одна стрелка, разделитель прячем, название — с многоточием. */
      .topbar .back-link {
        font-size: 0 !important; padding: 6px 8px !important; flex-shrink: 0;
      }
      .topbar .back-link::before { content: "←"; font-size: 19px; }
      .topbar .crumb-sep { display: none !important; }
      .topbar .crumb-current {
        font-size: 14px !important; flex: 1 1 auto; min-width: 0;
        white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
      }

      /* Любой элемент топбара с иконкой (кнопки/ссылки действий, поиск, выход,
         back-link) схлопываем до иконки. font-size:0 убирает текстовые узлы
         («Поиск», «Создать предмет», «Дашборд»), а у SVG размеры заданы
         атрибутами и от font-size не зависят. Лого и гамбургер исключаем,
         чтобы сохранить надпись «VisualMath» и работающее меню. */
      .topbar a:has(svg):not(.topbar-logo):not(.topbar-brand),
      .topbar button:has(svg):not(.vm-hamburger) {
        font-size: 0 !important;
        gap: 0 !important;
        padding: 8px 10px !important;
      }
      #vm-search-btn kbd { display: none !important; }

      /* user-chip: имя пользователя — голый текстовый узел рядом с аватаром,
         span'а нет. font-size:0 схлопывает имя, а букве/картинке аватара
         возвращаем размер явно. Остаётся компактный кружок-аватар. */
      .user-chip, .user-info { font-size: 0 !important; gap: 0 !important; }
      .user-chip .user-avatar, .user-avatar, .user-chip img { font-size: 14px !important; }
      .user-login, .user-name, .topbar-user-name { display: none !important; }

      /* Кнопка выхода → только иконка (текст «Выйти» — тоже голый узел) */
      .logout-btn { font-size: 0 !important; gap: 0 !important; padding: 8px 10px !important; }
      .logout-btn span { display: none; }

      /* Карточки и двухколоночные раскладки: 1 колонка.
         .two-col (напр. конструктор лекции: доступные | выбранные модули)
         иначе остаётся в 2 колонки и уезжает за экран. */
      .modules-grid, .lectures-grid, .subjects-grid,
      .two-col, .two-column, .split-cols,
      [style*="grid-template-columns"] {
        grid-template-columns: 1fr !important;
      }

      /* Стат-карточки: 2 колонки */
      .stats-row { grid-template-columns: repeat(2, 1fr) !important; }

      /* Основной контент без боковых отступов */
      .main { margin-left: 0 !important; padding-left: 16px !important; padding-right: 16px !important; }

      /* Хлебные крошки скрыть на узком экране */
      .vm-breadcrumbs { display: none; }

      /* Кнопки действий: меньше padding */
      .page-actions .btn, .page-actions .btn-accent { padding: 8px 14px; font-size: 13px; }

      /* ── Глобальные страховки от горизонтального скролла ── */
      img, video, canvas { max-width: 100%; height: auto; }
      /* Декоративные баннеры/hero часто имеют фигуры, торчащие за край —
         клипуем их, чтобы не давали лишних пикселей переполнения */
      .banner, .welcome-banner, .hero, [class*="-banner"] { overflow: hidden; }
      .main { overflow-x: hidden; }
      pre { overflow-x: auto; }
      /* Таблицы без своей обёртки делаем прокручиваемыми */
      .main table { display: block; overflow-x: auto; white-space: nowrap; max-width: 100%; }
      .table-wrap table, .results-table-wrap table { display: table; white-space: normal; }
      /* Модалки/оверлеи не должны упираться в края экрана */
      .overlay-card, .result-card, .modal-card, .popup { max-width: 92vw !important; }

      /* ── MathJax: длинные формулы не должны рвать страницу ──
         display-формулы ($$…$$) рендерятся в mjx-container[display="true"];
         делаем их прокручиваемыми ВНУТРИ блока (вместо переполнения вправо).
         Все контейнеры ограничиваем шириной родителя. Правило class-agnostic —
         работает в ридере лекций, слайдах, module-view, тестах. */
      mjx-container { max-width: 100%; }
      mjx-container[display="true"] {
        display: block; overflow-x: auto; overflow-y: hidden;
        max-width: 100%; padding-bottom: 2px;
      }
      /* Текстовые блоки с инлайн-формулами: разрешаем усадку во flex и перенос,
         чтобы отдельные формулы-острова переносились по пробелам, а не вылезали */
      .question-text, .answer-text, .lecture-text, .module-text, .text-content,
      .slide-text, .reader-content, .mod-body, .module-content {
        min-width: 0; overflow-wrap: anywhere;
      }

      /* ── Тач-таргеты >=44px (WCAG 2.5.5 / Apple HIG) ──
         Маленькие кнопки/иконки тяжело нажать пальцем. Расширяем целевую
         область через min-height/min-width + центрирование (не фиксируем
         высоту, чтобы не ломать переносы текста в кнопках). */
      .vm-hamburger { width: 44px; height: 44px; }
      /* Иконочные кнопки/ссылки топбара и карточек */
      .topbar a:has(svg):not(.topbar-logo):not(.topbar-brand),
      .topbar button:has(svg):not(.vm-hamburger),
      .foot-btn, .icon-btn, .cp-close, .page-nav button {
        min-width: 44px; min-height: 44px;
        display: inline-flex; align-items: center; justify-content: center;
      }
      /* Текстовые кнопки и пункты бокового меню */
      .btn, .btn-primary, .btn-create, .btn-open, .btn-edit, .tb-btn,
      .nav-btn, .empty-btn, .check-btn, .cp-send, .filter-select,
      .sb-link, .nav-link, .logout-btn {
        min-height: 44px;
      }
      /* Поля ввода тоже удобнее тапать при 44px */
      input[type="text"], input[type="password"], input[type="email"],
      input[type="number"], select, textarea {
        min-height: 44px;
      }
    }

    @media (max-width: 480px) {
      .stats-row { grid-template-columns: 1fr 1fr !important; }
      .stat-card { padding: 14px 12px !important; }
      .stat-value { font-size: 22px !important; }
    }
  `;
  document.head.appendChild(style);

  // ── PWA: манифест + theme-color + регистрация service worker ──
  // Делаем здесь, т.к. mobile-nav.js подключён почти во всех шаблонах —
  // не нужно править <head> каждого. SW регистрируется один раз и его scope
  // покрывает весь origin, поэтому офлайн работает и на страницах без этого JS.
  if (!document.querySelector('link[rel="manifest"]')) {
    var mf = document.createElement('link');
    mf.rel = 'manifest';
    mf.href = '/static/manifest.webmanifest';
    document.head.appendChild(mf);
  }
  if (!document.querySelector('meta[name="theme-color"]')) {
    var tc = document.createElement('meta');
    tc.name = 'theme-color';
    tc.content = '#2563eb';
    document.head.appendChild(tc);
  }
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', function () {
      navigator.serviceWorker.register('/sw.js').catch(function () {});
    });
  }

  // ── A11y: skip-link + клавиатурная доступность кликабельных элементов ──
  // Отдельным слушателем, т.к. обработчик гамбургера ниже выходит рано
  // (return), если на странице нет сайдбара/топбара — а эти улучшения
  // нужны на ВСЕХ страницах.
  document.addEventListener('DOMContentLoaded', function () {
    // Skip-to-content: первая Tab после загрузки фокусирует ссылку,
    // позволяя перепрыгнуть навигацию к основному содержимому.
    var main = document.querySelector('main, .main, .slide-area');
    if (main) {
      if (!main.id) main.id = 'vm-main';
      if (!main.hasAttribute('tabindex')) main.setAttribute('tabindex', '-1');
      var skip = document.createElement('a');
      skip.className = 'vm-skip-link';
      skip.href = '#' + main.id;
      skip.textContent = 'К основному содержимому';
      document.body.insertBefore(skip, document.body.firstChild);
    }
    // Кликабельные div/span (карточки с onclick) делаем доступными с клавиатуры:
    // focus + Enter/Space → click. role=button добавляем только если внутри нет
    // вложенных интерактивных элементов (иначе ARIA-конфликт «nested interactive»).
    document.querySelectorAll('[onclick]').forEach(function (el) {
      var t = el.tagName;
      if (t==='A'||t==='BUTTON'||t==='INPUT'||t==='SELECT'||t==='TEXTAREA'||t==='LABEL') return;
      if (el.getAttribute('tabindex') === null) el.setAttribute('tabindex', '0');
      if (!el.hasAttribute('role') && !el.querySelector('a,button,input,select,textarea')) {
        el.setAttribute('role', 'button');
      }
      el.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
          e.preventDefault();
          el.click();
        }
      });
    });
  });

  document.addEventListener('DOMContentLoaded', function () {
    var sidebar = document.querySelector('.sidebar');
    var topbar  = document.querySelector('.topbar');
    if (!sidebar || !topbar) return;

    var overlay = document.createElement('div');
    overlay.className = 'vm-overlay';
    document.body.appendChild(overlay);

    if (!sidebar.id) sidebar.id = 'vm-sidebar';
    var btn = document.createElement('button');
    btn.className = 'vm-hamburger';
    btn.setAttribute('aria-label', 'Открыть меню');
    btn.setAttribute('aria-expanded', 'false');
    btn.setAttribute('aria-controls', sidebar.id);
    btn.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>';
    topbar.insertBefore(btn, topbar.children[1] || null);

    function open()  { sidebar.classList.add('open');  overlay.classList.add('open');  btn.setAttribute('aria-expanded', 'true');  btn.setAttribute('aria-label', 'Закрыть меню'); }
    function close() { sidebar.classList.remove('open'); overlay.classList.remove('open'); btn.setAttribute('aria-expanded', 'false'); btn.setAttribute('aria-label', 'Открыть меню'); }

    btn.addEventListener('click', function () {
      sidebar.classList.contains('open') ? close() : open();
    });
    overlay.addEventListener('click', close);
    sidebar.querySelectorAll('a').forEach(function (a) {
      a.addEventListener('click', close);
    });
  });
})();
