package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// notify gửi tin cảnh báo ra Telegram/Webhook/email nếu đã cấu hình. Không giữ khóa, không chặn watchdog.
func (s *Server) notify(cfg Settings, title, body string) {
	if !cfg.TgOff && cfg.Telegram != "" && cfg.TgChat != "" {
		go func() {
			c := &http.Client{Timeout: 10 * time.Second}
			c.Post("https://api.telegram.org/bot"+cfg.Telegram+"/sendMessage?chat_id="+url.QueryEscape(cfg.TgChat)+"&text="+url.QueryEscape(title+"\n"+body), "", nil)
		}()
	}
	if !cfg.WhOff && cfg.Webhook != "" {
		go func() {
			b, _ := json.Marshal(map[string]string{"text": title + "\n" + body})
			c := &http.Client{Timeout: 10 * time.Second}
			c.Post(cfg.Webhook, "application/json", bytes.NewReader(b))
		}()
	}
	if !cfg.SmtpOff && cfg.SmtpHost != "" && cfg.SmtpTo != "" {
		go func() {
			addr := cfg.SmtpHost + ":" + nzs(cfg.SmtpPort, "587")
			var auth smtp.Auth
			if cfg.SmtpUser != "" {
				auth = smtp.PlainAuth("", cfg.SmtpUser, cfg.SmtpPass, cfg.SmtpHost)
			}
			from := cfg.SmtpUser
			if from == "" {
				from = "mam@" + cfg.SmtpHost
			}
			to := strings.Split(cfg.SmtpTo, ",")
			for i := range to {
				to[i] = strings.TrimSpace(to[i])
			}
			msg := "Subject: " + title + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
			smtp.SendMail(addr, auth, from, to, []byte(msg))
		}()
	}
}

func nzs(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
