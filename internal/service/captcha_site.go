package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
)

// ---- 站点拖拽拼图验证码 ----
// 设计为「边缘层闸门」：访客访问站点时由 nginx 的 auth_request 调用本服务的 check 接口，
// 未通过验证（无有效 cookie）则被返回挑战页；访客拖拽拼图成功后 verify 接口下发
// HttpOnly cookie，之后该站点的请求全部放行。cookie 由 HMAC 签名（绑定 site_id + 过期时间），
// 无法伪造；验证通过后的放行与访问量无关，不会随流量增长而变慢。

const (
	capW      = 320 // 拼图背景宽
	capH      = 160 // 拼图背景高
	capPiece  = 42  // 拼图块边长
	capTol    = 6   // 校验容差（像素）：拖拽落点与正确位置误差在此范围内即通过
	capExpire = 5 * time.Minute
)

// SiteCaptchaChallenge 拼图挑战数据（返回给前端渲染）
type SiteCaptchaChallenge struct {
	Token     string `json:"token"`      // 服务端加密的令牌（内含正确答案 tx 与过期时间，客户端不可读）
	TargetY   int    `json:"target_y"`   // 拼图块纵向位置（非机密，前端定位用；横向答案 tx 绝不下发）
	PieceSize int    `json:"piece_size"` // 拼图块边长
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Bg        string `json:"bg"`    // 背景图（含缺口）data URL
	Piece     string `json:"piece"` // 拼图块图片 data URL
}

// ---- 密钥（进程内持久化到 DataDir/data，重启后仍有效，已下发 cookie / token 不失效）----
//
// 安全模型（两类令牌用途不同，采用不同原语）：
//  1. 拼图 token —— 内含正确答案 tx，必须【加密】（AES-256-GCM，机密性 + 完整性），
//     否则攻击者 base64 解码即可读到答案、秒过验证码。客户端只负责原样回传，无需解密。
//  2. 放行 cookie —— 只含 "siteID:exp"（不含答案），只需【签名】（HMAC-SHA256，完整性），
//     采用 "base64url(payload).hexsig" 结构，便于 Apache 侧用纯 Lua 复算 HMAC 校验（见 lp_captcha lua）。
//
// 两类令牌共用同一份 32 字节主密钥 siteCapKey：HMAC 直接用它；AES 密钥 = SHA256(主密钥)。
var (
	siteCapKey     []byte // 32 字节主密钥
	siteCapKeyOnce sync.Once
)

const siteCapKeyPath = "data/lp_cap_hmac.key" // 相对 DataDir

func ensureSiteCapKey() {
	siteCapKeyOnce.Do(func() {
		p := filepath.Join(config.Get().DataDir, siteCapKeyPath)
		if b, err := os.ReadFile(p); err == nil && len(b) >= 16 {
			siteCapKey = b
			return
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			// 极 unlikely：回退到时间种子派生
			sum := sha256.Sum256([]byte(fmt.Sprintf("kypanel-cap-%d", time.Now().UnixNano())))
			b = sum[:]
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, b, 0o600)
		siteCapKey = b
	})
}

// SiteCaptchaKeyHex 返回主密钥的十六进制表示，供 Apache mod_lua 侧复算 HMAC 校验放行 cookie。
// 面板会在启用验证码时把它写入 Apache 可读的密钥文件（见 writeApacheCaptchaLua）。
func SiteCaptchaKeyHex() string {
	ensureSiteCapKey()
	return hex.EncodeToString(siteCapKey)
}

