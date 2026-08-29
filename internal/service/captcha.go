package service

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"strings"
	"sync"
	"time"

	"kypanel/internal/model"
)

// ---- 登录图形验证码 ----
// 内存维护 captcha 状态（IP + 用户名维度），密码错误 1 次后下一次登录必须带 captcha。
// 验证码有效期 5 分钟，最大同时存活 10000 条（超过则淘汰最旧）。
// 图片渲染使用纯标准库 image/draw，字符用内置的 5x7 点阵字模（自研，见 font5x7.go）。

const (
	captchaLen        = 4
	captchaExpire     = 5 * time.Minute
	captchaMaxEntries = 10000
	captchaWidth      = 150
	captchaHeight     = 50
	captchaScale      = 3 // 每个字模像素放大倍数（5x7 字模 → 15x21 字符）
)

// captchaCharColor 字符颜色（深色，确保可读）
var captchaCharColor = color.RGBA{40, 50, 80, 255}

// captchaEntry 验证码存储条目
type captchaEntry struct {
	code      string
	expiresAt time.Time
}

var (
	captchaStore   = make(map[string]*captchaEntry)
	captchaStoreMu sync.Mutex
	captchaCharset = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789abcdefghjkmnpqrstuvwxyz") // 排除易混：0/O/1/I/l
	bgColor        = color.RGBA{245, 247, 250, 255}
	whiteColor     = color.RGBA{255, 255, 255, 255}
	lineColors     = []color.RGBA{
		{220, 100, 100, 255}, {100, 180, 120, 255}, {100, 130, 200, 255},
		{200, 160, 80, 255}, {160, 100, 200, 255}, {80, 160, 200, 255},
	}
)

// captchaKey 验证码按 IP 维度存储（不绑定账号）
func captchaKey(ip string) string {
	return ip
}

// GenerateCaptchaCode 生成随机 4 位字符串（crypto/rand 安全源）
func GenerateCaptchaCode() (string, error) {
	buf := make([]rune, captchaLen)
	n := big.NewInt(int64(len(captchaCharset)))
	for i := range buf {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", err
		}
		buf[i] = captchaCharset[idx.Int64()]
	}
	return string(buf), nil
}

// SaveCaptcha 写入/更新验证码（清理过期 + 超出上限淘汰）
func SaveCaptcha(ip, code string) {
	captchaStoreMu.Lock()
	defer captchaStoreMu.Unlock()
	now := time.Now()
	for k, v := range captchaStore {
		if !v.expiresAt.IsZero() && now.After(v.expiresAt) {
			delete(captchaStore, k)
		}
	}
	for len(captchaStore) >= captchaMaxEntries {
		for k := range captchaStore {
			delete(captchaStore, k)
			break
		}
	}
	captchaStore[captchaKey(ip)] = &captchaEntry{
		code:      strings.ToLower(code),
		expiresAt: now.Add(captchaExpire),
	}
}

// VerifyAndClearCaptcha 校验并消费（一次性）
func VerifyAndClearCaptcha(ip, code string) bool {
	captchaStoreMu.Lock()
	defer captchaStoreMu.Unlock()
	k := captchaKey(ip)
	entry, ok := captchaStore[k]
	if !ok {
		return false
	}
	delete(captchaStore, k)
	if time.Now().After(entry.expiresAt) {
		return false
	}
	return entry.code == strings.ToLower(strings.TrimSpace(code))
}

// NeedCaptchaForLogin 登录是否需要验证码（该 IP 密码错误次数 >= 1 就要）
func NeedCaptchaForLogin(ip string) bool {
	var rec model.LoginFailRecord
	if err := model.DB.Where("ip = ? AND username = ?", ip, "").First(&rec).Error; err != nil {
		return false
	}
	return rec.Count >= 1
}

// DrawCaptchaPNG 渲染 4 位字符串为 PNG（字符使用 captchaScale 倍放大，便于人眼辨认）
func DrawCaptchaPNG(code string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, captchaWidth, captchaHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bgColor}, image.Point{}, draw.Src)
	// 干扰线（2 条，浅色）
	for i := 0; i < 2; i++ {
		c := lineColors[i%len(lineColors)]
		drawLine(img, randInt(captchaWidth), randInt(captchaHeight),
			randInt(captchaWidth), randInt(captchaHeight), c)
	}
	// 噪点（30 个浅色像素）
	for i := 0; i < 30; i++ {
		c := lineColors[i%len(lineColors)]
		img.Set(randInt(captchaWidth), randInt(captchaHeight), c)
	}
	// 字符：5x7 字模 × captchaScale 放大（每个像素画 captchaScale × captchaScale 的方块）
	charW := 5 * captchaScale
	gap := 10
	totalW := charW*captchaLen + gap*(captchaLen-1)
	x := (captchaWidth - totalW) / 2
	if x < 4 {
		x = 4
	}
	// 垂直居中：字符高 7*scale，按居中放置
	charH := 7 * captchaScale
	baseY := (captchaHeight - charH) / 2
	for _, ch := range code {
		yOff := (int(ch) % 7) - 3 // -3..+3 垂直偏移
		drawChar5x7Scaled(img, x, baseY+yOff, ch, captchaCharColor, captchaScale)
		x += charW + gap
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode captcha: %w", err)
	}
	return buf.Bytes(), nil
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

// drawLine Bresenham 画线
func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	dx := absInt(x2 - x1)
	dy := -absInt(y2 - y1)
	sx, sy := 1, 1
	if x1 >= x2 {
		sx = -1
	}
	if y1 >= y2 {
		sy = -1
	}
	err := dx + dy
	for {
		img.Set(x1, y1, c)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

// drawChar5x7 用内置字模绘制单个 ASCII 字符（每个像素 1x1）
func drawChar5x7(img *image.RGBA, x, y int, ch rune, col color.RGBA) {
	glyph, ok := font5x7[ch]
	if !ok {
		glyph = font5x7['?']
	}
	for row := 0; row < 7; row++ {
		bits := glyph[row]
		for colIdx := 0; colIdx < 5; colIdx++ {
			// 字模字节按 bit 0 = 最左 / bit 4 = 最右 布局，colIdx=0 是最左
			if bits&(1<<colIdx) != 0 {
				img.Set(x+colIdx, y+row, col)
			}
		}
	}
}

// drawChar5x7Scaled 用内置字模绘制单个 ASCII 字符，每个字模像素放大为 scale×scale 的方块（视觉更清晰）
func drawChar5x7Scaled(img *image.RGBA, x, y int, ch rune, col color.RGBA, scale int) {
	if scale < 1 {
		scale = 1
	}
	glyph, ok := font5x7[ch]
	if !ok {
		glyph = font5x7['?']
	}
	for row := 0; row < 7; row++ {
		bits := glyph[row]
		for colIdx := 0; colIdx < 5; colIdx++ {
			// 字模字节按 bit 0 = 最左 / bit 4 = 最右 布局
			if bits&(1<<colIdx) != 0 {
				// 画 scale×scale 的方块
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(x+colIdx*scale+dx, y+row*scale+dy, col)
					}
				}
			}
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}