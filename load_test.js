/**
 * k6 нагрузочный тест для VisualMath
 *
 * Запуск:
 *   k6 run load_test.js
 *   k6 run --env BASE_URL=http://45.9.43.42:8080 load_test.js
 *   k6 run --env STAGE=smoke load_test.js        # быстрая проверка
 *   k6 run --env STAGE=stress load_test.js       # максимальная нагрузка
 *   k6 run --env STAGE=spike load_test.js        # внезапный скачок
 */

import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep, group, fail } from 'k6';
import { Counter, Rate, Trend, Gauge } from 'k6/metrics';

// ── Конфигурация ──────────────────────────────────────────────────────────────
const BASE_URL   = (__ENV.BASE_URL  || 'http://45.9.43.42:8080').replace(/\/$/, '');
const WS_URL     = BASE_URL.replace(/^http/, 'ws');
const STAGE      = __ENV.STAGE || 'load';

const TEACHER_LOGIN    = __ENV.TEACHER_LOGIN    || 'teacher';
const TEACHER_PASSWORD = __ENV.TEACHER_PASSWORD || 'test';
const STUDENT_LOGIN    = __ENV.STUDENT_LOGIN    || 'student';
const STUDENT_PASSWORD = __ENV.STUDENT_PASSWORD || 'test';

// ── Метрики ───────────────────────────────────────────────────────────────────
const loginErrors     = new Counter('login_errors');
const apiErrors       = new Counter('api_errors');
const wsConnects      = new Counter('ws_connects');
const wsErrors        = new Counter('ws_errors');
const lectureReadTime = new Trend('lecture_read_duration', true);
const commentPostRate = new Rate('comment_post_success');

// ── Профили нагрузки ──────────────────────────────────────────────────────────
const PROFILES = {
  smoke: {
    // Smoke: минимум, проверяем что всё живо
    scenarios: {
      smoke: {
        executor: 'per-vu-iterations',
        vus: 3,
        iterations: 5,
        maxDuration: '60s',
        exec: 'studentBrowse',
      },
    },
    thresholds: {
      http_req_duration:  ['p(95)<2000'],
      http_req_failed:    ['rate<0.05'],
    },
  },

  load: {
    // Load: нормальная нагрузка
    scenarios: {
      students: {
        executor: 'ramping-vus',
        startVUs: 0,
        stages: [
          { duration: '30s', target: 20  },
          { duration: '2m',  target: 50  },
          { duration: '1m',  target: 50  },
          { duration: '30s', target: 0   },
        ],
        exec: 'studentScenario',
        gracefulRampDown: '15s',
      },
      teachers: {
        executor: 'constant-vus',
        vus: 3,
        duration: '4m',
        exec: 'teacherScenario',
        startTime: '30s',
      },
      anon_browse: {
        executor: 'ramping-arrival-rate',
        startRate: 5,
        timeUnit: '1s',
        preAllocatedVUs: 10,
        maxVUs: 30,
        stages: [
          { duration: '1m',  target: 10 },
          { duration: '2m',  target: 20 },
          { duration: '1m',  target: 5  },
        ],
        exec: 'anonScenario',
        startTime: '15s',
      },
    },
    thresholds: {
      http_req_duration:          ['p(95)<3000', 'p(99)<5000'],
      http_req_failed:            ['rate<0.05'],
      'http_req_duration{type:api}': ['p(95)<500'],
      login_errors:               ['count<5'],
    },
  },

  stress: {
    // Stress: ищем предел
    scenarios: {
      stress: {
        executor: 'ramping-vus',
        startVUs: 0,
        stages: [
          { duration: '1m',  target: 50  },
          { duration: '2m',  target: 100 },
          { duration: '2m',  target: 200 },
          { duration: '2m',  target: 300 },
          { duration: '1m',  target: 100 },
          { duration: '30s', target: 0   },
        ],
        exec: 'studentBrowse',
      },
    },
    thresholds: {
      http_req_duration: ['p(95)<8000'],
      http_req_failed:   ['rate<0.2'],
    },
  },

  spike: {
    // Spike: резкий выброс трафика (например, все студенты заходят в 9:00)
    scenarios: {
      base: {
        executor: 'constant-vus',
        vus: 10,
        duration: '5m',
        exec: 'studentBrowse',
      },
      spike: {
        executor: 'ramping-vus',
        startVUs: 0,
        stages: [
          { duration: '10s', target: 0   },
          { duration: '10s', target: 150 }, // резкий скачок
          { duration: '1m',  target: 150 },
          { duration: '10s', target: 0   },
          { duration: '3m',  target: 0   },
        ],
        exec: 'studentBrowse',
        startTime: '1m',
      },
    },
    thresholds: {
      http_req_duration: ['p(95)<5000'],
      http_req_failed:   ['rate<0.1'],
    },
  },
};