func capHmac(payload string) string {
	ensureSiteCapKey()
	mac := hmac.New(sha256.New, siteCapKey)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// capAEAD 返回基于主密钥派生的 AES-256-GCM（用于加密拼图 token）。
func capAEAD() (cipher.AEAD, error) {
	ensureSiteCapKey()
	aesKey := sha256.Sum256(siteCapKey) // 固定 32 字节 → AES-256
	block, err := aes.NewCipher(aesKey[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// buildEncToken 生成【加密】令牌：base64url(nonce || ciphertext||tag)。
// 用于拼图 token —— 正确答案 tx 被加密，客户端无法读取。
func buildEncToken(payload string) (string, error) {
	aead, err := capAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, []byte(payload), nil) // 前置 nonce
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// parseEncToken 解密并校验加密令牌，返回明文 payload。
func parseEncToken(tok string) (string, bool) {
	aead, err := capAEAD()
	if err != nil {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return "", false
	}
	ns := aead.NonceSize()
	pt, err := aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", false
	}
	return string(pt), true
}

// buildSignedToken 生成【签名】令牌："base64url(payload).hexsig"。
// 用于放行 cookie —— payload 不含答案，仅需防伪造。
func buildSignedToken(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + capHmac(payload)
}

// parseSignedToken 校验签名并返回 payload（常量时间比较，失败返回 ok=false）。
func parseSignedToken(tok string) (string, bool) {
	idx := strings.LastIndex(tok, ".")
	if idx < 0 {
		return "", false
	}
	b64, sig := tok[:idx], tok[idx+1:]
	raw, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return "", false
	}
	payload := string(raw)
	expect := capHmac(payload)
	// 常量时间比较，避免计时侧信道
	if !hmac.Equal([]byte(expect), []byte(sig)) {
		return "", false
	}
	return payload, true
}

// ---- 一次性 token（防 verify 接口盲猜爆破）----
// 拼图横向答案 tx 取值约 [50,270)、容差 capTol=6，若允许同一 token 反复提交，机器人无需
// 识别图片、盲猜约 17 次即可命中。这里让每个 token 只能用于一次 verify（无论对错即作废），
// 攻击者每次尝试都必须重新拉取带图片生成成本的 puzzle，大幅提高爆破代价。
// 纯内存实现（不入库），带惰性过期清理；进程重启即清空（旧 token 因过期也自然失效）。
var (
	capBurnMu sync.Mutex
	capBurned = map[string]int64{} // token -> 作废记录保留截止时间戳（秒）
)

// burnCaptchaToken 尝试作废 token：若此前已作废过返回 true（应拒绝本次验证），
// 否则登记并返回 false（首次使用，继续校验答案）。
func burnCaptchaToken(token string, exp int64) bool {
	capBurnMu.Lock()
	defer capBurnMu.Unlock()
	now := time.Now().Unix()
	if len(capBurned) > 1024 { // 惰性清理，避免 map 无限增长（仅超阈值时全量扫一遍）
		for k, until := range capBurned {
			if until < now {
				delete(capBurned, k)
			}
		}
	}
	if _, used := capBurned[token]; used {
		return true
	}
	until := exp + 60 // 保留到 token 过期后再多 60s 即可（过期 token 本就被拒）
	if until < now+60 {
		until = now + 60
	}
	capBurned[token] = until
	return false
}

// ---- 拼图图片生成 ----

func capRandColor() color.RGBA {
	return color.RGBA{uint8(randInt(256)), uint8(randInt(256)), uint8(randInt(256)), 255}
}

func capRandColorA(a uint8) color.RGBA {
	return color.RGBA{uint8(randInt(256)), uint8(randInt(256)), uint8(randInt(256)), a}
}

func capLerpColor(c1, c2 color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		uint8(float64(c1.R) + t*float64(c2.R-c1.R)),
		uint8(float64(c1.G) + t*float64(c2.G-c1.G)),
		uint8(float64(c1.B) + t*float64(c2.B-c1.B)),
		255,
	}
}

// genCapBg 生成带渐变 + 随机图形的背景图（让拼图块对齐后有明显视觉差异）
func genCapBg(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c1 := capRandColor()
	c2 := capRandColor()
	for y := 0; y < h; y++ {
		col := capLerpColor(c1, c2, float64(y)/float64(h))
		for x := 0; x < w; x++ {
			img.Set(x, y, col)
		}
	}
	// 随机圆（半透明）
	for i := 0; i < 8; i++ {
		cx, cy := randInt(w), randInt(h)
		r := randInt(40) + 10
		capDrawCircle(img, cx, cy, r, capRandColorA(90))
	}
	// 随机干扰线
	for i := 0; i < 3; i++ {
		capDrawLine(img, randInt(w), randInt(h), randInt(w), randInt(h), capRandColorA(110))
	}
	return img
}

func capDrawCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, col)
			}
		}
	}
}

