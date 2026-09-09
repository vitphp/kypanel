package service

import (
	"os"
	"strconv"
	"strings"

	"kypanel/internal/model"
)

// Apache 版拖拽验证码闸门：Apache 没有 nginx 的 auth_request，且 mod_proxy 不可靠地支持子请求反代，
// 因此改用标准模块 mod_lua 在 access 阶段【本地】用纯 Lua 复算 HMAC-SHA256 校验放行 cookie 的签名，
// 从根本上杜绝「伪造 cookie 格式即绕过」的问题（旧实现只用正则匹配 cookie 格式，不验签）。
//
// 安全设计：
//   - 放行 cookie 为 "base64url(siteID:exp).hexsig" 结构，sig=HMAC-SHA256(主密钥, "siteID:exp")；
//     与 nginx 侧面板 /api/site/captcha/check 使用同一主密钥、同一算法，两端校验结果一致。
//   - 主密钥在生成脚本时内联进 lua（等价于面板 0600 密钥文件的可读副本，仅用于本地验签）。
//   - 纯 Lua 算术实现位运算/SHA256，兼容 Lua 5.1/5.3，无需 bit 库、无需 io/os 强依赖。
//   - fail-open：任何脚本异常一律放行（DECLINED），避免 lua 出错导致整站 500；宁可降级为不强制验证码。

const apacheCaptchaLuaPathPrefix = "/etc/apache2/lp_captcha_"

// apacheCaptchaLuaPath 返回某站点的 mod_lua 校验脚本绝对路径。
func apacheCaptchaLuaPath(name string) string {
	return apacheCaptchaLuaPathPrefix + name + ".lua"
}

// writeApacheCaptchaFiles 生成并写入站点的 mod_lua 校验脚本（含 site id 与 HMAC 主密钥）。
// 在 ensureApacheModules 里于启用验证码时调用，须先于 Apache 配置生效/reload。
func writeApacheCaptchaFiles(s *model.Site) error {
	lua := strings.ReplaceAll(apacheCaptchaLua, "__SITE_ID__", strconv.FormatUint(uint64(s.ID), 10))
	lua = strings.ReplaceAll(lua, "__KEY_HEX__", SiteCaptchaKeyHex())
	return os.WriteFile(apacheCaptchaLuaPath(s.Name), []byte(lua), 0o644)
}

