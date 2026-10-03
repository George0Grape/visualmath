(function () {
  /* ── 1. Применяем тему до рендера (анти-FOUC) ── */
  var theme = localStorage.getItem('vm-theme') || 'light';
  document.documentElement.setAttribute('data-theme', theme);

  /* ── 2. Инжектим CSS переопределения ── */
  var style = document.createElement('style');
  style.textContent = `
    html[data-theme="dark"] {
      --bg: #0f1117;
      --surface: #1a1d23;
      --border: #2a2d35;
      --text: #e8eaed;
      --muted: #9ca3af;
      --subtle: #6b7280;
      --accent: #3b82f6;
      --accent-light: #1e3657;
      --shadow: 0 1px 3px rgba(0,0,0,.4), 0 4px 16px rgba(0,0,0,.3);
      --shadow-hover: 0 4px 12px rgba(0,0,0,.5), 0 16px 40px rgba(0,0,0,.4);
    }
    html[data-theme="dark"] body {
      background: #0f1117 !important;
      color: #e8eaed !important;
    }
    html[data-theme="dark"] .topbar,
    html[data-theme="dark"] .sidebar {
      background: #1a1d23 !important;
      border-color: #2a2d35 !important;
    }
    html[data-theme="dark"] .sidebar-link { color: #9ca3af !important; }
    html[data-theme="dark"] .sidebar-link:hover,
    html[data-theme="dark"] .sidebar-link.active { background: #23272f !important; color: #e8eaed !important; }
    html[data-theme="dark"] .sidebar-section { color: #6b7280 !important; }
    html[data-theme="dark"] .topbar-logo { color: #e8eaed !important; }
    html[data-theme="dark"] .user-chip {
      background: #23272f !important;
      border-color: #2a2d35 !important;
      color: #e8eaed !important;
    }
    html[data-theme="dark"] .logout-btn {
      border-color: #2a2d35 !important;
      color: #9ca3af !important;
    }
    html[data-theme="dark"] .card,
    html[data-theme="dark"] .stat-card,
    html[data-theme="dark"] .module-card,
    html[data-theme="dark"] .lecture-card,
    html[data-theme="dark"] .subject-card,
    html[data-theme="dark"] .assignment-card {
      background: #1a1d23 !important;
      border-color: #2a2d35 !important;
    }
    html[data-theme="dark"] .card-body,
    html[data-theme="dark"] .module-body { color: #e8eaed !important; }
    html[data-theme="dark"] input,
    html[data-theme="dark"] select,
    html[data-theme="dark"] textarea {
      background: #23272f !important;
      color: #e8eaed !important;
      border-color: #2a2d35 !important;
    }
    html[data-theme="dark"] input::placeholder,
    html[data-theme="dark"] textarea::placeholder { color: #6b7280 !important; }
    html[data-theme="dark"] input:focus,
    html[data-theme="dark"] select:focus,
    html[data-theme="dark"] textarea:focus {
      border-color: #3b82f6 !important;
      box-shadow: 0 0 0 3px rgba(59,130,246,.2) !important;
    }
    html[data-theme="dark"] table { color: #e8eaed; }
    html[data-theme="dark"] th { background: #23272f !important; color: #9ca3af !important; border-color: #2a2d35 !important; }
    html[data-theme="dark"] td { border-color: #2a2d35 !important; }
    html[data-theme="dark"] tr:hover td { background: #23272f !important; }
    html[data-theme="dark"] .section,
    html[data-theme="dark"] .content-section,
    html[data-theme="dark"] .results-table-wrap,
    html[data-theme="dark"] .submission-card,
    html[data-theme="dark"] .annotation-panel { background: #1a1d23 !important; border-color: #2a2d35 !important; }
    html[data-theme="dark"] .type-card.selected { background: #1e3657 !important; border-color: #3b82f6 !important; box-shadow: 0 0 0 3px rgba(59,130,246,.15) !important; }
    html[data-theme="dark"] .badge,
    html[data-theme="dark"] .chip { background: #23272f !important; color: #9ca3af !important; }
    html[data-theme="dark"] .divider::before,
    html[data-theme="dark"] .divider::after { background: #2a2d35 !important; }
    html[data-theme="dark"] hr { border-color: #2a2d35 !important; }
    html[data-theme="dark"] .answer-chip { background: #23272f !important; border-color: #2a2d35 !important; color: #e8eaed !important; }
    html[data-theme="dark"] .answer-chip.correct { background: #14532d !important; border-color: #166534 !important; color: #bbf7d0 !important; }
    html[data-theme="dark"] .btn-outline {
      border-color: #2a2d35 !important;
      color: #e8eaed !important;
    }
    html[data-theme="dark"] .btn-outline:hover { border-color: #3b82f6 !important; }

    /* OAuth-кнопки (VK / Яндекс) на логине и главной */
    html[data-theme="dark"] .btn-social,
    html[data-theme="dark"] .btn-oauth {
      background: #23272f !important;
      border-color: #2a2d35 !important;
      color: #e8eaed !important;
    }
    html[data-theme="dark"] .btn-social:hover,
    html[data-theme="dark"] .btn-oauth:hover {
      border-color: #3b82f6 !important;
      background: #2a2f3a !important;
    }
    html[data-theme="dark"] .btn-social.btn-vk:hover { border-color: #4d9aff !important; background: #16243a !important; }
    html[data-theme="dark"] .btn-social.btn-ya:hover { border-color: #ff6242 !important; background: #3a1d16 !important; }
    html[data-theme="dark"] .links a { color: #60a5fa !important; }
    html[data-theme="dark"] .field label { color: #e8eaed !important; }
    html[data-theme="dark"] .form-title { color: #e8eaed !important; }
    html[data-theme="dark"] .heading { color: #e8eaed !important; }

    /* кнопка переключения темы */
    .theme-btn {
      display: flex; align-items: center; justify-content: center;
      width: 34px; height: 34px;
      border: 1.5px solid var(--border);
      border-radius: 8px;
      background: transparent;
      color: var(--muted);
      cursor: pointer;
      transition: border-color .15s, color .15s, background .15s;
      flex-shrink: 0;
    }
    .theme-btn:hover { border-color: var(--accent); color: var(--accent); }
    .theme-btn-float {
      position: fixed; bottom: 20px; right: 20px; z-index: 9999;
      width: 40px; height: 40px; border-radius: 10px;
      box-shadow: 0 4px 12px rgba(0,0,0,.15);
      background: var(--surface);
    }
  `;
  document.head.appendChild(style);

  /* ── 3. После загрузки DOM — добавляем кнопку ── */
  document.addEventListener('DOMContentLoaded', function () {
    var btn = document.createElement('button');
    btn.className = 'theme-btn';
    btn.title = 'Сменить тему';
    btn.setAttribute('aria-label', 'Сменить тему');
    btn.innerHTML = currentIcon();

    btn.addEventListener('click', function () {
      var next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('vm-theme', next);
      btn.innerHTML = currentIcon();
    });

    var topbarRight = document.querySelector('.topbar-right, .topbar-actions');
    if (topbarRight) {
      topbarRight.insertBefore(btn, topbarRight.firstChild);
    } else {
      btn.classList.add('theme-btn-float');
      document.body.appendChild(btn);
    }
  });

  function currentIcon() {
    var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    return isDark
      ? '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>'
      : '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
  }
})();
