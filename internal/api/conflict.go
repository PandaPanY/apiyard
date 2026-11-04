package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/PandaPanY/apiyard/internal/store"
)

// interfaceSolveConflict 是编辑接口时的 WebSocket 连接（YApi 的 solveConflict）：
// 没人在编辑时把当前用户记为编辑者，并发送 {errno: 0, data: 接口}；连接关闭时清除编辑者。
// 别人正在编辑时发送 {errno: 编辑者 uid, data: {uid, username}}，前端据此提示“xxx 正在编辑”。
//
// 与 YApi 不同：只有成功占用了编辑状态的连接在关闭时才清除编辑者（YApi 被挡住的连接关闭时也会清除，
// 导致第三个人可以同时进入编辑），且要求对接口所在项目有编辑权限。
func (a *API) interfaceSolveConflict(w http.ResponseWriter, r *http.Request) {
	user, err := a.currentUser(r)
	if err != nil {
		a.logger.Error("解析登录状态失败", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// 默认只接受同源连接，防止其他网站借用户的 Cookie 占用编辑状态
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept 已经写好了错误响应
	}
	defer func() { _ = conn.CloseNow() }()
	ctx := r.Context()

	if user == nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "请登录...")
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if id == 0 {
		_ = conn.Write(ctx, websocket.MessageText, []byte("id 参数有误"))
		return
	}
	it, err := a.store.InterfaceByID(ctx, id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			a.logger.Error("查询接口失败", "err", err)
		}
		return
	}
	if ok, err := a.canProject(ctx, user, it.ProjectID, actionEdit); err != nil || !ok {
		_ = conn.Close(websocket.StatusPolicyViolation, "没有权限")
		return
	}

	owned := false
	if it.EditUID == 0 || it.EditUID == user.ID {
		if owned, err = a.store.ClaimInterfaceEditor(ctx, id, user.ID); err != nil {
			a.logger.Error("记录编辑者失败", "err", err)
			return
		}
		if !owned { // 刚被别人占用，重新读取编辑者
			if it, err = a.store.InterfaceByID(ctx, id); err != nil {
				return
			}
		}
	}
	msg := map[string]any{"errno": 0, "data": interfaceDoc(it)}
	if !owned {
		username := ""
		if editor, err := a.store.UserByID(ctx, it.EditUID); err == nil {
			username = editor.Username
		}
		msg = map[string]any{"errno": it.EditUID, "data": map[string]any{"uid": it.EditUID, "username": username}}
	}
	if owned {
		defer func() {
			// 请求的 ctx 可能已随连接关闭而取消，所以用新的 ctx 清除编辑者
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.store.ReleaseInterfaceEditor(bg, id, user.ID); err != nil {
				a.logger.Error("清除编辑者失败", "err", err)
			}
		}()
	}

	data, err := encodeJSON(msg, "")
	if err != nil {
		a.logger.Error("序列化消息失败", "err", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return
	}
	// 前端不会再发消息，一直读到连接关闭为止
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}
