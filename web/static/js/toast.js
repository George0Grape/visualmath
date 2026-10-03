(function () {
  var style = document.createElement('style');
  style.textContent = `
    #vm-toast-container {
      position: fixed; bottom: 24px; right: 24px; z-index: 99999;
      display: flex; flex-direction: column; gap: 10px;
      pointer-events: none;
    }
    .vm-toast {
      display: flex; align-items: center; gap: 10px;
      padding: 12px 16px; border-radius: 10px;
      font-family: 'DM Sans', sans-serif; font-size: 14px; font-weight: 500;
      box-shadow: 0 4px 16px rgba(0,0,0,.12), 0 1px 4px rgba(0,0,0,.08);
      pointer-events: all;
      min-width: 220px; max-width: 360px;
      animation: vm-toast-in .25s ease both;
      position: relative; overflow: hidden;
    }
    .vm-toast.hiding { animation: vm-toast-out .2s ease both; }
    .vm-toast-success { background: #f0fdf4; color: #166534; border: 1px solid #bbf7d0; }
    .vm-toast-error   { background: #fef2f2; color: #991b1b; border: 1px solid #fecaca; }
    .vm-toast-info    { background: #eff6ff; color: #1d4ed8; border: 1px solid #bfdbfe; }
    .vm-toast-warning { background: #fffbeb; color: #92400e; border: 1px solid #fde68a; }
    html[data-theme="dark"] .vm-toast-success { background: #14532d; color: #bbf7d0; border-color: #166534; }
    html[data-theme="dark"] .vm-toast-error   { background: #7f1d1d; color: #fecaca; border-color: #991b1b; }
    html[data-theme="dark"] .vm-toast-info    { background: #1e3a5f; color: #bfdbfe; border-color: #1d4ed8; }
    html[data-theme="dark"] .vm-toast-warning { background: #78350f; color: #fde68a; border-color: #92400e; }
    .vm-toast-icon { flex-shrink: 0; }
    .vm-toast-close {
      margin-left: auto; flex-shrink: 0; cursor: pointer;
      opacity: .5; transition: opacity .15s;
      background: none; border: none; color: inherit; font-size: 16px; line-height: 1;
      padding: 0 0 0 8px;
    }
    .vm-toast-close:hover { opacity: 1; }
    .vm-toast-progress {
      position: absolute; bottom: 0; left: 0; height: 2px;
      background: currentColor; opacity: .25;
      animation: vm-toast-progress linear both;
    }
    @keyframes vm-toast-in  { from { opacity:0; transform: translateX(24px); } to { opacity:1; transform: none; } }
    @keyframes vm-toast-out { from { opacity:1; transform: none; } to { opacity:0; transform: translateX(24px); } }
    @keyframes vm-toast-progress { from { width: 100%; } to { width: 0%; } }
  `;
  document.head.appendChild(style);

  var container;
  function getContainer() {
    if (!container) {
      container = document.createElement('div');
      container.id = 'vm-toast-container';
      document.body.appendChild(container);
    }
    return container;
  }

  var icons = {
    success: '<svg class="vm-toast-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>',
    error:   '<svg class="vm-toast-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>',
    info:    '<svg class="vm-toast-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>',
    warning: '<svg class="vm-toast-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
  };

  window.showToast = function (message, type, duration) {
    type = type || 'info';
    duration = duration || 3500;

    var toast = document.createElement('div');
    toast.className = 'vm-toast vm-toast-' + type;
    toast.innerHTML =
      (icons[type] || '') +
      '<span>' + String(message).replace(/</g, '&lt;') + '</span>' +
      '<button class="vm-toast-close" aria-label="Закрыть">&#x2715;</button>' +
      '<div class="vm-toast-progress" style="animation-duration:' + duration + 'ms"></div>';

    function dismiss() {
      toast.classList.add('hiding');
      toast.addEventListener('animationend', function () { toast.remove(); }, { once: true });
    }

    toast.querySelector('.vm-toast-close').addEventListener('click', dismiss);
    setTimeout(dismiss, duration);

    getContainer().appendChild(toast);
  };
})();
