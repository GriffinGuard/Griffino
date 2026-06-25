// Copyright 2025 GriffinGuard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/GriffinGuard/Griffino/internal/store"
)

// handleGetSMTP returns the current SMTP configuration. Password is never returned / 返回 SMTP 配置，密码字段不返回
//
//	@Summary	Get SMTP configuration
//	@Tags		System
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/system/smtp [get]
func (s *Server) handleGetSMTP(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.st.GetSMTPConfig()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to read SMTP configuration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":       cfg.Host,
		"port":       cfg.Port,
		"username":   cfg.Username,
		"fromEmail":  cfg.FromEmail,
		"encryption": cfg.Encryption,
		"configured": cfg.Configured,
	})
}

// handlePutSMTP saves SMTP configuration / 保存 SMTP 配置
//
//	@Summary	Update SMTP configuration
//	@Tags		System
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	400	{object}	api.AppError
//	@Failure	500	{object}	api.AppError
//	@Router		/system/smtp [put]
func (s *Server) handlePutSMTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		FromEmail  string `json:"fromEmail"`
		Encryption string `json:"encryption"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrConfigValueInvalid, "Invalid request body")
		return
	}
	if req.Host == "" {
		writeAppError(w, http.StatusBadRequest, ErrConfigValueInvalid, "host is required")
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		writeAppError(w, http.StatusBadRequest, ErrConfigValueInvalid, "port must be between 1 and 65535")
		return
	}
	switch req.Encryption {
	case "", "none", "ssl", "tls":
	default:
		writeAppError(w, http.StatusBadRequest, ErrConfigValueInvalid, "encryption must be 'none', 'ssl', or 'tls'")
		return
	}
	if req.Encryption == "" {
		req.Encryption = "none"
	}

	// If password is empty, keep existing password / 密码为空时保留已存配置
	if req.Password == "" {
		if existing, err := s.st.GetSMTPConfig(); err == nil && existing.Password != "" {
			req.Password = existing.Password
		}
	}

	cfg := store.SMTPConfig{
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   req.Password,
		FromEmail:  req.FromEmail,
		Encryption: req.Encryption,
	}
	if err := s.st.SetSMTPConfig(cfg); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to save SMTP configuration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":       req.Host,
		"port":       req.Port,
		"username":   req.Username,
		"fromEmail":  req.FromEmail,
		"encryption": req.Encryption,
		"configured": true,
	})
}

// handleTestSMTP sends a test email using the current SMTP configuration / 使用当前 SMTP 配置发送测试邮件
//
//	@Summary	Test SMTP configuration
//	@Tags		System
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		object	true	"toEmail"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	502		{object}	api.AppError
//	@Router		/system/smtp/test [post]
func (s *Server) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToEmail string `json:"toEmail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ToEmail == "" {
		writeAppError(w, http.StatusBadRequest, ErrEmailInvalid, "toEmail is required")
		return
	}

	cfg, err := s.st.GetSMTPConfig()
	if err != nil || !cfg.Configured {
		writeAppError(w, http.StatusBadRequest, ErrConfigKeyInvalid, "SMTP is not configured")
		return
	}

	if err := sendTestEmail(cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.FromEmail, req.ToEmail, cfg.Encryption); err != nil {
		code := ErrSMTPSendFailed
		if isConnErr(err) {
			code = ErrSMTPConnectionFailed
		} else if isAuthErr(err) {
			code = ErrSMTPAuthFailed
		}
		writeAppError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Email sent successfully"})
}

func sendTestEmail(host string, port int, username, password, from, to, encryption string) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: Griffino SMTP Test\r\n\r\nThis is a test email from Griffino.\r\n",
		from, to,
	))

	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}

	if encryption == "ssl" {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return fmt.Errorf("connect (SSL): %w", err)
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		defer c.Close()
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		wc, err := c.Data()
		if err != nil {
			return err
		}
		defer wc.Close()
		_, err = wc.Write(msg)
		return err
	}

	// "none" or "tls" (STARTTLS via smtp.SendMail) / 无加密或 STARTTLS
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

func isConnErr(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func isAuthErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "535") || strings.HasPrefix(msg, "534") || strings.HasPrefix(msg, "530")
}
