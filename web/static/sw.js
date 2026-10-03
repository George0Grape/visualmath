/* VisualMath service worker — установка как PWA + офлайн.
   Стратегии:
   - /static/* (шрифты, MathJax, Plotly, JS/CSS, иконки) — cache-first (они
     неизменны/версионированы; ускоряет повторные загрузки и работает офлайн).
   - навигации по /lectures/published* — network-first с записью копии в кэш,
     чтобы опубликованные лекции открывались офлайн.
   - прочие навигации — network-first без кэширования (не кэшируем приватные
     дашборды/чужие данные); офлайн-фолбэк — простая страница.
   НЕ трогаем /api/*, /ws/*, не-GET — пропускаем в сеть как есть. */

const VERSION = 'vm-v1';
const STATIC_CACHE = 'vm-static-' + VERSION;
const PAGE_CACHE = 'vm-pages-' + VERSION;

// Лёгкий app-shell, прекэшируем при установке (без тяжёлого MathJax/Plotly —
// они докэшируются runtime-ом при первом использовании).
const PRECACHE = [
  '/static/js/mobile-nav.js',
  '/static/js/theme.js',
  '/static/js/ui.js',
  '/static/vendor/fonts/fonts.css',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
  '/static/manifest.webmanifest',
];

const OFFLINE_HTML =
  '<!doctype html><html lang="ru"><head><meta charset="utf-8">' +
  '<meta name="viewport" content="width=device-width,initial-scale=1">' +
  '<title>Офлайн — VisualMath</title>' +
  '<style>body{font-family:system-ui,sans-serif;background:#f4f5f7;color:#1a1d23;' +
  'display:flex;min-height:100vh;margin:0;align-items:center;justify-content:center;text-align:center}' +
  '.b{max-width:340px;padding:32px}h1{font-size:20px;margin:0 0 8px}p{color:#6b7280;font-size:14px;line-height:1.6}' +
  'a{color:#2563eb;text-decoration:none;font-weight:600}</style></head>' +
  '<body><div class="b"><h1>Нет соединения</h1>' +
  '<p>Эта страница недоступна офлайн. Откройте ранее просмотренные опубликованные лекции или вернитесь, когда появится сеть.</p>' +
  '<p><a href="/lectures/published">К опубликованным лекциям</a></p></div></body></html>';

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE)
      .then((c) => c.addAll(PRECACHE).catch(() => {})) // не валим установку, если что-то не закэшировалось
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.filter((k) => k !== STATIC_CACHE && k !== PAGE_CACHE).map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;       // сторонние — мимо SW
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws/')) return;

  // Статика — cache-first
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(req).then((hit) =>
        hit || fetch(req).then((res) => {
          const copy = res.clone();
          caches.open(STATIC_CACHE).then((c) => c.put(req, copy));
          return res;
        }).catch(() => hit)
      )
    );
    return;
  }

  // Навигации
  if (req.mode === 'navigate') {
    const cachePublished = url.pathname.startsWith('/lectures/published');
    event.respondWith(
      fetch(req)
        .then((res) => {
          if (cachePublished && res.ok) {
            const copy = res.clone();
            caches.open(PAGE_CACHE).then((c) => c.put(req, copy));
          }
          return res;
        })
        .catch(() =>
          caches.match(req).then((hit) =>
            hit || new Response(OFFLINE_HTML, { headers: { 'Content-Type': 'text/html; charset=utf-8' } })
          )
        )
    );
  }
});
