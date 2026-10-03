package handlers

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Путь к Ghostscript кэшируется после первого вызова. sync.Once — потому что
// сюда приходят конкурентные HTTP-хендлеры (две одновременные сдачи = data race
// на обычной переменной).
var gsPath string
var gsOnce sync.Once

func findGhostscript() string {
	gsOnce.Do(func() {
		// на Linux бинарь называется gs, оставим запас на gswin под Windows-дев
		for _, name := range []string{"gs", "gswin64c", "gswin32c"} {
			if p, err := exec.LookPath(name); err == nil {
				gsPath = p
				break
			}
		}
		if gsPath == "" {
			log.Println("⚠️  Ghostscript не найден — PDF сохраняются без сжатия")
		}
	})
	return gsPath
}

// processPDFGrayscale переводит залитый PDF в оттенки серого, НЕ трогая
// разрешение картинок. Обрабатывает файл "на месте": пишет результат во
// временный файл и подменяет оригинал только если получилось и стало меньше.
//
// Раньше тут был ещё даунсэмплинг до 150 dpi + пресет /ebook — преподаватели
// пожаловались, что сканы становятся нечитаемыми. Оставили только конверсию в
// серый: она и так уменьшает размер (3 канала → 1), а разрешение сохраняется.
//
// Главный принцип — НИКОГДА не терять работу студента: при любой ошибке
// (нет gs, битый PDF, таймаут, результат больше оригинала) оставляем оригинал
// как есть. Сжатие — это бонус, а не обязательное условие приёма работы.
func processPDFGrayscale(path string) {
	gs := findGhostscript()
	if gs == "" {
		return // Ghostscript недоступен — оставляем оригинал
	}

	origInfo, err := os.Stat(path)
	if err != nil {
		return
	}

	tmp := path + ".gs.tmp"
	defer os.Remove(tmp) // подчистим, даже если подмена не состоялась

	// 60 секунд более чем достаточно для типичной работы; защита от зависания
	// на специально сломанном/огромном PDF.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, gs,
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.5",
		// перевод всех цветов в оттенки серого — единственная цель
		"-sColorConversionStrategy=Gray",
		"-dProcessColorModel=/DeviceGray",
		// разрешение картинок НЕ трогаем: никакого даунсэмплинга
		"-dDownsampleColorImages=false",
		"-dDownsampleGrayImages=false",
		"-dDownsampleMonoImages=false",
		// уже сжатые JPEG не перекодируем повторно (без лишней потери качества)
		"-dPassThroughJPEGImages=true",
		"-dNOPAUSE", "-dBATCH", "-dQUIET", "-dSAFER",
		"-sOutputFile="+tmp,
		// Качество JPEG при перекодировке фото в серый: авто-фильтр использует
		// ACS-словари (QFactor в обычном GrayImageDict он игнорирует). QFactor 0.2
		// ≈ вдвое больше данных, чем дефолтные 0.75 → чётче. Линию-арт/текст
		// авто-фильтр всё равно оставляет lossless (Flate) — артефактов нет.
		"-c", "<< /ColorACSImageDict << /QFactor 0.2 /Blend 1 /HSamples [1 1 1 1] /VSamples [1 1 1 1] >> /GrayACSImageDict << /QFactor 0.2 /Blend 1 /HSamples [1 1 1 1] /VSamples [1 1 1 1] >> >> setdistillerparams",
		"-f", path,
	)

	if err := cmd.Run(); err != nil {
		log.Printf("PDF: gs не смог обработать %s: %v — оставляю оригинал", path, err)
		return
	}

	newInfo, err := os.Stat(tmp)
	if err != nil || newInfo.Size() == 0 {
		return // gs ничего вменяемого не выдал
	}

	// Подменяем только если реально стало меньше. Для «настоящих» векторных PDF
	// (LaTeX/Word) gs нередко даёт файл такого же или большего размера — тогда
	// смысла в подмене нет.
	if newInfo.Size() >= origInfo.Size() {
		log.Printf("PDF: %s не уменьшился (%d → %d), оставляю оригинал",
			path, origInfo.Size(), newInfo.Size())
		return
	}

	if err := os.Rename(tmp, path); err != nil {
		log.Printf("PDF: не удалось заменить %s: %v — оставляю оригинал", path, err)
		return
	}

	log.Printf("PDF: %s сжат %d → %d байт (−%.0f%%)",
		path, origInfo.Size(), newInfo.Size(),
		100*(1-float64(newInfo.Size())/float64(origInfo.Size())))
}
