package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/bits"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
)

// ip2region.xdb 纯 Go 离线解析器（无第三方依赖）。
// 参考官方 XdbSearcher 二进制格式：
//
//	Header(256B) | VectorIndex(256*256*8) | SegmentIndex | Data
//
// 查询流程：
//  1. 由 IP 前两段 (il0,il1) 定位 vector index，得到 [sPtr,ePtr] 段索引区间；
//  2. 在段索引区间内二分查找命中的段；
//  3. 读取对应数据区返回字符串（格式：国家|区域|省份|城市|ISP）。
const (
	xdbHeaderSize      = 256
	xdbVectorIndexRows = 256
	xdbVectorIndexCols = 256
	xdbVectorIndexSize = 8
	xdbSegmentIndexSz  = 14
)

// IpRegion 一条区域信息
type IpRegion struct {
	Country  string `json:"country"`  // 国家
	Province string `json:"province"` // 省
	City     string `json:"city"`     // 市
	ISP      string `json:"isp"`      // 运营商
	Raw      string `json:"raw"`      // 原始字符串
}

// ip2regionSearcher 内存态 xdb 查询器
type ip2regionSearcher struct {
	mu     sync.RWMutex
	data   []byte // 完整 xdb 内容
	loaded bool
	err    string
}

var ipRegion *ip2regionSearcher

func init() {
	ipRegion = &ip2regionSearcher{}
}

// defaultXdbPaths 依次尝试的 xdb 文件位置
var defaultXdbPaths = []string{
	"/opt/kypanel/data/ip2region.xdb",
	"/www/server/ip2region.xdb",
	"/www/server/panel/ip2region.xdb",
	"/etc/kypanel/ip2region.xdb",
	"/www/server/kypanel/data/ip2region.xdb",
}

// LoadIpRegionXdb 从文件加载 xdb 数据到内存（返回 true 表示加载成功）。
// 内部自带默认路径扫描（含面板数据目录），也可手动指定。
func LoadIpRegionXdb(path string) bool {
	ipRegion.mu.Lock()
	defer ipRegion.mu.Unlock()
	if path == "" {
		for _, p := range defaultXdbPaths {
			if b, err := os.ReadFile(p); err == nil && len(b) > xdbHeaderSize {
				ipRegion.data = b
				ipRegion.loaded = true
				ipRegion.err = ""
				return true
			}
		}
		ipRegion.loaded = false
		ipRegion.err = "未找到 ip2region.xdb 离线库"
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		ipRegion.loaded = false
		ipRegion.err = "读取 xdb 失败: " + err.Error()
		return false
	}
	ipRegion.data = b
	ipRegion.loaded = true
	ipRegion.err = ""
	return true
}

// InitIpRegion 启动时自动加载离线 IP 库（静默失败，不影响面板运行）
func InitIpRegion() {
	LoadIpRegionXdb("")
}

// IpRegionEnabled 返回离线库是否可用
func IpRegionEnabled() bool {
	ipRegion.mu.RLock()
	defer ipRegion.mu.RUnlock()
	return ipRegion.loaded
}

// IpRegionStatus 返回离线库状态信息
func IpRegionStatus() (loaded bool, size int, errMsg string) {
	ipRegion.mu.RLock()
	defer ipRegion.mu.RUnlock()
	return ipRegion.loaded, len(ipRegion.data), ipRegion.err
}

func leUint32(b []byte, i int) uint32 {
	return binary.LittleEndian.Uint32(b[i : i+4])
}
func leUint16(b []byte, i int) uint16 {
	return binary.LittleEndian.Uint16(b[i : i+2])
}

// searchIP 使用内存缓冲区查询给定 uint32 IP 所在区域数据字节
func (s *ip2regionSearcher) searchIP(ip uint32) ([]byte, bool) {
	if !s.loaded {
		return nil, false
	}
	b := s.data
	il0 := (ip >> 24) & 0xFF
	il1 := (ip >> 16) & 0xFF
	idx := il0*xdbVectorIndexCols*xdbVectorIndexSize + il1*xdbVectorIndexSize
	vidx := xdbHeaderSize + int(idx)
	if vidx+8 > len(b) {
		return nil, false
	}
	sPtr := leUint32(b, vidx)
	ePtr := leUint32(b, vidx+4)
	if sPtr == 0 || ePtr == 0 || ePtr <= sPtr {
		return nil, false
	}
	l, h := uint32(0), (ePtr-sPtr)/xdbSegmentIndexSz
	var dataPtr uint32
	var dataLen uint16
	found := false
	for l <= h {
		m := (l + h) >> 1
		p := int(sPtr + m*xdbSegmentIndexSz)
		if p+xdbSegmentIndexSz > len(b) {
			break
		}
		sip := leUint32(b, p)
		if ip < sip {
			h = m - 1
			continue
		}
		eip := leUint32(b, p+4)
		if ip > eip {
			l = m + 1
			continue
		}
		dataLen = leUint16(b, p+8)
		dataPtr = leUint32(b, p+10)
		found = true
		break
	}
	if !found || dataLen == 0 {
		return nil, false
	}
	dp := int(dataPtr)
	if dp+int(dataLen) > len(b) {
		return nil, false
	}
	return b[dp : dp+int(dataLen)], true
}

