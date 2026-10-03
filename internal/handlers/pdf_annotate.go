package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// applyAnnotationsToPDF накладывает поверх PDF мазки, маркеры-пины и ТЕКСТ
// комментариев: растеризуем всё в PNG на каждую страницу и прибиваем через
// pdfcpu ImageWatermark. Пины и подписи к подчёркиваниям/хайлайтам раньше жили
// только в веб-просмотрщике — в скачанном файле их не было.
func applyAnnotationsToPDF(src, dst string, pages []PageAnnotations, pins []PinData, annotations []AnnotationItem) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read src: %w", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("write dst: %w", err)
	}

	conf := model.NewDefaultConfiguration()

	for _, p := range pages {
		// размеры приходят из клиентского JSON — зажимаем в разумные пределы,
		// иначе page_width=1e9 заставит NewNRGBA выделить гигантский буфер (OOM)
		pdfW := p.PageWidth
		if pdfW < 1 || pdfW > 5000 {
			pdfW = 595
		}
		pdfH := p.PageHeight
		if pdfH < 1 || pdfH > 5000 {
			pdfH = 842
		}

		// собираем пины и комментированные аннотации этой страницы
		var pagePins []PinData
		for _, pin := range pins {
			if pin.Page == p.Page {
				pagePins = append(pagePins, pin)
			}
		}
		var pageAnnots []AnnotationItem
		for _, a := range annotations {
			if a.Page == p.Page && strings.TrimSpace(a.Comment) != "" {
				pageAnnots = append(pageAnnots, a)
			}
		}
		if len(p.Strokes) == 0 && len(pagePins) == 0 && len(pageAnnots) == 0 {
			continue
		}

		pngData, drawn, err := overlayToPNG(p.Strokes, pagePins, pageAnnots, pdfW, pdfH)
		if err != nil {
			log.Printf("annotation: overlayToPNG page %d: %v", p.Page, err)
			continue
		}
		if !drawn {
			continue
		}
		wm, err := api.ImageWatermarkForReader(
			bytes.NewReader(pngData),
			"scale:1 rel, pos:tl, rot:0, op:1",
			true, false, types.POINTS,
		)
		if err != nil {
			log.Printf("annotation: ImageWatermarkForReader page %d: %v", p.Page, err)
			continue
		}

		pageStr := fmt.Sprintf("%d", p.Page)
		if err := api.AddWatermarksFile(dst, dst+".tmp", []string{pageStr}, wm, conf); err != nil {
			log.Printf("annotation: AddWatermarksFile page %d: %v", p.Page, err)
			continue
		}
		if err := os.Rename(dst+".tmp", dst); err != nil {
			log.Printf("annotation: Rename page %d: %v", p.Page, err)
		}
	}

	return nil
}

