package service

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 自研 WebSocket（RFC 6455）：仅实现面板终端所需的服务器端最小子集，
// 替代 gorilla/websocket。支持文本/二进制帧、close、ping/pong。

const (
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA

	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// WsConn 一个 WebSocket 连接
type WsConn struct {
	conn    net.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
	closed  bool
}

// wsUpgrade 完成 WebSocket 握手，返回连接。需在已劫持的 net.Conn 上调用。
func wsUpgrade(conn net.Conn, br *bufio.Reader, r *http.Request) (*WsConn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("缺少 Sec-WebSocket-Key")
	}
	// 计算 Sec-WebSocket-Accept
	h := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h[:])

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, err
	}
	return &WsConn{conn: conn, br: br}, nil
}

// ReadMessage 读取一帧消息，返回消息类型和数据。
// 支持分片（continuation）与 ping 自动回 pong、忽略控制帧。
func (c *WsConn) ReadMessage() (int, []byte, error) {
	for {
		op, data, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case wsOpText:
			return 1, data, nil // TextMessage
		case wsOpBinary:
			return 2, data, nil // BinaryMessage
		case wsOpPing:
			_ = c.writeFrame(wsOpPong, data)
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			_ = c.writeFrame(wsOpClose, nil)
			return 0, nil, io.EOF
		case wsOpContinuation:
			// 终端场景不会用到分片文本，跳过
			continue
		}
	}
}

// readFrame 读取一个完整的 WebSocket 帧
func (c *WsConn) readFrame() (opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err = io.ReadFull(c.br, header[:]); err != nil {
		return 0, nil, err
	}
	opcode = header[0] & 0x0F
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	// 限制帧大小，防内存炸弹（终端单帧不会超过几 KB）
	if length > 1<<20 {
		return 0, nil, errors.New("websocket 帧过大")
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}

	payload = make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(c.br, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}
	}
	return opcode, payload, nil
}

// WriteMessage 发送一帧消息（opType: 1=文本, 2=二进制）
func (c *WsConn) WriteMessage(opType int, data []byte) error {
	var op byte
	switch opType {
	case 1:
		op = wsOpText
	case 2:
		op = wsOpBinary
	default:
		op = wsOpBinary
	}
	return c.writeFrame(op, data)
}

// writeFrame 发送一个服务端帧（服务器不掩码）
func (c *WsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var header []byte
	n := len(payload)
	switch {
	case n < 126:
		header = []byte{0x80 | opcode, byte(n)}
	case n < 65536:
		header = make([]byte, 4)
		header[0] = 0x80 | opcode
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:], uint16(n))
	default:
		header = make([]byte, 10)
		header[0] = 0x80 | opcode
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭连接
func (c *WsConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

// SetReadDeadline 设置读超时
func (c *WsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// wsSameOrigin 校验 Origin 与 Host 同源，防跨站 WebSocket 劫持（CSWSH）
func wsSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 非浏览器客户端（curl/wscat 等）放行
	}
	originHost := origin
	if idx := strings.Index(origin, "://"); idx >= 0 {
		originHost = origin[idx+3:]
	}
	if idx := strings.Index(originHost, "/"); idx >= 0 {
		originHost = originHost[:idx]
	}
	return strings.EqualFold(originHost, r.Host)
}
