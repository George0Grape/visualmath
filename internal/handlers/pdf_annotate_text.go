package handlers

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Текст комментариев запекаем встроенным Go-шрифтом (в нём есть кириллица) —
// чтобы не тащить внешний .ttf. Раньше текст в PDF вообще не попадал, и
// преподавательские комментарии были видны только в веб-просмотрщике.
var (
	annotFont     *opentype.Font
	annotFontOnce sync.Once
)

func annotationFont() *opentype.Font {
	annotFontOnce.Do(func() {
		if f, err := opentype.Parse(goregular.TTF); err == nil {
			annotFont = f
		}
	})
	return annotFont
}

func newFace(sizePx float64) font.Face {
	f := annotationFont()
	if f == nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: sizePx, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return face
}

func textWidth(face font.Face, s string) int {
	return font.MeasureString(face, s).Round()
}

// wrapText жадно переносит текст по словам под maxW пикселей; уважает \n.
func wrapText(face font.Face, text string, maxW int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := ""
		for _, wd := range words {
			try := wd
			if cur != "" {
				try = cur + " " + wd
			}
			if cur == "" || textWidth(face, try) <= maxW {
				cur = try
			} else {
				lines = append(lines, cur)
				cur = wd
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}

func drawString(img *image.NRGBA, face font.Face, x, y int, s string, col color.Color) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// drawBox — заливка + рамка (для «стикера» комментария).
func drawBox(img *image.NRGBA, x, y, w, h int, bg, border color.NRGBA) {
	fx, fy := float64(x), float64(y)
	stampRect(img, fx, fy, float64(x+w), float64(y+h), bg)
	bt := 2.0
	stampRect(img, fx, fy, float64(x+w), fy+bt, border)               // верх
	stampRect(img, fx, float64(y+h)-bt, float64(x+w), float64(y+h), border) // низ
	stampRect(img, fx, fy, fx+bt, float64(y+h), border)              // лево
	stampRect(img, float64(x+w)-bt, fy, float64(x+w), float64(y+h), border) // право
}

// clampCoord держит координату в пределах страницы (защита от кривого JSON).
func clampCoord(v, max float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// drawCommentBox рисует «стикер» с текстом комментария у точки (ax, ay) в
// пикселях изображения, прижимая его к границам, чтобы не уехал за страницу.
func drawCommentBox(img *image.NRGBA, ax, ay float64, text string, scale float64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	face := newFace(13 * scale)
	if face == nil {
		return
	}
	pad := int(6 * scale)
	maxTextW := int(230 * scale)
	lines := wrapText(face, text, maxTextW)
	lineH := face.Metrics().Height.Ceil()
	boxW := 0
	for _, ln := range lines {
		if w := textWidth(face, ln); w > boxW {
			boxW = w
		}
	}
	boxW += pad * 2
	boxH := lineH*len(lines) + pad*2

	b := img.Bounds()
	x := int(ax) + int(12*scale)
	y := int(ay) + int(8*scale)
	if x+boxW > b.Max.X {
		x = b.Max.X - boxW - 1
	}
	if x < b.Min.X {
		x = b.Min.X
	}
	if y+boxH > b.Max.Y {
		y = b.Max.Y - boxH - 1
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}

	drawBox(img, x, y, boxW, boxH,
		color.NRGBA{R: 255, G: 249, B: 219, A: 244}, // бледно-жёлтый стикер
		color.NRGBA{R: 217, G: 119, B: 6, A: 255})   // янтарная рамка
	tx := x + pad
	ty := y + pad + face.Metrics().Ascent.Ceil()
	textCol := color.NRGBA{R: 31, G: 41, B: 55, A: 255}
	for _, ln := range lines {
		drawString(img, face, tx, ty, ln, textCol)
		ty += lineH
	}
}

// drawPinMarker рисует нумерованный маркер-пин остриём в точке (px, py).
func drawPinMarker(img *image.NRGBA, px, py float64, num int, scale float64) {
	r := 9 * scale
	cx := px
	cy := py - r // кружок над точкой, остриё в (px, py)
	stampLine(img, cx, cy, px, py, 2.5*scale, color.NRGBA{R: 245, G: 158, B: 11, A: 255})
	stampCircle(img, cx, cy, r+2, color.NRGBA{R: 146, G: 64, B: 14, A: 255}) // тёмная окантовка
	stampCircle(img, cx, cy, r, color.NRGBA{R: 245, G: 158, B: 11, A: 255})  // оранжевый
	face := newFace(11 * scale)
	if face != nil {
		s := strconv.Itoa(num)
		w := textWidth(face, s)
		ty := int(cy) + int(float64(face.Metrics().Ascent.Round())*0.34)
		drawString(img, face, int(cx)-w/2, ty, s, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	}
}
