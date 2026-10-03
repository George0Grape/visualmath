package handlers

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// minimalPDF — валидный одностраничный PDF с корректным xref (pdfcpu требует
// парсабельный файл для запекания).
func minimalPDF() []byte {
	var buf bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(body)
	}
	buf.WriteString("%PDF-1.4\n")
	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	stream := "BT /F1 24 Tf 72 720 Td (test) Tj ET"
	obj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(stream), stream))
	obj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	xrefPos := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefPos))
	return buf.Bytes()
}

// overlayToPNG должен рисовать пин и текст комментария (кириллица) — раньше
// текст в запечённый PDF не попадал вовсе.
func TestOverlayToPNG_DrawsPinAndCyrillicComment(t *testing.T) {
	pins := []PinData{{Page: 1, X: 100, Y: 200, Comment: "Тут ошибка в знаке — проверь производную"}}
	annots := []AnnotationItem{{Type: "underline", Page: 1, X1: 50, Y1: 400, X2: 200, Y2: 400, Comment: "См. формулу"}}
	strokes := []StrokeData{{Tool: "pen", Color: "#ef4444", Width: 3, Points: [][2]float64{{60, 100}, {160, 140}}}}

	pngData, drawn, err := overlayToPNG(strokes, pins, annots, 595, 842)
	if err != nil {
		t.Fatalf("overlayToPNG: %v", err)
	}
	if !drawn {
		t.Fatal("ожидали drawn=true")
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("PNG не декодируется: %v", err)
	}
	// считаем непрозрачные пиксели — должно быть заметное число (маркер + текст)
	b := img.Bounds()
	opaque := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				opaque++
			}
		}
	}
	if opaque < 500 {
		t.Errorf("слишком мало нарисовано (%d непрозрачных пикселей) — текст/пин не отрисовались", opaque)
	}

	// дамп для визуальной проверки (кириллица) — путь из env, иначе пропускаем
	if out := os.Getenv("VM_DUMP_PNG"); out != "" {
		os.WriteFile(out, pngData, 0644)
	}
}

// Полный запек: pdfcpu должен принять исходный PDF и выдать валидный PDF с
// наложенным оверлеем (мазки + пин + подписи).
func TestApplyAnnotationsToPDF_ProducesValidPDF(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.pdf")
	dst := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(src, minimalPDF(), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	pages := []PageAnnotations{{
		Page: 1, PageWidth: 595, PageHeight: 842,
		Strokes: []StrokeData{{Tool: "pen", Color: "#ef4444", Width: 3, Points: [][2]float64{{60, 100}, {160, 140}}}},
	}}
	pins := []PinData{{Page: 1, X: 100, Y: 200, Comment: "Проверь знак"}}
	annots := []AnnotationItem{{Type: "underline", Page: 1, X1: 50, Y1: 400, X2: 200, Y2: 400, Comment: "См. формулу"}}

	if err := applyAnnotationsToPDF(src, dst, pages, pins, annots); err != nil {
		t.Fatalf("applyAnnotationsToPDF: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil || info.Size() == 0 {
		t.Fatalf("выходной PDF пуст/отсутствует: %v", err)
	}
	// pdfcpu должен прочитать результат и увидеть 1 страницу
	n, err := api.PageCountFile(dst)
	if err != nil {
		t.Fatalf("pdfcpu не читает результат: %v", err)
	}
	if n != 1 {
		t.Errorf("ожидали 1 страницу, got %d", n)
	}
	// с оверлеем файл заметно больше исходного
	if srcInfo, _ := os.Stat(src); info.Size() <= srcInfo.Size() {
		t.Errorf("выходной PDF не больше исходного — оверлей не наложился")
	}
}