export const options = PROFILES[STAGE] || PROFILES.load;

// ── Helpers ───────────────────────────────────────────────────────────────────
function pick(arr) { return arr[Math.floor(Math.random() * arr.length)]; }

function authHdr(token) {
  return {
    headers: { 'Authorization': `Bearer ${token}`, 'Content-Type': 'application/json' },
    tags:    { type: 'api' },
  };
}

function pageHdr(token) {
  return {
    headers: token ? { 'Authorization': `Bearer ${token}` } : {},
    tags:    { type: 'page' },
  };
}

function apiGet(token, path) {
  const res = http.get(`${BASE_URL}${path}`, authHdr(token));
  if (res.status >= 500) apiErrors.add(1);
  return res;
}

function apiPost(token, path, body) {
  const res = http.post(
    `${BASE_URL}${path}`,
    JSON.stringify(body),
    authHdr(token),
  );
  if (res.status >= 500) apiErrors.add(1);
  return res;
}

// ── Setup: логинимся один раз, возвращаем токены и данные ────────────────────
export function setup() {
  // Логин преподавателя
  const teacherRes = http.post(`${BASE_URL}/api/login`,
    JSON.stringify({ login: TEACHER_LOGIN, password: TEACHER_PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (teacherRes.status !== 200) {
    loginErrors.add(1);
    console.error(`Teacher login failed (${teacherRes.status}): ${teacherRes.body}`);
  }
  const teacherToken = teacherRes.status === 200
    ? JSON.parse(teacherRes.body).token : null;

  // Логин студента
  const studentRes = http.post(`${BASE_URL}/api/login`,
    JSON.stringify({ login: STUDENT_LOGIN, password: STUDENT_PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (studentRes.status !== 200) loginErrors.add(1);
  const studentToken = studentRes.status === 200
    ? JSON.parse(studentRes.body).token : null;

  // Получаем список опубликованных лекций
  let lectureIDs = [];
  if (teacherToken) {
    const lr = http.get(`${BASE_URL}/api/lectures/published`, authHdr(teacherToken));
    if (lr.status === 200) {
      const data = JSON.parse(lr.body);
      const lectures = data.lectures || data || [];
      lectureIDs = lectures.slice(0, 10).map(l => l.id).filter(Boolean);
    }
  }

  // Сессия WebSocket (если есть активная)
  let activeSessionID = null;
  if (studentToken) {
    const sr = http.get(`${BASE_URL}/api/sessions/active`, authHdr(studentToken));
    if (sr.status === 200) {
      try {
        const sessions = JSON.parse(sr.body);
        if (Array.isArray(sessions) && sessions.length > 0) {
          activeSessionID = sessions[0].session_id || sessions[0].id;
        }
      } catch(_) {}
    }
  }

  console.log(`Setup done. Lectures: ${lectureIDs.length}, WS session: ${activeSessionID || 'none'}`);
  return { teacherToken, studentToken, lectureIDs, activeSessionID };
}

// ── Сценарий: анонимный браузинг (не авторизован) ─────────────────────────────
export function anonScenario() {
  group('anon_browse', () => {
    // Главная страница → редирект на /login
    http.get(`${BASE_URL}/`, { tags: { type: 'page' } });
    sleep(0.5);

    // Страница входа
    const loginPage = http.get(`${BASE_URL}/login`, { tags: { type: 'page' } });
    check(loginPage, { 'login page ok': r => r.status === 200 });
    sleep(Math.random() * 1 + 0.5);

    // Статика
    http.batch([
      ['GET', `${BASE_URL}/static/js/theme.js`,    null, { tags: { type: 'static' } }],
      ['GET', `${BASE_URL}/static/js/toast.js`,    null, { tags: { type: 'static' } }],
      ['GET', `${BASE_URL}/static/favicon.svg`,    null, { tags: { type: 'static' } }],
    ]);
    sleep(0.3);
  });
}

// ── Сценарий: студент листает лекции и Classroom ──────────────────────────────
export function studentBrowse(data) {
  const token = data.studentToken;
  if (!token) { sleep(2); return; }

  const action = pick(['lectures', 'profile', 'classroom', 'dashboard', 'lecture_read']);

  if (action === 'dashboard') {
    group('student_dashboard', () => {
      const r = http.get(`${BASE_URL}/dashboard`, pageHdr(token));
      check(r, { 'dashboard ok': res => res.status === 200 });
      sleep(0.5);
      // Значок уведомлений
      apiGet(token, '/api/notifications/badges');
      sleep(Math.random() * 2 + 1);
    });
    return;
  }

  if (action === 'lectures') {
    group('browse_lectures', () => {
      const r = http.get(`${BASE_URL}/lectures/published`, pageHdr(token));
      check(r, { 'lectures page ok': res => res.status === 200 });
      sleep(0.5);
      // Список через API
      const api = apiGet(token, '/api/lectures/published');
      check(api, { 'lectures api ok': res => res.status === 200 });
      sleep(Math.random() * 2 + 1);
    });
    return;
  }

  if (action === 'lecture_read' && data.lectureIDs.length > 0) {
    group('read_lecture', () => {
      const lid = pick(data.lectureIDs);
      const start = Date.now();
      const page = http.get(`${BASE_URL}/lectures/published/${lid}`, pageHdr(token));
      check(page, { 'reader page ok': res => res.status === 200 });
      sleep(0.3);
      const api = apiGet(token, `/api/lectures/${lid}`);
      lectureReadTime.add(Date.now() - start);
      check(api, { 'lecture api ok': res => res.status === 200 });
      sleep(0.5);
      // Комментарии
      const cr = apiGet(token, `/api/lectures/${lid}/comments`);
      check(cr, { 'comments ok': res => res.status === 200 || res.status === 404 });
      sleep(Math.random() * 5 + 3);  // «читает» слайды
    });
    return;
  }

  if (action === 'classroom') {
    group('classroom', () => {
      const r = http.get(`${BASE_URL}/classroom/subjects`, pageHdr(token));
      check(r, { 'classroom ok': res => res.status === 200 });
      sleep(Math.random() * 2 + 1);
    });
    return;
  }

  if (action === 'profile') {
    group('profile', () => {
      const r = http.get(`${BASE_URL}/profile`, pageHdr(token));
      check(r, { 'profile page ok': res => res.status === 200 });
      sleep(0.3);
      const api = apiGet(token, '/api/profile');
      check(api, { 'profile api ok': res => res.status === 200 });
      sleep(Math.random() * 1.5 + 0.5);
    });
    return;
  }

  sleep(1);
}

// ── Сценарий: полный путь студента ────────────────────────────────────────────
export function studentScenario(data) {
  const token = data.studentToken;
  if (!token) { sleep(3); return; }

  // 1. Открывает дашборд
  group('student_full', () => {
    http.get(`${BASE_URL}/dashboard`, pageHdr(token));
    sleep(0.5);
    apiGet(token, '/api/notifications/badges');
    sleep(1);

    // 2. Идёт в Classroom
    http.get(`${BASE_URL}/classroom/subjects`, pageHdr(token));
    sleep(Math.random() * 1 + 0.5);

    // 3. Читает лекцию
    if (data.lectureIDs.length > 0) {
      const lid = pick(data.lectureIDs);
      http.get(`${BASE_URL}/lectures/published/${lid}`, pageHdr(token));
      sleep(0.5);
      apiGet(token, `/api/lectures/${lid}`);
      apiGet(token, `/api/lectures/${lid}/comments`);

      // 4. Пишет комментарий (20% времени)
      if (Math.random() < 0.2) {
        const cr = apiPost(token, `/api/lectures/${lid}/comments`, {
          text: `Тест нагрузки ${Date.now()}`,
        });
        commentPostRate.add(cr.status === 200);
      }

      sleep(Math.random() * 3 + 2);

      // 5. Отмечает модуль завершённым
      const lectRes = apiGet(token, `/api/lectures/${lid}`);
      if (lectRes.status === 200) {
        try {
          const modules = JSON.parse(lectRes.body).modules || [];
          if (modules.length > 0) {
            apiPost(token, '/api/lectures/complete', {
              lecture_id: lid,
              module_id:  modules[0].id,
              score:      Math.random(),
            });
          }
        } catch(_) {}
      }
    }

    // 6. Профиль
    sleep(Math.random() * 1 + 0.5);
    apiGet(token, '/api/profile');

    // 7. Активные сессии
    apiGet(token, '/api/sessions/active');

    // 8. Подключается к WebSocket если есть сессия
    if (data.activeSessionID && Math.random() < 0.3) {
      wsConnects.add(1);
      ws.connect(
        `${WS_URL}/ws/session/${data.activeSessionID}/student`,
        { headers: { 'Authorization': `Bearer ${token}` } },
        (socket) => {
          socket.on('open', () => sleep(Math.random() * 5 + 2));
          socket.on('error', (e) => { wsErrors.add(1); });
          socket.setTimeout(() => socket.close(), 10000);
        }
      );
    }
  });

  sleep(Math.random() * 2 + 1);
}

// ── Сценарий: преподаватель управляет контентом ───────────────────────────────
export function teacherScenario(data) {
  const token = data.teacherToken;
  if (!token) { sleep(3); return; }

  group('teacher_flow', () => {
    // 1. Дашборд
    http.get(`${BASE_URL}/dashboard`, pageHdr(token));
    sleep(0.5);

    // 2. Список лекций
    const la = apiGet(token, '/api/lectures');
    check(la, { 'lectures list': r => r.status === 200 });
    sleep(1);

    // 3. Список модулей
    const ma = apiGet(token, '/api/modules/list');
    check(ma, { 'modules list': r => r.status === 200 });
    sleep(Math.random() * 1 + 0.5);

    // 4. Опубликованные лекции
    const pl = apiGet(token, '/api/lectures/published');
    check(pl, { 'published': r => r.status === 200 });

    // 5. Просмотр лекции (если есть)
    if (data.lectureIDs.length > 0) {
      const lid = pick(data.lectureIDs);
      apiGet(token, `/api/lectures/${lid}`);
      sleep(0.5);
      apiGet(token, `/api/lectures/${lid}/comments`);
    }

    // 6. Classroom
    sleep(1);
    http.get(`${BASE_URL}/classroom/subjects`, pageHdr(token));
    sleep(Math.random() * 2 + 1);

    // 7. Профиль
    apiGet(token, '/api/profile');
    sleep(1);

    // 8. Активные сессии (препод видит своих студентов)
    apiGet(token, '/api/sessions/active');
    sleep(Math.random() * 2 + 1);
  });
}

// ── Teardown ──────────────────────────────────────────────────────────────────
export function teardown(data) {
  console.log('Test complete.');
  console.log(`  WS connects: ${wsConnects.count || 0}`);
  console.log(`  WS errors:   ${wsErrors.count || 0}`);
  console.log(`  API errors:  ${apiErrors.count || 0}`);
  console.log(`  Login errors: ${loginErrors.count || 0}`);
}
