package httpapi

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

type PushPublicKeyResponse struct {
	PublicKey string `json:"public_key"`
}

type PushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

type NotificationPreferencesRequest struct {
	PushEnabled     *bool `json:"push_enabled,omitempty"`
	TelegramEnabled *bool `json:"telegram_enabled,omitempty"`
}

// @Summary		Get the Web Push application key
// @Tags			push
// @Security		BearerAuth
// @Produce		json
// @Success		200	{object}	PushPublicKeyResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		503	{object}	ErrorResponse
// @Router			/push/public-key [get]
func (s *Server) handlePushPublicKey(w http.ResponseWriter, _ *http.Request) {
	if s.vapidPublicKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "push notifications are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, PushPublicKeyResponse{PublicKey: s.vapidPublicKey})
}

// @Summary		Register a Web Push subscription
// @Tags			me
// @Security		BearerAuth
// @Accept			json
// @Produce		json
// @Param			body	body		PushSubscriptionRequest	true	"browser subscription"
// @Success		200		{object}	domain.PushSubscription
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Router			/me/push-subscriptions [post]
func (s *Server) handleUpsertPushSubscription(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "push notifications are unavailable")
		return
	}
	var req PushSubscriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	endpoint, err := url.ParseRequestURI(req.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" ||
		len(req.Endpoint) > 2048 || req.P256DH == "" || req.Auth == "" ||
		len(req.P256DH) > 512 || len(req.Auth) > 512 {
		writeErr(w, http.StatusBadRequest, "invalid push subscription")
		return
	}
	sub, err := s.accounts.UpsertPushSubscription(
		r.Context(),
		userFrom(r.Context()).ID,
		req.Endpoint,
		req.P256DH,
		req.Auth,
	)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// @Summary		Delete a Web Push subscription
// @Tags			me
// @Security		BearerAuth
// @Success		204
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Router			/me/push-subscriptions/{id} [delete]
func (s *Server) handleDeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "push notifications are unavailable")
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid subscription id")
		return
	}
	if err := s.accounts.DeletePushSubscription(r.Context(), userFrom(r.Context()).ID, id); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Update notification channels
// @Tags			me
// @Security		BearerAuth
// @Accept			json
// @Produce		json
// @Param			body	body		NotificationPreferencesRequest	true	"channel preferences"
// @Success		200		{object}	domain.AccountState
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Router			/me/notification-preferences [patch]
func (s *Server) handleNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "notification settings are unavailable")
		return
	}
	var req NotificationPreferencesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PushEnabled == nil && req.TelegramEnabled == nil {
		writeErr(w, http.StatusBadRequest, "at least one preference is required")
		return
	}
	state, err := s.accounts.UpdateNotificationPreferences(
		r.Context(),
		userFrom(r.Context()).ID,
		req.PushEnabled,
		req.TelegramEnabled,
	)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}
