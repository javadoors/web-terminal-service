/*
 * Copyright (c) 2024 Huawei Technologies Co., Ltd.
 * openFuyao is licensed under Mulan PSL v2.
 * You can use this software according to the terms and conditions of the Mulan PSL v2.
 * You may obtain a copy of Mulan PSL v2 at:
 *          http://license.coscl.org.cn/MulanPSL2
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
 * EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
 * MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
 * See the Mulan PSL v2 for more details.
 */

package webterminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"

	"openfuyao.com/web-terminal-service/api/v1beta1"
	"openfuyao.com/web-terminal-service/pkg/zlog"
)

// Window 结构体
type Window struct {
	conn       *websocket.Conn
	sizeChan   chan remotecommand.TerminalSize
	ctx        context.Context
	terminaler *terminaler
}

// Message 结构体
type Message struct {
	Op, Data   string
	Rows, Cols uint16
}

// Close closes the window and logs the reason for closing.
func (w *Window) Close(reason string) {
	zlog.LogInfof("Terminal closed : %s", reason)
	close(w.sizeChan)
	if err := w.conn.Close(); err != nil {
		zlog.LogWarn("failed to close websocket: ", err)
	}
}

// Next returns the next terminal size from the size channel.
// If the size is invalid (both height and width are 0), it returns nil.
func (w *Window) Next() *remotecommand.TerminalSize {
	size := <-w.sizeChan
	if size.Height == 0 && size.Width == 0 {
		return nil
	}
	return &size
}

// Renewtime 更新窗口的某些状态或属性，例如更新终端模板或处理与用户上下文相关的信息。
func (w *Window) Renewtime() {
	var err error
	webTerminalTemplate := &v1beta1.WebterminalTemplate{}
	podName := "openfuyao-" + w.ctx.Value("username").(string)
	err = w.terminaler.MgrClient.Get(w.ctx,
		types.NamespacedName{Name: podName, Namespace: UserPodNamespace}, webTerminalTemplate)
	if err != nil {
		zlog.LogInfof("not get user pod", err)
	}
	webTerminalTemplate.Spec.RenewTime = metav1.NewTime(time.Now())
	err = w.terminaler.MgrClient.Update(w.ctx, webTerminalTemplate)
	zlog.LogInfof("RenewTime updated")
	if err != nil {
		zlog.LogInfof("update user pod failed", err)
	}

}

func (w *Window) Read(buffer []byte) (int, error) {
	var msg Message
	if err := w.conn.ReadJSON(&msg); err != nil {
		fmt.Println("ReadJSON error:", err)
		return copy(buffer, endOfWindow), err
	}

	fmt.Printf("Received message: %+v\n", msg)

	switch msg.Op {
	case "stdin":
		if strings.Contains(w.ctx.Value("path").(string), KubectlApi) {
			w.Renewtime()
		}
		fmt.Println("Processing stdin message")
		return copy(buffer, msg.Data), nil
	case "resize":
		fmt.Println("Processing resize message")
		w.sizeChan <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}
		return 0, nil
	default:
		fmt.Printf("Unknown message type: %s\n", msg.Op)
		return copy(buffer, endOfWindow), fmt.Errorf("unknown message type '%s'", msg.Op)
	}
}

func (w *Window) Write(buffer []byte) (int, error) { // Write 将容器内输出数据传到Websocket
	message := Message{
		Op:   "stdout",
		Data: string(buffer),
		Rows: 200,
		Cols: 200,
	}
	if strings.Contains(w.ctx.Value("path").(string), KubectlApi) {
		w.Renewtime()
	}
	msg, marshalErr := json.Marshal(message)
	if marshalErr != nil {
		return 0, marshalErr
	}
	deadline := time.Now().Add(WaitWirte)
	setErr := w.conn.SetWriteDeadline(deadline)
	if setErr != nil {
		fmt.Println("setErr", setErr)
		return 0, setErr
	}
	if w.conn == nil {
		fmt.Println("websocket nil")
		return 0, nil
	}
	writeErr := w.conn.WriteMessage(websocket.TextMessage, msg)
	if writeErr != nil {
		fmt.Println("detail", w.conn)
		fmt.Println("writeErr", writeErr)
		return 0, writeErr
	}
	return len(buffer), nil

}

func (w *Window) Toast(buffer string) error { // Toast 发送输入错误的信息
	message := Message{
		Op:   "toast",
		Data: buffer,
	}
	msg, marshalErr := json.Marshal(message)
	if marshalErr != nil {
		return marshalErr
	}
	deadline := time.Now().Add(WaitWirte)
	setErr := w.conn.SetWriteDeadline(deadline)
	if setErr != nil {
		return setErr
	}
	writeErr := w.conn.WriteMessage(websocket.TextMessage, msg)
	return writeErr
}

func (w *Window) sendMessage() {
	disconnectMessage := Message{
		Op:   "disconnect",
		Data: "Connection closed due to inactivity.",
	}
	msg, err := json.Marshal(disconnectMessage)
	if err != nil {
		return
	}
	_ = w.conn.WriteMessage(websocket.TextMessage, msg)
}
