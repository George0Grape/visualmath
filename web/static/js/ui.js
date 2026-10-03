(function () {

  /* ═══════════════════════════════════════════════
     1. Scroll to top
  ═══════════════════════════════════════════════ */
  var scrollStyle = document.createElement('style');
  scrollStyle.textContent = `
    #vm-scroll-top {
      position: fixed; bottom: 72px; right: 24px; z-index: 8000;
      width: 38px; height: 38px; border-radius: 9px;
      background: var(--surface, #fff);
      border: 1.5px solid var(--border, #e4e7ec);
      color: var(--muted, #6b7280);
      display: flex; align-items: center; justify-content: center;
      cursor: pointer; opacity: 0; pointer-events: none;
      transition: opacity .2s, transform .2s, border-color .15s;
      box-shadow: 0 2px 8px rgba(0,0,0,.08);
    }
    #vm-scroll-top.visible { opacity: 1; pointer-events: all; }
    #vm-scroll-top:hover { border-color: var(--accent, #2563eb); color: var(--accent, #2563eb); transform: translateY(-2px); }

    .btn-spin { pointer-events: none; opacity: .75; }
    .btn-spin::before {
      content: '';
      display: inline-block; width: 13px; height: 13px;
      border: 2px solid currentColor; border-top-color: transparent;
      border-radius: 50%;
      animation: vm-spin .6s linear infinite;
      vertical-align: middle; margin-right: 6px;
    }
    @keyframes vm-spin { to { transform: rotate(360deg); } }

    /* ── Микро-взаимодействия: подъём карточек при наведении ── */
    .module-card, .lecture-card, .subject-card, .assignment-card,
    .tile, .stat-card, .submission-card {
      transition: transform .18s ease, box-shadow .18s ease, border-color .15s ease;
    }
    .module-card:hover, .lecture-card:hover, .subject-card:hover,
    .assignment-card:hover, .tile:hover, .submission-card:hover {
      transform: translateY(-3px);
      box-shadow: var(--shadow-hover, 0 8px 28px rgba(0,0,0,.10));
    }

    /* Пользователи, отключившие анимации, не получают движения */
    @media (prefers-reduced-motion: reduce) {
      .module-card, .lecture-card, .subject-card, .assignment-card,
      .tile, .stat-card, .submission-card { transition: none; }
      .module-card:hover, .lecture-card:hover, .subject-card:hover,
      .assignment-card:hover, .tile:hover, .submission-card:hover { transform: none; }
    }
  `;
  document.head.appendChild(scrollStyle);

  document.addEventListener('DOMContentLoaded', function () {
    var btn = document.createElement('button');
    btn.id = 'vm-scroll-top';
    btn.setAttribute('aria-label', 'Наверх');
    btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="18 15 12 9 6 15"/></svg>';
    document.body.appendChild(btn);

    var scrollTarget = document.querySelector('.main') || window;
    var getScrollY = function () {
      return scrollTarget === window ? window.scrollY : scrollTarget.scrollTop;
    };
    var scrollListener = function () {
      btn.classList.toggle('visible', getScrollY() > 300);
    };
    scrollTarget.addEventListener('scroll', scrollListener, { passive: true });
    btn.addEventListener('click', function () {
      if (scrollTarget === window) window.scrollTo({ top: 0, behavior: 'smooth' });
      else scrollTarget.scrollTo({ top: 0, behavior: 'smooth' });
    });
  });

  /* ═══════════════════════════════════════════════
     2. Button spinner  — window.btnLoading(btn, true/false)
  ═══════════════════════════════════════════════ */
  window.btnLoading = function (btn, loading) {
    if (!btn) return;
    if (loading) {
      btn._vmLabel = btn.innerHTML;
      btn.disabled = true;
      btn.classList.add('btn-spin');
      btn.innerHTML = btn.getAttribute('data-loading-text') || btn.textContent.trim();
    } else {
      btn.disabled = false;
      btn.classList.remove('btn-spin');
      if (btn._vmLabel !== undefined) btn.innerHTML = btn._vmLabel;
    }
  };

  /* ═══════════════════════════════════════════════
     3. Animated counters
     Используй:  <span class="vm-counter" data-target="128">0</span>
  ═══════════════════════════════════════════════ */
  document.addEventListener('DOMContentLoaded', function () {
    var counters = document.querySelectorAll('.vm-counter');
    if (!counters.length) return;

    var animate = function (el) {
      var target = parseFloat(el.dataset.target || el.textContent) || 0;
      var decimals = (String(target).split('.')[1] || '').length;
      var duration = 900;
      var start = performance.now();
      var step = function (now) {
        var progress = Math.min((now - start) / duration, 1);
        var eased = 1 - Math.pow(1 - progress, 3);
        el.textContent = (target * eased).toFixed(decimals);
        if (progress < 1) requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
    };

    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { animate(e.target); observer.unobserve(e.target); }
      });
    }, { threshold: 0.5 });

    counters.forEach(function (el) { observer.observe(el); });
  });

  /* ═══════════════════════════════════════════════
     4. Аватар в топбаре — подгружаем фото если оно есть
  ═══════════════════════════════════════════════ */
  document.addEventListener('DOMContentLoaded', function () {
    var avatarEl = document.getElementById('topbarAvatar');
    if (!avatarEl) return;
    // уже показывает img — не перезаписываем
    if (avatarEl.querySelector('img')) return;
    var token = localStorage.getItem('token') || '';
    fetch('/api/profile', {
      headers: token ? { 'Authorization': 'Bearer ' + token } : {}
    }).then(function(r) { return r.ok ? r.json() : null; })
      .then(function(data) {
        if (!data || !data.avatar_url) return;
        var img = document.createElement('img');
        img.src = data.avatar_url + '?v=' + Date.now();
        img.style.cssText = 'width:100%;height:100%;object-fit:cover;border-radius:50%';
        img.alt = 'аватар';
        avatarEl.innerHTML = '';
        avatarEl.appendChild(img);
      }).catch(function() {});
  });

  /* ═══════════════════════════════════════════════
     5. Copy to clipboard  — window.copyToClipboard(text, label)
        Работает и на HTTP (без Secure Context)
  ═══════════════════════════════════════════════ */
  window.copyToClipboard = function (text, label) {
    function fallback() {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.style.cssText = 'position:fixed;top:-9999px;left:-9999px;opacity:0';
      document.body.appendChild(ta);
      ta.focus(); ta.select();
      try { document.execCommand('copy'); } catch (e) { /* silent */ }
      ta.remove();
      if (window.showToast) showToast((label || 'Текст') + ' скопирован', 'success');
    }
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(function () {
        if (window.showToast) showToast((label || 'Текст') + ' скопирован', 'success');
      }).catch(fallback);
    } else {
      fallback();
    }
  };

  /* ═══════════════════════════════════════════════
     6. Confirmation dialog  — window.vmConfirm({title, message, danger, onConfirm})
        Заменяет браузерный confirm() стильным модалом
  ═══════════════════════════════════════════════ */
  var confirmStyle = document.createElement('style');
  confirmStyle.textContent = `
    #vm-confirm-overlay {
      display: none; position: fixed; inset: 0; z-index: 99999;
      background: rgba(0,0,0,.5); backdrop-filter: blur(4px);
      align-items: center; justify-content: center;
    }
    #vm-confirm-overlay.open { display: flex; }
    #vm-confirm-box {
      background: var(--surface, #fff);
      border: 1.5px solid var(--border, #e4e7ec);
      border-radius: 16px;
      box-shadow: 0 24px 64px rgba(0,0,0,.22);
      padding: 28px 28px 22px;
      width: 100%; max-width: 400px; margin: 16px;
      animation: vm-confirm-in .18s ease;
    }
    @keyframes vm-confirm-in {
      from { opacity: 0; transform: scale(.94) translateY(8px); }
      to   { opacity: 1; transform: none; }
    }
    #vm-confirm-icon {
      width: 44px; height: 44px; border-radius: 12px;
      display: flex; align-items: center; justify-content: center;
      margin-bottom: 16px;
    }
    #vm-confirm-icon.danger { background: #fee2e2; color: #dc2626; }
    #vm-confirm-icon.warn   { background: #fef3c7; color: #d97706; }
    #vm-confirm-title {
      font-size: 16px; font-weight: 700;
      color: var(--text, #111); margin-bottom: 8px;
    }
    #vm-confirm-msg {
      font-size: 14px; line-height: 1.55;
      color: var(--muted, #6b7280); margin-bottom: 24px;
    }
    #vm-confirm-btns {
      display: flex; gap: 10px; justify-content: flex-end;
    }
    #vm-confirm-btns button {
      padding: 8px 20px; border-radius: 9px;
      font-size: 14px; font-weight: 600; cursor: pointer;
      border: 1.5px solid transparent; transition: all .15s;
    }
    #vm-confirm-cancel {
      background: var(--bg, #f0f2f5);
      border-color: var(--border, #e4e7ec) !important;
      color: var(--text, #374151);
    }
    #vm-confirm-cancel:hover { border-color: var(--accent, #2563eb) !important; color: var(--accent, #2563eb); }
    #vm-confirm-ok.danger { background: #dc2626; color: #fff; }
    #vm-confirm-ok.danger:hover { background: #b91c1c; }
    #vm-confirm-ok.warn { background: var(--accent, #2563eb); color: #fff; }
    #vm-confirm-ok.warn:hover { background: #1d4ed8; }
    [data-theme="dark"] #vm-confirm-box { box-shadow: 0 24px 64px rgba(0,0,0,.5); }
    [data-theme="dark"] #vm-confirm-cancel { background: #23272f; }
  `;
  document.head.appendChild(confirmStyle);

  (function () {
    var overlay, box, titleEl, msgEl, okBtn, cancelBtn, iconEl;
    var pending = null;

    function build() {
      overlay = document.createElement('div');
      overlay.id = 'vm-confirm-overlay';
      overlay.innerHTML = `
        <div id="vm-confirm-box">
          <div id="vm-confirm-icon"></div>
          <div id="vm-confirm-title"></div>
          <div id="vm-confirm-msg"></div>
          <div id="vm-confirm-btns">
            <button id="vm-confirm-cancel">Отмена</button>
            <button id="vm-confirm-ok">Подтвердить</button>
          </div>
        </div>`;
      document.body.appendChild(overlay);
      titleEl  = document.getElementById('vm-confirm-title');
      msgEl    = document.getElementById('vm-confirm-msg');
      okBtn    = document.getElementById('vm-confirm-ok');
      cancelBtn = document.getElementById('vm-confirm-cancel');
      iconEl   = document.getElementById('vm-confirm-icon');

      cancelBtn.addEventListener('click', close);
      overlay.addEventListener('click', function (e) { if (e.target === overlay) close(); });
      document.addEventListener('keydown', function (e) {
        if (!overlay.classList.contains('open')) return;
        if (e.key === 'Escape') close();
        if (e.key === 'Enter') confirm();
      });
      okBtn.addEventListener('click', confirm);
    }

    function close() {
      if (!overlay) return;
      overlay.classList.remove('open');
      pending = null;
    }

    function confirm() {
      overlay.classList.remove('open');
      var cb = pending;
      pending = null;
      if (cb) cb();
    }

    window.vmConfirm = function (opts) {
      if (!overlay) build();
      var danger = opts.danger !== false;
      var cls = danger ? 'danger' : 'warn';
      iconEl.className = cls;
      iconEl.innerHTML = danger
        ? '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 011-1h4a1 1 0 011 1v2"/></svg>'
        : '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>';
      titleEl.textContent = opts.title || 'Подтверждение';
      msgEl.textContent   = opts.message || '';
      okBtn.textContent   = opts.okText || (danger ? 'Удалить' : 'Подтвердить');
      okBtn.className     = cls;
      pending = opts.onConfirm || null;
      overlay.classList.add('open');
      okBtn.focus();
    };
  })();

  /* ═══════════════════════════════════════════════
     7. Topbar blur при скролле
  ═══════════════════════════════════════════════ */
  var topbarScrollStyle = document.createElement('style');
  topbarScrollStyle.textContent = `
    .topbar { transition: background .2s, box-shadow .2s, border-color .2s; }
    .topbar.vm-scrolled {
      backdrop-filter: blur(14px) saturate(1.6) !important;
      -webkit-backdrop-filter: blur(14px) saturate(1.6) !important;
      background: rgba(255,255,255,.82) !important;
      box-shadow: 0 1px 0 rgba(0,0,0,.06), 0 4px 16px rgba(0,0,0,.06) !important;
    }
    html[data-theme="dark"] .topbar.vm-scrolled {
      background: rgba(26,29,35,.85) !important;
      box-shadow: 0 1px 0 rgba(0,0,0,.3), 0 4px 16px rgba(0,0,0,.3) !important;
    }
  `;
  document.head.appendChild(topbarScrollStyle);

  document.addEventListener('DOMContentLoaded', function () {
    var topbar = document.querySelector('.topbar');
    if (!topbar) return;
    var main = document.querySelector('.main') || window;
    var getY = function () { return main === window ? window.scrollY : main.scrollTop; };
    (main === window ? window : main).addEventListener('scroll', function () {
      topbar.classList.toggle('vm-scrolled', getY() > 10);
    }, { passive: true });
  });

  /* ═══════════════════════════════════════════════
     8. Staggered card animations
  ═══════════════════════════════════════════════ */
  var cardAnimStyle = document.createElement('style');
  cardAnimStyle.textContent = `
    /* Только прозрачность, без вертикального сдвига — движение контента после
       первой отрисовки воспринималось как «страница загружается дважды». */
    @keyframes vm-fade-up {
      from { opacity: 0; }
      to   { opacity: 1; }
    }
    .vm-anim {
      animation: vm-fade-up .25s ease both;
    }
  `;
  document.head.appendChild(cardAnimStyle);

  var cardSelector = '.module-card, .lecture-card, .subject-card, .assignment-card, .stat-card, .card';
  var reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Применяет stagger-анимацию к набору карточек (delay сбрасывается на каждую пачку)
  function animateCards(cards) {
    if (reduceMotion || !cards.length) return;
    var delay = 0;
    cards.forEach(function (el) {
      if (el.classList.contains('vm-anim') || el.dataset.vmAnimated) return;
      el.classList.add('vm-anim');
      el.style.animationDelay = delay + 'ms';
      delay = Math.min(delay + 45, 320);
      // После появления снимаем класс: иначе animation-fill-mode:both держит
      // transform и перебивает hover-подъём карточки.
      el.addEventListener('animationend', function () {
        el.classList.remove('vm-anim');
        el.style.animationDelay = '';
        el.dataset.vmAnimated = '1';
      }, { once: true });
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    // Карточки, отрендеренные сервером (предметы, дашборд)
    animateCards(Array.prototype.slice.call(document.querySelectorAll(cardSelector)));

    // Карточки, подгружаемые через API позже (модули, лекции) — ловим вставку в DOM
    var observer = new MutationObserver(function (mutations) {
      var fresh = [];
      mutations.forEach(function (m) {
        m.addedNodes.forEach(function (node) {
          if (node.nodeType !== 1) return;                   // только элементы
          if (node.matches && node.matches(cardSelector)) {
            fresh.push(node);
          } else if (node.querySelectorAll) {
            Array.prototype.push.apply(fresh, node.querySelectorAll(cardSelector));
          }
        });
      });
      if (fresh.length) animateCards(fresh);
    });
    observer.observe(document.body, { childList: true, subtree: true });
  });

  /* ═══════════════════════════════════════════════
     Единый логотип-знак в топбаре (оси + парабола)
     Вставляем централизованно, чтобы не править ~20 шаблонов.
  ═══════════════════════════════════════════════ */
  var markStyle = document.createElement('style');
  markStyle.textContent = `
    /* НЕ используем gap — иначе между "Visual" и "Math" появляется пробел.
       Отступ вешаем только на сам знак. */
    .topbar-logo { display: inline-flex; align-items: center; }
    .vm-logo-mark { width: 26px; height: 26px; flex-shrink: 0; border-radius: 7px; display: block; margin-right: 9px; }
  `;
  document.head.appendChild(markStyle);

  document.addEventListener('DOMContentLoaded', function () {
    var logos = document.querySelectorAll('.topbar-logo');
    logos.forEach(function (logo) {
      if (logo.querySelector('.vm-logo-mark')) return;       // уже есть
      var uid = 'vmlg' + Math.random().toString(36).slice(2, 7);
      var svg =
        '<svg class="vm-logo-mark" viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">' +
          '<defs><linearGradient id="' + uid + '" x1="0" y1="0" x2="1" y2="1">' +
            '<stop offset="0" stop-color="#1e3a8a"/><stop offset="1" stop-color="#2563eb"/>' +
          '</linearGradient></defs>' +
          '<rect width="32" height="32" rx="8" fill="url(#' + uid + ')"/>' +
          '<path d="M7 25 H25 M7 25 V7" stroke="rgba(255,255,255,.45)" stroke-width="1.4" stroke-linecap="round" fill="none"/>' +
          '<path d="M8 9 Q16 30 24 9" stroke="#fff" stroke-width="2.4" fill="none" stroke-linecap="round"/>' +
        '</svg>';
      logo.insertAdjacentHTML('afterbegin', svg);
    });
  });

})();
