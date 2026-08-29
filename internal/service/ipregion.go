package service

import (
	"encoding/binary"
	"net"
	"os"
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
