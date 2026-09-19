package serverplugin

import (
	"bufio"
	"bytes"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

type responseCapture struct {
	gin.ResponseWriter
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newResponseCapture(writer gin.ResponseWriter) *responseCapture {
	return &responseCapture{ResponseWriter: writer, status: http.StatusOK}
}

func (w *responseCapture) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
	}
}

func (w *responseCapture) WriteHeaderNow() {
	w.wroteHeader = true
}

func (w *responseCapture) Write(data []byte) (int, error) {
	w.wroteHeader = true
	return w.body.Write(data)
}

func (w *responseCapture) WriteString(value string) (int, error) {
	w.wroteHeader = true
	return w.body.WriteString(value)
}

func (w *responseCapture) Status() int   { return w.status }
func (w *responseCapture) Size() int     { return w.body.Len() }
func (w *responseCapture) Written() bool { return w.wroteHeader }
func (w *responseCapture) Flush()        { w.wroteHeader = true }
func (w *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.Hijack()
}
func (w *responseCapture) CloseNotify() <-chan bool { return w.ResponseWriter.CloseNotify() }
func (w *responseCapture) Pusher() http.Pusher      { return w.ResponseWriter.Pusher() }

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

func (w *responseCapture) commit() {
	for name, values := range w.Header() {
		w.ResponseWriter.Header()[name] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(w.body.Bytes())
}