// SearchIp 查询 IP 归属地，IP 可为 IPv4 字符串（IPv6 返回空）
func SearchIp(ipStr string) (*IpRegion, bool) {
	ipRegion.mu.RLock()
	defer ipRegion.mu.RUnlock()
	if !ipRegion.loaded {
		return nil, false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return nil, false
	}
	v4 := ip.To4()
	u := uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3])
	raw, ok := ipRegion.searchIP(u)
	if !ok {
		return nil, false
	}
	parts := splitRegion(string(raw))
	return &IpRegion{
		Country:  parts[0],
		Province: parts[2],
		City:     parts[3],
		ISP:      parts[4],
		Raw:      string(raw),
	}, true
}

// splitRegion 将 "国家|区域|省份|城市|ISP" 按 | 拆分，不足部分补空
func splitRegion(s string) []string {
	out := make([]string, 5)
	start := 0
	idx := 0
	for i := 0; i < len(s) && idx < 5; i++ {
		if s[i] == '|' {
			out[idx] = s[start:i]
			idx++
			start = i + 1
		}
	}
	if idx < 5 {
		out[idx] = s[start:]
	}
	// 数据来源常见 "中国|0|广东省|广州市|电信"
	if out[1] == "0" {
		out[1] = ""
	}
	return out
}

// IterateRegionSegments 遍历 xdb 所有聚合段（start_ip, end_ip, region 原始字符串）。
// 用于离线生成"国家 → CIDR"映射，供 nginx map 在请求时实时判断 geo（不再扫日志加 IP 规则）。
//
// xdb 布局：Header(256B) | VectorIndex(256*256*8) | SegmentIndex | Data
// Header 前 8 字节即段索引区的真实边界：index_start_ptr(0:4) / index_end_ptr(4:8)。
// 段索引每条 14B：start_ip(4) end_ip(4) data_len(2) data_ptr(4)，data_ptr 指向其后的 Data 区。
// 直接读取 Header 边界，避免依赖 vector index（个别坏条目会让推导出的 segEnd 越界而漏段）。
func IterateRegionSegments(cb func(startIP, endIP uint32, raw string)) {
	ipRegion.mu.RLock()
	defer ipRegion.mu.RUnlock()
	b := ipRegion.data
	if len(b) < xdbHeaderSize+8 {
		return
	}
	// 段索引区真实边界：官方 ip2region xdb 格式把段索引边界放在
	// header[8:12]=index_start_ptr、header[12:16]=index_end_ptr，
	// header[0:4] 是版本号（如 0x00010002）而非段索引起点。
	// 旧代码误读 b[0]/b[4] 得到远超文件的偏移，导致本函数直接 return、
	// 遍历 0 段、geo 国家 CIDR 映射文件为空、所有国家规则失效并误杀中国 IP。
	segStart := leUint32(b, 8)
	segEnd := leUint32(b, 12)
	if segStart == 0 || segEnd == 0 || segEnd <= segStart {
		return
	}
	if int(segEnd) > len(b) {
		return
	}
	// 段索引条目的 data_ptr 必须落在文件内，且不能指向段索引区自身
	// （个别坏条目会让推导出的 data 指针越界）。xdb 布局为
	// header|vector index|data|segment index，故合法 data_ptr 位于段索引区之前；
	// 若版本布局为 data 在段索引之后，dptr >= segEnd 同样合法，统一用范围校验兜底。
	for p := int(segStart); p+xdbSegmentIndexSz <= int(segEnd) && p+xdbSegmentIndexSz <= len(b); p += xdbSegmentIndexSz {
		sip := leUint32(b, p)
		eip := leUint32(b, p+4)
		dlen := leUint16(b, p+8)
		dptr := leUint32(b, p+10)
		if sip == 0 && eip == 0 {
			continue // padding
		}
		if sip > eip {
			continue // 无效段
		}
		if int(dptr) < xdbHeaderSize || int(dptr)+int(dlen) > len(b) {
			continue // data 指针越界（不会出现在合法段索引条目）
		}
		if dptr >= segStart && dptr < segEnd {
			continue // data 指针指向段索引区自身，跳过
		}
		raw := string(b[dptr : dptr+uint32(dlen)])
		cb(sip, eip, raw)
	}
}

// regionSeg 一条聚合 IP 段（用于离线生成国家 CIDR 映射）
type regionSeg struct{ start, end uint32 }

