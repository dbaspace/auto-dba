package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type captchaEntry struct {
	answer    string
	expiresAt time.Time
}

var (
	captchaMu    sync.Mutex
	captchaStore = map[string]captchaEntry{}
)

const captchaTTL = 5 * time.Minute

func purgeExpiredCaptchaLocked(now time.Time) {
	for id, e := range captchaStore {
		if now.After(e.expiresAt) {
			delete(captchaStore, id)
		}
	}
}

// Captcha GET /api/v1/auth/captcha — 简单加减算术图，无外部依赖。
func (h *AuthHandler) Captcha(c *gin.Context) {
	a, _ := rand.Int(rand.Reader, big.NewInt(9))
	b, _ := rand.Int(rand.Reader, big.NewInt(9))
	n1 := int(a.Int64()) + 1
	n2 := int(b.Int64()) + 1
	answer := fmt.Sprintf("%d", n1+n2)
	text := fmt.Sprintf("%d+%d=?", n1, n2)

	img := renderCaptchaPNG(text)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "验证码生成失败"})
		return
	}
	id := uuid.NewString()
	now := time.Now()
	captchaMu.Lock()
	purgeExpiredCaptchaLocked(now)
	captchaStore[id] = captchaEntry{answer: answer, expiresAt: now.Add(captchaTTL)}
	captchaMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"captcha_id":     id,
		"image_base64":   "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		"expires_in_sec": int(captchaTTL.Seconds()),
	})
}

// verifyCaptcha 校验并消耗一次性验证码；成功返回 true。
func verifyCaptcha(id, code string) bool {
	id = strings.TrimSpace(id)
	code = strings.TrimSpace(code)
	if id == "" || code == "" {
		return false
	}
	now := time.Now()
	captchaMu.Lock()
	defer captchaMu.Unlock()
	purgeExpiredCaptchaLocked(now)
	e, ok := captchaStore[id]
	if !ok {
		return false
	}
	delete(captchaStore, id)
	if now.After(e.expiresAt) {
		return false
	}
	return strings.EqualFold(e.answer, code)
}

func renderCaptchaPNG(text string) image.Image {
	const w, h = 140, 44
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 245, G: 247, B: 250, A: 255}}, image.Point{}, draw.Src)
	// 噪点
	for i := 0; i < 80; i++ {
		x, _ := rand.Int(rand.Reader, big.NewInt(w))
		y, _ := rand.Int(rand.Reader, big.NewInt(h))
		img.Set(int(x.Int64()), int(y.Int64()), color.RGBA{R: 180, G: 190, B: 200, A: 255})
	}
	fg := color.RGBA{R: 40, G: 60, B: 90, A: 255}
	drawText5x7(img, 12, 14, text, fg)
	return img
}

// 极简 5x7 点阵字体（数字与 + = ?）
var glyph5x7 = map[rune][7]string{
	'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	'3': {"01110", "10001", "00001", "00110", "00001", "10001", "01110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
	'+': {"00000", "00100", "00100", "11111", "00100", "00100", "00000"},
	'=': {"00000", "00000", "11111", "00000", "11111", "00000", "00000"},
	'?': {"01110", "10001", "00001", "00010", "00100", "00000", "00100"},
}

func drawText5x7(img *image.RGBA, x, y int, text string, fg color.Color) {
	scale := 3
	cx := x
	for _, r := range text {
		g, ok := glyph5x7[r]
		if !ok {
			cx += 4 * scale
			continue
		}
		for row := 0; row < 7; row++ {
			line := g[row]
			for col := 0; col < 5; col++ {
				if line[col] != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(cx+col*scale+dx, y+row*scale+dy, fg)
					}
				}
			}
		}
		cx += 6 * scale
	}
}