// apacheCaptchaLua 是 mod_lua AccessChecker 脚本模板。
// 占位符：__SITE_ID__（站点数字 id）、__KEY_HEX__（32 字节 HMAC 主密钥的十六进制）。
const apacheCaptchaLua = `-- kypanel 拖拽验证码 Apache 闸门（mod_lua AccessChecker），面板自动生成，请勿手动修改。
-- 纯 Lua 本地复算 HMAC-SHA256 校验放行 cookie 签名，杜绝伪造 cookie 绕过验证码。
-- 任何意外错误一律放行（fail-open），避免脚本异常导致整站 500。

local SITE_ID = __SITE_ID__
local KEY_HEX = "__KEY_HEX__"

-- ---------- 32 位算术位运算（兼容 Lua 5.1/5.3，无需 bit 库）----------
local function band(a, b)
  local r, p = 0, 1
  for _ = 1, 32 do
    local aa, bb = a % 2, b % 2
    if aa == 1 and bb == 1 then r = r + p end
    a, b, p = (a - aa) / 2, (b - bb) / 2, p * 2
  end
  return r
end

local function bxor(a, b)
  local r, p = 0, 1
  for _ = 1, 32 do
    local aa, bb = a % 2, b % 2
    if aa ~= bb then r = r + p end
    a, b, p = (a - aa) / 2, (b - bb) / 2, p * 2
  end
  return r
end

local function ror(x, n)
  local p = 2 ^ n
  local low = x % p
  return (x - low) / p + low * (2 ^ (32 - n))
end

local function add32(x, y, z, w, v)
  return (x + y + z + w + v) % 4294967296
end

local K = {
  0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
  0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
  0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
  0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
  0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
  0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
  0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
  0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2,
}

local function sha256(msg)
  local h1,h2,h3,h4 = 0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a
  local h5,h6,h7,h8 = 0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19
  local bitlen = (#msg) * 8
  msg = msg .. "\128"
  while (#msg % 64) ~= 56 do msg = msg .. "\0" end
  msg = msg .. "\0\0\0\0"
  msg = msg .. string.char(
    math.floor(bitlen / 16777216) % 256,
    math.floor(bitlen / 65536) % 256,
    math.floor(bitlen / 256) % 256,
    bitlen % 256)
  for off = 1, #msg, 64 do
    local w = {}
    for i = 0, 15 do
      local b0, b1, b2, b3 = string.byte(msg, off + i*4, off + i*4 + 3)
      w[i] = ((b0 * 256 + b1) * 256 + b2) * 256 + b3
    end
    for i = 16, 63 do
      local x = w[i-15]
      local s0 = bxor(bxor(ror(x,7), ror(x,18)), math.floor(x / 8))
      local y = w[i-2]
      local s1 = bxor(bxor(ror(y,17), ror(y,19)), math.floor(y / 1024))
      w[i] = (w[i-16] + s0 + w[i-7] + s1) % 4294967296
    end
    local a,b,c,d,e,f,g,h = h1,h2,h3,h4,h5,h6,h7,h8
    for i = 0, 63 do
      local S1 = bxor(bxor(ror(e,6), ror(e,11)), ror(e,25))
      local ch = bxor(band(e,f), band(4294967295 - e, g))
      local t1 = add32(h, S1, ch, K[i+1], w[i])
      local S0 = bxor(bxor(ror(a,2), ror(a,13)), ror(a,22))
      local maj = bxor(bxor(band(a,b), band(a,c)), band(b,c))
      local t2 = (S0 + maj) % 4294967296
      h = g; g = f; f = e; e = (d + t1) % 4294967296
      d = c; c = b; b = a; a = (t1 + t2) % 4294967296
    end
    h1 = (h1 + a) % 4294967296; h2 = (h2 + b) % 4294967296
    h3 = (h3 + c) % 4294967296; h4 = (h4 + d) % 4294967296
    h5 = (h5 + e) % 4294967296; h6 = (h6 + f) % 4294967296
    h7 = (h7 + g) % 4294967296; h8 = (h8 + h) % 4294967296
  end
  return string.format("%08x%08x%08x%08x%08x%08x%08x%08x",
    math.floor(h1), math.floor(h2), math.floor(h3), math.floor(h4),
    math.floor(h5), math.floor(h6), math.floor(h7), math.floor(h8))
end

local function hex2bin(h)
  return (h:gsub("%x%x", function(cc) return string.char(tonumber(cc, 16)) end))
end

local function hmac_sha256(key, msg)
  if #key > 64 then key = hex2bin(sha256(key)) end
  if #key < 64 then key = key .. string.rep("\0", 64 - #key) end
  local ip, op = {}, {}
  for i = 1, 64 do
    local kb = string.byte(key, i)
    ip[i] = string.char(bxor(kb, 0x36))
    op[i] = string.char(bxor(kb, 0x5c))
  end
  local inner = hex2bin(sha256(table.concat(ip) .. msg))
  return sha256(table.concat(op) .. inner)
end

local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
local B64REV = {}
for i = 1, #B64 do B64REV[B64:sub(i,i)] = i - 1 end

local function b64url_decode(s)
  s = (s:gsub("=", ""))
  local out, buf, bits = {}, 0, 0
  for i = 1, #s do
    local v = B64REV[s:sub(i,i)]
    if v == nil then return nil end
    buf = buf * 64 + v
    bits = bits + 6
    if bits >= 8 then
      bits = bits - 8
      local d = 2 ^ bits
      out[#out+1] = string.char(math.floor(buf / d) % 256)
      buf = buf % d
    end
  end
  return table.concat(out)
end

local KEY_BIN = hex2bin(KEY_HEX)

local function verify(val, now)
  local b64, sig = val:match("^(.+)%.([0-9a-fA-F]+)$")
  if not b64 or not sig then return false end
  local payload = b64url_decode(b64)
  if not payload then return false end
  local expect = hmac_sha256(KEY_BIN, payload)
  if expect:lower() ~= sig:lower() then return false end
  local sid, exp = payload:match("^(%d+):(%d+)$")
  if not sid or not exp then return false end
  if tonumber(sid) ~= SITE_ID then return false end
  if now > 0 and tonumber(exp) < now then return false end
  return true
end

local function do_check(r)
  local uri = r.uri or ""
  if uri == "/captcha-challenge" or uri == "/_lp_captcha_challenge"
     or uri:match("^/_lp_captcha_api/")
     or uri:match("^/api/site/captcha/")
     or uri:match("^/%.well%-known/") then
    return apache2.DECLINED
  end
  local cookie = r.headers_in["Cookie"]
  local val = cookie and cookie:match("lp_captcha=([^;]+)")
  if not val then
    return apache2.HTTP_UNAUTHORIZED
  end
  local now = 0
  if os and os.time then now = os.time() end
  if verify(val, now) then
    return apache2.DECLINED
  end
  return apache2.HTTP_UNAUTHORIZED
end

function authorize(r)
  local ok, res = pcall(do_check, r)
  if not ok then
    -- fail-open：脚本异常绝不返回 500，放行请求（降级为不强制验证码）。
    if r and r.err then r:err("kypanel captcha lua error: " .. tostring(res)) end
    return apache2.DECLINED
  end
  return res
end
`
