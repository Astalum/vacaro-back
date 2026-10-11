package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/astalum/vacaro-back/internal/domain"
	appmiddleware "github.com/astalum/vacaro-back/internal/middleware"
	"github.com/astalum/vacaro-back/internal/service"
)

const maxRequestBody = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}

func writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "サーバー内部でエラーが発生しました"
	var fields map[string]string

	var validationError *service.ValidationError
	switch {
	case errors.As(err, &validationError):
		status, code, message, fields = http.StatusUnprocessableEntity, "validation_failed", "入力内容を確認してください", validationError.Fields
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = http.StatusNotFound, "resource_not_found", "対象が見つかりません"
	case errors.Is(err, domain.ErrPermissionDenied):
		status, code, message = http.StatusForbidden, "permission_denied", "この操作を実行する権限がありません"
	case errors.Is(err, domain.ErrAlreadyJoined):
		status, code, message = http.StatusConflict, "already_joined", "既にイベントへ参加しています"
	case errors.Is(err, domain.ErrEventNotOpen):
		status, code, message = http.StatusConflict, "event_not_open", "イベントは受付中ではありません"
	case errors.Is(err, domain.ErrResponseDeadlinePassed):
		status, code, message = http.StatusConflict, "response_deadline_passed", "回答期限を過ぎています"
	case errors.Is(err, domain.ErrOrganizerCannotLeave):
		status, code, message = http.StatusConflict, "organizer_cannot_leave", "主催者はイベントから退出できません"
	case errors.Is(err, domain.ErrInvalidTransition):
		status, code, message = http.StatusConflict, "invalid_event_transition", "イベントの状態を変更できません"
	case errors.Is(err, domain.ErrGroupNotEditable):
		status, code, message = http.StatusConflict, "group_not_editable", "小グループを変更できない状態です"
	case errors.Is(err, domain.ErrGroupConfirmationExists):
		status, code, message = http.StatusConflict, "group_confirmation_exists", "確定済みの小グループがあります"
	case errors.Is(err, domain.ErrDuplicateGroupName):
		status, code, message = http.StatusUnprocessableEntity, "validation_failed", "同じ名前の小グループが既にあります"
	case errors.Is(err, domain.ErrGoogleNotConfigured):
		status, code, message = http.StatusServiceUnavailable, "google_not_configured", "Google連携は現在利用できません"
	case errors.Is(err, domain.ErrCalendarNotConnected):
		status, code, message = http.StatusConflict, "calendar_not_connected", "Googleカレンダーが連携されていません"
	case errors.Is(err, domain.ErrOAuthFailed):
		status, code, message = http.StatusBadRequest, "oauth_failed", "Google認証を完了できませんでした"
	case errors.Is(err, domain.ErrGoogleAuthRequired):
		status, code, message = http.StatusForbidden, "google_authentication_required", "この機能にはGoogle認証が必要です"
	default:
		slog.Error("request failed", "error", err, "request_id", appmiddleware.RequestIDFromContext(r.Context()))
	}

	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code": code, "message": message, "fields": fields,
			"request_id": appmiddleware.RequestIDFromContext(r.Context()),
		},
	})
}

func writeInvalidJSON(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": map[string]any{
			"code": "invalid_json", "message": "JSON形式を確認してください",
			"request_id": appmiddleware.RequestIDFromContext(r.Context()),
		},
	})
}
