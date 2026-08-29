package router

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// PMASocket phpMyAdmin 本地运行入口（Unix Socket，不对外开放端口）
const PMASocket = "/run/lp_pma.sock"

// handlePMAProxy 将 /phpmyadmin/* 反向代理到本机 phpMyAdmin（Unix Socket）
func handlePMAProxy(c *gin.Context) {
	if !service.PmaInstalled() {
		utils.Fail(c, 404, "phpMyAdmin 未安装，请先到应用商店或数据库管理页安装")
		return
	}
	service.EnsurePmaConfig()
	if _, err := os.Stat(PMASocket); os.IsNotExist(err) {
		utils.Fail(c, 500, "phpMyAdmin 运行入口不存在，请重新安装")
		return
	}

	target, _ := url.Parse("http://unix")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", PMASocket)
		},
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = "http"
		req.URL.Host = "unix"
		// 去掉 /phpmyadmin 前缀，映射到 phpMyAdmin 根路径
		p := strings.TrimPrefix(req.URL.Path, "/phpmyadmin")
		if p == "" {
			p = "/"
		}
		req.URL.Path = p
		// 鉴权 token 不转发给上游
		q := req.URL.Query()
		q.Del("token")
		req.URL.RawQuery = q.Encode()
		req.Host = ""
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
