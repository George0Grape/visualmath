/**
 * formatText(html) — обрабатывает **жирный** и *курсив* в тексте,
 * не трогая LaTeX-блоки ($...$, $$...$$, \(...\), \[...\]).
 *
 * Использование: text = formatText(text) перед вставкой в innerHTML.
 */
// Экранирует HTML-спецсимволы. Применяется ко ВСЕМУ вводу до разметки, поэтому
// любые теги/атрибуты пользователя становятся текстом (защита от XSS), а сама
// разметка ниже добавляет только свои теги. Для math-токенов это безопасно:
// браузер декодирует сущности в текстовом узле, и MathJax видит исходные символы.
function formatTextEscape(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function formatText(html) {
    if (!html || typeof html !== 'string') return html || '';

    html = formatTextEscape(html);

    // Разбиваем текст на токены: LaTeX-блоки оставляем как есть, остальное форматируем.
    // Порядок: сначала $$...$$, потом $...$, потом \[...\] и \(...\)
    var tokenPattern = /(\$\$[\s\S]*?\$\$|\$[^\$\n]+?\$|\\\[[\s\S]*?\\\]|\\\([\s\S]*?\\\))/g;
    var parts = html.split(tokenPattern);

    for (var i = 0; i < parts.length; i++) {
        // Нечётные индексы — это LaTeX-токены, пропускаем
        if (i % 2 === 1) continue;

        var s = parts[i];
        // ![alt](url) — изображения
        s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" style="max-width:100%;border-radius:8px;margin:8px 0;display:block">');
        // LaTeX текстовые команды вне math-режима
        s = s.replace(/\\textbf\{([^}]*)\}/g, '<strong>$1</strong>');
        s = s.replace(/\\textit\{([^}]*)\}/g, '<em>$1</em>');
        s = s.replace(/\\textsl\{([^}]*)\}/g, '<em>$1</em>');
        s = s.replace(/\\texttt\{([^}]*)\}/g, '<code style="font-family:monospace">$1</code>');
        s = s.replace(/\\textrm\{([^}]*)\}/g, '<span style="font-family:Georgia,serif">$1</span>');
        s = s.replace(/\\textsf\{([^}]*)\}/g, '<span style="font-family:system-ui,sans-serif">$1</span>');
        s = s.replace(/\\textsc\{([^}]*)\}/g, '<span style="font-variant:small-caps">$1</span>');
        s = s.replace(/\\textmd\{([^}]*)\}/g, '<span style="font-weight:400;font-style:normal">$1</span>');
        s = s.replace(/\\textup\{([^}]*)\}/g, '<span style="font-style:normal">$1</span>');
        s = s.replace(/\\textnormal\{([^}]*)\}/g, '$1');
        // **жирный** (сначала двойные звёздочки)
        s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
        // *курсив* (потом одинарные)
        s = s.replace(/\*(.+?)\*/g, '<em>$1</em>');
        parts[i] = s;
    }

    return parts.join('');
}
