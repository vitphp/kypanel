package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Resp 统一响应体
type Resp struct {
	Code int         `json:"code"` // 0 成功，非 0 失败
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// Ok 成功响应
func Ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Resp{Code: 0, Msg: "success", Data: data})
}

// Fail 失败响应
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, Resp{Code: code, Msg: msg})
}

// FailWithStatus 失败响应（带 HTTP 状态码）
func FailWithStatus(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Resp{Code: code, Msg: msg})
}