func capDrawLine(img *image.RGBA, x1, y1, x2, y2 int, col color.RGBA) {
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
		if x1 >= 0 && y1 >= 0 && x1 < img.Bounds().Dx() && y1 < img.Bounds().Dy() {
			img.Set(x1, y1, col)
		}
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

// capPieceMask 判断 (px,py) 是否属于拼图形状（圆角矩形 + 右/下两个凸块，呈拼图感）
func capPieceMask(px, py, size int) bool {
	r := size / 6
	if !inRoundRect(px, py, size, size, r) {
		return false
	}
	// 右侧凸块
	if dist(px, py, size-1, size/2) <= int(float64(size)*0.30) {
		return true
	}
	// 下方凸块
	if dist(px, py, size/2, size-1) <= int(float64(size)*0.30) {
		return true
	}
	return true
}

func inRoundRect(px, py, w, h, r int) bool {
	if px < 0 || py < 0 || px >= w || py >= h {
		return false
	}
	if r <= 0 {
		return true
	}
	// 四个角：超出圆角半径的部分需落在圆内
	corners := [][2]int{{r, r}, {w - r - 1, r}, {r, h - r - 1}, {w - r - 1, h - r - 1}}
	for _, c := range corners {
		if px >= c[0]-r && px <= c[0]+r && py >= c[1]-r && py <= c[1]+r {
			// 仅当该角确为圆角区域时才检查（简单近似：离角点 > r 即安全）
			if px > r && px < w-r-1 && py > r && py < h-r-1 {
				continue
			}
			ddx := px - c[0]
			ddy := py - c[1]
			if ddx*ddx+ddy*ddy > r*r {
				return false
			}
		}
	}
	return true
}

func dist(x1, y1, x2, y2 int) int {
	dx, dy := x1-x2, y1-y2
	return int(sqrt(float64(dx*dx + dy*dy)))
}

// cutCapPiece 从背景切出拼图块（返回块图像），并在背景上挖出对应缺口（压暗）
func cutCapPiece(bg *image.RGBA, x, y, size int) *image.RGBA {
	piece := image.NewRGBA(image.Rect(0, 0, size, size))
	bw, bh := bg.Bounds().Dx(), bg.Bounds().Dy()
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			if !capPieceMask(px, py, size) {
				continue
			}
			sx, sy := x+px, y+py
			if sx >= 0 && sy >= 0 && sx < bw && sy < bh {
				piece.Set(px, py, bg.At(sx, sy))
				// 在背景上挖缺口：压暗（半透明黑覆盖）
				ob := bg.At(sx, sy)
				or_, og_, ob_, oa_ := ob.RGBA()
				bg.Set(sx, sy, color.RGBA{
					R: uint8(float64(or_) * 0.45 / 257),
					G: uint8(float64(og_) * 0.45 / 257),
					B: uint8(float64(ob_) * 0.45 / 257),
					A: uint8(oa_ / 257),
				})
			}
		}
	}
	return piece
}

