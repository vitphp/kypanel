package service

import (
	"errors"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// LoginReq 登录请求
type LoginReq struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	OTPCode     string `json:"otp_code"`     // 2FA 验证码（启用 TOTP 时必填）
	CaptchaCode string `json:"captcha_code"` // 图形验证码（密码错误 1 次后必填）
}

// LoginResp 登录响应
type LoginResp struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

// LoginErrResp 登录失败响应（带额外字段，告诉前端是否需要显示验证码 / TOTP）
type LoginErrResp struct {
	utils.Resp
	NeedCaptcha bool `json:"need_captcha,omitempty"`
	NeedTotp    bool `json:"need_totp,omitempty"`
}

// ---- 登录暴力破解防护（持久化）----
// 同一 IP 连续失败 maxLoginFails 次后锁定 lockDuration；同时按「IP + 用户名」维度
// 双重记录，防止单 IP 轮换账号爆破 / 分布式爆破。记录落库（SQLite），面板重启后依然有效。
const (
	maxLoginFails = 5
	loginLockDur  = 5 * time.Minute
)

// checkLoginLocked 检查是否处于锁定期（IP 维度 + IP/用户名维度任一命中即锁定）
func checkLoginLocked(ip, username string) error {
	if err := checkLockedByKey(ip, ""); err != nil {
		return err
	}
	if err := checkLockedByKey(ip, username); err != nil {
		return err
	}
	return nil
}

// checkLockedByKey 检查指定维度（ip 或 ip+username）是否被锁定
func checkLockedByKey(ip, username string) error {
	var rec model.LoginFailRecord
	if err := model.DB.Where("ip = ? AND username = ?", ip, username).First(&rec).Error; err != nil {
		return nil // 无记录，不拦截
	}
	if !rec.LockedAt.IsZero() && time.Since(rec.LockedAt) < loginLockDur {
		return errors.New("登录失败次数过多，请 5 分钟后再试")
	}
	return nil
}

// recordLoginFail 记录一次登录失败（持久化）
func recordLoginFail(ip, username string) {
	now := time.Now()
	var rec model.LoginFailRecord
	// 精确匹配 ip + username
	err := model.DB.Where("ip = ? AND username = ?", ip, username).First(&rec).Error
	if err != nil {
		rec = model.LoginFailRecord{IP: ip, Username: username}
	}
	// 若已过锁定期，重置计数
	if !rec.LockedAt.IsZero() && now.Sub(rec.LockedAt) >= loginLockDur {
		rec.LockedAt = time.Time{}
		rec.Count = 0
	}
	rec.Count++
	if rec.Count >= maxLoginFails {
		rec.LockedAt = now
		rec.Count = 0
	}
	_ = model.Upsert(rec.ID, &rec)

	// 同时维护纯 IP 维度的锁定（IP 达到阈值即锁整个 IP，无论账号）
	var ipRec model.LoginFailRecord
	err = model.DB.Where("ip = ? AND username = ?", ip, "").First(&ipRec).Error
	if err != nil {
		ipRec = model.LoginFailRecord{IP: ip, Username: ""}
	}
	if !ipRec.LockedAt.IsZero() && now.Sub(ipRec.LockedAt) >= loginLockDur {
		ipRec.LockedAt = time.Time{}
		ipRec.Count = 0
	}
	ipRec.Count++
	if ipRec.Count >= maxLoginFails {
		ipRec.LockedAt = now
		ipRec.Count = 0
	}
	_ = model.Upsert(ipRec.ID, &ipRec)
}

// recordLoginSuccess 登录成功后清除该 IP / 账号的失败记录
func recordLoginSuccess(ip, username string) {
	_ = model.DB.Where("ip = ?", ip).Delete(&model.LoginFailRecord{}).Error
}

// LoginResult 登录结果：resp 为 nil 表示失败；needCaptcha/needTotp 用于失败时告诉前端是否要显示对应输入框
type LoginResult struct {
	Resp        *LoginResp
	NeedCaptcha bool // 下次登录是否需要图形验证码（密码错误 1 次后置 true）
	NeedTotp    bool // 当前账号是否启用了 TOTP
	Err         error
}