// overlayToPNG рисует на одну страницу: мазки + нумерованные пины с текстом +
// подписи к комментированным подчёркиваниям/хайлайтам. drawn=false если рисовать
// нечего (тогда страницу пропускаем).
func overlayToPNG(strokes []StrokeData, pins []PinData, annots []AnnotationItem, pdfW, pdfH float64) ([]byte, bool, error) {
	const scale = 2.0
	w := int(math.Ceil(pdfW * scale))
	h := int(math.Ceil(pdfH * scale))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	drawn := false
	if len(strokes) > 0 {
		drawStrokes(img, strokes, scale)
		drawn = true
	}
	for i, p := range pins {
		px := clampCoord(p.X, pdfW) * scale
		py := clampCoord(p.Y, pdfH) * scale
		drawPinMarker(img, px, py, i+1, scale)
		drawCommentBox(img, px, py, p.Comment, scale)
		drawn = true
	}
	for _, a := range annots {
		if strings.TrimSpace(a.Comment) == "" {
			continue
		}
		ax := clampCoord(a.X1, pdfW) * scale
		ay := clampCoord(a.Y1, pdfH) * scale
		drawCommentBox(img, ax, ay, a.Comment, scale)
		drawn = true
	}
	if !drawn {
		return nil, false, nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

// strokesToPNG рисует мазки в RGBA-PNG (pdfW*scale × pdfH*scale пикселей).
// Фон прозрачный, линии — stamping кружками вдоль пути.
func strokesToPNG(strokes []StrokeData, pdfW, pdfH float64) ([]byte, error) {
	const scale = 2.0
	w := int(math.Ceil(pdfW * scale))
	h := int(math.Ceil(pdfH * scale))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	drawStrokes(img, strokes, scale)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawStrokes рисует мазки (ручка/маркер/подчёркивание/хайлайт) на изображении.
func drawStrokes(img *image.NRGBA, strokes []StrokeData, scale float64) {
	for _, s := range strokes {
		r, g, b := parseHexColor(s.Color)
		op := s.Opacity
		if op <= 0 || op > 1 || math.IsNaN(op) {
			op = 1.0
		}
		if s.Tool == "marker" {
			op *= 0.4
		}
		a := uint8(op * 255)
		col := color.NRGBA{R: r, G: g, B: b, A: a}
		// толщина тоже клиентская: без клампа width=1e9 даёт circle-loop на
		// миллиарды пикселей на каждую точку (CPU DoS)
		width := s.Width
		if width < 0 || width > 100 || math.IsNaN(width) {
			width = 3
		}
		radius := width * scale / 2

		switch s.Tool {
		case "pen", "marker":
			if len(s.Points) >= 2 {
				for i, pt := range s.Points {
					x := pt[0] * scale
					y := pt[1] * scale
					stampCircle(img, x, y, radius, col)
					if i > 0 {
						px := s.Points[i-1][0] * scale
						py := s.Points[i-1][1] * scale
						stampLine(img, px, py, x, y, radius, col)
					}
				}
			}
		case "underline":
			if len(s.Points) >= 2 {
				x1 := s.Points[0][0] * scale
				y1 := s.Points[0][1] * scale
				x2 := s.Points[len(s.Points)-1][0] * scale
				stampLine(img, x1, y1, x2, y1, radius, col)
			}
		case "highlight":
			if len(s.Points) >= 2 {
				x1 := s.Points[0][0] * scale
				y1 := s.Points[0][1] * scale
				x2 := s.Points[1][0] * scale
				y2 := s.Points[1][1] * scale
				// жёлтый полупрозрачный прямоугольник (~30% opacity)
				hCol := color.NRGBA{R: 251, G: 191, B: 36, A: 80}
				stampRect(img, x1, y1, x2, y2, hCol)
			}
		// "text" (свободный текст-заметки) сюда не приходит — пины и подписи
		// рисуются отдельно в overlayToPNG
		}
	}
}

// stampCircle рисует закрашенный круг с лёгким anti-aliasing (coverage на границе).
func stampCircle(img *image.NRGBA, cx, cy, r float64, col color.NRGBA) {
	x0 := int(math.Floor(cx - r - 1))
	x1 := int(math.Ceil(cx + r + 1))
	y0 := int(math.Floor(cy - r - 1))
	y1 := int(math.Ceil(cy + r + 1))
	bounds := img.Bounds()
	for py := y0; py <= y1; py++ {
		if py < bounds.Min.Y || py >= bounds.Max.Y {
			continue
		}
		for px := x0; px <= x1; px++ {
			if px < bounds.Min.X || px >= bounds.Max.X {
				continue
			}
			dx := float64(px) + 0.5 - cx
			dy := float64(py) + 0.5 - cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > r+1 {
				continue
			}
			// coverage: 1.0 inside, плавный спад на краю
			cov := 1.0 - math.Max(0, dist-r)
			if cov <= 0 {
				continue
			}
			blendPixel(img, px, py, col, cov)
		}
	}
}

// stampRect заполняет прямоугольник заданным цветом (для highlight).
func stampRect(img *image.NRGBA, x1, y1, x2, y2 float64, col color.NRGBA) {
	left := int(math.Min(x1, x2))
	right := int(math.Ceil(math.Max(x1, x2)))
	top := int(math.Min(y1, y2))
	bottom := int(math.Ceil(math.Max(y1, y2)))
	bounds := img.Bounds()
	for py := top; py <= bottom; py++ {
		if py < bounds.Min.Y || py >= bounds.Max.Y {
			continue
		}
		for px := left; px <= right; px++ {
			if px < bounds.Min.X || px >= bounds.Max.X {
				continue
			}
			blendPixel(img, px, py, col, 1.0)
		}
	}
}

// stampLine рисует отрезок с округлёнными капами, ставя диски вдоль пути.
func stampLine(img *image.NRGBA, x1, y1, x2, y2, r float64, col color.NRGBA) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if !(length > 0) { // отсекает и 0, и NaN
		return
	}
	// шаг = половина радиуса для хорошего покрытия
	step := math.Max(r*0.5, 0.5)
	n := int(length/step) + 1
	// координаты клиентские: без капа отрезок до x=1e12 даст миллиарды итераций.
	// Легитимный максимум (диагональ A4 при scale 2, шаг 0.5) ≈ 7000 штампов.
	if n > 20000 {
		n = 20000
	}
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		px := x1 + dx*t
		py := y1 + dy*t
		stampCircle(img, px, py, r, col)
	}
}

// blendPixel смешивает col поверх текущего пикселя через "over" compositing.
func blendPixel(img *image.NRGBA, x, y int, col color.NRGBA, coverage float64) {
	off := img.PixOffset(x, y)
	pix := img.Pix[off : off+4]

	srcA := float64(col.A) * coverage / 255.0
	dstA := float64(pix[3]) / 255.0

	outA := srcA + dstA*(1-srcA)
	if outA == 0 {
		return
	}
	// pre-multiplied "over"
	pix[0] = uint8((float64(col.R)*srcA + float64(pix[0])*dstA*(1-srcA)) / outA)
	pix[1] = uint8((float64(col.G)*srcA + float64(pix[1])*dstA*(1-srcA)) / outA)
	pix[2] = uint8((float64(col.B)*srcA + float64(pix[2])*dstA*(1-srcA)) / outA)
	pix[3] = uint8(outA * 255)
}

// parseHexColor разбирает "#RRGGBB" или "#RGB" в r, g, b компоненты.
func parseHexColor(hex string) (r, g, b uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) < 6 {
		return 255, 0, 0
	}
	ri, _ := strconv.ParseUint(hex[0:2], 16, 8)
	gi, _ := strconv.ParseUint(hex[2:4], 16, 8)
	bi, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return uint8(ri), uint8(gi), uint8(bi)
}

// pxToPoints пересчитывает пиксели canvas в PDF points.
func pxToPoints(px, canvasSize, pdfSize float64) float64 {
	if canvasSize == 0 {
		return px
	}
	return math.Round((px/canvasSize)*pdfSize*100) / 100
}