func pngDataURL(img image.Image) string {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// GenerateSiteCaptcha 生成一次拼图挑战（无状态：正确位置编码在 token 中）
func GenerateSiteCaptcha(siteID uint) (*SiteCaptchaChallenge, error) {
	bg := genCapBg(capW, capH)
	minX := capPiece + 8
	maxX := capW - capPiece - 8
	tx := minX + randInt(maxX-minX)
	ty := 10 + randInt(capH-capPiece-20)
	piece := cutCapPiece(bg, tx, ty, capPiece)
	exp := time.Now().Add(capExpire).Unix()
	// token 加密生成：正确答案 tx 不再以明文/base64 暴露给客户端，杜绝“解码 token 秒过”。
	token, err := buildEncToken(fmt.Sprintf("%d:%d:%d:%d", siteID, tx, ty, exp))
	if err != nil {
		return nil, err
	}
	return &SiteCaptchaChallenge{
		Token:     token,
		TargetY:   ty,
		PieceSize: capPiece,
		Width:     capW,
		Height:    capH,
		Bg:        pngDataURL(bg),
		Piece:     pngDataURL(piece),
	}, nil
}

// SiteIDFromCaptchaToken 从拼图令牌中解析 siteID（令牌为 AES-GCM 加密串，明文格式 "<siteID>:<tx>:<ty>:<exp>"）。
// 供 verify 接口在缺少 site 查询参数时回退使用（token 已加密签名，答案 tx 不外泄、不可伪造）。
func SiteIDFromCaptchaToken(token string) uint {
	payload, ok := parseEncToken(token)
	if !ok {
		return 0
	}
	parts := strings.Split(payload, ":")
	if len(parts) < 1 {
		return 0
	}
	sid, _ := strconv.Atoi(parts[0])
	if sid <= 0 {
		return 0
	}
	return uint(sid)
}

// VerifySiteCaptcha 校验拖拽结果：令牌有效、未过期、落点误差在容差内
// 返回 (是否通过, 过期时间戳)
func VerifySiteCaptcha(siteID uint, token string, x int) (bool, int64) {
	payload, ok := parseEncToken(token)
	if !ok {
		return false, 0
	}
	parts := strings.Split(payload, ":")
	if len(parts) != 4 {
		return false, 0
	}
	sid, _ := strconv.Atoi(parts[0])
	tx, _ := strconv.Atoi(parts[1])
	ty, _ := strconv.Atoi(parts[2])
	exp, _ := strconv.ParseInt(parts[3], 10, 64)
	_ = ty // 纵向固定对齐，校验横向落点即可
	if sid != int(siteID) {
		return false, 0
	}
	if time.Now().Unix() > exp {
		return false, 0
	}
	// 一次性 token：无论本次答案对错，token 立即作废，杜绝用同一 token 反复盲猜 x 爆破
	// （tx 取值约 220 个、容差 6，不限流时约 17 次即可命中）。失败时挑战页本就会重新拉取
	// 新 puzzle，故与现有 UX 一致。
	if burnCaptchaToken(token, exp) {
		return false, 0 // 该 token 已被使用过
	}
	if absInt(x-tx) > capTol {
		return false, 0
	}
	return true, exp
}

// IssueSiteCaptchaCookie 验证通过后下发放行 cookie 的值与有效期
func IssueSiteCaptchaCookie(siteID uint, ttl int) (string, int) {
	if ttl <= 0 {
		ttl = 1800
	}
	exp := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	// 放行 cookie 用【签名】令牌（不含答案，仅防伪造）：Apache 侧可用纯 Lua 复算 HMAC 校验。
	return buildSignedToken(fmt.Sprintf("%d:%d", siteID, exp)), ttl
}

// CheckSiteCaptchaCookie 校验放行 cookie（auth_request 调用，仅做 HMAC + 过期判断，无 DB 压力）
func CheckSiteCaptchaCookie(siteID uint, cookie string) bool {
	if cookie == "" {
		return false
	}
	payload, ok := parseSignedToken(cookie)
	if !ok {
		return false
	}
	parts := strings.Split(payload, ":")
	if len(parts) != 2 {
		return false
	}
	sid, _ := strconv.Atoi(parts[0])
	exp, _ := strconv.ParseInt(parts[1], 10, 64)
	if sid != int(siteID) {
		return false
	}
	if time.Now().Unix() > exp {
		return false
	}
	return true
}

// GetSiteCaptchaTTL 读取该站点验证码放行时长（秒），<=0 回落默认
func GetSiteCaptchaTTL(siteID uint) int {
	cfg := GetSiteSecurityConfig(siteID)
	if cfg.CaptchaTTL <= 0 {
		return 1800
	}
	return cfg.CaptchaTTL
}

// sqrt 简单平方根（仅用于拼图距离计算）
func sqrt(f float64) float64 {
	if f <= 0 {
		return 0
	}
	r := f
	for i := 0; i < 16; i++ {
		r = (r + f/r) / 2
	}
	return r
}
