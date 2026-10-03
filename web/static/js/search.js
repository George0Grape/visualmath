(function () {
  var ICONS = {
    module:  '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>',
    lecture: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="23 7 16 12 23 17 23 7"/><rect x="1" y="5" width="15" height="14" rx="2" ry="2"/></svg>',
    subject: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 9l9-7 9 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>',
  };
  var LABELS = { module: 'Модуль', lecture: 'Лекция', subject: 'Предмет' };

  var CSS = `
    #vm-search-overlay {
      display: none; position: fixed; inset: 0; z-index: 9999;
      background: rgba(0,0,0,.45); backdrop-filter: blur(3px);
      align-items: flex-start; justify-content: center;
      padding-top: 80px;
    }
    #vm-search-overlay.open { display: flex; }
    #vm-search-box {
      width: 100%; max-width: 560px;
      background: var(--surface, #fff);
      border-radius: 14px;
      border: 1.5px solid var(--border, #e4e7ec);
      box-shadow: 0 20px 60px rgba(0,0,0,.2);
      overflow: hidden;
      animation: vm-search-in .15s ease;
    }
    @keyframes vm-search-in { from { opacity:0; transform: translateY(-12px) scale(.97); } to { opacity:1; transform: none; } }
    #vm-search-input {
      width: 100%; padding: 16px 20px;
      font-size: 16px; font-family: 'DM Sans', sans-serif;
      border: none; outline: none;
      background: transparent;
      color: var(--text, #111);
      border-bottom: 1px solid var(--border, #e4e7ec);
    }
    #vm-search-input::placeholder { color: var(--muted, #9ca3af); }
    #vm-search-results { max-height: 340px; overflow-y: auto; }
    .vm-sr-item {
      display: flex; align-items: center; gap: 12px;
      padding: 12px 20px; cursor: pointer;
      transition: background .1s;
      text-decoration: none; color: var(--text, #111);
    }
    .vm-sr-item:hover, .vm-sr-item.focused { background: var(--accent-light, #eff6ff); }
    .vm-sr-icon {
      width: 30px; height: 30px; border-radius: 8px;
      background: var(--bg, #f0f2f5);
      display: flex; align-items: center; justify-content: center;
      color: var(--accent, #2563eb); flex-shrink: 0;
    }
    .vm-sr-text { flex: 1; min-width: 0; }
    .vm-sr-title { font-size: 14px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .vm-sr-sub { font-size: 12px; color: var(--muted, #6b7280); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .vm-sr-badge { font-size: 11px; font-weight: 600; color: var(--accent, #2563eb); background: var(--accent-light, #eff6ff); padding: 2px 8px; border-radius: 20px; white-space: nowrap; }
    .vm-sr-empty { padding: 24px 20px; text-align: center; color: var(--muted, #9ca3af); font-size: 14px; }
    #vm-search-btn {
      display: flex; align-items: center; gap: 7px;
      padding: 6px 13px; border-radius: 8px;
      background: var(--surface, #fff);
      border: 1.5px solid var(--border, #e4e7ec);
      color: var(--muted, #6b7280);
      font-size: 13px; cursor: pointer;
      transition: border-color .15s, color .15s;
    }
    #vm-search-btn:hover { border-color: var(--accent, #2563eb); color: var(--accent, #2563eb); }
    [data-theme="dark"] #vm-search-box { box-shadow: 0 20px 60px rgba(0,0,0,.6); }
  `;

  document.addEventListener('DOMContentLoaded', function () {
    var style = document.createElement('style');
    style.textContent = CSS;
    document.head.appendChild(style);

    // кнопка поиска в topbar
    var topbarRight = document.querySelector('.topbar-right, .topbar-actions');
    if (topbarRight) {
      var btn = document.createElement('button');
      btn.id = 'vm-search-btn';
      btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg> Поиск <kbd>/</kbd>';
      topbarRight.insertBefore(btn, topbarRight.firstChild);
      btn.addEventListener('click', openSearch);
    }

    // оверлей
    var overlay = document.createElement('div');
    overlay.id = 'vm-search-overlay';
    overlay.innerHTML = `
      <div id="vm-search-box" role="dialog" aria-label="Поиск">
        <input id="vm-search-input" type="text" placeholder="Поиск модулей, лекций, предметов…" autocomplete="off" spellcheck="false">
        <div id="vm-search-results"></div>
      </div>`;
    document.body.appendChild(overlay);

    var input = document.getElementById('vm-search-input');
    var results = document.getElementById('vm-search-results');
    var timer = null;
    var focusIdx = -1;

    function openSearch() {
      overlay.classList.add('open');
      input.value = '';
      results.innerHTML = '';
      focusIdx = -1;
      setTimeout(function () { input.focus(); }, 50);
    }
    function closeSearch() {
      overlay.classList.remove('open');
    }

    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) closeSearch();
    });

    document.addEventListener('keydown', function (e) {
      if ((e.key === '/' || (e.key === 'k' && (e.ctrlKey || e.metaKey))) && !overlay.classList.contains('open')) {
        var tag = document.activeElement && document.activeElement.tagName;
        if (tag !== 'INPUT' && tag !== 'TEXTAREA') {
          e.preventDefault();
          openSearch();
        }
      }
      if (!overlay.classList.contains('open')) return;
      if (e.key === 'Escape') { closeSearch(); return; }
      var items = results.querySelectorAll('.vm-sr-item');
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        focusIdx = Math.min(focusIdx + 1, items.length - 1);
        updateFocus(items);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        focusIdx = Math.max(focusIdx - 1, -1);
        updateFocus(items);
      } else if (e.key === 'Enter' && focusIdx >= 0 && items[focusIdx]) {
        items[focusIdx].click();
      }
    });

    function updateFocus(items) {
      items.forEach(function (el, i) { el.classList.toggle('focused', i === focusIdx); });
      if (focusIdx >= 0 && items[focusIdx]) items[focusIdx].scrollIntoView({ block: 'nearest' });
    }

    input.addEventListener('input', function () {
      clearTimeout(timer);
      var q = input.value.trim();
      if (q.length < 2) { results.innerHTML = ''; focusIdx = -1; return; }
      timer = setTimeout(function () { doSearch(q); }, 200);
    });

    function doSearch(q) {
      fetch('/api/search?q=' + encodeURIComponent(q))
        .then(function (r) { return r.json(); })
        .then(function (data) {
          focusIdx = -1;
          if (!data.length) {
            results.innerHTML = '<div class="vm-sr-empty">Ничего не найдено</div>';
            return;
          }
          results.innerHTML = data.map(function (item) {
            return '<a class="vm-sr-item" href="' + item.url + '">' +
              '<span class="vm-sr-icon">' + (ICONS[item.type] || '') + '</span>' +
              '<span class="vm-sr-text">' +
                '<div class="vm-sr-title">' + esc(item.title) + '</div>' +
                (item.sub ? '<div class="vm-sr-sub">' + esc(item.sub) + '</div>' : '') +
              '</span>' +
              '<span class="vm-sr-badge">' + (LABELS[item.type] || item.type) + '</span>' +
              '</a>';
          }).join('');
        })
        .catch(function () {});
    }

    function esc(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
  });
})();