// GenerateGeoCountryConf 生成 nginx map 用的"CIDR 国家名"映射文本（每行：CIDR 国家名）。
// 采用"中国 vs 海外"二分类：所有 IP 段归类为"中国"或"OVERSEAS"（未知段命中 map 默认 "--"）。
// 供"禁海外"功能在请求时实时判断：仅 $lp_ss_cc == "OVERSEAS" 拦截，"中国" 与 "--"（未知）均放行，
// 避免离线库不准时把国内 IP（含归属未知 / 仅识别到 ISP 为国内运营商的 IP）误杀。
// 中国段优先写入，确保被防御性上限截断时中国大陆覆盖优先。
func GenerateGeoCountryConf() (string, error) {
	if !IpRegionEnabled() {
		return "", errors.New("离线库未加载")
	}
	byCountry := map[string][]regionSeg{}
	total := 0
	IterateRegionSegments(func(s, e uint32, raw string) {
		total++
		parts := splitRegion(raw)
		country := parts[0]
		isp := parts[4]
		if country == "" || country == "0" {
			country = "UNKNOWN"
		}
		// country 字段缺失/为 0 时，若 ISP 是国内运营商（移动/联通/电信/铁通等），
		// 仍归类为中国——xdb 数据格式参差不齐，部分国内 IP 的 country 字段会被
		// 识别为 "0" 但 ISP 字段是"移动/联通/电信"，避免禁海外误杀。
		if country != "中国" && isChineseISP(isp) {
			country = "中国"
		}
		// 非中国且非未知 → 归类为 OVERSEAS（写入 map，会被禁海外拦截）
		if country != "中国" && country != "UNKNOWN" {
			country = "OVERSEAS"
		}
		// UNKNOWN 不写入 map（在 nginx map 中会命中默认 "--"，表示未识别，禁海外放行）
		byCountry[country] = append(byCountry[country], regionSeg{s, e})
	})
	if total == 0 {
		slog.Warn("GenerateGeoCountryConf: 遍历 0 段，xdb 段索引可能未正确解析")
	}
	count := 0
	var sb strings.Builder
	sb.WriteString("# kypanel 国家 CIDR 映射（自动生成，请勿手动修改）\n")
	sb.WriteString("# 格式：CIDR 国家名（仅二分类：中国 / OVERSEAS）\n")
	// 防御性上限：极端情况下超过该值直接截断并告警，避免生成超大字符串撑爆内存。
	// 中国优先写入，保证截断时中国大陆覆盖尽量完整。
	const maxGeoCidrs = 200000
	for _, country := range []string{"中国", "OVERSEAS"} {
		segs, ok := byCountry[country]
		if !ok {
			continue
		}
		for _, m := range mergeSegs(segs) {
			for _, c := range rangeToCidrs(m.start, m.end) {
				fmt.Fprintf(&sb, "%s \"%s\";\n", c, country)
				count++
				if count >= maxGeoCidrs {
					slog.Warn("GenerateGeoCountryConf 生成的 CIDR 超过上限，已截断（海外部分可能不完整；未知段与国内运营商段均放行，不影响正常访问）",
						"max", maxGeoCidrs)
					return sb.String(), nil
				}
			}
		}
	}
	slog.Info("GenerateGeoCountryConf 生成完成", "cidrs", count)
	return sb.String(), nil
}

// mergeSegs 按 start 升序合并相邻（或不重叠且连续）的同国家段
func mergeSegs(in []regionSeg) []regionSeg {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].start < in[j].start })
	out := []regionSeg{in[0]}
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.start <= last.end+1 {
			if s.end > last.end {
				last.end = s.end
			}
		} else {
			out = append(out, s)
		}
	}
	return out
}

// rangeToCidrs 将 [start,end] 区间展开为最小 CIDR 集合。
// 关键：必须聚合成"最大可用块"（最小掩码），绝不能逐 IP 展开成 /32——
// 否则一个连续的大段（如 16M 个 IP）会生成 1600 万行，字符串直接撑爆内存（OOM）。
// 算法：对每个 start，受其对齐限制（尾随零位数）取最大块，再向下找第一个能放进 [start,end] 的块。
func rangeToCidrs(start, end uint32) []string {
	out := make([]string, 0)
	for start <= end {
		// start 的尾随零位数决定可用的最大块（最小掩码）。
		// m 为 CIDR 前缀长度（网络位），host 位 = 32-m，块大小 = 2^(32-m)。
		tz := uint32(bits.TrailingZeros32(start))
		bestM := uint32(32)
		for m := 32 - tz; m <= 32; m++ {
			size := uint32(1) << (32 - m)
			if start+size-1 <= end {
				bestM = m
				break
			}
		}
		if bestM <= 0 {
			bestM = 1 // 兜底，避免死循环
		}
		out = append(out, fmt.Sprintf("%s/%d", uint32ToIP(start), bestM))
		start += uint32(1) << (32 - bestM)
	}
	return out
}

func uint32ToIP(u uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", u>>24, (u>>16)&0xFF, (u>>8)&0xFF, u&0xFF)
}

// isChineseISP 判断 ISP 字符串是否包含国内常见运营商关键词。
// 用于离线生成 geo_country.conf 时，country 字段缺失/为 0 但 ISP 字段是
// 移动/联通/电信等国内运营商时仍归类为中国，避免禁海外误杀国内 IP。
func isChineseISP(isp string) bool {
	if isp == "" || isp == "0" {
		return false
	}
	for _, kw := range []string{
		"移动", "联通", "电信", "铁通",
		"教育网", "鹏博士", "长城宽带", "方正", "歌华", "有线",
		"中国电信", "中国移动", "中国联通", "中国铁通",
	} {
		if strings.Contains(isp, kw) {
			return true
		}
	}
	return false
}
