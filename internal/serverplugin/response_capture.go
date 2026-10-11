package serverplugin

import (
	"bufio"
	"bytes"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

// responseCapture 缓冲插件响应，使宿主在拿到响应后仍可改写状态与头部
// 缓冲期间不向客户端写入任何字节，commit 前修改均不会外泄
type responseCapture struct {
	gin.ResponseWriter
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

// newResponseCapture 以默认 200 状态包装原 ResponseWriter
func newResponseCapture(writer gin.ResponseWriter) *responseCapture {
	return &responseCapture{ResponseWriter: writer, status: http.StatusOK}
}

// WriteHeader 记录插件设置的状态码，仅在首次生效
func (w *responseCapture) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
	}
}

// WriteHeaderNow 标记响应头已写入，固定后续状态码
func (w *responseCapture) WriteHeaderNow() {
	w.wroteHeader = true
}

// Write 将响应体写入内部缓冲区
func (w *responseCapture) Write(data []byte) (int, error) {
	w.wroteHeader = true
	return w.body.Write(data)
}

// WriteString 将字符串响应体写入内部缓冲区
func (w *responseCapture) WriteString(value string) (int, error) {
	w.wroteHeader = true
	return w.body.WriteString(value)
}

// Status 返回已捕获的状态码，未写入时为首选的 200
func (w *responseCapture) Status() int { return w.status }

// Size 返回已捕获响应体的字节数
func (w *responseCapture) Size() int { return w.body.Len() }

// Written 表示插件是否已开始产出响应内容
func (w *responseCapture) Written() bool { return w.wroteHeader }

// Flush 仅标记写入完成，缓冲内容仍由 commit 统一输出
func (w *responseCapture) Flush() { w.wroteHeader = true }

// Hijack 透传给底层连接，劫持后缓冲响应失效
func (w *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.Hijack()
}

// CloseNotify 透传底层连接的关闭通知
func (w *responseCapture) CloseNotify() <-chan bool { return w.ResponseWriter.CloseNotify() }

// Pusher 透传底层 HTTP/2 推送能力
func (w *responseCapture) Pusher() http.Pusher { return w.ResponseWriter.Pusher() }

// replace 用插件响应整体替换已捕获的状态、头部与响应体
// 原有头部被清空，只保留插件声明的头部
func (w *responseCapture) replace(response PluginResponse) error {
	body, err := decodePluginBody(response)
	if err != nil {
		return err
	}
	for name := range w.Header() {
		w.Header().Del(name)
	}
	for name, value := range response.Headers {
		w.Header().Set(name, value)
	}
	w.status = response.Status
	w.body.Reset()
	_, err = w.body.Write(body)
	w.wroteHeader = true
	return err
}

// commit 把捕获到的头部、状态码与响应体一次性写给客户端
func (w *responseCapture) commit() {
	for name, values := range w.Header() {
		w.ResponseWriter.Header()[name] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(w.body.Bytes())
}