// Login 管理员登录
func Login(req LoginReq, ip string, userAgent string) LoginResult {
	if err := checkLoginLocked(ip, req.Username); err != nil {
		RecordOp(0, "login", "登录被锁定: "+req.Username, ip, "fail")
		return LoginResult{Err: err}
	}

	// 登录 IP 白名单校验（白名单为空则不限；本机回环始终放行）
	if !CheckLoginAllowed(ip) {
		RecordOp(0, "login", "IP 不在登录白名单: "+req.Username, ip, "fail")
		return LoginResult{Err: errors.New("当前 IP 不在登录白名单内，禁止登录")}
	}

	// 图形验证码校验：该 IP 密码错误次数 >= 1 时必填（一次性消费）
	if NeedCaptchaForLogin(ip) {
		if !VerifyAndClearCaptcha(ip, req.CaptchaCode) {
			RecordOp(0, "login", "图形验证码错误: "+req.Username, ip, "fail")
			return LoginResult{
				Err:         errors.New("图形验证码错误或已过期"),
				NeedCaptcha: true,
			}
		}
	}

	var admin model.Admin
	err := model.DB.Where("username = ?", req.Username).First(&admin).Error
	if err != nil || !admin.CheckPassword(req.Password) {
		recordLoginFail(ip, req.Username)
		RecordOp(0, "login", "用户名或密码错误: "+req.Username, ip, "fail")
		// 密码错误后下一次登录需要验证码
		return LoginResult{
			Err:         errors.New("用户名或密码错误"),
			NeedCaptcha: true,
			NeedTotp:    admin.TOTPSecret != "",
		}
	}

	// 2FA 校验：已启用 TOTP 时必须校验验证码
	if admin.TOTPSecret != "" {
		if req.OTPCode == "" {
			RecordOp(admin.ID, "login", "未提供 2FA 验证码: "+req.Username, ip, "fail")
			return LoginResult{
				Err:      errors.New("该账号已启用双因素认证，请输入 6 位验证码"),
				NeedTotp: true,
			}
		}
		if !VerifyTOTP(admin.TOTPSecret, req.OTPCode) {
			// 2FA 错误也计入失败（防暴力爆破验证码）
			recordLoginFail(ip, req.Username)
			RecordOp(admin.ID, "login", "2FA 验证码错误: "+req.Username, ip, "fail")
			return LoginResult{
				Err:      errors.New("验证码错误"),
				NeedTotp: true,
			}
		}
	}

	recordLoginSuccess(ip, req.Username)
	cfg := config.Get()
	token, err := utils.GenerateToken(admin.ID, admin.Username, cfg.Auth.TokenHour, admin.TokenVer)
	if err != nil {
		return LoginResult{Err: errors.New("生成 Token 失败")}
	}

	// 记录活跃会话
	CreateSession(admin.ID, admin.Username, token, ip, userAgent)

	RecordOp(admin.ID, "login", "管理员登录", ip, "success")
	return LoginResult{Resp: &LoginResp{Token: token, Username: admin.Username}}
}

// TokenTTLHours 返回 token 有效期（小时），用于设置登录态 cookie 的过期时间
func TokenTTLHours() int {
	h := config.Get().Auth.TokenHour
	if h <= 0 {
		h = 24
	}
	return h
}

// ChangePasswordReq 修改密码请求
type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ChangePassword 修改管理员密码（改密后 TokenVer+1，使所有旧 token 立即失效，需重新登录）
func ChangePassword(adminID uint, req ChangePasswordReq) error {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return errors.New("管理员不存在")
	}
	if !admin.CheckPassword(req.OldPassword) {
		return errors.New("原密码错误")
	}
	if err := admin.SetPassword(req.NewPassword); err != nil {
		return errors.New("密码设置失败")
	}
	// 令牌版本号 +1，旧 token 的 ver 不再匹配，立即失效
	admin.TokenVer++
	return model.DB.Save(&admin).Error
}

// ChangeUsername 修改管理员用户名
func ChangeUsername(adminID uint, newName string) error {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return errors.New("管理员不存在")
	}
	admin.Username = newName
	return model.DB.Save(&admin).Error
}
